package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"

	"revboard/internal/review"
	"revboard/internal/store"
)

// fixture creates a git repository with a feature branch and a review session on it.
type fixture struct {
	t       *testing.T
	repo    string
	store   *store.Store
	handler http.Handler
	session store.Session
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	repo := t.TempDir()
	git(t, repo, "init", "-q", "-b", "dev")
	writeFile(t, repo, "src/app.go", "package app\n\nfunc A() {}\n")
	writeFile(t, repo, "README.md", "# readme\n")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-q", "-m", "init")
	git(t, repo, "checkout", "-q", "-b", "feat/x")
	writeFile(t, repo, "src/app.go", "package app\n\nfunc A() { B() }\n\nfunc B() {}\n")
	writeFile(t, repo, "src/new.go", "package app\n")
	writeFile(t, repo, "README.md", "# readme changed\n")
	git(t, repo, "add", ".")
	git(t, repo, "commit", "-q", "-m", "feature")

	st, err := store.Open(filepath.Join(t.TempDir(), "grb.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	svc := &review.Service{Store: st}
	res, err := svc.Start(context.Background(), repo, []string{"diff", "dev", "feat/x", "--", ":(exclude)*.md"}, "")
	if err != nil {
		t.Fatal(err)
	}
	static := fstest.MapFS{"index.html": {Data: []byte("<html>spa</html>")}, "main-ABCDEFGH.js": {Data: []byte("js")}}
	return &fixture{t: t, repo: repo, store: st, handler: New(st, "test", static).Handler(), session: res.Session}
}

func (f *fixture) do(method, path string, body any, out any) *httptest.ResponseRecorder {
	f.t.Helper()
	var buf bytes.Buffer
	if body != nil {
		json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, "http://127.0.0.1:7777"+path, &buf)
	req.Header.Set(ClientHeader, "web")
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, req)
	if out != nil {
		if rec.Code >= 300 {
			f.t.Fatalf("%s %s: status %d: %s", method, path, rec.Code, rec.Body)
		}
		if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
			f.t.Fatalf("%s %s: decode: %v (%s)", method, path, err, rec.Body)
		}
	}
	return rec
}

func TestEndToEnd(t *testing.T) {
	f := newFixture(t)
	if f.session.Name != "diff dev feat/x" {
		t.Fatalf("default session name = %q", f.session.Name)
	}

	var detail store.SessionDetail
	f.do("GET", "/api/sessions/"+itoa(f.session.ID), nil, &detail)
	if len(detail.Files) != 2 {
		t.Fatalf("expected README.md to be excluded, got files %+v", detail.Files)
	}
	app := detail.Files[0]
	if app.Path != "src/app.go" || len(app.Hunks) == 0 {
		t.Fatalf("unexpected first file: %+v", app)
	}

	var fd store.FileDetail
	f.do("GET", "/api/files/"+itoa(app.ID), nil, &fd)
	if len(fd.Hunks) == 0 || len(fd.Hunks[0].Lines) == 0 {
		t.Fatalf("file detail without lines: %+v", fd)
	}

	var sc store.StatusChange
	f.do("PUT", "/api/hunks/"+itoa(fd.Hunks[0].ID)+"/status", map[string]string{"status": "needs_attention"}, &sc)
	if sc.Files[0].Status != store.StatusNeedsAttention || sc.Progress.AttentionFiles != 1 {
		t.Fatalf("hunk status change: %+v", sc)
	}
	f.do("POST", "/api/sessions/"+itoa(f.session.ID)+"/status",
		map[string]any{"fileIds": []int64{detail.Files[1].ID}, "status": "reviewed"}, &sc)
	if sc.Progress.ReviewedFiles != 1 {
		t.Fatalf("bulk status: %+v", sc.Progress)
	}

	var c store.Comment
	f.do("POST", "/api/sessions/"+itoa(f.session.ID)+"/comments",
		map[string]any{"filePath": "src/app.go", "side": "new", "line": 3, "body": "check B"}, &c)
	f.do("PATCH", "/api/comments/"+itoa(c.ID), map[string]string{"body": "check B again"}, &c)
	if c.Body != "check B again" {
		t.Fatalf("comment not updated: %+v", c)
	}
	if rec := f.do("POST", "/api/sessions/"+itoa(f.session.ID)+"/comments", map[string]any{"body": " "}, nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("empty comment: status %d", rec.Code)
	}

	var se store.Session
	f.do("PATCH", "/api/sessions/"+itoa(f.session.ID), map[string]any{"uiState": map[string]any{"selectedFileId": app.ID}}, &se)

	// New commit on the branch, then refresh: the untouched file keeps its status.
	writeFile(t, f.repo, "src/other.go", "package app\n")
	git(t, f.repo, "add", ".")
	git(t, f.repo, "commit", "-q", "-m", "more")
	f.do("POST", "/api/sessions/"+itoa(f.session.ID)+"/refresh", nil, &detail)
	if len(detail.Files) != 3 {
		t.Fatalf("refresh: files = %d", len(detail.Files))
	}
	byPath := map[string]store.FileSummary{}
	for _, fs := range detail.Files {
		byPath[fs.Path] = fs
	}
	if byPath["src/app.go"].Status != store.StatusNeedsAttention || byPath["src/new.go"].Status != store.StatusReviewed {
		t.Fatalf("refresh lost statuses: %+v", byPath)
	}
	if byPath["src/app.go"].CommentCount != 1 {
		t.Fatalf("refresh lost comments: %+v", byPath["src/app.go"])
	}
	if !strings.Contains(string(detail.UIState), "selectedFileId") {
		t.Fatalf("ui state lost: %s", detail.UIState)
	}

	var projects []store.Project
	f.do("GET", "/api/projects", nil, &projects)
	if len(projects) != 1 || !projects[0].IsCurrent || projects[0].ActiveSessions != 1 {
		t.Fatalf("projects: %+v", projects)
	}
	if rec := f.do("DELETE", "/api/sessions/"+itoa(f.session.ID), nil, nil); rec.Code != http.StatusNoContent {
		t.Fatalf("delete: %d", rec.Code)
	}
	if rec := f.do("GET", "/api/sessions/"+itoa(f.session.ID), nil, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("deleted session: %d", rec.Code)
	}
}

func TestGuardsAndSPA(t *testing.T) {
	f := newFixture(t)
	req := httptest.NewRequest("GET", "http://127.0.0.1:7777/api/projects", nil)
	rec := httptest.NewRecorder()
	f.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("missing header: status %d", rec.Code)
	}

	req = httptest.NewRequest("GET", "http://evil.example.com:7777/api/projects", nil)
	req.Header.Set(ClientHeader, "web")
	rec = httptest.NewRecorder()
	f.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("foreign host: status %d", rec.Code)
	}

	for path, want := range map[string]string{"/": "spa", "/sessions/1": "spa", "/main-ABCDEFGH.js": "js"} {
		rec := f.do("GET", path, nil, nil)
		if rec.Code != http.StatusOK || rec.Body.String() != "<html>spa</html>" && want == "spa" || want == "js" && rec.Body.String() != "js" {
			t.Fatalf("GET %s: %d %q", path, rec.Code, rec.Body)
		}
	}
	if rec := f.do("GET", "/missing.js", nil, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("missing asset: %d", rec.Code)
	}
	if rec := f.do("GET", "/api/nope", nil, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown api: %d", rec.Code)
	}
	if rec := f.do("PUT", "/api/files/1/status", map[string]string{"status": "bogus"}, nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid status: %d", rec.Code)
	}
}

func TestStartErrors(t *testing.T) {
	f := newFixture(t)
	svc := &review.Service{Store: f.store}
	if _, err := svc.Start(context.Background(), f.repo, []string{"diff", "feat/x", "feat/x"}, ""); err != review.ErrEmptyDiff {
		t.Fatalf("empty diff: %v", err)
	}
	if _, err := svc.Start(context.Background(), f.repo, []string{"diff", "no-such-branch"}, ""); err == nil || !strings.Contains(err.Error(), "no-such-branch") {
		t.Fatalf("git error: %v", err)
	}
	if _, err := svc.Start(context.Background(), t.TempDir(), []string{"diff"}, ""); err == nil || !strings.Contains(err.Error(), "not a git repository") {
		t.Fatalf("outside repo: %v", err)
	}
}

func itoa(id int64) string { return strconv.FormatInt(id, 10) }

func TestExclusionsAndCommits(t *testing.T) {
	f := newFixture(t)
	sid := itoa(f.session.ID)

	var se store.Session
	f.do("PATCH", "/api/sessions/"+sid, map[string]any{"exclusions": []string{"new.go", " src/new.go "}}, &se)
	if len(se.Exclusions) != 2 || se.Progress.TotalFiles != 1 || se.Progress.ExcludedFiles != 1 {
		t.Fatalf("exclusions not applied: %+v", se)
	}
	var detail store.SessionDetail
	f.do("GET", "/api/sessions/"+sid, nil, &detail)
	excluded := 0
	for _, fs := range detail.Files {
		if fs.Excluded {
			excluded++
		}
	}
	if excluded != 1 || len(detail.Files) != 2 {
		t.Fatalf("excluded flags: %+v", detail.Files)
	}
	if rec := f.do("PATCH", "/api/sessions/"+sid, map[string]any{"exclusions": []string{"!x"}}, nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid rule: status %d", rec.Code)
	}
	f.do("PATCH", "/api/sessions/"+sid, map[string]any{"name": "renamed"}, &se)
	if se.Name != "renamed" {
		t.Fatalf("rename: %+v", se)
	}

	var list review.CommitList
	f.do("GET", "/api/sessions/"+sid+"/commits", nil, &list)
	if list.Range != "dev..feat/x" || len(list.Commits) != 1 || list.Commits[0].Subject != "feature" || len(list.Commits[0].Files) != 3 {
		t.Fatalf("commits: %+v", list)
	}
	f.do("GET", "/api/sessions/"+sid+"/commits?path=README.md", nil, &list)
	if len(list.Commits) != 1 || len(list.Commits[0].Files) != 1 {
		t.Fatalf("commits by path: %+v", list)
	}
	f.do("GET", "/api/sessions/"+sid+"/commits?path=nope.txt", nil, &list)
	if len(list.Commits) != 0 {
		t.Fatalf("commits of an untouched path: %+v", list)
	}
}
