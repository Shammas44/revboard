import { FileVM } from './models';

/** Directory node of the file tree. */
export interface DirNode {
  /** Display name; single-child directory chains are compressed ("src/app/core"). */
  name: string;
  /** Full path of the directory, used as a stable key. */
  path: string;
  dirs: DirNode[];
  files: FileVM[];
}

/** One visible row of the flattened tree (rendered by a virtual scroll list). */
export type TreeRow =
  | { kind: 'dir'; key: string; depth: number; dir: DirNode; files: readonly FileVM[]; collapsed: boolean }
  | { kind: 'file'; key: string; depth: number; file: FileVM; name: string };

interface MutableDir {
  name: string;
  path: string;
  dirs: Map<string, MutableDir>;
  files: FileVM[];
}

const byName = (a: string, b: string) => a.localeCompare(b, undefined, { numeric: true, sensitivity: 'base' });

export function fileName(path: string): string {
  const i = path.lastIndexOf('/');
  return i < 0 ? path : path.slice(i + 1);
}

/** Builds a sorted directory tree (directories first) from a flat file list. */
export function buildTree(files: readonly FileVM[]): DirNode {
  const root: MutableDir = { name: '', path: '', dirs: new Map(), files: [] };
  for (const f of files) {
    const parts = f.summary.path.split('/');
    let dir = root;
    for (let i = 0; i < parts.length - 1; i++) {
      let child = dir.dirs.get(parts[i]);
      if (!child) {
        const path = dir.path ? `${dir.path}/${parts[i]}` : parts[i];
        child = { name: parts[i], path, dirs: new Map(), files: [] };
        dir.dirs.set(parts[i], child);
      }
      dir = child;
    }
    dir.files.push(f);
  }
  return freeze(root, true);
}

function freeze(d: MutableDir, isRoot: boolean): DirNode {
  // Compress chains of directories that only contain one directory.
  let current = d;
  let name = d.name;
  while (!isRoot && current.files.length === 0 && current.dirs.size === 1) {
    const only = current.dirs.values().next().value as MutableDir;
    name = `${name}/${only.name}`;
    current = only;
  }
  return {
    name,
    path: current.path,
    dirs: [...current.dirs.values()].sort((a, b) => byName(a.name, b.name)).map((c) => freeze(c, false)),
    files: [...current.files].sort((a, b) => byName(fileName(a.summary.path), fileName(b.summary.path))),
  };
}

/** All files below a directory, in tree order. */
export function filesUnder(dir: DirNode, out: FileVM[] = []): FileVM[] {
  for (const d of dir.dirs) {
    filesUnder(d, out);
  }
  out.push(...dir.files);
  return out;
}

/** Flattens the tree into visible rows, skipping the content of collapsed directories. */
export function flattenTree(root: DirNode, collapsed: ReadonlySet<string>): TreeRow[] {
  const rows: TreeRow[] = [];
  const walk = (dir: DirNode, depth: number) => {
    for (const d of dir.dirs) {
      const isCollapsed = collapsed.has(d.path);
      rows.push({ kind: 'dir', key: `d:${d.path}`, depth, dir: d, files: filesUnder(d), collapsed: isCollapsed });
      if (!isCollapsed) {
        walk(d, depth + 1);
      }
    }
    for (const f of dir.files) {
      rows.push({ kind: 'file', key: `f:${f.id}`, depth, file: f, name: fileName(f.summary.path) });
    }
  };
  walk(root, 0);
  return rows;
}
