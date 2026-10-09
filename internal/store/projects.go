package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
)

const settingCurrentProject = "current_project_id"

const projectColumns = `p.id, p.name, p.repo_path, p.created_at, p.last_used_at,
	(SELECT COUNT(*) FROM review_sessions s WHERE s.project_id = p.id AND s.state = 'active')`

func scanProject(row interface{ Scan(...any) error }) (Project, error) {
	var p Project
	var created, used string
	if err := row.Scan(&p.ID, &p.Name, &p.RepoPath, &created, &used, &p.ActiveSessions); err != nil {
		return p, err
	}
	p.CreatedAt, p.LastUsedAt = parseTime(created), parseTime(used)
	return p, nil
}

// EnsureProject returns the project of repoPath, creating it when needed.
// New projects are named after the repository directory, de-duplicated with a numeric suffix.
func (s *Store) EnsureProject(ctx context.Context, repoPath string) (Project, error) {
	var id int64
	err := s.tx(ctx, func(tx *sql.Tx) error {
		err := tx.QueryRowContext(ctx, `SELECT id FROM projects WHERE repo_path = ?`, repoPath).Scan(&id)
		if err == nil {
			_, err = tx.ExecContext(ctx, `UPDATE projects SET last_used_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now') WHERE id = ?`, id)
			return err
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		base := filepath.Base(repoPath)
		name := base
		for i := 2; ; i++ {
			var n int
			if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM projects WHERE name = ?`, name).Scan(&n); err != nil {
				return err
			}
			if n == 0 {
				break
			}
			name = fmt.Sprintf("%s-%d", base, i)
		}
		res, err := tx.ExecContext(ctx, `INSERT INTO projects(name, repo_path) VALUES (?, ?)`, name, repoPath)
		if err != nil {
			return err
		}
		id, err = res.LastInsertId()
		return err
	})
	if err != nil {
		return Project{}, err
	}
	return s.GetProject(ctx, id)
}

// GetProject returns a project by id.
func (s *Store) GetProject(ctx context.Context, id int64) (Project, error) {
	p, err := scanProject(s.db.QueryRowContext(ctx, `SELECT `+projectColumns+` FROM projects p WHERE p.id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return p, notFound("project", id)
	}
	if err != nil {
		return p, err
	}
	cur, _ := s.currentProjectID(ctx)
	p.IsCurrent = cur == p.ID
	return p, nil
}

// FindProject resolves a project by name or numeric id.
func (s *Store) FindProject(ctx context.Context, ref string) (Project, error) {
	p, err := scanProject(s.db.QueryRowContext(ctx, `SELECT `+projectColumns+` FROM projects p WHERE p.name = ?`, ref))
	if err == nil {
		cur, _ := s.currentProjectID(ctx)
		p.IsCurrent = cur == p.ID
		return p, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return p, err
	}
	if id, perr := strconv.ParseInt(ref, 10, 64); perr == nil {
		return s.GetProject(ctx, id)
	}
	return p, fmt.Errorf("project %q: %w", ref, ErrNotFound)
}

// ListProjects returns all projects, most recently used first.
func (s *Store) ListProjects(ctx context.Context) ([]Project, error) {
	// Read the setting first: the single connection is held while rows are open.
	cur, err := s.currentProjectID(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+projectColumns+` FROM projects p ORDER BY p.last_used_at DESC, p.id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	projects := []Project{}
	for rows.Next() {
		p, err := scanProject(rows)
		if err != nil {
			return nil, err
		}
		p.IsCurrent = p.ID == cur
		projects = append(projects, p)
	}
	return projects, rows.Err()
}

// ProjectContaining returns the project whose repository contains dir (deepest match).
func (s *Store) ProjectContaining(ctx context.Context, dir string) (Project, error) {
	projects, err := s.ListProjects(ctx)
	if err != nil {
		return Project{}, err
	}
	var best Project
	dir = filepath.Clean(dir)
	for _, p := range projects {
		if dir == p.RepoPath || strings.HasPrefix(dir, p.RepoPath+string(filepath.Separator)) {
			if len(p.RepoPath) > len(best.RepoPath) {
				best = p
			}
		}
	}
	if best.ID == 0 {
		return best, fmt.Errorf("project containing %s: %w", dir, ErrNotFound)
	}
	return best, nil
}

// SetCurrentProject records the project used by CLI commands by default.
func (s *Store) SetCurrentProject(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO settings(key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, settingCurrentProject, strconv.FormatInt(id, 10))
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `UPDATE projects SET last_used_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now') WHERE id = ?`, id)
	return err
}

// CurrentProject returns the current project, or ErrNotFound when none is set.
func (s *Store) CurrentProject(ctx context.Context) (Project, error) {
	id, err := s.currentProjectID(ctx)
	if err != nil {
		return Project{}, err
	}
	if id == 0 {
		return Project{}, fmt.Errorf("current project: %w", ErrNotFound)
	}
	return s.GetProject(ctx, id)
}

func (s *Store) currentProjectID(ctx context.Context) (int64, error) {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, settingCurrentProject).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	id, _ := strconv.ParseInt(v, 10, 64)
	return id, nil
}

// RenameProject changes the name of a project.
func (s *Store) RenameProject(ctx context.Context, id int64, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("project name cannot be empty: %w", ErrInvalid)
	}
	res, err := s.db.ExecContext(ctx, `UPDATE projects SET name = ? WHERE id = ?`, name, id)
	if isUniqueViolation(err) {
		return fmt.Errorf("a project named %q already exists: %w", name, ErrConflict)
	}
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return notFound("project", id)
	}
	return nil
}

// DeleteProject removes a project and all of its sessions.
func (s *Store) DeleteProject(ctx context.Context, id int64) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `DELETE FROM projects WHERE id = ?`, id)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return notFound("project", id)
		}
		_, err = tx.ExecContext(ctx, `DELETE FROM settings WHERE key = ? AND value = ?`, settingCurrentProject, strconv.FormatInt(id, 10))
		return err
	})
}
