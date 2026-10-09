import { CdkVirtualScrollViewport, ScrollingModule } from '@angular/cdk/scrolling';
import { ChangeDetectionStrategy, Component, ElementRef, afterRenderEffect, inject, untracked, viewChild } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { IconField } from 'primeng/iconfield';
import { InputIcon } from 'primeng/inputicon';
import { InputText } from 'primeng/inputtext';
import { Select } from 'primeng/select';
import { Tooltip } from 'primeng/tooltip';
import { fileName, TreeRow } from '../../core/file-tree';
import { FileFilter, FileVM, ReviewStatus } from '../../core/models';
import { ReviewStore } from '../../core/review-store';
import { CHANGE_LETTER, STATUS_ICON, STATUS_LABEL } from '../../core/status';

/** Height of one tree row in px (fixed, required by the virtual scroll). */
const ROW_HEIGHT = 28;

/** Search, filter, pinned files and the (virtualised) file tree. */
@Component({
  selector: 'grb-file-tree-panel',
  imports: [ScrollingModule, FormsModule, IconField, InputIcon, InputText, Select, Tooltip],
  templateUrl: './file-tree-panel.html',
  styleUrl: './file-tree-panel.scss',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class FileTreePanel {
  protected readonly store = inject(ReviewStore);
  protected readonly rowHeight = ROW_HEIGHT;
  protected readonly statusIcon = STATUS_ICON;
  protected readonly statusLabel = STATUS_LABEL;
  protected readonly changeLetter = CHANGE_LETTER;
  protected readonly fileName = fileName;

  protected readonly filters: { label: string; value: FileFilter }[] = [
    { label: 'All files', value: 'all' },
    { label: 'To review', value: 'unreviewed' },
    { label: 'Needs attention', value: 'needs_attention' },
    { label: 'Reviewed', value: 'reviewed' },
    { label: 'Pinned', value: 'pinned' },
    { label: 'With notes', value: 'commented' },
    { label: 'Excluded', value: 'excluded' },
  ];

  private readonly viewport = viewChild(CdkVirtualScrollViewport);
  private readonly searchInput = viewChild<ElementRef<HTMLInputElement>>('search');

  constructor() {
    // Keep the selected file visible when it changes (keyboard navigation).
    afterRenderEffect(() => {
      const id = this.store.selectedFileId();
      const viewport = this.viewport();
      if (id == null || !viewport) return;
      untracked(() => {
        const index = this.store.treeRows().findIndex((r) => r.kind === 'file' && r.file.id === id);
        if (index < 0) return;
        const top = index * ROW_HEIGHT;
        const offset = viewport.measureScrollOffset('top');
        const height = viewport.getViewportSize();
        if (top < offset || top + ROW_HEIGHT > offset + height) {
          viewport.scrollToOffset(Math.max(0, top - height / 3));
        }
      });
    });
  }

  focusSearch(): void {
    this.searchInput()?.nativeElement.focus();
  }

  protected trackRow(_: number, row: TreeRow): string {
    return row.key;
  }

  /** Number of reviewed files below a directory (reads the status signals of those files only). */
  protected doneCount(files: readonly FileVM[]): number {
    let n = 0;
    for (const f of files) if (f.status() === 'reviewed') n++;
    return n;
  }

  protected hasAttention(files: readonly FileVM[]): boolean {
    return files.some((f) => f.status() === 'needs_attention');
  }

  protected noteCount(f: FileVM): number {
    return this.store.commentsByPath().get(f.summary.path)?.length ?? 0;
  }

  protected markDir(files: readonly FileVM[], status: ReviewStatus, event: Event): void {
    event.stopPropagation();
    this.store.setFilesStatus(files, status);
  }

  protected excludeDir(path: string, event: Event): void {
    event.stopPropagation();
    this.store.addExclusion(`${path}/`);
  }

  protected onSearchKey(e: KeyboardEvent): void {
    if (e.key === 'Escape' || e.key === 'Enter') {
      (e.target as HTMLInputElement).blur();
      if (e.key === 'Enter') {
        const first = this.store.fileOrder()[0];
        if (first) this.store.selectFile(first.id);
      }
    }
  }
}
