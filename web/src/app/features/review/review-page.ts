import { ChangeDetectionStrategy, Component, effect, inject, input, signal, untracked, viewChild } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { Title } from '@angular/platform-browser';
import { RouterLink } from '@angular/router';
import { Button } from 'primeng/button';
import { Dialog } from 'primeng/dialog';
import { Drawer } from 'primeng/drawer';
import { InputText } from 'primeng/inputtext';
import { ProgressBar } from 'primeng/progressbar';
import { SelectButton } from 'primeng/selectbutton';
import { Splitter } from 'primeng/splitter';
import { Tag } from 'primeng/tag';
import { ToggleSwitch } from 'primeng/toggleswitch';
import { Tooltip } from 'primeng/tooltip';
import { ReviewStore } from '../../core/review-store';
import { DiffMode, ReviewStatus } from '../../core/models';
import { CommitsPanel } from './commits-panel';
import { ExclusionsDialog } from './exclusions-dialog';
import { NotesPanel } from './notes-panel';
import { FileDiff } from './file-diff';
import { FileTreePanel } from './file-tree-panel';
import { Autofocus } from './note-thread';

interface Shortcut {
  keys: string;
  action: string;
}

/** Review workspace of one session: toolbar, file tree and diff. */
@Component({
  selector: 'grb-review-page',
  imports: [
    RouterLink,
    FormsModule,
    Button,
    Dialog,
    Drawer,
    InputText,
    ProgressBar,
    SelectButton,
    Splitter,
    Tag,
    ToggleSwitch,
    Tooltip,
    Autofocus,
    FileTreePanel,
    FileDiff,
    ExclusionsDialog,
    NotesPanel,
    CommitsPanel,
  ],
  providers: [ReviewStore],
  templateUrl: './review-page.html',
  styleUrl: './review-page.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
  host: { '(document:keydown)': 'onKeydown($event)' },
})
export class ReviewPage {
  protected readonly store = inject(ReviewStore);
  private readonly title = inject(Title);

  /** Route parameter (bound by withComponentInputBinding). */
  readonly id = input.required<string>();

  readonly showHelp = signal(false);
  readonly showNotes = signal(false);
  readonly showExclusions = signal(false);
  /** Session name being edited, or null. */
  readonly nameDraft = signal<string | null>(null);

  readonly diffModes: { label: string; value: DiffMode }[] = [
    { label: 'Unified', value: 'unified' },
    { label: 'Split', value: 'split' },
  ];
  private readonly tree = viewChild(FileTreePanel);

  readonly shortcuts: Shortcut[] = [
    { keys: 'j / k', action: 'Next / previous hunk (crosses files)' },
    { keys: 'n / p', action: 'Next / previous file' },
    { keys: 'u', action: 'Next file that is not reviewed' },
    { keys: 'r', action: 'Mark hunk reviewed and go to the next one' },
    { keys: 'a', action: 'Mark hunk "needs attention"' },
    { keys: 'x', action: 'Reset hunk to unreviewed' },
    { keys: 'R / A / X', action: 'Same for the whole file' },
    { keys: 's', action: 'Pin / unpin the file' },
    { keys: 'c', action: 'Add a note on the file' },
    { keys: 'N', action: 'All notes, with their code' },
    { keys: 'z', action: 'Toggle "collapse reviewed hunks"' },
    { keys: 'v', action: 'Switch unified / split diff' },
    { keys: 'l', action: 'Commits of the review' },
    { keys: 'L', action: 'Commits touching the file' },
    { keys: 'e', action: 'Excluded files' },
    { keys: '/', action: 'Search files' },
    { keys: '?', action: 'Show this help' },
  ];

  constructor() {
    effect(() => {
      const id = Number(this.id());
      untracked(() => this.store.load(id));
    });
    effect(() => {
      const s = this.store.session();
      this.title.setTitle(s ? `${s.name} · ${s.projectName} — revboard` : 'revboard');
    });
  }

  command(): string {
    const args = this.store.session()?.gitArgs ?? [];
    return 'git ' + args.map((a) => (/[\s'"*?()[\]$`\\]/.test(a) || a === '' ? `'${a}'` : a)).join(' ');
  }

  onKeydown(e: KeyboardEvent): void {
    const target = e.target as HTMLElement | null;
    if (e.ctrlKey || e.metaKey || e.altKey || this.showHelp() || this.showNotes() || this.showExclusions() || this.store.commitsView()) {
      return;
    }
    if (target && (target.isContentEditable || ['INPUT', 'TEXTAREA', 'SELECT'].includes(target.tagName))) {
      return;
    }
    const s = this.store;
    const handled = (() => {
      switch (e.key) {
        case 'j':
          s.moveHunk(1);
          return true;
        case 'k':
          s.moveHunk(-1);
          return true;
        case 'n':
          s.moveFile(1);
          return true;
        case 'p':
          s.moveFile(-1);
          return true;
        case 'u':
          s.nextUnreviewedFile();
          return true;
        case 'r':
          this.markHunk('reviewed', true);
          return true;
        case 'a':
          this.markHunk('needs_attention', false);
          return true;
        case 'x':
          this.markHunk('unreviewed', false);
          return true;
        case 'R':
          this.markFile('reviewed', true);
          return true;
        case 'A':
          this.markFile('needs_attention', false);
          return true;
        case 'X':
          this.markFile('unreviewed', false);
          return true;
        case 's': {
          const f = s.selectedFile();
          if (f) s.togglePin(f.id);
          return true;
        }
        case 'c': {
          const f = s.selectedFile();
          if (f) s.composer.set({ filePath: f.summary.path, side: '', line: 0 });
          return true;
        }
        case 'N':
          this.showNotes.set(true);
          return true;
        case 'z':
          s.collapseReviewed.update((v) => !v);
          return true;
        case 'v':
          s.diffMode.update((m) => (m === 'unified' ? 'split' : 'unified'));
          return true;
        case 'l':
          s.commitsView.set({ path: null });
          return true;
        case 'L': {
          const f = s.selectedFile();
          s.commitsView.set({ path: f ? f.summary.path : null });
          return true;
        }
        case 'e':
          this.showExclusions.set(true);
          return true;
        case '/':
          this.tree()?.focusSearch();
          return true;
        case '?':
          this.showHelp.set(true);
          return true;
      }
      return false;
    })();
    if (handled) {
      e.preventDefault();
    }
  }

  startRename(): void {
    this.nameDraft.set(this.store.session()?.name ?? '');
  }

  commitRename(): void {
    const draft = this.nameDraft();
    if (draft !== null) {
      this.store.rename(draft);
      this.nameDraft.set(null);
    }
  }

  onRenameKey(e: KeyboardEvent): void {
    if (e.key === 'Enter') {
      this.commitRename();
    } else if (e.key === 'Escape') {
      this.nameDraft.set(null);
    }
  }

  private markHunk(status: ReviewStatus, advance: boolean): void {
    const s = this.store;
    const hunkId = s.activeHunkId();
    const file = s.selectedFile();
    if (hunkId != null && file?.hunks.some((h) => h.id === hunkId)) {
      s.setHunkStatus(hunkId, status);
      if (advance) s.moveHunk(1);
    } else if (file) {
      // Files without hunks (binary, renames) carry their status themselves.
      this.markFile(status, advance);
    }
  }

  private markFile(status: ReviewStatus, advance: boolean): void {
    const file = this.store.selectedFile();
    if (!file) return;
    this.store.setFileStatus(file.id, status);
    if (advance) this.store.moveFile(1);
  }
}
