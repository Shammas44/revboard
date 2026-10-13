package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

func scanComment(row interface{ Scan(...any) error }) (Comment, error) {
	var c Comment
	var created, updated string
	if err := row.Scan(&c.ID, &c.SessionID, &c.Number, &c.FilePath, &c.Side, &c.Line, &c.Body, &c.Export, &created, &updated); err != nil {
		return c, err
	}
	c.CreatedAt, c.UpdatedAt = parseTime(created), parseTime(updated)
	return c, nil
}

const commentColumns = `id, session_id, number, file_path, side, line, body, export, created_at, updated_at`

// listComments returns the notes of a session, optionally restricted to one file path.
func listComments(ctx context.Context, q dbtx, sessionID int64, path *string) ([]Comment, error) {
	query := `SELECT ` + commentColumns + ` FROM comments WHERE session_id = ?`
	args := []any{sessionID}
	if path != nil {
		query += ` AND file_path = ?`
		args = append(args, *path)
	}
	rows, err := q.QueryContext(ctx, query+` ORDER BY file_path, line, id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	comments := []Comment{}
	for rows.Next() {
		c, err := scanComment(rows)
		if err != nil {
			return nil, err
		}
		comments = append(comments, c)
	}
	return comments, rows.Err()
}

// ListComments returns all notes of a session.
func (s *Store) ListComments(ctx context.Context, sessionID int64) ([]Comment, error) {
	return listComments(ctx, s.db, sessionID, nil)
}

func validateComment(c Comment) error {
	if strings.TrimSpace(c.Body) == "" {
		return fmt.Errorf("note body cannot be empty: %w", ErrInvalid)
	}
	switch {
	case c.FilePath == "" && (c.Side != "" || c.Line != 0):
		return fmt.Errorf("a session note cannot have a side or a line: %w", ErrInvalid)
	case c.Line < 0:
		return fmt.Errorf("line must be positive: %w", ErrInvalid)
	case c.Line == 0 && c.Side != "":
		return fmt.Errorf("a file note cannot have a side: %w", ErrInvalid)
	case c.Line > 0 && c.Side != "old" && c.Side != "new":
		return fmt.Errorf("a line note needs side \"old\" or \"new\": %w", ErrInvalid)
	}
	return nil
}

// CreateComment adds a note to a session. The hunk holding a line note, or the
// file of a file note, is marked "needs attention" in the same transaction.
func (s *Store) CreateComment(ctx context.Context, c Comment) (NoteCreated, error) {
	if err := validateComment(c); err != nil {
		return NoteCreated{}, err
	}
	if _, err := s.GetSession(ctx, c.SessionID); err != nil {
		return NoteCreated{}, err
	}
	var out NoteCreated
	err := s.tx(ctx, func(tx *sql.Tx) error {
		var number int
		if err := tx.QueryRowContext(ctx, `SELECT next_note_number FROM review_sessions WHERE id = ?`, c.SessionID).Scan(&number); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE review_sessions SET next_note_number = ? WHERE id = ?`, number+1, c.SessionID); err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx, `INSERT INTO comments(session_id, number, file_path, side, line, body) VALUES (?, ?, ?, ?, ?, ?)`,
			c.SessionID, number, c.FilePath, c.Side, c.Line, c.Body)
		if err != nil {
			return err
		}
		id, err := res.LastInsertId()
		if err != nil {
			return err
		}
		if out.Comment, err = scanComment(tx.QueryRowContext(ctx, `SELECT `+commentColumns+` FROM comments WHERE id = ?`, id)); err != nil {
			return err
		}
		fileID, err := flagForNote(ctx, tx, c)
		if err != nil || fileID == 0 {
			return err
		}
		if err := touchSessionTx(ctx, tx, c.SessionID); err != nil {
			return err
		}
		sc, err := s.statusChange(ctx, tx, c.SessionID, []int64{fileID})
		out.Change = &sc
		return err
	})
	return out, err
}

func (s *Store) getComment(ctx context.Context, id int64) (Comment, error) {
	c, err := scanComment(s.db.QueryRowContext(ctx, `SELECT `+commentColumns+` FROM comments WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return c, notFound("note", id)
	}
	return c, err
}

// CommentPatch lists the note fields to update (nil = unchanged).
type CommentPatch struct {
	Body   *string
	Export *bool
}

// UpdateComment changes the body and/or the export flag of a note.
func (s *Store) UpdateComment(ctx context.Context, id int64, p CommentPatch) (Comment, error) {
	var sets []string
	var args []any
	if p.Body != nil {
		if strings.TrimSpace(*p.Body) == "" {
			return Comment{}, fmt.Errorf("note body cannot be empty: %w", ErrInvalid)
		}
		sets, args = append(sets, "body = ?", "updated_at = "+now()), append(args, *p.Body)
	}
	if p.Export != nil {
		sets, args = append(sets, "export = ?"), append(args, boolToInt(*p.Export))
	}
	if len(sets) > 0 {
		res, err := s.db.ExecContext(ctx, `UPDATE comments SET `+strings.Join(sets, ", ")+` WHERE id = ?`, append(args, id)...)
		if err != nil {
			return Comment{}, err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return Comment{}, notFound("note", id)
		}
	}
	return s.getComment(ctx, id)
}

// SetCommentsExport sets the export flag of every note of a session.
func (s *Store) SetCommentsExport(ctx context.Context, sessionID int64, export bool) error {
	_, err := s.db.ExecContext(ctx, `UPDATE comments SET export = ? WHERE session_id = ?`, boolToInt(export), sessionID)
	return err
}

// DeleteComment removes a note.
func (s *Store) DeleteComment(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM comments WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return notFound("note", id)
	}
	return nil
}
