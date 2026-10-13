import { ChangeDetectionStrategy, Component, computed, inject, model } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { Button } from 'primeng/button';
import { Dialog } from 'primeng/dialog';
import { InputText } from 'primeng/inputtext';
import { ClearButton } from '../../core/clear-button';
import { ReviewStore } from '../../core/review-store';

/** Edits the exclusion rules of the session. Every change is saved immediately. */
@Component({
  selector: 'grb-exclusions-dialog',
  imports: [FormsModule, Button, Dialog, InputText, ClearButton],
  template: `
    <p-dialog header="Excluded files" [visible]="visible()" (visibleChange)="visible.set($event)" [modal]="true" [dismissableMask]="true" [style]="{ width: '60rem', maxWidth: '95vw', height: '80vh' }" [contentStyle]="{ display: 'flex', flexDirection: 'column', flex: '1', minHeight: '0' }">
      <p class="muted small help">
        Excluded files are hidden from the tree and do not count in the progress. Their statuses and notes are kept.
        Rules: <code>*.md</code> (name at any depth), <code>docs/</code> (folder), <code>src/gen/</code> (path from root),
        <code>**/*.snap</code>, <code>!docs/keep.md</code> (re-include). The last matching rule wins.
      </p>

      <form class="add" (ngSubmit)="add()">
        <div class="clearable rule-input">
          <input pInputText name="rule" [(ngModel)]="draft" placeholder="Add a rule, e.g. *.lock or docs/" [fluid]="true" autocomplete="off" />
          <grb-clear-button [visible]="!!draft" (cleared)="draft = ''" />
        </div>
        <p-button type="submit" label="Add" icon="pi pi-plus" [disabled]="!draft.trim()" />
      </form>

      @if (suggestions().length > 0) {
        <div class="suggestions small">
          <span class="muted">Quick add:</span>
          @for (s of suggestions(); track s) {
            <button type="button" class="chip mono" (click)="addRule(s)">{{ s }}</button>
          }
        </div>
      }

      <ul class="rules">
        @for (r of rules(); track r) {
          <li>
            @if (r.startsWith('!')) {
              <span class="mono rule"><span class="include">include</span> {{ r.slice(1) }}</span>
            } @else {
              <span class="mono rule">{{ r }}</span>
            }
            <p-button icon="pi pi-times" size="small" severity="secondary" [text]="true" (onClick)="remove(r)" ariaLabel="Remove rule" />
          </li>
        } @empty {
          <li class="muted">No exclusion rule.</li>
        }
      </ul>
      <div class="small muted">
        {{ store.excludedCount() }} of {{ store.files().length }} files excluded.
        @if (store.excludedCount() > 0) {
          <a href="" (click)="showExcluded($event)">Show them</a>
        }
      </div>
    </p-dialog>
  `,
  styles: `
    .help {
      margin-top: 0;
      line-height: 1.5;
    }
    .add {
      display: flex;
      gap: 0.5rem;
    }
    .rule-input {
      flex: 1;
    }
    .suggestions {
      display: flex;
      flex-wrap: wrap;
      align-items: center;
      gap: 0.4rem;
      margin-top: 0.6rem;
    }
    .chip {
      border: 1px solid var(--p-content-border-color);
      background: var(--grb-code-bg);
      color: var(--p-text-color);
      border-radius: 999px;
      max-width: 100%;
      overflow-wrap: anywhere;
      text-align: left;
      padding: 0.1rem 0.6rem;
      cursor: pointer;
      font-size: 0.75rem;
    }
    .rules {
      list-style: none;
      padding: 0;
      margin: 1rem 0 0.75rem;
      flex: 1;
      min-height: 0;
      overflow-y: auto;
      border-top: 1px solid var(--p-content-border-color);
      .rule {
        overflow-wrap: anywhere;
        min-width: 0;
      }
      .include {
        font-family: system-ui, sans-serif;
        font-size: 0.7rem;
        text-transform: uppercase;
        color: var(--grb-reviewed);
        margin-right: 0.35rem;
      }
      li {
        gap: 0.5rem;
        display: flex;
        justify-content: space-between;
        align-items: center;
        padding: 0.15rem 0.25rem 0.15rem 0.5rem;
        border-bottom: 1px solid var(--p-content-border-color);
      }
    }
  `,
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class ExclusionsDialog {
  protected readonly store = inject(ReviewStore);

  readonly visible = model(false);
  protected draft = '';

  protected readonly rules = computed(() => this.store.session()?.exclusions ?? []);

  /** Rules derived from the selected file: its name, extension and folder. */
  protected readonly suggestions = computed(() => {
    const f = this.store.selectedFile();
    if (!f) return [];
    const path = f.summary.path;
    const slash = path.lastIndexOf('/');
    const name = path.slice(slash + 1);
    const dot = name.lastIndexOf('.');
    const out = [path];
    if (dot > 0) out.push(`*${name.slice(dot)}`);
    if (slash > 0) out.push(`${path.slice(0, slash)}/`);
    const existing = new Set(this.rules());
    return out.filter((r) => !existing.has(r));
  });

  protected add(): void {
    this.addRule(this.draft);
  }

  protected addRule(rule: string): void {
    const r = rule.trim();
    if (!r || this.rules().includes(r)) return;
    this.store.setExclusions([...this.rules(), r], () => (this.draft = ''));
  }

  protected remove(rule: string): void {
    this.store.setExclusions(this.rules().filter((r) => r !== rule));
  }

  protected showExcluded(event: Event): void {
    event.preventDefault();
    this.store.filter.set('excluded');
    this.visible.set(false);
  }
}
