package store

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"revboard/internal/diff"
)

const sampleDiff = `diff --git a/a.go b/a.go
--- a/a.go
+++ b/a.go
@@ -1,2 +1,2 @@
-old1
+new1
 ctx
@@ -10,2 +10,2 @@
-old2
+new2
 ctx
diff --git a/img.png b/img.png
Binary files a/img.png and b/img.png differ
`

func openTest(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "grb.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func parse(t *testing.T, s string) *diff.Diff {
	t.Helper()
	d, err := diff.Parse(strings.NewReader(s))
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func newSession(t *testing.T, s *Store, d string) (Project, SessionDetail) {
	t.Helper()
	ctx := context.Background()
	p, err := s.EnsureProject(ctx, "/repos/app")
	if err != nil {
		t.Fatal(err)
	}
	id, err := s.CreateSession(ctx, NewSession{ProjectID: p.ID, Name: "review", GitArgs: []string{"diff", "main"}, WorkDir: "/repos/app"}, parse(t, d))
	if err != nil {
		t.Fatal(err)
	}
	detail, err := s.GetSessionDetail(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	return p, detail
}

func TestProjects(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	p1, err := s.EnsureProject(ctx, "/a/app")
	if err != nil {
		t.Fatal(err)
	}
	p2, _ := s.EnsureProject(ctx, "/b/app")
	again, _ := s.EnsureProject(ctx, "/a/app")
	if p1.Name != "app" || p2.Name != "app-2" || again.ID != p1.ID {
		t.Fatalf("unexpected names/ids: %+v %+v %+v", p1, p2, again)
	}
	if err := s.RenameProject(ctx, p2.ID, "app"); !errors.Is(err, ErrConflict) {
		t.Fatalf("rename to existing name: err = %v", err)
	}
	if err := s.SetCurrentProject(ctx, p2.ID); err != nil {
		t.Fatal(err)
	}
	cur, err := s.CurrentProject(ctx)
	if err != nil || cur.ID != p2.ID || !cur.IsCurrent {
		t.Fatalf("current project = %+v, %v", cur, err)
	}
	found, err := s.FindProject(ctx, "app-2")
	if err != nil || found.ID != p2.ID {
		t.Fatalf("FindProject by name: %+v %v", found, err)
	}
	found, err = s.FindProject(ctx, "1")
	if err != nil || found.ID != p1.ID {
		t.Fatalf("FindProject by id: %+v %v", found, err)
	}
	in, err := s.ProjectContaining(ctx, "/a/app/src/pkg")
	if err != nil || in.ID != p1.ID {
		t.Fatalf("ProjectContaining: %+v %v", in, err)
	}
	if _, err := s.ProjectContaining(ctx, "/a/application"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("ProjectContaining prefix match should fail: %v", err)
	}
	if err := s.DeleteProject(ctx, p2.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CurrentProject(ctx); !errors.Is(err, ErrNotFound) {
		t.Fatalf("current project should be cleared, got %v", err)
	}
}

func TestStatusRules(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	_, d := newSession(t, s, sampleDiff)
	if len(d.Files) != 2 || len(d.Files[0].Hunks) != 2 || d.Session.Progress.TotalHunks != 2 {
		t.Fatalf("unexpected detail: %+v", d)
	}
	code, bin := d.Files[0], d.Files[1]

	sc, err := s.SetHunkStatus(ctx, code.Hunks[0].ID, StatusReviewed, StatusOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if sc.Files[0].Status != StatusUnreviewed || sc.Progress.ReviewedHunks != 1 {
		t.Fatalf("after one hunk: %+v", sc)
	}
	sc, _ = s.SetHunkStatus(ctx, code.Hunks[1].ID, StatusReviewed, StatusOptions{})
	if sc.Files[0].Status != StatusReviewed || sc.Progress.ReviewedFiles != 1 {
		t.Fatalf("after all hunks: %+v", sc)
	}
	sc, _ = s.SetHunkStatus(ctx, code.Hunks[1].ID, StatusNeedsAttention, StatusOptions{})
	if sc.Files[0].Status != StatusNeedsAttention || sc.Progress.AttentionHunks != 1 {
		t.Fatalf("attention: %+v", sc)
	}
	sc, _ = s.SetFileStatus(ctx, code.ID, StatusReviewed, StatusOptions{})
	if sc.Files[0].Hunks[0].Status != StatusReviewed || sc.Files[0].Hunks[1].Status != StatusReviewed {
		t.Fatalf("file status should cascade: %+v", sc)
	}
	sc, _ = s.SetFilesStatus(ctx, d.Session.ID, []int64{bin.ID}, StatusReviewed, StatusOptions{})
	if sc.Progress.ReviewedFiles != 2 {
		t.Fatalf("bulk: %+v", sc.Progress)
	}
	if _, err := s.SetFilesStatus(ctx, d.Session.ID+1, []int64{bin.ID}, StatusReviewed, StatusOptions{}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("bulk on foreign session: %v", err)
	}
	if _, err := s.SetFileStatus(ctx, code.ID, "bogus", StatusOptions{}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid status: %v", err)
	}
	fs, _ := s.SetPinned(ctx, code.ID, true)
	if !fs.Pinned {
		t.Fatal("pin not stored")
	}
}

func TestDetailCommentsAndUIState(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	_, d := newSession(t, s, sampleDiff)
	if _, err := s.CreateComment(ctx, Comment{SessionID: d.Session.ID, FilePath: "a.go", Side: "new", Line: 1, Body: "why?"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateComment(ctx, Comment{SessionID: d.Session.ID, Body: "overall ok"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateComment(ctx, Comment{SessionID: d.Session.ID, FilePath: "a.go", Line: 3, Body: "x"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("line note without side: %v", err)
	}
	fd, err := s.GetFileDetail(ctx, d.Files[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(fd.Hunks) != 2 || len(fd.Hunks[0].Lines) != 3 || len(fd.Comments) != 1 || fd.File.CommentCount != 1 {
		t.Fatalf("unexpected file detail: %+v", fd)
	}
	edited := "edited"
	c, err := s.UpdateComment(ctx, fd.Comments[0].ID, CommentPatch{Body: &edited})
	if err != nil || c.Body != "edited" {
		t.Fatalf("update comment: %+v %v", c, err)
	}
	all, _ := s.ListComments(ctx, d.Session.ID)
	if len(all) != 2 {
		t.Fatalf("comments = %d", len(all))
	}
	if err := s.DeleteComment(ctx, c.ID); err != nil {
		t.Fatal(err)
	}

	ui := json.RawMessage(`{"selectedFileId":3}`)
	if _, err := s.UpdateSession(ctx, d.Session.ID, SessionPatch{UIState: ui}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdateSession(ctx, d.Session.ID, SessionPatch{UIState: json.RawMessage(`{`)}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("invalid json: %v", err)
	}
	d2, _ := s.GetSessionDetail(ctx, d.Session.ID)
	if string(d2.UIState) != string(ui) {
		t.Fatalf("ui state = %s", d2.UIState)
	}
	archived := SessionArchived
	if _, err := s.UpdateSession(ctx, d.Session.ID, SessionPatch{State: &archived}); err != nil {
		t.Fatal(err)
	}
	if list, _ := s.ListSessions(ctx, 0, false); len(list) != 0 {
		t.Fatalf("archived session listed: %+v", list)
	}
	if list, _ := s.ListSessions(ctx, 0, true); len(list) != 1 {
		t.Fatalf("archived session missing: %+v", list)
	}
}

func TestReplaceSnapshotCarryOver(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	_, d := newSession(t, s, sampleDiff)
	code, bin := d.Files[0], d.Files[1]
	s.SetHunkStatus(ctx, code.Hunks[0].ID, StatusReviewed, StatusOptions{})
	s.SetHunkStatus(ctx, code.Hunks[1].ID, StatusReviewed, StatusOptions{})
	s.SetFileStatus(ctx, bin.ID, StatusNeedsAttention, StatusOptions{})
	s.SetPinned(ctx, code.ID, true)

	// Second hunk of a.go changed, a new file appeared.
	next := strings.Replace(sampleDiff, "+new2", "+new2 changed", 1) + `diff --git a/b.go b/b.go
new file mode 100644
--- /dev/null
+++ b/b.go
@@ -0,0 +1 @@
+b
`
	if err := s.ReplaceSnapshot(ctx, d.Session.ID, "abc", parse(t, next)); err != nil {
		t.Fatal(err)
	}
	d2, _ := s.GetSessionDetail(ctx, d.Session.ID)
	if len(d2.Files) != 3 || d2.Session.HeadSHA != "abc" {
		t.Fatalf("unexpected snapshot: %+v", d2)
	}
	a := d2.Files[0]
	if a.Hunks[0].Status != StatusReviewed || a.Hunks[1].Status != StatusUnreviewed || a.Status != StatusUnreviewed || !a.Pinned {
		t.Fatalf("a.go carry-over wrong: %+v", a)
	}
	if d2.Files[1].Status != StatusNeedsAttention {
		t.Fatalf("binary file status not carried: %+v", d2.Files[1])
	}
	if d2.Files[2].Status != StatusUnreviewed {
		t.Fatalf("new file should be unreviewed")
	}
}

func TestListNotesWithContext(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	_, d := newSession(t, s, sampleDiff)
	sid := d.Session.ID
	add := func(path, side string, line int, body string) {
		t.Helper()
		if _, err := s.CreateComment(ctx, Comment{SessionID: sid, FilePath: path, Side: side, Line: line, Body: body}); err != nil {
			t.Fatal(err)
		}
	}
	add("a.go", "new", 11, "second hunk") // context line "ctx" of hunk 2
	add("a.go", "old", 1, "deleted line")
	add("a.go", "new", 99, "gone")
	add("a.go", "", 0, "file note")
	add("", "", 0, "session note")

	notes, err := s.ListNotesWithContext(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	var order []string
	for _, n := range notes {
		order = append(order, n.Body)
	}
	want := []string{"session note", "file note", "deleted line", "second hunk", "gone"}
	if strings.Join(order, "|") != strings.Join(want, "|") {
		t.Fatalf("order = %q, want %q", order, want)
	}
	del := notes[2]
	if del.Outdated || del.HunkID != d.Files[0].Hunks[0].ID || len(del.Excerpt) != 3 || !del.Excerpt[0].Focus || del.Excerpt[0].Content != "old1" || del.Excerpt[1].Focus {
		t.Fatalf("deleted line note: %+v", del)
	}
	ctxNote := notes[3]
	focus := -1
	for i, l := range ctxNote.Excerpt {
		if l.Focus {
			focus = i
		}
	}
	if ctxNote.HunkHeader != "@@ -10,2 +10,2 @@" || focus < 0 || ctxNote.Excerpt[focus].NewLine != 11 {
		t.Fatalf("context line note: %+v", ctxNote)
	}
	if !notes[4].Outdated || len(notes[4].Excerpt) != 0 || notes[4].FileID == 0 {
		t.Fatalf("outdated note: %+v", notes[4])
	}
	if notes[0].FileID != 0 || notes[1].FileID != d.Files[0].ID {
		t.Fatalf("file ids: %+v %+v", notes[0], notes[1])
	}
}

func TestNotesDriveStatuses(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	_, d := newSession(t, s, sampleDiff)
	sid := d.Session.ID
	code, bin := d.Files[0], d.Files[1]

	// A line note flags its hunk and therefore its file.
	created, err := s.CreateComment(ctx, Comment{SessionID: sid, FilePath: "a.go", Side: "new", Line: 10, Body: "rename"})
	if err != nil {
		t.Fatal(err)
	}
	if created.Change == nil || created.Change.Files[0].Status != StatusNeedsAttention ||
		created.Change.Files[0].Hunks[1].Status != StatusNeedsAttention || created.Change.Files[0].Hunks[0].Status != StatusUnreviewed {
		t.Fatalf("line note should flag hunk 2 only: %+v", created.Change)
	}
	if !created.Comment.Export {
		t.Fatal("notes are exported by default")
	}

	// Reviewing the hunk with a note requires confirming the deletion.
	_, err = s.SetHunkStatus(ctx, code.Hunks[1].ID, StatusReviewed, StatusOptions{})
	var notesErr *NotesError
	if !errors.As(err, &notesErr) || notesErr.Count != 1 || !errors.Is(err, ErrConflict) {
		t.Fatalf("expected NotesError, got %v", err)
	}
	sc, err := s.SetHunkStatus(ctx, code.Hunks[1].ID, StatusReviewed, StatusOptions{DeleteNotes: true})
	if err != nil || len(sc.DeletedNoteIDs) != 1 || sc.DeletedNoteIDs[0] != created.Comment.ID {
		t.Fatalf("deleting notes: %+v %v", sc, err)
	}
	if all, _ := s.ListComments(ctx, sid); len(all) != 0 {
		t.Fatalf("note should be deleted: %+v", all)
	}
	// Other hunks are not affected by the note guard.
	if _, err := s.SetHunkStatus(ctx, code.Hunks[0].ID, StatusReviewed, StatusOptions{}); err != nil {
		t.Fatal(err)
	}

	// A file note flags the file; it stays flagged while the note exists.
	created, _ = s.CreateComment(ctx, Comment{SessionID: sid, FilePath: "a.go", Body: "split this file"})
	if created.Change.Files[0].Status != StatusNeedsAttention || created.Change.Files[0].Hunks[0].Status != StatusReviewed {
		t.Fatalf("file note should flag the file only: %+v", created.Change.Files[0])
	}
	sc, _ = s.SetHunkStatus(ctx, code.Hunks[0].ID, StatusReviewed, StatusOptions{})
	if sc.Files[0].Status != StatusNeedsAttention {
		t.Fatalf("file with a file note must stay flagged: %+v", sc.Files[0])
	}
	// Deleting a note does not clear the status.
	if err := s.DeleteComment(ctx, created.Comment.ID); err != nil {
		t.Fatal(err)
	}
	fd, _ := s.GetFileDetail(ctx, code.ID)
	if fd.File.Status != StatusNeedsAttention {
		t.Fatalf("deleting a note must keep the status: %s", fd.File.Status)
	}

	// Reviewing a file deletes all its notes (with confirmation); bulk as well.
	s.CreateComment(ctx, Comment{SessionID: sid, FilePath: "img.png", Body: "binary?"})
	if _, err := s.SetFilesStatus(ctx, sid, []int64{code.ID, bin.ID}, StatusReviewed, StatusOptions{}); !errors.As(err, &notesErr) {
		t.Fatalf("bulk review must be confirmed: %v", err)
	}
	sc, err = s.SetFilesStatus(ctx, sid, []int64{code.ID, bin.ID}, StatusReviewed, StatusOptions{DeleteNotes: true})
	if err != nil || len(sc.DeletedNoteIDs) != 1 || sc.Progress.ReviewedFiles != 2 {
		t.Fatalf("bulk review: %+v %v", sc, err)
	}

	// Session notes never change statuses.
	created, _ = s.CreateComment(ctx, Comment{SessionID: sid, Body: "general"})
	if created.Change != nil {
		t.Fatalf("session note changed statuses: %+v", created.Change)
	}
	off := false
	c, err := s.UpdateComment(ctx, created.Comment.ID, CommentPatch{Export: &off})
	if err != nil || c.Export {
		t.Fatalf("export flag: %+v %v", c, err)
	}
	if err := s.SetCommentsExport(ctx, sid, true); err != nil {
		t.Fatal(err)
	}
	if c, _ := s.getComment(ctx, created.Comment.ID); !c.Export {
		t.Fatal("SetCommentsExport should mark every note")
	}
}

func TestNoteNumbers(t *testing.T) {
	s := openTest(t)
	ctx := context.Background()
	_, d := newSession(t, s, sampleDiff)
	add := func(body string) Comment {
		t.Helper()
		created, err := s.CreateComment(ctx, Comment{SessionID: d.Session.ID, Body: body})
		if err != nil {
			t.Fatal(err)
		}
		return created.Comment
	}
	a, b := add("a"), add("b")
	if a.Number != 1 || b.Number != 2 {
		t.Fatalf("numbers = %d, %d", a.Number, b.Number)
	}
	// Numbers are never reused, even after deleting the last note.
	if err := s.DeleteComment(ctx, b.ID); err != nil {
		t.Fatal(err)
	}
	if c := add("c"); c.Number != 3 {
		t.Fatalf("number after deletion = %d, want 3", c.Number)
	}
	// Numbering is per session.
	id, err := s.CreateSession(ctx, NewSession{ProjectID: d.Session.ProjectID, Name: "other", GitArgs: []string{"diff"}, WorkDir: "/x"}, parse(t, sampleDiff))
	if err != nil {
		t.Fatal(err)
	}
	created, _ := s.CreateComment(ctx, Comment{SessionID: id, Body: "first"})
	if created.Comment.Number != 1 {
		t.Fatalf("other session starts at %d", created.Comment.Number)
	}
}
