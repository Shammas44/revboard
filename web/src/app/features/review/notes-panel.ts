import { ChangeDetectionStrategy, Component, ElementRef, afterRenderEffect, computed, inject, output, signal, untracked } from '@angular/core';
import { toObservable, toSignal } from '@angular/core/rxjs-interop';
import { FormsModule } from '@angular/forms';
import { Button } from 'primeng/button';
import { IconField } from 'primeng/iconfield';
import { InputIcon } from 'primeng/inputicon';
import { InputText } from 'primeng/inputtext';
import { Tag } from 'primeng/tag';
import { catchError, debounceTime, map, of, switchMap } from 'rxjs';
import { ApiService, errorMessage } from '../../core/api.service';
import { DiffLine, NoteContext } from '../../core/models';
import { ReviewStore } from '../../core/review-store';
import { ClearButton } from '../../core/clear-button';
import { NoteThread } from './note-thread';

interface FileGroup {
  path: string;
  fileId: number;
  notes: NoteContext[];
}

const SIGN: Record<DiffLine['kind'], string> = { context: ' ', add: '+', delete: '-' };

/**
 * Overview of every note of the session: session notes, then file and line
 * notes grouped by file, each line note with its hunk excerpt and the noted
 * line highlighted. "Go to" opens the line in the diff.
 */
@Component({
  selector: 'grb-notes-panel',
  imports: [FormsModule, Button, IconField, InputIcon, InputText, Tag, NoteThread, ClearButton],
  template: `
    <div class="export-bar">
      <span class="export-summary">
        <i class="pi pi-file-export"></i>
        Export <strong>{{ exportCount() }}</strong> of {{ store.comments().length }} note{{ store.comments().length === 1 ? '' : 's' }}
      </span>
      <p-button label="All" size="small" [text]="true" (onClick)="store.setAllNotesExport(true)" [disabled]="exportCount() === store.comments().length" />
      <p-button label="None" size="small" [text]="true" (onClick)="store.setAllNotesExport(false)" [disabled]="exportCount() === 0" />
      <p-button label="Copy as Markdown" icon="pi pi-copy" size="small" (onClick)="store.copyNotesExport()" [disabled]="exportCount() === 0" />
    </div>
    <p class="muted small export-help">
      The export is a Markdown document listing the checked notes with their file, line and code context, ready to
      paste into an LLM (or a pull request) to have the requested changes implemented.
    </p>

    <section class="session-notes">
      <h3>Session notes</h3>
      <grb-note-thread [notes]="store.sessionNotes()" [anchor]="{ filePath: '', side: '', line: 0 }" [alwaysCompose]="true" [exportable]="true" />
    </section>

    <div class="toolbar">
      <h3>File and line notes <span class="muted">({{ fileNoteCount() }})</span></h3>
      <div class="clearable">
        <p-iconfield>
          <p-inputicon class="pi pi-search" />
          <input pInputText pSize="small" placeholder="Filter notes" [ngModel]="query()" (ngModelChange)="query.set($event)" />
        </p-iconfield>
        <grb-clear-button [visible]="!!query()" (cleared)="query.set('')" />
      </div>
    </div>

    @if (state().error; as err) {
      <div class="error-box">{{ err }}</div>
    } @else if (state().loading) {
      <div class="muted"><i class="pi pi-spin pi-spinner"></i> Loading notes…</div>
    } @else if (groups().length === 0) {
      <p class="muted">
        @if (query()) {
          No note matches "{{ query() }}".
        } @else {
          No file or line note yet. Click a line number in the diff to add one.
        }
      </p>
    }

    @for (g of groups(); track g.path) {
      <section class="file">
        <header>
          <span class="path mono" [class.link]="g.fileId" (click)="openFile(g)">{{ g.path }}</span>
          @if (!g.fileId) {
            <p-tag value="not in diff" severity="secondary" />
          }
        </header>
        @for (n of g.notes; track n.id) {
          <article class="note-card" [class.not-exported]="!(commentById().get(n.id) ?? n).export">
            <div class="where small">
              @if (n.line === 0) {
                <span class="muted">File note</span>
              } @else {
                <span>Line {{ n.line }}</span>
                <span class="muted">({{ n.side }})</span>
                @if (n.outdated) {
                  <p-tag value="outdated" severity="warn" />
                }
              }
              @if (n.excluded) {
                <p-tag value="excluded" severity="secondary" />
              }
              <span class="spacer"></span>
              @if (n.fileId && n.line > 0 && !n.outdated) {
                <p-button label="Go to line" icon="pi pi-arrow-right" iconPos="right" size="small" [text]="true" (onClick)="goTo(n)" />
              }
            </div>
            @if (n.excerpt.length > 0) {
              <div class="excerpt-head" (click)="goTo(n)">
                <span class="mono">{{ n.hunkHeader }}</span>
                @if (n.hunkSection) {
                  <span class="section" title="Enclosing context reported by git"><span class="in">in</span> <span class="mono">{{ n.hunkSection }}</span></span>
                }
              </div>
              <div class="excerpt-scroll">
              <table class="excerpt" (click)="goTo(n)">
                <colgroup><col class="c-num" /><col class="c-num" /><col /></colgroup>
                <tbody>
                  @for (l of n.excerpt; track $index) {
                    <tr [class]="'ln-' + l.kind" [class.focus]="l.focus">
                      <td class="num" [class.noted]="l.focus && n.side === 'old'" [title]="l.oldLine ? 'Line ' + l.oldLine : ''">
                        @if (l.focus && n.side === 'old') {<i class="pi pi-comment"></i>} @else {{{ l.oldLine || '' }}}
                      </td>
                      <td class="num" [class.noted]="l.focus && n.side === 'new'" [title]="l.newLine ? 'Line ' + l.newLine : ''">
                        @if (l.focus && n.side === 'new') {<i class="pi pi-comment"></i>} @else {{{ l.newLine || '' }}}
                      </td>
                      <td class="code"><span class="sign">{{ sign[l.kind] }}</span>{{ l.content }}</td>
                    </tr>
                  }
                </tbody>
              </table>
              </div>
            }
            <grb-note-thread [notes]="[commentById().get(n.id) ?? n]" [anchor]="{ filePath: n.filePath, side: n.side, line: n.line }" [allowReply]="false" [exportable]="true" />
          </article>
        }
      </section>
    }
  `,
  styles: `
    :host {
      display: block;
    }
    h3 {
      font-size: 0.95rem;
      margin: 0 0 0.5rem;
    }
    .export-bar {
      position: sticky;
      top: 0;
      z-index: 1;
      display: flex;
      align-items: center;
      flex-wrap: wrap;
      gap: 0.25rem 0.5rem;
      padding: 0.5rem 0.6rem;
      margin-bottom: 0.25rem;
      border: 1px solid var(--p-content-border-color);
      border-radius: 6px;
      background: var(--p-content-background);
    }
    .export-summary {
      flex: 1;
      display: inline-flex;
      align-items: center;
      gap: 0.4rem;
    }
    .export-help {
      margin: 0 0 1rem;
    }
    .note-card.not-exported table,
    .note-card.not-exported .excerpt-head {
      opacity: 0.55;
    }
    .session-notes {
      padding-bottom: 1rem;
      margin-bottom: 1rem;
      border-bottom: 1px solid var(--p-content-border-color);
    }
    .toolbar {
      display: flex;
      align-items: center;
      justify-content: space-between;
      gap: 1rem;
      margin-bottom: 0.75rem;
      h3 {
        margin: 0;
      }
    }
    .file {
      margin-bottom: 1.25rem;
      header {
        display: flex;
        align-items: center;
        gap: 0.5rem;
        margin-bottom: 0.4rem;
      }
    }
    .path {
      font-weight: 600;
      font-size: 0.85rem;
      overflow-wrap: anywhere;
      &.link {
        cursor: pointer;
        &:hover {
          color: var(--p-primary-color);
          text-decoration: underline;
        }
      }
    }
    .note-card {
      border: 1px solid var(--p-content-border-color);
      border-radius: 6px;
      padding: 0.5rem 0.6rem;
      margin-bottom: 0.6rem;
    }
    .where {
      display: flex;
      align-items: center;
      gap: 0.4rem;
      min-height: 2rem;
    }
    .spacer {
      flex: 1;
    }
    .excerpt-head {
      display: flex;
      gap: 0.5rem;
      align-items: baseline;
      margin-top: 0.25rem;
      padding: 0.15rem 0.4rem;
      font-size: 11px;
      color: var(--p-text-muted-color);
      background: var(--grb-hunk-header-bg);
      border: 1px solid var(--p-content-border-color);
      border-bottom: none;
      cursor: pointer;
      white-space: nowrap;
      overflow: hidden;
      .section {
        overflow: hidden;
        text-overflow: ellipsis;
        font-style: italic;
      }
      .in {
        font-style: normal;
        font-size: 10px;
        text-transform: uppercase;
        letter-spacing: 0.04em;
        margin-right: 0.35rem;
      }
    }
    /* Whole hunk, about 15 lines visible, scrolled to the noted line. */
    .excerpt-scroll {
      max-height: 16rem;
      overflow-y: auto;
      border: 1px solid var(--p-content-border-color);
      margin-bottom: 0.5rem;
    }
    table.excerpt {
      width: 100%;
      border-collapse: collapse;
      table-layout: fixed;
      font-family: var(--grb-mono);
      font-size: 12px;
      line-height: 1.4;
      cursor: pointer;
      td {
        padding: 0;
        vertical-align: top;
      }
      col.c-num {
        width: 3rem;
      }
      td.num {
        padding: 0 0.4rem;
        text-align: right;
        color: var(--p-text-muted-color);
      }
      td.code {
        white-space: pre-wrap;
        overflow-wrap: anywhere;
        padding-right: 0.5rem;
      }
      .sign {
        display: inline-block;
        width: 1rem;
        color: var(--p-text-muted-color);
      }
      tr.ln-add td {
        background: var(--grb-add-bg);
      }
      tr.ln-delete td {
        background: var(--grb-del-bg);
      }
      tr.focus > td:first-child {
        box-shadow: inset 3px 0 0 var(--grb-note);
      }
      td.num.noted .pi {
        color: var(--grb-note);
        font-size: 0.75rem;
      }
    }
  `,
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class NotesPanel {
  private readonly api = inject(ApiService);
  protected readonly store = inject(ReviewStore);

  /** Emitted after navigating to a note, so the drawer can close. */
  readonly navigated = output<void>();

  protected readonly sign = SIGN;
  protected readonly query = signal('');

  /** Reloaded whenever notes are added, edited or deleted. */
  protected readonly state = toSignal(
    toObservable(this.store.comments).pipe(
      debounceTime(100),
      switchMap(() => {
        const id = this.store.session()?.id;
        return id == null
          ? of({ loading: true, notes: [] as NoteContext[], error: '' })
          : this.api.listNotes(id).pipe(
              map((notes) => ({ loading: false, notes, error: '' })),
              catchError((err) => of({ loading: false, notes: [] as NoteContext[], error: errorMessage(err) })),
            );
      }),
    ),
    { initialValue: { loading: true, notes: [] as NoteContext[], error: '' } },
  );

  constructor() {
    // Center each excerpt on its noted line (scrolls the excerpt box only).
    const host = inject<ElementRef<HTMLElement>>(ElementRef).nativeElement;
    afterRenderEffect(() => {
      this.groups();
      untracked(() => {
        for (const box of Array.from(host.querySelectorAll<HTMLElement>('.excerpt-scroll'))) {
          const row = box.querySelector<HTMLElement>('tr.focus');
          if (row) box.scrollTop = row.offsetTop - box.clientHeight / 2 + row.offsetHeight / 2;
        }
      });
    });
  }

  /** Live notes of the store (export marks update without waiting for a reload). */
  protected readonly commentById = computed(() => new Map(this.store.comments().map((c) => [c.id, c])));
  protected readonly exportCount = computed(() => this.store.comments().filter((c) => c.export).length);

  protected readonly fileNoteCount = computed(() => this.state().notes.filter((n) => n.filePath !== '').length);

  protected readonly groups = computed<FileGroup[]>(() => {
    const q = this.query().trim().toLowerCase();
    const groups: FileGroup[] = [];
    for (const n of this.state().notes) {
      if (n.filePath === '') continue;
      if (q && !n.body.toLowerCase().includes(q) && !n.filePath.toLowerCase().includes(q)) continue;
      let g = groups[groups.length - 1];
      if (!g || g.path !== n.filePath) {
        g = { path: n.filePath, fileId: n.fileId, notes: [] };
        groups.push(g);
      }
      g.notes.push(n);
    }
    return groups;
  });

  protected goTo(n: NoteContext): void {
    if (!n.fileId || n.outdated) return;
    this.store.goToNote(n);
    this.navigated.emit();
  }

  protected openFile(g: FileGroup): void {
    if (!g.fileId) return;
    const file = this.store.fileById().get(g.fileId);
    if (file?.excluded && this.store.filter() !== 'excluded') this.store.filter.set('excluded');
    this.store.selectFile(g.fileId);
    this.navigated.emit();
  }
}
