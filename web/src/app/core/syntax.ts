import hljs from 'highlight.js/lib/core';
import bash from 'highlight.js/lib/languages/bash';
import c from 'highlight.js/lib/languages/c';
import cpp from 'highlight.js/lib/languages/cpp';
import csharp from 'highlight.js/lib/languages/csharp';
import css from 'highlight.js/lib/languages/css';
import dockerfile from 'highlight.js/lib/languages/dockerfile';
import go from 'highlight.js/lib/languages/go';
import ini from 'highlight.js/lib/languages/ini';
import java from 'highlight.js/lib/languages/java';
import javascript from 'highlight.js/lib/languages/javascript';
import json from 'highlight.js/lib/languages/json';
import kotlin from 'highlight.js/lib/languages/kotlin';
import less from 'highlight.js/lib/languages/less';
import makefile from 'highlight.js/lib/languages/makefile';
import markdown from 'highlight.js/lib/languages/markdown';
import php from 'highlight.js/lib/languages/php';
import properties from 'highlight.js/lib/languages/properties';
import python from 'highlight.js/lib/languages/python';
import ruby from 'highlight.js/lib/languages/ruby';
import rust from 'highlight.js/lib/languages/rust';
import scss from 'highlight.js/lib/languages/scss';
import sql from 'highlight.js/lib/languages/sql';
import swift from 'highlight.js/lib/languages/swift';
import typescript from 'highlight.js/lib/languages/typescript';
import xml from 'highlight.js/lib/languages/xml';
import yaml from 'highlight.js/lib/languages/yaml';
import { DiffLine } from './models';

const LANGUAGES = {
  bash, c, cpp, csharp, css, dockerfile, go, ini, java, javascript, json, kotlin, less, makefile,
  markdown, php, properties, python, ruby, rust, scss, sql, swift, typescript, xml, yaml,
};
for (const [name, def] of Object.entries(LANGUAGES)) {
  hljs.registerLanguage(name, def);
}

/** File extension (lower case, without dot) → highlight.js language. */
const BY_EXTENSION: Record<string, string> = {
  sh: 'bash', bash: 'bash', zsh: 'bash',
  c: 'c', h: 'c',
  cc: 'cpp', cpp: 'cpp', cxx: 'cpp', hpp: 'cpp', hh: 'cpp',
  cs: 'csharp',
  css: 'css', less: 'less', scss: 'scss', sass: 'scss',
  go: 'go',
  ini: 'ini', cfg: 'ini', conf: 'ini', toml: 'ini', editorconfig: 'ini',
  java: 'java',
  js: 'javascript', mjs: 'javascript', cjs: 'javascript', jsx: 'javascript',
  ts: 'typescript', mts: 'typescript', cts: 'typescript', tsx: 'typescript',
  json: 'json', jsonc: 'json', json5: 'json',
  kt: 'kotlin', kts: 'kotlin',
  md: 'markdown', markdown: 'markdown',
  php: 'php',
  properties: 'properties',
  py: 'python', pyi: 'python',
  rb: 'ruby',
  rs: 'rust',
  sql: 'sql',
  swift: 'swift',
  html: 'xml', htm: 'xml', xml: 'xml', svg: 'xml', xsd: 'xml', vue: 'xml',
  yml: 'yaml', yaml: 'yaml',
};

/** Exact file names → language. */
const BY_NAME: Record<string, string> = {
  dockerfile: 'dockerfile',
  makefile: 'makefile',
  gnumakefile: 'makefile',
  '.bashrc': 'bash',
  '.zshrc': 'bash',
  'go.mod': 'go',
};

/** Hunks larger than this are not highlighted (keeps rendering fast). */
const MAX_LINES = 3000;
/** Minified or generated lines are not worth highlighting. */
const MAX_LINE_LENGTH = 2000;

/** Returns the highlight.js language of a path, or null when unknown. */
export function languageFor(path: string): string | null {
  const name = path.slice(path.lastIndexOf('/') + 1).toLowerCase();
  if (BY_NAME[name]) return BY_NAME[name];
  if (name.startsWith('dockerfile')) return 'dockerfile';
  const dot = name.lastIndexOf('.');
  return dot >= 0 ? (BY_EXTENSION[name.slice(dot + 1)] ?? null) : null;
}

/**
 * Highlights a hunk. The old side (context + deleted lines) and the new side
 * (context + added lines) are highlighted as continuous text so that
 * multi-line constructs (comments, strings) keep their colours, then mapped
 * back to the diff lines. Returns one HTML string per line, or null when the
 * language is unknown or the hunk is too large.
 */
export function highlightHunk(path: string, lines: readonly DiffLine[]): string[] | null {
  const language = languageFor(path);
  if (!language || lines.length > MAX_LINES || lines.some((l) => l.content.length > MAX_LINE_LENGTH)) {
    return null;
  }
  const oldText: string[] = [];
  const newText: string[] = [];
  for (const l of lines) {
    if (l.kind !== 'add') oldText.push(l.content);
    if (l.kind !== 'delete') newText.push(l.content);
  }
  const oldHtml = highlightBlock(language, oldText);
  const newHtml = highlightBlock(language, newText);
  let o = 0;
  let n = 0;
  return lines.map((l) => {
    switch (l.kind) {
      case 'delete':
        return oldHtml[o++];
      case 'add':
        return newHtml[n++];
      default:
        o++;
        return newHtml[n++];
    }
  });
}

function highlightBlock(language: string, lines: string[]): string[] {
  if (lines.length === 0) return [];
  const html = hljs.highlight(lines.join('\n'), { language, ignoreIllegals: true }).value;
  return splitHtmlLines(html, lines.length);
}

/**
 * Splits highlight.js HTML into lines. Spans open at a line break are closed
 * at the end of the line and re-opened at the start of the next one, so every
 * line is a self-contained fragment.
 */
export function splitHtmlLines(html: string, expected: number): string[] {
  const out: string[] = [];
  const open: string[] = [];
  let current = '';
  const token = /<span[^>]*>|<\/span>|\n|[^<\n]+/g;
  let m: RegExpExecArray | null;
  while ((m = token.exec(html)) !== null) {
    const t = m[0];
    if (t === '\n') {
      out.push(current + '</span>'.repeat(open.length));
      current = open.join('');
    } else if (t === '</span>') {
      open.pop();
      current += t;
    } else {
      if (t.startsWith('<span')) open.push(t);
      current += t;
    }
  }
  out.push(current + '</span>'.repeat(open.length));
  while (out.length < expected) out.push('');
  return out;
}
