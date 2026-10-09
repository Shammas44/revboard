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
	if err := row.Scan(&c.ID, &c.SessionID, &c.FilePath, &c.Side, &c.Line, &c.Body, &created, &updated); err != nil {
		return c, err
	}
	c.CreatedAt, c.UpdatedAt = parseTime(created), parseTime(updated)
	return c, nil
}

const commentColumns = `id, session_id, file_path, side, line, body, created_at, updated_at`

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

// CreateComment adds a note to a session.
func (s *Store) CreateComment(ctx context.Context, c Comment) (Comment, error) {
	if err := validateComment(c); err != nil {
		return Comment{}, err
	}
	if _, err := s.GetSession(ctx, c.SessionID); err != nil {
		return Comment{}, err
	}
	res, err := s.db.ExecContext(ctx, `INSERT INTO comments(session_id, file_path, side, line, body) VALUES (?, ?, ?, ?, ?)`,
		c.SessionID, c.FilePath, c.Side, c.Line, c.Body)
	if err != nil {
		return Comment{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Comment{}, err
	}
	return s.getComment(ctx, id)
}

func (s *Store) getComment(ctx context.Context, id int64) (Comment, error) {
	c, err := scanComment(s.db.QueryRowContext(ctx, `SELECT `+commentColumns+` FROM comments WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return c, notFound("note", id)
	}
	return c, err
}

// UpdateComment changes the body of a note.
func (s *Store) UpdateComment(ctx context.Context, id int64, body string) (Comment, error) {
	if strings.TrimSpace(body) == "" {
		return Comment{}, fmt.Errorf("note body cannot be empty: %w", ErrInvalid)
	}
	res, err := s.db.ExecContext(ctx, `UPDATE comments SET body = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now') WHERE id = ?`, body, id)
	if err != nil {
		return Comment{}, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return Comment{}, notFound("note", id)
	}
	return s.getComment(ctx, id)
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
