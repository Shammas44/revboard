import { ChangeDetectionStrategy, Component, computed, input } from '@angular/core';

/**
 * Units counted by a progress bar: hunks (or files without hunks). A unit is
 * done once it is reviewed or flagged "needs attention".
 */
export interface ProgressUnits {
  reviewed: number;
  attention: number;
  total: number;
}

/** Percentage of units handled (reviewed or flagged), rounded down. */
export function progressPercent(u: ProgressUnits): number {
  return u.total === 0 ? 0 : Math.floor(((u.reviewed + u.attention) * 100) / u.total);
}

/** Thin progress bar with a green "reviewed" and an orange "needs attention" segment. */
@Component({
  selector: 'grb-review-progress',
  template: `
    <div class="track" role="progressbar" aria-valuemin="0" aria-valuemax="100" [attr.aria-valuenow]="percent()" [title]="title()">
      <div class="seg reviewed" [style.width.%]="reviewedWidth()"></div>
      <div class="seg attention" [style.width.%]="attentionWidth()"></div>
    </div>
  `,
  styles: `
    :host {
      display: block;
    }
    .track {
      display: flex;
      height: 0.45rem;
      overflow: hidden;
      border-radius: 999px;
      background: var(--p-progressbar-background, var(--p-content-border-color));
    }
    .seg {
      height: 100%;
      transition: width 0.2s ease;
    }
    .reviewed {
      background: var(--grb-reviewed);
    }
    .attention {
      background: var(--grb-attention);
    }
  `,
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class ReviewProgress {
  readonly units = input.required<ProgressUnits>();

  readonly percent = computed(() => progressPercent(this.units()));
  readonly reviewedWidth = computed(() => this.share(this.units().reviewed));
  readonly attentionWidth = computed(() => this.share(this.units().attention));
  readonly title = computed(() => {
    const u = this.units();
    return `${u.reviewed} reviewed · ${u.attention} need attention · ${u.total - u.reviewed - u.attention} left`;
  });

  private share(n: number): number {
    const total = this.units().total;
    return total === 0 ? 0 : (n * 100) / total;
  }
}
