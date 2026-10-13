package store

import (
	"context"
	"fmt"
	"sort"

	"revboard/internal/diff"
)

// ExcerptLine is a line of the hunk of a note; Focus marks the noted line.
type ExcerptLine struct {
	diff.Line
	Focus bool `json:"focus"`
}

// NoteContext is a note with the code it refers to.
type NoteContext struct {
	Comment
	// FileID is the id of the file in the current snapshot (0 for session notes
	// or when the path is no longer part of the diff).
	FileID     int64  `json:"fileId"`
	FileStatus Status `json:"fileStatus,omitempty"`
	ChangeType string `json:"changeType,omitempty"`
	OldPath    string `json:"oldPath,omitempty"`
	Excluded   bool   `json:"excluded"`
	// HunkID, HunkHeader and HunkSection identify the hunk containing a line note;
	// Excerpt holds all of its lines.
	HunkID      int64         `json:"hunkId"`
	HunkHeader  string        `json:"hunkHeader"`
	HunkSection string        `json:"hunkSection"`
	Excerpt     []ExcerptLine `json:"excerpt"`
	// Outdated is set for line notes whose line is not part of the diff anymore.
	Outdated bool `json:"outdated"`
}

type noteHunk struct {
	id      int64
	header  string
	section string
	lines   []diff.Line
}

// ListNotesWithContext returns all notes of a session: session notes first, then
// file and line notes in diff order, each line note with an excerpt of its hunk.
func (s *Store) ListNotesWithContext(ctx context.Context, sessionID int64) ([]NoteContext, error) {
	if _, err := s.GetSession(ctx, sessionID); err != nil {
		return nil, err
	}
	comments, err := listComments(ctx, s.db, sessionID, nil)
	if err != nil {
		return nil, err
	}
	files, err := fileSummaries(ctx, s.db, sessionID, 0)
	if err != nil {
		return nil, err
	}
	byPath := make(map[string]FileSummary, len(files))
	for _, f := range files {
		byPath[f.Path] = f
	}

	// Load the hunks of the files having line notes.
	hunks := map[int64][]noteHunk{}
	for _, c := range comments {
		f, ok := byPath[c.FilePath]
		if !ok || c.Line == 0 {
			continue
		}
		if _, done := hunks[f.ID]; done {
			continue
		}
		if hunks[f.ID], err = loadNoteHunks(ctx, s.db, f.ID); err != nil {
			return nil, err
		}
	}

	notes := make([]NoteContext, 0, len(comments))
	for _, c := range comments {
		n := NoteContext{Comment: c, Excerpt: []ExcerptLine{}}
		if f, ok := byPath[c.FilePath]; ok {
			n.FileID, n.FileStatus, n.Excluded = f.ID, f.Status, f.Excluded
			n.ChangeType, n.OldPath = f.ChangeType, f.OldPath
		}
		if c.Line > 0 {
			n.Outdated = true
			for _, h := range hunks[n.FileID] {
				if i := findLine(h.lines, c.Side, c.Line); i >= 0 {
					n.Outdated = false
					n.HunkID, n.HunkHeader, n.HunkSection = h.id, h.header, h.section
					for j, l := range h.lines {
						n.Excerpt = append(n.Excerpt, ExcerptLine{Line: l, Focus: j == i})
					}
					break
				}
			}
		}
		notes = append(notes, n)
	}

	position := func(n NoteContext) int {
		if n.FilePath == "" {
			return -1
		}
		if f, ok := byPath[n.FilePath]; ok {
			return f.Position
		}
		return len(files) // files no longer in the diff come last
	}
	sort.SliceStable(notes, func(i, j int) bool {
		pi, pj := position(notes[i]), position(notes[j])
		if pi != pj {
			return pi < pj
		}
		if notes[i].FilePath != notes[j].FilePath {
			return notes[i].FilePath < notes[j].FilePath
		}
		if notes[i].Line != notes[j].Line {
			return notes[i].Line < notes[j].Line
		}
		return notes[i].ID < notes[j].ID
	})
	return notes, nil
}

func loadNoteHunks(ctx context.Context, q dbtx, fileID int64) ([]noteHunk, error) {
	rows, err := q.QueryContext(ctx, `SELECT id, old_start, old_lines, new_start, new_lines, section, body
		FROM review_hunks WHERE file_id = ? ORDER BY position`, fileID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []noteHunk
	for rows.Next() {
		var h noteHunk
		var oldStart, oldLines, newStart, newLines int
		var body string
		if err := rows.Scan(&h.id, &oldStart, &oldLines, &newStart, &newLines, &h.section, &body); err != nil {
			return nil, err
		}
		h.header = fmt.Sprintf("@@ -%d,%d +%d,%d @@", oldStart, oldLines, newStart, newLines)
		h.lines = diff.ParseHunkBody(oldStart, newStart, body)
		out = append(out, h)
	}
	return out, rows.Err()
}

// findLine returns the index of the line a note is anchored to, or -1.
// Notes on the new side match added and context lines, notes on the old side
// match deleted lines (and context lines, for notes created on the old side).
func findLine(lines []diff.Line, side string, number int) int {
	for i, l := range lines {
		switch {
		case side == "new" && l.Kind != diff.LineDelete && l.NewLine == number:
			return i
		case side == "old" && l.Kind != diff.LineAdd && l.OldLine == number:
			return i
		}
	}
	return -1
}
