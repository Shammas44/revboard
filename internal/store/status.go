package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"revboard/internal/diff"
)

// NotesError is returned when an item holding notes would be marked reviewed
// without confirming the deletion of its notes (a reviewed item holds no notes).
type NotesError struct {
	Count int
}

func (e *NotesError) Error() string {
	return fmt.Sprintf("%d note(s) attached: confirm their deletion to mark it reviewed", e.Count)
}

func (e *NotesError) Unwrap() error { return ErrConflict }

// StatusOptions modifies a status change.
type StatusOptions struct {
	// DeleteNotes confirms the deletion of the notes of items marked reviewed.
	DeleteNotes bool
}

func now() string { return "strftime('%Y-%m-%dT%H:%M:%fZ', 'now')" }

// fileRef returns the session and path of a file.
func fileRef(ctx context.Context, q dbtx, fileID int64) (int64, string, error) {
	var sessionID int64
	var path string
	err := q.QueryRowContext(ctx, `SELECT session_id, path FROM review_files WHERE id = ?`, fileID).Scan(&sessionID, &path)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, "", notFound("file", fileID)
	}
	return sessionID, path, err
}

// fileNoteIDs returns the ids of every note attached to a file (file and line notes).
func fileNoteIDs(ctx context.Context, q dbtx, sessionID int64, path string) ([]int64, error) {
	return queryIDs(ctx, q, `SELECT id FROM comments WHERE session_id = ? AND file_path = ?`, sessionID, path)
}

// hunkNoteIDs returns the ids of the line notes anchored on a line of a hunk.
func hunkNoteIDs(ctx context.Context, q dbtx, hunkID int64) ([]int64, error) {
	var sessionID int64
	var path, body string
	var oldStart, newStart int
	err := q.QueryRowContext(ctx, `SELECT f.session_id, f.path, h.old_start, h.new_start, h.body
		FROM review_hunks h JOIN review_files f ON f.id = h.file_id WHERE h.id = ?`, hunkID).
		Scan(&sessionID, &path, &oldStart, &newStart, &body)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, notFound("hunk", hunkID)
	}
	if err != nil {
		return nil, err
	}
	lines := diff.ParseHunkBody(oldStart, newStart, body)
	rows, err := q.QueryContext(ctx, `SELECT id, side, line FROM comments
		WHERE session_id = ? AND file_path = ? AND line > 0`, sessionID, path)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		var side string
		var line int
		if err := rows.Scan(&id, &side, &line); err != nil {
			return nil, err
		}
		if findLine(lines, side, line) >= 0 {
			ids = append(ids, id)
		}
	}
	return ids, rows.Err()
}

func queryIDs(ctx context.Context, q dbtx, query string, args ...any) ([]int64, error) {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// clearNotes deletes the notes of an item marked reviewed, or reports them.
func clearNotes(ctx context.Context, tx *sql.Tx, ids []int64, opt StatusOptions) error {
	if len(ids) == 0 {
		return nil
	}
	if !opt.DeleteNotes {
		return &NotesError{Count: len(ids)}
	}
	for _, id := range ids {
		if _, err := tx.ExecContext(ctx, `DELETE FROM comments WHERE id = ?`, id); err != nil {
			return err
		}
	}
	return nil
}

// hasFileNotes reports whether a file carries file-level notes.
func hasFileNotes(ctx context.Context, q dbtx, sessionID int64, path string) (bool, error) {
	var n int
	err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM comments WHERE session_id = ? AND file_path = ? AND line = 0`,
		sessionID, path).Scan(&n)
	return n > 0, err
}

// recomputeFile derives the status of a file from its hunks: "needs attention"
// when a hunk needs attention or the file carries file notes, "reviewed" when
// every hunk is reviewed, "unreviewed" otherwise. Files without hunks keep
// their status unless they carry file notes.
func recomputeFile(ctx context.Context, tx *sql.Tx, fileID int64) error {
	sessionID, path, err := fileRef(ctx, tx, fileID)
	if err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, `SELECT status FROM review_hunks WHERE file_id = ?`, fileID)
	if err != nil {
		return err
	}
	var statuses []Status
	for rows.Next() {
		var st Status
		if err := rows.Scan(&st); err != nil {
			rows.Close()
			return err
		}
		statuses = append(statuses, st)
	}
	rows.Close()
	notes, err := hasFileNotes(ctx, tx, sessionID, path)
	if err != nil {
		return err
	}
	var st Status
	switch {
	case notes:
		st = StatusNeedsAttention
	case len(statuses) == 0:
		return nil
	default:
		st = aggregateStatus(statuses)
	}
	_, err = tx.ExecContext(ctx, `UPDATE review_files SET status = ?, updated_at = `+now()+` WHERE id = ?`, st, fileID)
	return err
}

// setFileStatus sets the status of a file and cascades it to its hunks.
func setFileStatus(ctx context.Context, tx *sql.Tx, fileID int64, st Status) error {
	if _, err := tx.ExecContext(ctx, `UPDATE review_hunks SET status = ?, updated_at = `+now()+`
		WHERE file_id = ? AND status <> ?`, st, fileID, st); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `UPDATE review_files SET status = ?, updated_at = `+now()+` WHERE id = ?`, st, fileID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return notFound("file", fileID)
	}
	return nil
}

func (s *Store) statusChange(ctx context.Context, tx *sql.Tx, sessionID int64, fileIDs []int64) (StatusChange, error) {
	sc := StatusChange{Files: make([]FileSummary, 0, len(fileIDs)), DeletedNoteIDs: []int64{}}
	for _, id := range fileIDs {
		f, err := fileSummary(ctx, tx, id)
		if err != nil {
			return sc, err
		}
		sc.Files = append(sc.Files, f)
	}
	var err error
	sc.Progress, err = progress(ctx, tx, sessionID)
	return sc, err
}

func touchSessionTx(ctx context.Context, tx *sql.Tx, sessionID int64) error {
	_, err := tx.ExecContext(ctx, `UPDATE review_sessions SET updated_at = `+now()+` WHERE id = ?`, sessionID)
	return err
}

// SetFileStatus sets the status of a file and of all of its hunks. Marking a
// file reviewed deletes all its notes, which must be confirmed with opt.DeleteNotes.
func (s *Store) SetFileStatus(ctx context.Context, fileID int64, st Status, opt StatusOptions) (StatusChange, error) {
	if !st.Valid() {
		return StatusChange{}, fmt.Errorf("unknown status %q: %w", st, ErrInvalid)
	}
	var sc StatusChange
	err := s.tx(ctx, func(tx *sql.Tx) error {
		sessionID, path, err := fileRef(ctx, tx, fileID)
		if err != nil {
			return err
		}
		var deleted []int64
		if st == StatusReviewed {
			if deleted, err = fileNoteIDs(ctx, tx, sessionID, path); err != nil {
				return err
			}
			if err := clearNotes(ctx, tx, deleted, opt); err != nil {
				return err
			}
		}
		if err := setFileStatus(ctx, tx, fileID, st); err != nil {
			return err
		}
		if err := touchSessionTx(ctx, tx, sessionID); err != nil {
			return err
		}
		sc, err = s.statusChange(ctx, tx, sessionID, []int64{fileID})
		sc.DeletedNoteIDs = append(sc.DeletedNoteIDs, deleted...)
		return err
	})
	return sc, err
}

// SetFilesStatus sets the status of several files of a session (e.g. a folder).
func (s *Store) SetFilesStatus(ctx context.Context, sessionID int64, fileIDs []int64, st Status, opt StatusOptions) (StatusChange, error) {
	if !st.Valid() {
		return StatusChange{}, fmt.Errorf("unknown status %q: %w", st, ErrInvalid)
	}
	var sc StatusChange
	err := s.tx(ctx, func(tx *sql.Tx) error {
		var deleted []int64
		paths := make([]string, len(fileIDs))
		for i, id := range fileIDs {
			sid, path, err := fileRef(ctx, tx, id)
			if errors.Is(err, ErrNotFound) || (err == nil && sid != sessionID) {
				return notFound("file", id)
			}
			if err != nil {
				return err
			}
			paths[i] = path
		}
		if st == StatusReviewed {
			for _, path := range paths {
				ids, err := fileNoteIDs(ctx, tx, sessionID, path)
				if err != nil {
					return err
				}
				deleted = append(deleted, ids...)
			}
			if err := clearNotes(ctx, tx, deleted, opt); err != nil {
				return err
			}
		}
		for _, id := range fileIDs {
			if err := setFileStatus(ctx, tx, id, st); err != nil {
				return err
			}
		}
		if err := touchSessionTx(ctx, tx, sessionID); err != nil {
			return err
		}
		var err error
		sc, err = s.statusChange(ctx, tx, sessionID, fileIDs)
		sc.DeletedNoteIDs = append(sc.DeletedNoteIDs, deleted...)
		return err
	})
	return sc, err
}

// SetHunkStatus sets the status of a hunk and recomputes the status of its file.
// Marking a hunk reviewed deletes its line notes, which must be confirmed with
// opt.DeleteNotes.
func (s *Store) SetHunkStatus(ctx context.Context, hunkID int64, st Status, opt StatusOptions) (StatusChange, error) {
	if !st.Valid() {
		return StatusChange{}, fmt.Errorf("unknown status %q: %w", st, ErrInvalid)
	}
	var sc StatusChange
	err := s.tx(ctx, func(tx *sql.Tx) error {
		var fileID, sessionID int64
		err := tx.QueryRowContext(ctx, `SELECT h.file_id, f.session_id FROM review_hunks h
			JOIN review_files f ON f.id = h.file_id WHERE h.id = ?`, hunkID).Scan(&fileID, &sessionID)
		if errors.Is(err, sql.ErrNoRows) {
			return notFound("hunk", hunkID)
		}
		if err != nil {
			return err
		}
		var deleted []int64
		if st == StatusReviewed {
			if deleted, err = hunkNoteIDs(ctx, tx, hunkID); err != nil {
				return err
			}
			if err := clearNotes(ctx, tx, deleted, opt); err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx, `UPDATE review_hunks SET status = ?, updated_at = `+now()+` WHERE id = ?`, st, hunkID); err != nil {
			return err
		}
		if err := recomputeFile(ctx, tx, fileID); err != nil {
			return err
		}
		if err := touchSessionTx(ctx, tx, sessionID); err != nil {
			return err
		}
		sc, err = s.statusChange(ctx, tx, sessionID, []int64{fileID})
		sc.DeletedNoteIDs = append(sc.DeletedNoteIDs, deleted...)
		return err
	})
	return sc, err
}

// flagForNote marks the item a new note is attached to as "needs attention":
// the hunk containing the noted line, or the file for a file note. It returns
// the id of the affected file (0 when nothing changed).
func flagForNote(ctx context.Context, tx *sql.Tx, c Comment) (int64, error) {
	if c.FilePath == "" {
		return 0, nil
	}
	var fileID int64
	err := tx.QueryRowContext(ctx, `SELECT id FROM review_files WHERE session_id = ? AND path = ?`,
		c.SessionID, c.FilePath).Scan(&fileID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	if c.Line == 0 {
		_, err = tx.ExecContext(ctx, `UPDATE review_files SET status = ?, updated_at = `+now()+` WHERE id = ?`,
			StatusNeedsAttention, fileID)
		return fileID, err
	}
	hunks, err := loadNoteHunks(ctx, tx, fileID)
	if err != nil {
		return 0, err
	}
	for _, h := range hunks {
		if findLine(h.lines, c.Side, c.Line) >= 0 {
			if _, err := tx.ExecContext(ctx, `UPDATE review_hunks SET status = ?, updated_at = `+now()+` WHERE id = ?`,
				StatusNeedsAttention, h.id); err != nil {
				return 0, err
			}
			return fileID, recomputeFile(ctx, tx, fileID)
		}
	}
	return 0, nil
}
