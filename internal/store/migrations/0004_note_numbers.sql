-- Stable per-session note numbers (#1, #2, ...), never reused after a deletion,
-- so that notes can be referred to across exports.
ALTER TABLE comments ADD COLUMN number INTEGER NOT NULL DEFAULT 0;
ALTER TABLE review_sessions ADD COLUMN next_note_number INTEGER NOT NULL DEFAULT 1;

-- Number existing notes in creation order.
UPDATE comments SET number = (
    SELECT COUNT(*) FROM comments c2 WHERE c2.session_id = comments.session_id AND c2.id <= comments.id
);
UPDATE review_sessions SET next_note_number = 1 + (
    SELECT COALESCE(MAX(c.number), 0) FROM comments c WHERE c.session_id = review_sessions.id
);
