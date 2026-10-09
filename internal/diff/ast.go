// Package diff parses git unified diffs into an AST.
package diff

import "strings"

// ChangeType describes what happened to a file in the diff.
type ChangeType string

const (
	ChangeAdded    ChangeType = "added"
	ChangeDeleted  ChangeType = "deleted"
	ChangeModified ChangeType = "modified"
	ChangeRenamed  ChangeType = "renamed"
	ChangeCopied   ChangeType = "copied"
)

// LineKind is the role of a line inside a hunk.
type LineKind string

const (
	LineContext LineKind = "context"
	LineAdd     LineKind = "add"
	LineDelete  LineKind = "delete"
)

// Diff is the root of the AST produced by Parse.
type Diff struct {
	Files []*File
}

// File is one "diff --git" section.
type File struct {
	OldPath    string // "" when the file is added
	NewPath    string // "" when the file is deleted
	ChangeType ChangeType
	IsBinary   bool
	OldMode    string
	NewMode    string
	Similarity int
	Hunks      []*Hunk
	Additions  int
	Deletions  int
}

// Path returns the path used to display and identify the file.
func (f *File) Path() string {
	if f.NewPath != "" {
		return f.NewPath
	}
	return f.OldPath
}

// Hunk is one "@@ -a,b +c,d @@ section" block.
type Hunk struct {
	OldStart, OldLines int
	NewStart, NewLines int
	Section            string
	Lines              []Line
}

// Line is a single diff line with its resolved line numbers (0 = not present on that side).
type Line struct {
	Kind      LineKind `json:"kind"`
	Content   string   `json:"content"`
	OldLine   int      `json:"oldLine"`
	NewLine   int      `json:"newLine"`
	NoNewline bool     `json:"noNewline"`
}

// noNewlineMarker is emitted by git after a line lacking a trailing newline.
const noNewlineMarker = `\ No newline at end of file`

// Body returns the canonical raw text of the hunk (prefixed lines, no header).
// ParseHunkBody(h.OldStart, h.NewStart, h.Body()) reproduces h.Lines.
func (h *Hunk) Body() string {
	var b strings.Builder
	for i, l := range h.Lines {
		if i > 0 {
			b.WriteByte('\n')
		}
		switch l.Kind {
		case LineAdd:
			b.WriteByte('+')
		case LineDelete:
			b.WriteByte('-')
		default:
			b.WriteByte(' ')
		}
		b.WriteString(l.Content)
		if l.NoNewline {
			b.WriteByte('\n')
			b.WriteString(noNewlineMarker)
		}
	}
	return b.String()
}

// ParseHunkBody parses a stored hunk body back into lines with resolved line numbers.
func ParseHunkBody(oldStart, newStart int, body string) []Line {
	if body == "" {
		return nil
	}
	raw := strings.Split(body, "\n")
	lines := make([]Line, 0, len(raw))
	oldNo, newNo := oldStart, newStart
	for _, r := range raw {
		if strings.HasPrefix(r, `\`) {
			if n := len(lines); n > 0 {
				lines[n-1].NoNewline = true
			}
			continue
		}
		var l Line
		switch {
		case strings.HasPrefix(r, "+"):
			l = Line{Kind: LineAdd, Content: r[1:], NewLine: newNo}
			newNo++
		case strings.HasPrefix(r, "-"):
			l = Line{Kind: LineDelete, Content: r[1:], OldLine: oldNo}
			oldNo++
		default:
			content := r
			if content != "" {
				content = content[1:]
			}
			l = Line{Kind: LineContext, Content: content, OldLine: oldNo, NewLine: newNo}
			oldNo++
			newNo++
		}
		lines = append(lines, l)
	}
	return lines
}
