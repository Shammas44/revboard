package gitx

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

func TestSplitCommand(t *testing.T) {
	cases := []struct {
		in   []string
		want []string
	}{
		{[]string{`git diff dev feat/oai-article-16 -- ':(exclude)*.md'`},
			[]string{"diff", "dev", "feat/oai-article-16", "--", ":(exclude)*.md"}},
		{[]string{`diff "a b" c\ d 'x"y'`}, []string{"diff", "a b", "c d", `x"y`}},
		{[]string{`git show "HEAD~1" ""`}, []string{"show", "HEAD~1", ""}},
		{[]string{"git", "diff", "main", "a b"}, []string{"diff", "main", "a b"}},
	}
	for _, c := range cases {
		got, err := SplitCommand(c.in)
		if err != nil {
			t.Fatalf("SplitCommand(%q): %v", c.in, err)
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("SplitCommand(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	for _, bad := range []string{`git diff 'unterminated`, `git diff \`, `git`, ``} {
		if _, err := SplitCommand([]string{bad}); err == nil {
			t.Errorf("SplitCommand(%q): expected error", bad)
		}
	}
}

func TestValidate(t *testing.T) {
	ok := [][]string{
		{"diff", "main"},
		{"diff", "--cached", "-M"},
		{"show", "HEAD"},
		{"diff", "main", "--", "--stat"}, // after "--" it is a pathspec
	}
	for _, a := range ok {
		if err := Validate(a); err != nil {
			t.Errorf("Validate(%q): unexpected error %v", a, err)
		}
	}
	bad := [][]string{
		{"log", "-p"},
		{"-C", "/tmp", "diff"},
		{"diff", "--stat"},
		{"diff", "--name-only"},
		{"diff", "--word-diff=color"},
		{"diff", "--dirstat-by-file"},
		{"show", "-c", "HEAD"},
		{"diff", "--output=/tmp/x"},
	}
	for _, a := range bad {
		if err := Validate(a); err == nil {
			t.Errorf("Validate(%q): expected error", a)
		}
	}
}

func TestBuildArgs(t *testing.T) {
	got := BuildArgs([]string{"diff", "main", "--", "x"})
	want := []string{"-c", "core.quotePath=false", "diff", "--no-color", "--no-ext-diff",
		"--src-prefix=a/", "--dst-prefix=b/", "main", "--", "x"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("BuildArgs = %q", got)
	}
}

func TestCommitRangeAndLog(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=Ann", "GIT_AUTHOR_EMAIL=a@x", "GIT_COMMITTER_NAME=Ann", "GIT_COMMITTER_EMAIL=a@x")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write := func(name, content string) {
		if err := os.MkdirAll(filepath.Join(dir, filepath.Dir(name)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run("init", "-q", "-b", "main")
	write("a.txt", "a\n")
	run("add", ".")
	run("commit", "-q", "-m", "init")
	run("checkout", "-q", "-b", "feat")
	write("dir/b.txt", "b\n")
	run("add", ".")
	run("commit", "-q", "-m", "add b", "-m", "Longer body.")
	run("mv", "a.txt", "dir/a2.txt")
	run("commit", "-q", "-m", "move a")

	ctx := context.Background()
	cases := []struct {
		args []string
		want string
		n    int
	}{
		{[]string{"diff", "main", "feat", "--", "dir"}, "main..feat", 2},
		{[]string{"diff", "main...feat"}, "main..feat", 2},
		{[]string{"diff", "main..feat"}, "main..feat", 2},
		{[]string{"diff", "main"}, "main..HEAD (plus uncommitted changes)", 2},
		{[]string{"diff", "--cached"}, "working tree", 0},
		{[]string{"show"}, "HEAD", 1},
		{[]string{"show", "HEAD~1", "--stat"}, "HEAD~1", 1},
	}
	for _, c := range cases {
		revs, desc, err := CommitRange(ctx, dir, c.args)
		if err != nil || desc != c.want {
			t.Fatalf("CommitRange(%q) = %q, %v; want %q", c.args, desc, err, c.want)
		}
		commits, err := Log(ctx, dir, revs, "")
		if err != nil || len(commits) != c.n {
			t.Fatalf("Log(%q) = %d commits, %v; want %d", c.args, len(commits), err, c.n)
		}
	}
	revs, _, _ := CommitRange(ctx, dir, []string{"diff", "main", "feat"})
	commits, err := Log(ctx, dir, revs, "dir/b.txt")
	if err != nil || len(commits) != 1 {
		t.Fatalf("Log by path: %+v %v", commits, err)
	}
	c := commits[0]
	if c.Subject != "add b" || c.Body != "Longer body." || c.Author != "Ann" || c.Files[0].Path != "dir/b.txt" || c.Files[0].Additions != 1 || c.Date.IsZero() {
		t.Fatalf("unexpected commit: %+v", c)
	}
	all, _ := Log(ctx, dir, revs, "")
	if all[0].Subject != "move a" || all[0].Files[0].Path != "dir/a2.txt" {
		t.Fatalf("rename path not resolved: %+v", all[0].Files)
	}
}

func TestNumstatPath(t *testing.T) {
	for in, want := range map[string]string{
		"a.txt":                 "a.txt",
		"a.txt => b.txt":        "b.txt",
		"src/{old => new}/x.go": "src/new/x.go",
		"{ => sub}/x.go":        "sub/x.go",
		"src/{a => }/x.go":      "src/x.go",
	} {
		if got := numstatPath(in); got != want {
			t.Errorf("numstatPath(%q) = %q, want %q", in, got, want)
		}
	}
}
