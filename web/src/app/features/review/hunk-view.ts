import { ChangeDetectionStrategy, Component, computed, inject, input, linkedSignal } from '@angular/core';
import { Tooltip } from 'primeng/tooltip';
import { Hunk, HunkVM, ReviewStatus } from '../../core/models';
import { ReviewStore } from '../../core/review-store';
import { STATUS_LABEL } from '../../core/status';
import { HunkLines } from './hunk-lines';

/**
 * One hunk: header with status actions, and its lines.
 *
 * Only this component reads the hunk status signal; the lines live in the
 * OnPush child HunkLines whose input never changes, so a status change does
 * not re-check the line rows (see docs/SPEC.md §11.4).
 */
@Component({
  selector: 'grb-hunk-view',
  imports: [Tooltip, HunkLines],
  template: `
    @let s = vm().status();
    <div class="hunk-header" (click)="activate()">
      @if (collapsible()) {
        <i class="pi toggle" [class.pi-chevron-right]="collapsed()" [class.pi-chevron-down]="!collapsed()" (click)="toggle($event)"></i>
      }
      <span class="range mono">&#64;&#64; -{{ hunk().oldStart }},{{ hunk().oldLines }} +{{ hunk().newStart }},{{ hunk().newLines }} &#64;&#64;</span>
      <span class="section" title="Enclosing context reported by git">
        @if (hunk().section) {
          <span class="in">in</span> <span class="mono">{{ hunk().section }}</span>
        }
      </span>
      <span class="status-label">{{ statusLabel[s] }}</span>
      <span class="hunk-actions">
        <i class="pi pi-check" [class.on]="s === 'reviewed'" (click)="set('reviewed', $event)" pTooltip="Reviewed (r)" tooltipPosition="left"></i>
        <i class="pi pi-flag" [class.on]="s === 'needs_attention'" (click)="set('needs_attention', $event)" pTooltip="Needs attention (a)" tooltipPosition="left"></i>
        <i class="pi pi-undo" (click)="set('unreviewed', $event)" pTooltip="Reset (x)" tooltipPosition="left"></i>
      </span>
    </div>
    @if (!collapsed()) {
      <grb-hunk-lines [hunk]="hunk()" [path]="path()" />
    }
  `,
  changeDetection: ChangeDetectionStrategy.OnPush,
  host: {
    class: 'hunk',
    '[class.active]': 'active()',
    '[attr.data-status]': 'vm().status()',
  },
})
export class HunkView {
  private readonly store = inject(ReviewStore);

  readonly hunk = input.required<Hunk>();
  readonly vm = input.required<HunkVM>();
  readonly path = input.required<string>();

  protected readonly statusLabel = STATUS_LABEL;
  protected readonly active = computed(() => this.store.activeHunkId() === this.vm().id);
  protected readonly collapsible = computed(() => this.store.collapseReviewed() && this.vm().status() === 'reviewed');
  /** Manual expand/collapse, reset whenever the hunk becomes (un)collapsible. */
  private readonly expanded = linkedSignal({ source: this.collapsible, computation: () => false });
  /** A focused line (e.g. opened from the notes overview) is always shown. */
  private readonly hasFocus = computed(() => this.store.focusLine()?.hunkId === this.vm().id);
  protected readonly collapsed = computed(() => this.collapsible() && !this.expanded() && !this.hasFocus());

  protected activate(): void {
    this.store.activeHunkId.set(this.vm().id);
  }

  protected toggle(event: Event): void {
    event.stopPropagation();
    this.expanded.update((v) => !v);
  }

  protected set(status: ReviewStatus, event: Event): void {
    event.stopPropagation();
    this.store.activeHunkId.set(this.vm().id);
    this.store.setHunkStatus(this.vm().id, status);
  }
}
