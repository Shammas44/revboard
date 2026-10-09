// Package review implements the use-cases that capture a git diff into a session.
package review

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"

	"revboard/internal/diff"
	"revboard/internal/gitx"
	"revboard/internal/store"
)

// ErrEmptyDiff is returned when the git command produced no file to review.
var ErrEmptyDiff = errors.New("the diff is empty, nothing to review")

// Service captures diffs and stores them.
type Service struct {
	Store *store.Store
}

// StartResult describes a newly created session.
type StartResult struct {
	Project store.Project
	Session store.Session
}

// capture runs the git command in workDir and parses its output.
func capture(ctx context.Context, workDir string, args []string) (*diff.Diff, error) {
	if err := gitx.Validate(args); err != nil {
		return nil, err
	}
	out, err := gitx.Run(ctx, workDir, gitx.BuildArgs(args)...)
	if err != nil {
		return nil, err
	}
	d, err := diff.Parse(bytes.NewReader(out))
	if err != nil {
		return nil, err
	}
	if len(d.Files) == 0 {
		return nil, ErrEmptyDiff
	}
	return d, nil
}

// Start runs the git command (args without the leading "git") in workDir,
// registers the repository as a project, makes it current and stores the session.
func (s *Service) Start(ctx context.Context, workDir string, args []string, name string) (StartResult, error) {
	if err := gitx.Validate(args); err != nil {
		return StartResult{}, err
	}
	root, err := gitx.RepoRoot(ctx, workDir)
	if err != nil {
		return StartResult{}, err
	}
	d, err := capture(ctx, workDir, args)
	if err != nil {
		return StartResult{}, err
	}
	project, err := s.Store.EnsureProject(ctx, root)
	if err != nil {
		return StartResult{}, err
	}
	if err := s.Store.SetCurrentProject(ctx, project.ID); err != nil {
		return StartResult{}, err
	}
	if strings.TrimSpace(name) == "" {
		name = DefaultName(args)
	}
	id, err := s.Store.CreateSession(ctx, store.NewSession{
		ProjectID: project.ID,
		Name:      name,
		GitArgs:   args,
		WorkDir:   workDir,
		HeadSHA:   gitx.HeadSHA(ctx, workDir),
	}, d)
	if err != nil {
		return StartResult{}, err
	}
	session, err := s.Store.GetSession(ctx, id)
	if err != nil {
		return StartResult{}, err
	}
	project.IsCurrent = true
	return StartResult{Project: project, Session: session}, nil
}

// Refresh re-runs the stored git command of a session and replaces its snapshot,
// keeping the review state of unchanged hunks.
func (s *Service) Refresh(ctx context.Context, sessionID int64) (store.Session, error) {
	se, err := s.Store.GetSession(ctx, sessionID)
	if err != nil {
		return se, err
	}
	d, err := capture(ctx, se.WorkDir, se.GitArgs)
	if err != nil {
		return se, fmt.Errorf("refresh session %d: %w", sessionID, err)
	}
	if err := s.Store.ReplaceSnapshot(ctx, sessionID, gitx.HeadSHA(ctx, se.WorkDir), d); err != nil {
		return se, err
	}
	return s.Store.GetSession(ctx, sessionID)
}

// DefaultName derives a readable session name from git arguments,
// e.g. ["diff", "dev", "feat/x", "--", "*.go"] -> "diff dev feat/x".
func DefaultName(args []string) string {
	parts := []string{}
	for _, a := range args {
		if a == "--" {
			break
		}
		if strings.HasPrefix(a, "-") {
			continue
		}
		parts = append(parts, a)
	}
	name := strings.Join(parts, " ")
	if r := []rune(name); len(r) > 80 {
		name = string(r[:80])
	}
	if name == "" {
		name = "review"
	}
	return name
}

// CommitList is the list of commits covered by a session.
type CommitList struct {
	Range   string        `json:"range"`
	Commits []gitx.Commit `json:"commits"`
}

// Commits lists the commits covered by the git command of a session, optionally
// restricted to the commits touching path. The repository is queried live.
func (s *Service) Commits(ctx context.Context, sessionID int64, path string) (CommitList, error) {
	se, err := s.Store.GetSession(ctx, sessionID)
	if err != nil {
		return CommitList{}, err
	}
	revs, desc, err := gitx.CommitRange(ctx, se.WorkDir, se.GitArgs)
	if err != nil {
		return CommitList{}, err
	}
	commits, err := gitx.Log(ctx, se.WorkDir, revs, path)
	if err != nil {
		return CommitList{}, err
	}
	return CommitList{Range: desc, Commits: commits}, nil
}
