import { DatePipe } from '@angular/common';
import { ChangeDetectionStrategy, Component, Directive, booleanAttribute, ElementRef, afterNextRender, computed, inject, input, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { Button } from 'primeng/button';
import { Checkbox } from 'primeng/checkbox';
import { Textarea } from 'primeng/textarea';
import { ClearButton } from '../../core/clear-button';
import { Comment, NoteAnchor } from '../../core/models';
import { ReviewStore } from '../../core/review-store';

/** Focuses its host element once rendered. */
@Directive({ selector: '[grbAutofocus]' })
export class Autofocus {
  readonly grbAutofocus = input(true, { transform: booleanAttribute });

  constructor() {
    const el = inject<ElementRef<HTMLElement>>(ElementRef);
    afterNextRender(() => {
      if (this.grbAutofocus()) el.nativeElement.focus();
    });
  }
}

/** List of private notes attached to one anchor, with an editor. */
@Component({
  selector: 'grb-note-thread',
  imports: [DatePipe, FormsModule, Button, Checkbox, Textarea, Autofocus, ClearButton],
  template: `
    @for (n of notes(); track n.id) {
      <div class="note" [class.not-exported]="exportable() && !n.export">
        @if (editingId() === n.id) {
          <div class="clearable multiline">
            <textarea pTextarea [autoResize]="true" rows="2" [(ngModel)]="editDraft" (keydown)="editKey($event, n)" class="w-full" grbAutofocus></textarea>
            <grb-clear-button [visible]="!!editDraft" (cleared)="editDraft = ''" />
          </div>
          <div class="note-actions">
            <p-button label="Save" size="small" (onClick)="saveEdit(n)" [disabled]="!editDraft.trim()" />
            <p-button label="Cancel" size="small" severity="secondary" [text]="true" (onClick)="editingId.set(null)" />
          </div>
        } @else {
          <div class="note-body">{{ n.body }}</div>
          <div class="note-meta">
            @if (exportable()) {
              <label class="export-toggle" title="Include this note in the Markdown export">
                <p-checkbox [binary]="true" [ngModel]="n.export" (ngModelChange)="store.setNoteExport(n, $event)" />
                Export
              </label>
            }
            <span>{{ n.updatedAt | date: 'short' }}</span>
            <i class="pi pi-pencil" (click)="startEdit(n)" title="Edit"></i>
            <i class="pi pi-trash" (click)="store.deleteComment(n.id)" title="Delete"></i>
          </div>
        }
      </div>
    }
    @if (composing()) {
      <div class="composer">
        <div class="clearable multiline">
          <textarea
            pTextarea
            [autoResize]="true"
            rows="2"
            placeholder="Private note… (Ctrl+Enter to save, Esc to cancel)"
            [(ngModel)]="draft"
            (keydown)="composeKey($event)"
            [grbAutofocus]="!alwaysCompose()"
            class="w-full"
          ></textarea>
          <grb-clear-button [visible]="!!draft" (cleared)="draft = ''" />
        </div>
        <div class="note-actions">
          <p-button label="Add note" size="small" (onClick)="add()" [disabled]="!draft.trim()" />
          @if (!alwaysCompose()) {
            <p-button label="Cancel" size="small" severity="secondary" [text]="true" (onClick)="cancel()" />
          }
        </div>
      </div>
    } @else if (allowReply() && notes().length > 0) {
      <p-button label="Add note" icon="pi pi-plus" size="small" [text]="true" severity="secondary" (onClick)="open()" />
    }
  `,
  changeDetection: ChangeDetectionStrategy.OnPush,
  host: { class: 'note-thread' },
})
export class NoteThread {
  protected readonly store = inject(ReviewStore);

  readonly notes = input.required<readonly Comment[]>();
  readonly anchor = input.required<NoteAnchor>();
  /** Keep the composer open (session notes dialog). */
  readonly alwaysCompose = input(false);
  readonly allowReply = input(true);
  /** Show the "export" checkbox of each note (notes panel). */
  readonly exportable = input(false);

  protected draft = '';
  protected editDraft = '';
  protected readonly editingId = signal<number | null>(null);

  protected readonly composing = computed(() => {
    if (this.alwaysCompose()) return true;
    const c = this.store.composer();
    const a = this.anchor();
    return c !== null && c.filePath === a.filePath && c.side === a.side && c.line === a.line;
  });

  protected open(): void {
    this.store.composer.set(this.anchor());
  }

  protected add(): void {
    const body = this.draft.trim();
    if (!body) return;
    this.store.addComment(this.anchor(), body, () => {
      this.draft = '';
      if (!this.alwaysCompose()) this.store.composer.set(null);
    });
  }

  protected cancel(): void {
    this.draft = '';
    this.store.composer.set(null);
  }

  protected composeKey(e: KeyboardEvent): void {
    if (e.key === 'Enter' && (e.ctrlKey || e.metaKey)) {
      e.preventDefault();
      this.add();
    } else if (e.key === 'Escape' && !this.alwaysCompose()) {
      e.stopPropagation();
      this.cancel();
    }
  }

  protected startEdit(n: Comment): void {
    this.editDraft = n.body;
    this.editingId.set(n.id);
  }

  protected saveEdit(n: Comment): void {
    const body = this.editDraft.trim();
    if (!body) return;
    this.store.updateComment(n.id, body, () => this.editingId.set(null));
  }

  protected editKey(e: KeyboardEvent, n: Comment): void {
    if (e.key === 'Enter' && (e.ctrlKey || e.metaKey)) {
      e.preventDefault();
      this.saveEdit(n);
    } else if (e.key === 'Escape') {
      e.stopPropagation();
      this.editingId.set(null);
    }
  }
}
