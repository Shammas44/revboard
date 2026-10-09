import { highlightHunk, languageFor, splitHtmlLines } from './syntax';
import { DiffLine } from './models';

const line = (kind: DiffLine['kind'], content: string): DiffLine => ({ kind, content, oldLine: 0, newLine: 0, noNewline: false });

describe('syntax highlighting', () => {
  it('detects languages from paths', () => {
    expect(languageFor('src/app/x.component.ts')).toBe('typescript');
    expect(languageFor('cmd/main.go')).toBe('go');
    expect(languageFor('build/Dockerfile.prod')).toBe('dockerfile');
    expect(languageFor('Makefile')).toBe('makefile');
    expect(languageFor('LICENSE')).toBeNull();
  });

  it('re-opens spans across line breaks', () => {
    const lines = splitHtmlLines('<span class="a">x\ny<span class="b">z\nw</span></span>', 3);
    expect(lines).toEqual([
      '<span class="a">x</span>',
      '<span class="a">y<span class="b">z</span></span>',
      '<span class="a"><span class="b">w</span></span>',
    ]);
  });

  it('keeps multi-line comments coloured and maps old/new sides', () => {
    const html = highlightHunk('a.go', [
      line('context', '/* start of'),
      line('delete', 'old comment */ x := 1'),
      line('add', 'new comment */ y := "s"'),
    ])!;
    expect(html.length).toBe(3);
    expect(html[1]).toContain('hljs-comment');
    expect(html[1]).toContain('old comment');
    expect(html[2]).toContain('new comment');
    expect(html[2]).toContain('hljs-string');
  });

  it('escapes HTML and skips unknown languages', () => {
    expect(highlightHunk('x.ts', [line('add', 'const a = "<b>";')])![0]).not.toContain('<b>');
    expect(highlightHunk('notes.unknown', [line('add', 'x')])).toBeNull();
  });
});
