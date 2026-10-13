import { ChangeDetectionStrategy, Component, input, output } from '@angular/core';

/**
 * "×" button clearing the input it is placed next to. Put it in a
 * `.clearable` wrapper together with the input; it is shown only when there
 * is something to clear.
 */
@Component({
  selector: 'grb-clear-button',
  template: `
    @if (visible()) {
      <button
        type="button"
        class="clear-button"
        aria-label="Clear"
        title="Clear"
        (mousedown)="$event.preventDefault()"
        (click)="$event.stopPropagation(); cleared.emit()"
      >
        <i class="pi pi-times"></i>
      </button>
    }
  `,
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class ClearButton {
  /** Show the button (bind it to "the input is not empty"). */
  readonly visible = input(false);
  readonly cleared = output<void>();
}
