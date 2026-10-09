import { DestroyRef, Injectable, computed, effect, inject, signal, untracked } from '@angular/core';
import { takeUntilDestroyed, toObservable } from '@angular/core/rxjs-interop';
import { MessageService } from 'primeng/api';
import { EMPTY, Observable, Subject, catchError, concatMap, debounceTime, finalize, of, switchMap, tap } from 'rxjs';
import { ApiService, errorMessage } from './api.service';
import { buildTree, filesUnder, flattenTree } from './file-tree';
import {
  Comment,
  FileDetail,
  DiffMode,
  FileFilter,
  FileSummary,
  FileVM,
  FocusLine,
  HunkVM,
  NoteAnchor,
  NoteContext,
  Progress,
  ReviewStatus,
  Session,
  SessionDetail,
  StatusChange,
  UiState,
} from './models';
import { aggregateStatus } from './status';

/** A queued server write with the function restoring the previous local state. */
interface Write {
  call: () => Observable<StatusChange | FileSummary>;
  rollback: () => void;
}

const DETAIL_CACHE_SIZE = 20;

export function anchorKey(filePath: string, side: string, line: number): string {
  return `${filePath}\u0000${side}\u0000${line}`;
}

function toFileVM(f: FileSummary): FileVM {
  return {
    id: f.id,
    summary: f,
    status: signal(f.status),
    pinned: signal(f.pinned),
    excluded: f.excluded,
    hunks: f.hunks.map((h) => ({ id: h.id, status: signal(h.status) })),
  };
}

/**
 * State of one review page.
 *
 * The diff structure is immutable data; review status lives in one signal per
 * file and per hunk, so a status change only re-renders the bindings that read
 * that particular signal (see docs/SPEC.md §11.4).
 */
@Injectable()
export class ReviewStore {
  private readonly api = inject(ApiService);
  private readonly toast = inject(MessageService);
  private readonly destroyRef = inject(DestroyRef);

  /* ---- Loaded data ---- */
  readonly session = signal<Session | null>(null);
  readonly files = signal<readonly FileVM[]>([]);
  readonly comments = signal<readonly Comment[]>([]);
  readonly loading = signal(true);
  readonly refreshing = signal(false);
  readonly loadError = signal<string | null>(null);

  /* ---- UI state ---- */
  readonly filter = signal<FileFilter>('all');
  readonly search = signal('');
  readonly collapseReviewed = signal(false);
  readonly diffMode = signal<DiffMode>('unified');
  readonly syntaxHighlight = signal(true);
  readonly collapsedDirs = signal<ReadonlySet<string>>(new Set());
  readonly selectedFileId = signal<number | null>(null);
  readonly activeHunkId = signal<number | null>(null);
  /** Incremented by keyboard navigation to ask the diff view to scroll to the active hunk. */
  readonly scrollRequest = signal(0);
  /** Commits drawer: closed (null), whole range (path null) or history of one file. */
  readonly commitsView = signal<{ path: string | null } | null>(null);
  /** Line brought into view and highlighted (e.g. from the notes overview). */
  readonly focusLine = signal<FocusLine | null>(null);
  /** Line where the note composer is open, if any. */
  readonly composer = signal<NoteAnchor | null>(null);

  /* ---- Derived state ---- */
  readonly fileById = computed(() => new Map(this.files().map((f) => [f.id, f])));
  readonly fileByPath = computed(() => new Map(this.files().map((f) => [f.summary.path, f])));
  /** Files under review (not hidden by the exclusion rules). */
  readonly includedFiles = computed(() => this.files().filter((f) => !f.excluded));
  readonly excludedCount = computed(() => this.files().length - this.includedFiles().length);

  readonly hunkIndex = computed(() => {
    const index = new Map<number, { file: FileVM; hunk: HunkVM }>();
    for (const file of this.files()) {
      for (const hunk of file.hunks) {
        index.set(hunk.id, { file, hunk });
      }
    }
    return index;
  });

  readonly commentsByPath = computed(() => {
    const map = new Map<string, Comment[]>();
    for (const c of this.comments()) {
      const list = map.get(c.filePath);
      list ? list.push(c) : map.set(c.filePath, [c]);
    }
    return map;
  });

  readonly commentsByAnchor = computed(() => {
    const map = new Map<string, Comment[]>();
    for (const c of this.comments()) {
      if (c.line > 0) {
        const key = anchorKey(c.filePath, c.side, c.line);
        const list = map.get(key);
        list ? list.push(c) : map.set(key, [c]);
      }
    }
    return map;
  });

  readonly sessionNotes = computed(() => this.commentsByPath().get('') ?? []);

  /**
   * Files matching the search and the filter. Status signals are only read when
   * the filter depends on them, so status changes do not rebuild the tree otherwise.
   */
  readonly visibleFiles = computed(() => {
    const q = this.search().trim().toLowerCase();
    const filter = this.filter();
    let files = filter === 'excluded' ? this.files().filter((f) => f.excluded) : this.includedFiles();
    if (q) {
      files = files.filter((f) => f.summary.path.toLowerCase().includes(q));
    }
    switch (filter) {
      case 'all':
      case 'excluded':
        return files;
      case 'pinned':
        return files.filter((f) => f.pinned());
      case 'commented': {
        const byPath = this.commentsByPath();
        return files.filter((f) => byPath.has(f.summary.path));
      }
      default:
        return files.filter((f) => f.status() === filter);
    }
  });

  readonly tree = computed(() => buildTree(this.visibleFiles()));
  readonly treeRows = computed(() => flattenTree(this.tree(), this.collapsedDirs()));
  /** Navigation order of files (tree order, ignoring collapsed directories). */
  readonly fileOrder = computed(() => filesUnder(this.tree()));
  readonly pinnedFiles = computed(() => this.includedFiles().filter((f) => f.pinned()));

  readonly selectedFile = computed(() => {
    const id = this.selectedFileId();
    return id == null ? null : (this.fileById().get(id) ?? null);
  });

  readonly progress = computed<Progress>(() => {
    const p: Progress = {
      totalFiles: 0,
      reviewedFiles: 0,
      attentionFiles: 0,
      totalHunks: 0,
      reviewedHunks: 0,
      attentionHunks: 0,
      excludedFiles: this.excludedCount(),
    };
    for (const f of this.includedFiles()) {
      p.totalFiles++;
      const fs = f.status();
      if (fs === 'reviewed') p.reviewedFiles++;
      if (fs === 'needs_attention') p.attentionFiles++;
      for (const h of f.hunks) {
        p.totalHunks++;
        const hs = h.status();
        if (hs === 'reviewed') p.reviewedHunks++;
        if (hs === 'needs_attention') p.attentionHunks++;
      }
    }
    return p;
  });

  /** Review progress in percent; files without hunks count as one unit. */
  readonly percent = computed(() => {
    let done = 0;
    let total = 0;
    for (const f of this.includedFiles()) {
      if (f.hunks.length === 0) {
        total++;
        if (f.status() === 'reviewed') done++;
      } else {
        for (const h of f.hunks) {
          total++;
          if (h.status() === 'reviewed') done++;
        }
      }
    }
    return total === 0 ? 0 : Math.floor((done * 100) / total);
  });

  /* ---- Lazily loaded diff of the selected file ---- */
  private readonly detailCache = new Map<number, FileDetail>();
  readonly fileDetail = signal<FileDetail | null>(null);
  readonly detailLoading = signal(false);

  /* ---- Write queue ---- */
  private readonly writes = new Subject<Write>();
  private pendingWrites = 0;
  private readonly uiStateChanges = new Subject<UiState>();
  /** Last UI state known to the server, to avoid redundant writes. */
  private lastUiJson = '';

  constructor() {
    // Ordered server writes; reconcile only when no later optimistic change is pending.
    this.writes
      .pipe(
        concatMap((w) =>
          w.call().pipe(
            tap((res) => {
              if (this.pendingWrites === 1 && 'progress' in res) {
                this.reconcile(res);
              }
            }),
            catchError((err) => {
              w.rollback();
              this.error('Could not save', err);
              return EMPTY;
            }),
            finalize(() => this.pendingWrites--),
          ),
        ),
        takeUntilDestroyed(),
      )
      .subscribe();

    // Load the diff of the selected file; switchMap cancels stale requests.
    toObservable(this.selectedFileId)
      .pipe(
        switchMap((id) => {
          if (id == null) {
            return of(null);
          }
          const cached = this.detailCache.get(id);
          if (cached) {
            return of(cached);
          }
          this.detailLoading.set(true);
          return this.api.getFile(id).pipe(
            tap((d) => this.cacheDetail(d)),
            catchError((err) => {
              this.error('Could not load the file', err);
              return of(null);
            }),
            finalize(() => this.detailLoading.set(false)),
          );
        }),
        takeUntilDestroyed(),
      )
      .subscribe((d) => this.fileDetail.set(d));

    // Persist UI preferences (debounced).
    this.uiStateChanges
      .pipe(
        debounceTime(400),
        switchMap((uiState) => {
          const id = this.session()?.id;
          return id == null ? EMPTY : this.api.patchSession(id, { uiState }).pipe(catchError(() => EMPTY));
        }),
        takeUntilDestroyed(),
      )
      .subscribe();

    effect(() => {
      const state = this.currentUiState();
      if (untracked(() => this.loading())) {
        return;
      }
      const json = JSON.stringify(state);
      if (json !== this.lastUiJson) {
        this.lastUiJson = json;
        this.uiStateChanges.next(state);
      }
    });
  }

  private currentUiState(): UiState {
    return {
      selectedFileId: this.selectedFileId() ?? undefined,
      filter: this.filter(),
      collapseReviewed: this.collapseReviewed(),
      diffMode: this.diffMode(),
      syntaxHighlight: this.syntaxHighlight(),
    };
  }

  /* ---- Loading ---- */

  load(sessionId: number): void {
    this.loading.set(true);
    this.loadError.set(null);
    this.api
      .getSession(sessionId)
      .pipe(takeUntilDestroyed(this.destroyRef))
      .subscribe({
        next: (detail) => {
          this.applyDetail(detail, true);
          this.lastUiJson = JSON.stringify(detail.uiState ?? {});
          this.loading.set(false);
        },
        error: (err) => {
          this.loadError.set(errorMessage(err));
          this.loading.set(false);
        },
      });
    this.api
      .listComments(sessionId)
      .pipe(takeUntilDestroyed(this.destroyRef))
      .subscribe({ next: (c) => this.comments.set(c), error: (err) => this.error('Could not load notes', err) });
  }

  private applyDetail(detail: SessionDetail, restoreUi: boolean): void {
    this.session.set(detail.session);
    this.files.set(detail.files.map(toFileVM));
    this.detailCache.clear();
    const ui = detail.uiState ?? {};
    if (restoreUi) {
      this.filter.set(ui.filter ?? 'all');
      this.collapseReviewed.set(ui.collapseReviewed ?? false);
      this.diffMode.set(ui.diffMode ?? 'unified');
      this.syntaxHighlight.set(ui.syntaxHighlight ?? true);
    }
    const wanted = restoreUi ? ui.selectedFileId : this.selectedFileId();
    const byId = this.fileById();
    const keep = wanted != null && byId.has(wanted) && (!byId.get(wanted)!.excluded || this.filter() === 'excluded');
    const selected = keep ? wanted : (this.fileOrder()[0]?.id ?? null);
    this.selectedFileId.set(null); // force a reload of the detail
    this.selectFile(selected);
  }

  /** Re-runs the git command of the session on the server. */
  refresh(): void {
    const id = this.session()?.id;
    if (id == null || this.refreshing()) {
      return;
    }
    this.refreshing.set(true);
    this.api
      .refreshSession(id)
      .pipe(finalize(() => this.refreshing.set(false)))
      .subscribe({
        next: (detail) => {
          const before = this.files().length;
          this.applyDetail(detail, false);
          this.toast.add({
            severity: 'success',
            summary: 'Diff refreshed',
            detail: `${detail.files.length} files (was ${before}). Statuses of unchanged hunks were kept.`,
          });
        },
        error: (err) => this.error('Refresh failed', err),
      });
  }

  /** Renames the session. */
  rename(name: string): void {
    const id = this.session()?.id;
    const trimmed = name.trim();
    if (id == null || !trimmed || trimmed === this.session()?.name) {
      return;
    }
    this.api.patchSession(id, { name: trimmed }).subscribe({
      next: (s) => this.session.set(s),
      error: (err) => this.error('Could not rename the session', err),
    });
  }

  /** Replaces the exclusion rules of the session, then reloads the file list. */
  setExclusions(rules: string[], done?: () => void): void {
    const id = this.session()?.id;
    if (id == null) {
      return;
    }
    this.api
      .patchSession(id, { exclusions: rules })
      .pipe(switchMap(() => this.api.getSession(id)))
      .subscribe({
        next: (detail) => {
          this.applyDetail(detail, false);
          done?.();
        },
        error: (err) => this.error('Could not save the exclusion rules', err),
      });
  }

  /** Adds one exclusion rule (e.g. a folder from the tree). */
  addExclusion(rule: string): void {
    const rules = this.session()?.exclusions ?? [];
    if (!rules.includes(rule)) {
      this.setExclusions([...rules, rule], () =>
        this.toast.add({ severity: 'info', summary: 'Excluded', detail: `Rule "${rule}" added. Manage rules with the toolbar filter button.` }),
      );
    }
  }

  private cacheDetail(d: FileDetail): void {
    this.detailCache.delete(d.file.id);
    this.detailCache.set(d.file.id, d);
    if (this.detailCache.size > DETAIL_CACHE_SIZE) {
      this.detailCache.delete(this.detailCache.keys().next().value!);
    }
  }

  /* ---- Navigation ---- */

  selectFile(id: number | null): void {
    if (id === this.selectedFileId()) {
      return;
    }
    this.composer.set(null);
    this.focusLine.set(null);
    this.selectedFileId.set(id);
    const file = id == null ? undefined : this.fileById().get(id);
    this.activeHunkId.set(file?.hunks[0]?.id ?? null);
  }

  /** Opens the file of a note and highlights the noted line. */
  goToNote(n: NoteContext): void {
    const file = this.fileById().get(n.fileId);
    if (!file) {
      return;
    }
    if (file.excluded && this.filter() !== 'excluded') {
      this.filter.set('excluded');
    }
    this.selectFile(file.id);
    if (n.line > 0 && !n.outdated) {
      this.activeHunkId.set(n.hunkId);
      this.focusLine.set({ fileId: file.id, hunkId: n.hunkId, side: n.side, line: n.line });
    }
  }

  /** Moves to the next/previous file in tree order. */
  moveFile(delta: 1 | -1): void {
    const order = this.fileOrder();
    if (order.length === 0) {
      return;
    }
    const i = order.findIndex((f) => f.id === this.selectedFileId());
    const next = i < 0 ? 0 : Math.min(order.length - 1, Math.max(0, i + delta));
    this.selectFile(order[next].id);
  }

  /** Jumps to the next file that is not reviewed (wrapping around). */
  nextUnreviewedFile(): void {
    const order = this.fileOrder();
    const start = order.findIndex((f) => f.id === this.selectedFileId());
    for (let k = 1; k <= order.length; k++) {
      const f = order[(start + k + order.length) % order.length];
      if (f.status() !== 'reviewed') {
        this.selectFile(f.id);
        return;
      }
    }
    this.toast.add({ severity: 'success', summary: 'All done', detail: 'Every file of this review is reviewed.' });
  }

  /** Moves the active hunk, crossing file boundaries. */
  moveHunk(delta: 1 | -1): void {
    const file = this.selectedFile();
    if (!file) {
      this.moveFile(1);
      return;
    }
    const i = file.hunks.findIndex((h) => h.id === this.activeHunkId());
    const j = i + delta;
    this.scrollRequest.update((n) => n + 1);
    if (i >= 0 && j >= 0 && j < file.hunks.length) {
      this.activeHunkId.set(file.hunks[j].id);
      return;
    }
    const order = this.fileOrder();
    const fi = order.findIndex((f) => f.id === file.id);
    const target = order[fi + delta];
    if (target) {
      this.selectFile(target.id);
      if (delta < 0) {
        this.activeHunkId.set(target.hunks[target.hunks.length - 1]?.id ?? null);
      }
    }
  }

  toggleDir(path: string): void {
    this.collapsedDirs.update((set) => {
      const next = new Set(set);
      next.has(path) ? next.delete(path) : next.add(path);
      return next;
    });
  }

  /* ---- Status mutations (optimistic) ---- */

  setHunkStatus(hunkId: number, status: ReviewStatus): void {
    const entry = this.hunkIndex().get(hunkId);
    if (!entry) {
      return;
    }
    const { file, hunk } = entry;
    const prevHunk = hunk.status();
    const prevFile = file.status();
    hunk.status.set(status);
    file.status.set(aggregateStatus(file.hunks.map((h) => h.status())));
    this.enqueue({
      call: () => this.api.setHunkStatus(hunkId, status),
      rollback: () => {
        hunk.status.set(prevHunk);
        file.status.set(prevFile);
      },
    });
  }

  setFileStatus(fileId: number, status: ReviewStatus): void {
    const file = this.fileById().get(fileId);
    if (!file) {
      return;
    }
    const rollback = this.applyFileStatus([file], status);
    this.enqueue({ call: () => this.api.setFileStatus(fileId, status), rollback });
  }

  /** Sets the status of several files at once (e.g. a directory). */
  setFilesStatus(files: readonly FileVM[], status: ReviewStatus): void {
    const id = this.session()?.id;
    if (id == null || files.length === 0) {
      return;
    }
    const rollback = this.applyFileStatus(files, status);
    this.enqueue({ call: () => this.api.setFilesStatus(id, files.map((f) => f.id), status), rollback });
  }

  private applyFileStatus(files: readonly FileVM[], status: ReviewStatus): () => void {
    const previous = files.map((f) => ({ f, status: f.status(), hunks: f.hunks.map((h) => h.status()) }));
    for (const f of files) {
      f.status.set(status);
      for (const h of f.hunks) {
        h.status.set(status);
      }
    }
    return () => {
      for (const p of previous) {
        p.f.status.set(p.status);
        p.f.hunks.forEach((h, i) => h.status.set(p.hunks[i]));
      }
    };
  }

  togglePin(fileId: number): void {
    const file = this.fileById().get(fileId);
    if (!file) {
      return;
    }
    const pinned = !file.pinned();
    file.pinned.set(pinned);
    this.enqueue({ call: () => this.api.setPinned(fileId, pinned), rollback: () => file.pinned.set(!pinned) });
  }

  private enqueue(w: Write): void {
    this.pendingWrites++;
    this.writes.next(w);
  }

  /** Aligns local signals with the authoritative server response (no-op when equal). */
  private reconcile(change: StatusChange): void {
    const byId = this.fileById();
    for (const s of change.files) {
      const vm = byId.get(s.id);
      if (!vm) continue;
      vm.status.set(s.status);
      s.hunks.forEach((h, i) => vm.hunks[i]?.status.set(h.status));
    }
  }

  /* ---- Notes ---- */

  addComment(anchor: NoteAnchor, body: string, done?: () => void): void {
    const id = this.session()?.id;
    if (id == null) return;
    this.api.createComment(id, anchor, body).subscribe({
      next: (c) => {
        this.comments.update((list) => [...list, c]);
        done?.();
      },
      error: (err) => this.error('Could not save the note', err),
    });
  }

  updateComment(id: number, body: string, done?: () => void): void {
    this.api.updateComment(id, body).subscribe({
      next: (c) => {
        this.comments.update((list) => list.map((x) => (x.id === c.id ? c : x)));
        done?.();
      },
      error: (err) => this.error('Could not save the note', err),
    });
  }

  deleteComment(id: number): void {
    this.api.deleteComment(id).subscribe({
      next: () => this.comments.update((list) => list.filter((x) => x.id !== id)),
      error: (err) => this.error('Could not delete the note', err),
    });
  }

  private error(summary: string, err: unknown): void {
    this.toast.add({ severity: 'error', summary, detail: errorMessage(err), life: 6000 });
  }
}
