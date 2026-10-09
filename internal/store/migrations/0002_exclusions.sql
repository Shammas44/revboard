-- Session level exclusion rules (JSON array of gitignore-like patterns).
ALTER TABLE review_sessions ADD COLUMN exclusions TEXT NOT NULL DEFAULT '[]';
