-- Initial schema of the GRB database.

-- Key/value settings (e.g. current_project_id).
CREATE TABLE settings (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

-- A git repository known by GRB.
CREATE TABLE projects (
    id           INTEGER PRIMARY KEY,
    name         TEXT NOT NULL UNIQUE,
    repo_path    TEXT NOT NULL UNIQUE,              -- absolute top-level path
    created_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    last_used_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

-- One captured diff and the review state attached to it.
CREATE TABLE review_sessions (
    id             INTEGER PRIMARY KEY,
    project_id     INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name           TEXT NOT NULL,
    git_args       TEXT NOT NULL,                    -- JSON array: user argv without "git"
    work_dir       TEXT NOT NULL,                    -- directory the command runs in
    head_sha       TEXT NOT NULL DEFAULT '',         -- HEAD at capture time (informative)
    state          TEXT NOT NULL DEFAULT 'active' CHECK (state IN ('active', 'archived')),
    ui_state       TEXT NOT NULL DEFAULT '{}',       -- opaque JSON owned by the UI
    created_at     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    last_opened_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    captured_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
CREATE INDEX idx_sessions_project ON review_sessions(project_id, state, last_opened_at);

-- One file section of the diff, with its review status.
CREATE TABLE review_files (
    id          INTEGER PRIMARY KEY,
    session_id  INTEGER NOT NULL REFERENCES review_sessions(id) ON DELETE CASCADE,
    position    INTEGER NOT NULL,                    -- order in the diff output
    path        TEXT NOT NULL,                       -- display path (new path, or old if deleted)
    old_path    TEXT NOT NULL DEFAULT '',            -- '' when the file is added
    new_path    TEXT NOT NULL DEFAULT '',            -- '' when the file is deleted
    change_type TEXT NOT NULL CHECK (change_type IN ('added', 'deleted', 'modified', 'renamed', 'copied')),
    is_binary   INTEGER NOT NULL DEFAULT 0,
    old_mode    TEXT NOT NULL DEFAULT '',
    new_mode    TEXT NOT NULL DEFAULT '',
    similarity  INTEGER NOT NULL DEFAULT 0,          -- rename/copy similarity index (%)
    additions   INTEGER NOT NULL DEFAULT 0,
    deletions   INTEGER NOT NULL DEFAULT 0,
    status      TEXT NOT NULL DEFAULT 'unreviewed' CHECK (status IN ('unreviewed', 'reviewed', 'needs_attention')),
    pinned      INTEGER NOT NULL DEFAULT 0,
    updated_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
CREATE INDEX idx_files_session ON review_files(session_id, position);

-- One @@ hunk; the raw body is stored and re-parsed on demand.
CREATE TABLE review_hunks (
    id           INTEGER PRIMARY KEY,
    file_id      INTEGER NOT NULL REFERENCES review_files(id) ON DELETE CASCADE,
    position     INTEGER NOT NULL,
    old_start    INTEGER NOT NULL,
    old_lines    INTEGER NOT NULL,
    new_start    INTEGER NOT NULL,
    new_lines    INTEGER NOT NULL,
    section      TEXT NOT NULL DEFAULT '',           -- text after the closing @@ (function context)
    body         TEXT NOT NULL,                      -- raw hunk lines, prefixes included
    content_hash TEXT NOT NULL,                      -- sha256(body), used for carry-over on refresh
    status       TEXT NOT NULL DEFAULT 'unreviewed' CHECK (status IN ('unreviewed', 'reviewed', 'needs_attention')),
    updated_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
CREATE INDEX idx_hunks_file ON review_hunks(file_id, position);

-- Private notes. Anchored by path (not by file id) so they survive a refresh.
--   file_path = ''              -> session note
--   file_path set, line = 0     -> file note
--   file_path set, line > 0     -> line note on side 'old' or 'new'
CREATE TABLE comments (
    id         INTEGER PRIMARY KEY,
    session_id INTEGER NOT NULL REFERENCES review_sessions(id) ON DELETE CASCADE,
    file_path  TEXT NOT NULL DEFAULT '',
    side       TEXT NOT NULL DEFAULT '' CHECK (side IN ('', 'old', 'new')),
    line       INTEGER NOT NULL DEFAULT 0,
    body       TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
CREATE INDEX idx_comments_session_path ON comments(session_id, file_path);
