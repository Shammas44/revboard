-- Notes can be left out of the Markdown export (all notes are exported by default).
ALTER TABLE comments ADD COLUMN export INTEGER NOT NULL DEFAULT 1;
