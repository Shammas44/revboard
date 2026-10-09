import { ChangeDetectionStrategy, Component, computed, inject, input } from '@angular/core';
import { DiffLine, Hunk, NoteAnchor, Side } from '../../core/models';
import { anchorKey, ReviewStore } from '../../core/review-store';
import { highlightHunk } from '../../core/syntax';
import { NoteThread } from './note-thread';

/** One displayed line with the anchor its notes are attached to. */
interface Cell {
  line: DiffLine;
  /** Position of the line in the hunk. */
  index: number;
  side: Side;
  number: number;
  key: string;
}

/** Side-by-side row: old version on the left, new version on the right. */
interface SplitRow {
  left: Cell | null;
  right: Cell | null;
  /** Distinct note anchors of the row (a context line has the same anchor on both sides). */
  keys: Cell[];
}

const SIGN: Record<DiffLine['kind'], string> = { context: ' ', add: '+', delete: '-' };

/**
 * Lines of a hunk, unified or side by side. Its only input is the immutable
 * hunk data, so this view is not re-checked when review statuses change; it
 * reacts to notes and to the diff mode only.
 */
@Component({
  selector: 'grb-hunk-lines',
  imports: [NoteThread],
  template: `
    @if (store.diffMode() === 'unified') {
      <table class="diff">
        <colgroup><col class="c-num" /><col class="c-num" /><col /></colgroup>
        <tbody>
          @for (c of cells(); track $index) {
            <tr [class]="'ln-' + c.line.kind" [class.focused]="isFocus(c)" [class.marked]="isMarked(c)" [attr.data-anchor]="c.side + ':' + c.number">
              <td class="num" [class.noted]="c.side === 'old' && isMarked(c)" (click)="compose(c)" [title]="numTitle(c, c.line.oldLine)">@if (c.line.oldLine) {<span class="n">{{ c.line.oldLine }}</span><i class="pi pi-comment ic"></i>}</td>
              <td class="num" [class.noted]="c.side === 'new' && isMarked(c)" (click)="compose(c)" [title]="numTitle(c, c.line.newLine)">@if (c.line.newLine) {<span class="n">{{ c.line.newLine }}</span><i class="pi pi-comment ic"></i>}</td>
              <td class="code"><span class="sign">{{ sign[c.line.kind] }}</span>@if (html(); as h) {<span [innerHTML]="h[c.index]"></span>} @else {{{ c.line.content }}}@if (c.line.noNewline) {<span class="eof" title="No newline at end of file">⏎̸</span>}</td>
            </tr>
            @let notes = notesByKey().get(c.key);
            @if (notes || composingKey() === c.key) {
              <tr class="note-row">
                <td colspan="3"><grb-note-thread [notes]="notes ?? []" [anchor]="anchor(c)" /></td>
              </tr>
            }
          }
        </tbody>
      </table>
    } @else {
      <table class="diff split">
        <colgroup><col class="c-num" /><col /><col class="c-num" /><col /></colgroup>
        <tbody>
          @for (r of splitRows(); track $index) {
            <tr>
              @if (r.left; as c) {
                <td class="num" [class]="numClass(c, 'old')" (click)="compose(c)" [attr.data-anchor]="c.side + ':' + c.number" [title]="numTitle(c, c.line.oldLine)"><span class="n">{{ c.line.oldLine }}</span><i class="pi pi-comment ic"></i></td>
                <td class="code" [class]="cellClass(c)"><span class="sign">{{ sign[c.line.kind] }}</span>@if (html(); as h) {<span [innerHTML]="h[c.index]"></span>} @else {{{ c.line.content }}}@if (c.line.noNewline) {<span class="eof" title="No newline at end of file">⏎̸</span>}</td>
              } @else {
                <td class="num empty"></td>
                <td class="code empty"></td>
              }
              @if (r.right; as c) {
                <td class="num" [class]="numClass(c, 'new')" (click)="compose(c)" [attr.data-anchor]="c.side + ':' + c.number" [title]="numTitle(c, c.line.newLine)"><span class="n">{{ c.line.newLine }}</span><i class="pi pi-comment ic"></i></td>
                <td class="code" [class]="cellClass(c)"><span class="sign">{{ sign[c.line.kind] }}</span>@if (html(); as h) {<span [innerHTML]="h[c.index]"></span>} @else {{{ c.line.content }}}@if (c.line.noNewline) {<span class="eof" title="No newline at end of file">⏎̸</span>}</td>
              } @else {
                <td class="num empty"></td>
                <td class="code empty"></td>
              }
            </tr>
            @for (c of r.keys; track c.key) {
              @let notes = notesByKey().get(c.key);
              @if (notes || composingKey() === c.key) {
                <tr class="note-row">
                  <td colspan="4"><grb-note-thread [notes]="notes ?? []" [anchor]="anchor(c)" /></td>
                </tr>
              }
            }
          }
        </tbody>
      </table>
    }
  `,
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class HunkLines {
  protected readonly store = inject(ReviewStore);

  readonly hunk = input.required<Hunk>();
  readonly path = input.required<string>();

  protected readonly sign = SIGN;

  /** Notes are anchored on the new side, except for deleted lines. */
  protected readonly cells = computed<Cell[]>(() => {
    const path = this.path();
    return this.hunk().lines.map((line, index) => {
      const side: Side = line.kind === 'delete' ? 'old' : 'new';
      const number = side === 'old' ? line.oldLine : line.newLine;
      return { line, index, side, number, key: anchorKey(path, side, number) };
    });
  });

  /** Pairs the deleted and added lines of each change block (computed only in split mode). */
  protected readonly splitRows = computed<SplitRow[]>(() => {
    const rows: SplitRow[] = [];
    let dels: Cell[] = [];
    let adds: Cell[] = [];
    const flush = () => {
      for (let i = 0; i < Math.max(dels.length, adds.length); i++) {
        const left = dels[i] ?? null;
        const right = adds[i] ?? null;
        rows.push({ left, right, keys: [left, right].filter((c): c is Cell => c !== null) });
      }
      dels = [];
      adds = [];
    };
    for (const c of this.cells()) {
      if (c.line.kind === 'delete') {
        if (adds.length > 0) flush();
        dels.push(c);
      } else if (c.line.kind === 'add') {
        adds.push(c);
      } else {
        flush();
        rows.push({ left: c, right: c, keys: [c] });
      }
    }
    flush();
    return rows;
  });

  protected readonly notesByKey = this.store.commentsByAnchor;

  /** Highlighted HTML per line (escaped by highlight.js), or null when disabled/unsupported. */
  protected readonly html = computed(() =>
    this.store.syntaxHighlight() ? highlightHunk(this.path(), this.hunk().lines) : null,
  );

  protected readonly composingKey = computed(() => {
    const c = this.store.composer();
    return c && c.line > 0 ? anchorKey(c.filePath, c.side, c.line) : null;
  });

  /** "side:line" of the focused line, when it belongs to this hunk. */
  protected readonly focusAnchor = computed(() => {
    const f = this.store.focusLine();
    return f && f.hunkId === this.hunk().id ? `${f.side}:${f.line}` : null;
  });

  protected isFocus(c: Cell): boolean {
    return this.focusAnchor() === `${c.side}:${c.number}`;
  }

  /** The line carries notes (or is the focused line): yellow border and comment icon. */
  protected isMarked(c: Cell): boolean {
    return this.notesByKey().has(c.key) || this.isFocus(c);
  }

  protected cellClass(c: Cell): string {
    return this.isFocus(c) ? `ln-${c.line.kind} focused` : `ln-${c.line.kind}`;
  }

  /** Split view: only the number cell of the note's side carries the marker. */
  protected numClass(c: Cell, cellSide: Side): string {
    return cellSide === c.side && this.isMarked(c) ? `${this.cellClass(c)} noted` : this.cellClass(c);
  }

  protected numTitle(c: Cell, number: number): string {
    const n = this.notesByKey().get(c.key)?.length ?? 0;
    if (!number) return '';
    return n > 0 ? `Line ${c.number}: ${n} note${n > 1 ? 's' : ''}` : `Add a note on line ${number}`;
  }

  protected anchor(c: Cell): NoteAnchor {
    return { filePath: this.path(), side: c.side, line: c.number };
  }

  protected compose(c: Cell): void {
    this.store.composer.set(this.anchor(c));
  }
}
