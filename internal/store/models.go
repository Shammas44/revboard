package store

import (
	"encoding/json"
	"time"

	"revboard/internal/diff"
)

// Status is the review status of a file or a hunk.
type Status string

const (
	StatusUnreviewed     Status = "unreviewed"
	StatusReviewed       Status = "reviewed"
	StatusNeedsAttention Status = "needs_attention"
)

// Valid reports whether s is a known status.
func (s Status) Valid() bool {
	switch s {
	case StatusUnreviewed, StatusReviewed, StatusNeedsAttention:
		return true
	}
	return false
}

// Session states.
const (
	SessionActive   = "active"
	SessionArchived = "archived"
)

// Project is a git repository known by GRB.
type Project struct {
	ID             int64     `json:"id"`
	Name           string    `json:"name"`
	RepoPath       string    `json:"repoPath"`
	CreatedAt      time.Time `json:"createdAt"`
	LastUsedAt     time.Time `json:"lastUsedAt"`
	ActiveSessions int       `json:"activeSessions"`
	IsCurrent      bool      `json:"isCurrent"`
}

// Progress aggregates review statuses of a session.
type Progress struct {
	TotalFiles     int `json:"totalFiles"`
	ReviewedFiles  int `json:"reviewedFiles"`
	AttentionFiles int `json:"attentionFiles"`
	TotalHunks     int `json:"totalHunks"`
	ReviewedHunks  int `json:"reviewedHunks"`
	AttentionHunks int `json:"attentionHunks"`
	// ExcludedFiles counts files hidden by the exclusion rules (not part of the totals above).
	ExcludedFiles int `json:"excludedFiles"`
}

// Session is a captured diff with its metadata.
type Session struct {
	ID           int64     `json:"id"`
	ProjectID    int64     `json:"projectId"`
	ProjectName  string    `json:"projectName"`
	RepoPath     string    `json:"repoPath"`
	Name         string    `json:"name"`
	GitArgs      []string  `json:"gitArgs"`
	WorkDir      string    `json:"workDir"`
	HeadSHA      string    `json:"headSha"`
	State        string    `json:"state"`
	Exclusions   []string  `json:"exclusions"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
	LastOpenedAt time.Time `json:"lastOpenedAt"`
	CapturedAt   time.Time `json:"capturedAt"`
	Progress     Progress  `json:"progress"`
}

// HunkRef is the light representation of a hunk used in file lists.
type HunkRef struct {
	ID     int64  `json:"id"`
	Status Status `json:"status"`
}

// FileSummary describes a file of a session without its hunk bodies.
type FileSummary struct {
	ID           int64     `json:"id"`
	SessionID    int64     `json:"sessionId"`
	Position     int       `json:"position"`
	Path         string    `json:"path"`
	OldPath      string    `json:"oldPath"`
	NewPath      string    `json:"newPath"`
	ChangeType   string    `json:"changeType"`
	IsBinary     bool      `json:"isBinary"`
	OldMode      string    `json:"oldMode"`
	NewMode      string    `json:"newMode"`
	Similarity   int       `json:"similarity"`
	Additions    int       `json:"additions"`
	Deletions    int       `json:"deletions"`
	Status       Status    `json:"status"`
	Pinned       bool      `json:"pinned"`
	Excluded     bool      `json:"excluded"`
	Hunks        []HunkRef `json:"hunks"`
	CommentCount int       `json:"commentCount"`
}

// SessionDetail is a session with its file list and UI state.
type SessionDetail struct {
	Session Session         `json:"session"`
	Files   []FileSummary   `json:"files"`
	UIState json.RawMessage `json:"uiState"`
}

// Hunk is a hunk with its parsed lines.
type Hunk struct {
	ID       int64       `json:"id"`
	Position int         `json:"position"`
	OldStart int         `json:"oldStart"`
	OldLines int         `json:"oldLines"`
	NewStart int         `json:"newStart"`
	NewLines int         `json:"newLines"`
	Section  string      `json:"section"`
	Status   Status      `json:"status"`
	Lines    []diff.Line `json:"lines"`
}

// FileDetail is a file with its hunks and notes.
type FileDetail struct {
	File     FileSummary `json:"file"`
	Hunks    []Hunk      `json:"hunks"`
	Comments []Comment   `json:"comments"`
}

// Comment is a private note on a session, a file or a line.
type Comment struct {
	ID        int64 `json:"id"`
	SessionID int64 `json:"sessionId"`
	// Number identifies the note within its session (#1, #2, ...); it is never reused.
	Number   int    `json:"number"`
	FilePath string `json:"filePath"`
	Side     string `json:"side"`
	Line     int    `json:"line"`
	Body     string `json:"body"`
	// Export marks the note for the Markdown export (default true).
	Export    bool      `json:"export"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// StatusChange is returned by every status mutation.
type StatusChange struct {
	Files    []FileSummary `json:"files"`
	Progress Progress      `json:"progress"`
	// DeletedNoteIDs lists the notes deleted because their item was marked reviewed.
	DeletedNoteIDs []int64 `json:"deletedNoteIds"`
}

// NoteCreated is returned when a note is added: the note and, when its hunk or
// file was flagged "needs attention", the resulting status change.
type NoteCreated struct {
	Comment Comment       `json:"comment"`
	Change  *StatusChange `json:"change"`
}

// NewSession holds the metadata of a session to create.
type NewSession struct {
	ProjectID int64
	Name      string
	GitArgs   []string
	WorkDir   string
	HeadSHA   string
}

// SessionPatch lists the session fields to update (nil = unchanged).
type SessionPatch struct {
	Name       *string
	State      *string
	Exclusions *[]string
	UIState    json.RawMessage
}
