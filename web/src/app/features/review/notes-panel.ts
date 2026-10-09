import { ChangeDetectionStrategy, Component, computed, inject, output, signal } from '@angular/core';
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
  imports: [FormsModule, Button, IconField, InputIcon, InputText, Tag, NoteThread],
  template: `
    <section class="session-notes">
      <h3>Session notes</h3>
      <grb-note-thread [notes]="store.sessionNotes()" [anchor]="{ filePath: '', side: '', line: 0 }" [alwaysCompose]="true" />
    </section>

    <div class="toolbar">
      <h3>File and line notes <span class="muted">({{ fileNoteCount() }})</span></h3>
      <p-iconfield>
        <p-inputicon class="pi pi-search" />
        <input pInputText pSize="small" placeholder="Filter notes" [ngModel]="query()" (ngModelChange)="query.set($event)" />
      </p-iconfield>
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
          <article class="note-card">
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
              <table class="excerpt" (click)="goTo(n)">
                <caption class="mono">{{ n.hunkHeader }}</caption>
                <tbody>
                  @for (l of n.excerpt; track $index) {
                    <tr [class]="'ln-' + l.kind" [class.focus]="l.focus">
                      <td class="num">{{ l.oldLine || '' }}</td>
                      <td class="num">{{ l.newLine || '' }}</td>
                      <td class="code"><span class="sign">{{ sign[l.kind] }}</span>{{ l.content }}</td>
                    </tr>
                  }
                </tbody>
              </table>
            }
            <grb-note-thread [notes]="[n]" [anchor]="{ filePath: n.filePath, side: n.side, line: n.line }" [allowReply]="false" />
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
    table.excerpt {
      width: 100%;
      border-collapse: collapse;
      table-layout: fixed;
      font-family: var(--grb-mono);
      font-size: 12px;
      line-height: 1.4;
      margin: 0.25rem 0 0.5rem;
      cursor: pointer;
      border: 1px solid var(--p-content-border-color);
      caption {
        text-align: left;
        font-size: 11px;
        padding: 0.15rem 0.4rem;
        background: var(--grb-hunk-header-bg);
        color: var(--p-text-muted-color);
        caption-side: top;
        border: 1px solid var(--p-content-border-color);
        border-bottom: none;
      }
      td {
        padding: 0;
        vertical-align: top;
      }
      td.num {
        width: 2.8rem;
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
      tr.focus td {
        background: var(--grb-focus-bg);
        font-weight: 600;
      }
      tr.focus td.num {
        box-shadow: inset 3px 0 0 var(--grb-attention);
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
