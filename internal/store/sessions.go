package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"revboard/internal/diff"
	"revboard/internal/exclude"
)

const sessionColumns = `s.id, s.project_id, p.name, p.repo_path, s.name, s.git_args, s.work_dir,
	s.head_sha, s.state, s.exclusions, s.created_at, s.updated_at, s.last_opened_at, s.captured_at`

func scanSession(row interface{ Scan(...any) error }) (Session, error) {
	var se Session
	var args, exclusions, created, updated, opened, captured string
	err := row.Scan(&se.ID, &se.ProjectID, &se.ProjectName, &se.RepoPath, &se.Name, &args, &se.WorkDir,
		&se.HeadSHA, &se.State, &exclusions, &created, &updated, &opened, &captured)
	if err != nil {
		return se, err
	}
	if err := json.Unmarshal([]byte(args), &se.GitArgs); err != nil {
		return se, fmt.Errorf("decode git_args of session %d: %w", se.ID, err)
	}
	if err := json.Unmarshal([]byte(exclusions), &se.Exclusions); err != nil {
		return se, fmt.Errorf("decode exclusions of session %d: %w", se.ID, err)
	}
	se.CreatedAt, se.UpdatedAt = parseTime(created), parseTime(updated)
	se.LastOpenedAt, se.CapturedAt = parseTime(opened), parseTime(captured)
	return se, nil
}

// CreateSession stores a new session and its diff snapshot in one transaction.
func (s *Store) CreateSession(ctx context.Context, ns NewSession, d *diff.Diff) (int64, error) {
	args, err := json.Marshal(ns.GitArgs)
	if err != nil {
		return 0, err
	}
	var id int64
	err = s.tx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `INSERT INTO review_sessions(project_id, name, git_args, work_dir, head_sha)
			VALUES (?, ?, ?, ?, ?)`, ns.ProjectID, ns.Name, string(args), ns.WorkDir, ns.HeadSHA)
		if err != nil {
			return err
		}
		if id, err = res.LastInsertId(); err != nil {
			return err
		}
		return insertSnapshot(ctx, tx, id, d, nil)
	})
	return id, err
}

// GetSession returns a session with its progress.
func (s *Store) GetSession(ctx context.Context, id int64) (Session, error) {
	se, err := scanSession(s.db.QueryRowContext(ctx, `SELECT `+sessionColumns+`
		FROM review_sessions s JOIN projects p ON p.id = s.project_id WHERE s.id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return se, notFound("session", id)
	}
	if err != nil {
		return se, err
	}
	se.Progress, err = progress(ctx, s.db, id)
	return se, err
}

// ListSessions returns the sessions of a project (projectID 0 = all projects),
// most recently opened first.
func (s *Store) ListSessions(ctx context.Context, projectID int64, includeArchived bool) ([]Session, error) {
	var where []string
	var args []any
	if projectID != 0 {
		where = append(where, "s.project_id = ?")
		args = append(args, projectID)
	}
	if !includeArchived {
		where = append(where, "s.state = 'active'")
	}
	q := `SELECT ` + sessionColumns + ` FROM review_sessions s JOIN projects p ON p.id = s.project_id`
	if len(where) > 0 {
		q += " WHERE " + strings.Join(where, " AND ")
	}
	q += " ORDER BY s.last_opened_at DESC, s.id DESC"
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	sessions := []Session{}
	for rows.Next() {
		se, err := scanSession(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		sessions = append(sessions, se)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range sessions {
		if sessions[i].Progress, err = progress(ctx, s.db, sessions[i].ID); err != nil {
			return nil, err
		}
	}
	return sessions, nil
}

// LatestSession returns the most recently opened active session of a project.
func (s *Store) LatestSession(ctx context.Context, projectID int64) (Session, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, `SELECT id FROM review_sessions WHERE project_id = ? AND state = 'active'
		ORDER BY last_opened_at DESC, id DESC LIMIT 1`, projectID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, fmt.Errorf("active session of project %d: %w", projectID, ErrNotFound)
	}
	if err != nil {
		return Session{}, err
	}
	return s.GetSession(ctx, id)
}

// TouchSession marks a session as opened now.
func (s *Store) TouchSession(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE review_sessions SET last_opened_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now') WHERE id = ?`, id)
	return err
}

// UpdateSession applies a patch to a session.
func (s *Store) UpdateSession(ctx context.Context, id int64, p SessionPatch) (Session, error) {
	var sets []string
	var args []any
	if p.Name != nil {
		name := strings.TrimSpace(*p.Name)
		if name == "" {
			return Session{}, fmt.Errorf("session name cannot be empty: %w", ErrInvalid)
		}
		sets, args = append(sets, "name = ?"), append(args, name)
	}
	if p.State != nil {
		if *p.State != SessionActive && *p.State != SessionArchived {
			return Session{}, fmt.Errorf("unknown session state %q: %w", *p.State, ErrInvalid)
		}
		sets, args = append(sets, "state = ?"), append(args, *p.State)
	}
	if p.Exclusions != nil {
		rules, err := exclude.NormalizeAll(*p.Exclusions)
		if err != nil {
			return Session{}, fmt.Errorf("%v: %w", err, ErrInvalid)
		}
		encoded, _ := json.Marshal(rules)
		sets, args = append(sets, "exclusions = ?"), append(args, string(encoded))
	}
	if p.UIState != nil {
		if !json.Valid(p.UIState) {
			return Session{}, fmt.Errorf("uiState is not valid JSON: %w", ErrInvalid)
		}
		sets, args = append(sets, "ui_state = ?"), append(args, string(p.UIState))
	}
	if len(sets) > 0 {
		sets = append(sets, "updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')")
		args = append(args, id)
		res, err := s.db.ExecContext(ctx, `UPDATE review_sessions SET `+strings.Join(sets, ", ")+` WHERE id = ?`, args...)
		if err != nil {
			return Session{}, err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return Session{}, notFound("session", id)
		}
	}
	return s.GetSession(ctx, id)
}

// DeleteSession removes a session and all of its review state.
func (s *Store) DeleteSession(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM review_sessions WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return notFound("session", id)
	}
	return nil
}

// GetSessionDetail returns a session with its file list and UI state.
func (s *Store) GetSessionDetail(ctx context.Context, id int64) (SessionDetail, error) {
	se, err := s.GetSession(ctx, id)
	if err != nil {
		return SessionDetail{}, err
	}
	files, err := fileSummaries(ctx, s.db, id, 0)
	if err != nil {
		return SessionDetail{}, err
	}
	var ui string
	if err := s.db.QueryRowContext(ctx, `SELECT ui_state FROM review_sessions WHERE id = ?`, id).Scan(&ui); err != nil {
		return SessionDetail{}, err
	}
	return SessionDetail{Session: se, Files: files, UIState: json.RawMessage(ui)}, nil
}

// sessionMatcher compiles the exclusion rules of a session.
func sessionMatcher(ctx context.Context, q dbtx, sessionID int64) (*exclude.Matcher, error) {
	var raw string
	err := q.QueryRowContext(ctx, `SELECT exclusions FROM review_sessions WHERE id = ?`, sessionID).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return exclude.New(nil), nil
	}
	if err != nil {
		return nil, err
	}
	var rules []string
	if err := json.Unmarshal([]byte(raw), &rules); err != nil {
		return nil, fmt.Errorf("decode exclusions of session %d: %w", sessionID, err)
	}
	return exclude.New(rules), nil
}

// progress aggregates file and hunk statuses of a session, ignoring excluded files.
func progress(ctx context.Context, q dbtx, sessionID int64) (Progress, error) {
	var p Progress
	m, err := sessionMatcher(ctx, q, sessionID)
	if err != nil {
		return p, err
	}
	rows, err := q.QueryContext(ctx, `SELECT f.id, f.path, f.status, h.status FROM review_files f
		LEFT JOIN review_hunks h ON h.file_id = f.id WHERE f.session_id = ? ORDER BY f.id`, sessionID)
	if err != nil {
		return p, err
	}
	defer rows.Close()
	var lastID int64
	excluded := false
	for rows.Next() {
		var id int64
		var path string
		var fileStatus Status
		var hunkStatus sql.NullString
		if err := rows.Scan(&id, &path, &fileStatus, &hunkStatus); err != nil {
			return p, err
		}
		if id != lastID {
			lastID = id
			excluded = m.Match(path)
			if excluded {
				p.ExcludedFiles++
			} else {
				p.TotalFiles++
				p.ReviewedFiles += boolToInt(fileStatus == StatusReviewed)
				p.AttentionFiles += boolToInt(fileStatus == StatusNeedsAttention)
			}
		}
		if hunkStatus.Valid && !excluded {
			p.TotalHunks++
			p.ReviewedHunks += boolToInt(Status(hunkStatus.String) == StatusReviewed)
			p.AttentionHunks += boolToInt(Status(hunkStatus.String) == StatusNeedsAttention)
		}
	}
	return p, rows.Err()
}
