import { WritableSignal } from '@angular/core';

/* ---- API models (mirror the Go DTOs of internal/store) ---- */

export type ReviewStatus = 'unreviewed' | 'reviewed' | 'needs_attention';
export type ChangeType = 'added' | 'deleted' | 'modified' | 'renamed' | 'copied';
export type LineKind = 'context' | 'add' | 'delete';
export type Side = '' | 'old' | 'new';
export type DiffMode = 'unified' | 'split';

export interface Project {
  id: number;
  name: string;
  repoPath: string;
  createdAt: string;
  lastUsedAt: string;
  activeSessions: number;
  isCurrent: boolean;
}

export interface Progress {
  totalFiles: number;
  reviewedFiles: number;
  attentionFiles: number;
  totalHunks: number;
  reviewedHunks: number;
  attentionHunks: number;
  /** Files hidden by the exclusion rules (not counted in the totals above). */
  excludedFiles: number;
}

export interface Session {
  id: number;
  projectId: number;
  projectName: string;
  repoPath: string;
  name: string;
  gitArgs: string[];
  workDir: string;
  headSha: string;
  state: 'active' | 'archived';
  exclusions: string[];
  createdAt: string;
  updatedAt: string;
  lastOpenedAt: string;
  capturedAt: string;
  progress: Progress;
}

export interface HunkRef {
  id: number;
  status: ReviewStatus;
}

export interface FileSummary {
  id: number;
  sessionId: number;
  position: number;
  path: string;
  oldPath: string;
  newPath: string;
  changeType: ChangeType;
  isBinary: boolean;
  oldMode: string;
  newMode: string;
  similarity: number;
  additions: number;
  deletions: number;
  status: ReviewStatus;
  pinned: boolean;
  excluded: boolean;
  hunks: HunkRef[];
  commentCount: number;
}

export interface SessionDetail {
  session: Session;
  files: FileSummary[];
  uiState: UiState | null;
}

export interface DiffLine {
  kind: LineKind;
  content: string;
  oldLine: number;
  newLine: number;
  noNewline: boolean;
}

export interface Hunk {
  id: number;
  position: number;
  oldStart: number;
  oldLines: number;
  newStart: number;
  newLines: number;
  section: string;
  status: ReviewStatus;
  lines: DiffLine[];
}

export interface Comment {
  id: number;
  sessionId: number;
  filePath: string;
  side: Side;
  line: number;
  body: string;
  createdAt: string;
  updatedAt: string;
}

export interface FileDetail {
  file: FileSummary;
  hunks: Hunk[];
  comments: Comment[];
}

export interface StatusChange {
  files: FileSummary[];
  progress: Progress;
}

export type FileFilter = 'all' | 'unreviewed' | 'needs_attention' | 'reviewed' | 'pinned' | 'commented' | 'excluded';

/** Persisted per session in review_sessions.ui_state. */
export interface UiState {
  selectedFileId?: number;
  filter?: FileFilter;
  collapseReviewed?: boolean;
  diffMode?: DiffMode;
  syntaxHighlight?: boolean;
}

export interface CommitFile {
  path: string;
  additions: number;
  deletions: number;
  binary: boolean;
}

export interface Commit {
  sha: string;
  shortSha: string;
  author: string;
  email: string;
  date: string;
  parents: string[];
  subject: string;
  body: string;
  files: CommitFile[];
}

export interface CommitList {
  /** Revision range, e.g. "dev..feature". */
  range: string;
  commits: Commit[];
}

export interface ExcerptLine extends DiffLine {
  /** The noted line. */
  focus: boolean;
}

/** A note with the code it refers to (GET /api/sessions/{id}/notes). */
export interface NoteContext extends Comment {
  /** File in the current snapshot (0 for session notes or vanished paths). */
  fileId: number;
  fileStatus?: ReviewStatus;
  excluded: boolean;
  hunkId: number;
  hunkHeader: string;
  /** Enclosing context reported by git after the @@ header. */
  hunkSection: string;
  /** All lines of the hunk; the noted one has focus = true. */
  excerpt: ExcerptLine[];
  /** Line note whose line is no longer part of the diff. */
  outdated: boolean;
}

/** A line to bring into view and highlight in the diff. */
export interface FocusLine {
  fileId: number;
  hunkId: number;
  side: Side;
  line: number;
}

/** Where a note is attached. */
export interface NoteAnchor {
  filePath: string;
  side: Side;
  line: number;
}

/* ---- View models (client only): fine-grained signals ---- */

export interface HunkVM {
  readonly id: number;
  readonly status: WritableSignal<ReviewStatus>;
}

export interface FileVM {
  readonly id: number;
  /** Immutable part of the file, as received from the API. */
  readonly summary: FileSummary;
  readonly status: WritableSignal<ReviewStatus>;
  readonly pinned: WritableSignal<boolean>;
  /** Hidden by the session exclusion rules (fixed until the rules change). */
  readonly excluded: boolean;
  readonly hunks: readonly HunkVM[];
}
