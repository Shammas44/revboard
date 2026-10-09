import { ChangeDetectionStrategy, Component, ElementRef, afterRenderEffect, computed, inject, input, untracked } from '@angular/core';
import { Button } from 'primeng/button';
import { Tag } from 'primeng/tag';
import { Tooltip } from 'primeng/tooltip';
import { anchorKey, ReviewStore } from '../../core/review-store';
import { Comment, FileDetail, FileVM, NoteAnchor, ReviewStatus } from '../../core/models';
import { CHANGE_SEVERITY, STATUS_ICON, STATUS_LABEL } from '../../core/status';
import { HunkView } from './hunk-view';
import { NoteThread } from './note-thread';

/** Header, notes and hunks of the selected file. The host element is the scroll container. */
@Component({
  selector: 'grb-file-diff',
  imports: [Button, Tag, Tooltip, HunkView, NoteThread],
  templateUrl: './file-diff.html',
  styleUrl: './file-diff.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class FileDiff {
  protected readonly store = inject(ReviewStore);
  private readonly host = inject<ElementRef<HTMLElement>>(ElementRef);

  readonly file = input.required<FileVM>();
  readonly detail = input<FileDetail | null>(null);
  readonly loading = input(false);

  protected readonly statusIcon = STATUS_ICON;
  protected readonly statusLabel = STATUS_LABEL;
  protected readonly changeSeverity = CHANGE_SEVERITY;

  /** Detail of the current file only (ignores a stale detail while the next one loads). */
  protected readonly currentDetail = computed(() => {
    const d = this.detail();
    return d && d.file.id === this.file().id ? d : null;
  });

  protected readonly fileAnchor = computed<NoteAnchor>(() => ({ filePath: this.file().summary.path, side: '', line: 0 }));

  private readonly notes = computed(() => this.store.commentsByPath().get(this.file().summary.path) ?? []);
  protected readonly fileNotes = computed(() => this.notes().filter((c) => c.line === 0));

  /** Line notes whose line is not part of the current diff anymore (after a refresh). */
  protected readonly outdatedNotes = computed<Comment[]>(() => {
    const d = this.currentDetail();
    if (!d) return [];
    const visible = new Set<string>();
    const path = this.file().summary.path;
    for (const h of d.hunks) {
      for (const l of h.lines) {
        if (l.oldLine) visible.add(anchorKey(path, 'old', l.oldLine));
        if (l.newLine) visible.add(anchorKey(path, 'new', l.newLine));
      }
    }
    return this.notes().filter((c) => c.line > 0 && !visible.has(anchorKey(c.filePath, c.side, c.line)));
  });

  protected readonly composingFileNote = computed(() => {
    const c = this.store.composer();
    return c !== null && c.line === 0 && c.filePath === this.file().summary.path;
  });

  protected readonly modeChange = computed(() => {
    const s = this.file().summary;
    return s.oldMode && s.newMode && s.oldMode !== s.newMode ? `${s.oldMode} → ${s.newMode}` : '';
  });

  constructor() {
    // Back to the top when another file is shown.
    afterRenderEffect(() => {
      this.file();
      untracked(() => (this.host.nativeElement.scrollTop = 0));
    });
    // Bring the focused line into view (notes overview, "go to line").
    afterRenderEffect(() => {
      const focus = this.store.focusLine();
      const d = this.currentDetail();
      if (!focus || !d || focus.fileId !== d.file.id) return;
      untracked(() =>
        this.host.nativeElement
          .querySelector(`#hunk-${focus.hunkId} [data-anchor="${focus.side}:${focus.line}"]`)
          ?.scrollIntoView({ block: 'center' }),
      );
    });
    // Bring the active hunk into view after keyboard navigation.
    afterRenderEffect(() => {
      const request = this.store.scrollRequest();
      const d = this.currentDetail();
      if (request === 0 || !d) return;
      untracked(() => {
        const id = this.store.activeHunkId();
        const first = this.file().hunks[0]?.id;
        if (id === first) {
          this.host.nativeElement.scrollTop = 0;
          return;
        }
        this.host.nativeElement.querySelector(`#hunk-${id}`)?.scrollIntoView({ block: 'start' });
      });
    });
  }

  protected setStatus(status: ReviewStatus): void {
    this.store.setFileStatus(this.file().id, status);
  }

  protected addFileNote(): void {
    this.store.composer.set(this.fileAnchor());
  }
}
