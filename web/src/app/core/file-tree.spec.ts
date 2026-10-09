import { signal } from '@angular/core';
import { buildTree, flattenTree } from './file-tree';
import { FileSummary, FileVM } from './models';

function file(id: number, path: string): FileVM {
  const summary = { id, path, hunks: [] } as unknown as FileSummary;
  return { id, summary, status: signal('unreviewed'), pinned: signal(false), excluded: false, hunks: [] };
}

describe('file tree', () => {
  const files = [file(1, 'src/app/core/a.ts'), file(2, 'src/app/core/b.ts'), file(3, 'README.md'), file(4, 'src/main.ts'), file(5, 'docs/x/y/z.md')];

  it('sorts directories first and compresses single-child chains', () => {
    const rows = flattenTree(buildTree(files), new Set());
    expect(rows.map((r) => (r.kind === 'dir' ? `${r.depth}:${r.dir.name}/` : `${r.depth}:${r.name}`))).toEqual([
      '0:docs/x/y/',
      '1:z.md',
      '0:src/',
      '1:app/core/',
      '2:a.ts',
      '2:b.ts',
      '1:main.ts',
      '0:README.md',
    ]);
  });

  it('hides the content of collapsed directories and lists their files', () => {
    const rows = flattenTree(buildTree(files), new Set(['src']));
    const src = rows.find((r) => r.kind === 'dir' && r.dir.path === 'src');
    expect(rows.length).toBe(4);
    expect(src?.kind === 'dir' && src.files.map((f) => f.id)).toEqual([1, 2, 4]);
  });
});
