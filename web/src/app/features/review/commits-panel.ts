import { DatePipe } from '@angular/common';
import { ChangeDetectionStrategy, Component, computed, inject, input, output, signal } from '@angular/core';
import { toObservable, toSignal } from '@angular/core/rxjs-interop';
import { Button } from 'primeng/button';
import { catchError, map, of, startWith, switchMap } from 'rxjs';
import { ApiService, errorMessage } from '../../core/api.service';
import { CommitFile, CommitList } from '../../core/models';
import { ReviewStore } from '../../core/review-store';

interface LoadState {
  loading: boolean;
  list?: CommitList;
  error?: string;
}

/** Commits covered by the session (or touching one file), read live from git. */
@Component({
  selector: 'grb-commits-panel',
  imports: [DatePipe, Button],
  template: `
    @let s = state();
    @if (path(); as p) {
      <div class="scope">
        <span class="muted">History of</span> <span class="mono">{{ p }}</span>
        <p-button label="All commits" size="small" [text]="true" (onClick)="showAll.emit()" />
      </div>
    }
    @if (s.loading) {
      <div class="muted"><i class="pi pi-spin pi-spinner"></i> Reading git history…</div>
    } @else if (s.error) {
      <div class="error-box">{{ s.error }}</div>
    } @else if (s.list; as list) {
      <div class="muted small range">
        Range <span class="mono">{{ list.range }}</span> · {{ list.commits.length }} commit{{ list.commits.length === 1 ? '' : 's' }}
      </div>
      @if (list.commits.length === 0) {
        <p class="muted">
          @if (list.range === 'working tree') {
            This review compares uncommitted changes: there is no commit to show.
          } @else {
            No commit found.
          }
        </p>
      }
      @for (c of list.commits; track c.sha) {
        <article class="commit" [class.open]="expanded() === c.sha">
          <header (click)="toggle(c.sha)">
            <i class="pi" [class.pi-chevron-right]="expanded() !== c.sha" [class.pi-chevron-down]="expanded() === c.sha"></i>
            <div class="headline">
              <div class="subject">{{ c.subject }}</div>
              <div class="meta small muted">
                <span class="mono sha">{{ c.shortSha }}</span> · {{ c.author }} · {{ c.date | date: 'medium' }}
              </div>
            </div>
            <span class="small muted nowrap">{{ c.files.length }} file{{ c.files.length === 1 ? '' : 's' }}</span>
          </header>
          @if (expanded() === c.sha) {
            <div class="details">
              <div class="small muted">
                <span class="mono">{{ c.sha }}</span>
                @if (c.parents.length > 1) {
                  · merge of {{ c.parents.length }} parents
                }
                · {{ c.email }}
              </div>
              @if (c.body) {
                <pre class="body">{{ c.body }}</pre>
              }
              <ul class="files">
                @for (f of c.files; track f.path) {
                  @let inReview = store.fileByPath().get(f.path);
                  <li [class.link]="inReview" (click)="open(f)" [title]="inReview ? 'Open in the review' : 'Not part of this review'">
                    <span class="mono path">{{ f.path }}</span>
                    @if (f.binary) {
                      <span class="small muted">binary</span>
                    } @else {
                      <span class="mono small"><span class="add">+{{ f.additions }}</span> <span class="del">−{{ f.deletions }}</span></span>
                    }
                  </li>
                }
              </ul>
            </div>
          }
        </article>
      }
    }
  `,
  styles: `
    :host {
      display: block;
    }
    .scope {
      display: flex;
      align-items: center;
      gap: 0.4rem;
      flex-wrap: wrap;
      margin-bottom: 0.5rem;
    }
    .range {
      margin-bottom: 0.75rem;
    }
    .commit {
      border: 1px solid var(--p-content-border-color);
      border-radius: 6px;
      margin-bottom: 0.5rem;
      header {
        display: flex;
        align-items: flex-start;
        gap: 0.5rem;
        padding: 0.5rem 0.6rem;
        cursor: pointer;
        .pi {
          font-size: 0.7rem;
          margin-top: 0.35rem;
          color: var(--p-text-muted-color);
        }
      }
      &.open header {
        border-bottom: 1px solid var(--p-content-border-color);
      }
    }
    .headline {
      flex: 1;
      min-width: 0;
    }
    .subject {
      font-weight: 600;
      overflow-wrap: anywhere;
    }
    .sha {
      color: var(--p-primary-color);
    }
    .nowrap {
      white-space: nowrap;
    }
    .details {
      padding: 0.5rem 0.75rem 0.75rem;
    }
    .body {
      white-space: pre-wrap;
      font-family: var(--grb-mono);
      font-size: 0.8rem;
      background: var(--grb-code-bg);
      padding: 0.5rem;
      border-radius: 4px;
    }
    .files {
      list-style: none;
      padding: 0;
      margin: 0.5rem 0 0;
      li {
        display: flex;
        justify-content: space-between;
        gap: 0.75rem;
        padding: 0.15rem 0.25rem;
        border-radius: 4px;
        font-size: 0.8rem;
        color: var(--p-text-muted-color);
        &.link {
          color: var(--p-text-color);
          cursor: pointer;
          &:hover {
            background: var(--p-content-hover-background);
          }
        }
      }
      .path {
        overflow-wrap: anywhere;
      }
    }
  `,
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class CommitsPanel {
  private readonly api = inject(ApiService);
  protected readonly store = inject(ReviewStore);

  /** Restrict the list to the commits touching this path (null = whole range). */
  readonly path = input<string | null>(null);
  readonly fileOpened = output<void>();
  readonly showAll = output<void>();

  protected readonly expanded = signal<string | null>(null);

  private readonly query = computed(() => ({ sessionId: this.store.session()?.id, path: this.path() }));

  protected readonly state = toSignal(
    toObservable(this.query).pipe(
      switchMap(({ sessionId, path }) =>
        sessionId == null
          ? of<LoadState>({ loading: true })
          : this.api.listCommits(sessionId, path ?? undefined).pipe(
              map((list): LoadState => ({ loading: false, list })),
              catchError((err) => of<LoadState>({ loading: false, error: errorMessage(err) })),
              startWith<LoadState>({ loading: true }),
            ),
      ),
    ),
    { initialValue: { loading: true } as LoadState },
  );

  protected toggle(sha: string): void {
    this.expanded.update((cur) => (cur === sha ? null : sha));
  }

  protected open(f: CommitFile): void {
    const file = this.store.fileByPath().get(f.path);
    if (!file) return;
    if (file.excluded && this.store.filter() !== 'excluded') {
      this.store.filter.set('excluded');
    }
    this.store.selectFile(file.id);
    this.fileOpened.emit();
  }
}
