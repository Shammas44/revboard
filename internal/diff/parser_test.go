package diff

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func mustParse(t *testing.T, s string) *Diff {
	t.Helper()
	d, err := Parse(strings.NewReader(s))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return d
}

func TestParseModified(t *testing.T) {
	d := mustParse(t, `diff --git a/main.go b/main.go
index 83db48f..bf269f4 100644
--- a/main.go
+++ b/main.go
@@ -1,4 +1,5 @@ package main
 line1
-line2
+line2 changed
+inserted
 line3

@@ -10 +11 @@ func x() {
-a
+b
`)
	if len(d.Files) != 1 {
		t.Fatalf("files = %d, want 1", len(d.Files))
	}
	f := d.Files[0]
	if f.OldPath != "main.go" || f.NewPath != "main.go" || f.ChangeType != ChangeModified {
		t.Fatalf("unexpected file header: %+v", f)
	}
	if f.Additions != 3 || f.Deletions != 2 {
		t.Fatalf("additions/deletions = %d/%d, want 3/2", f.Additions, f.Deletions)
	}
	if len(f.Hunks) != 2 {
		t.Fatalf("hunks = %d, want 2", len(f.Hunks))
	}
	h := f.Hunks[0]
	if h.Section != "package main" || h.OldStart != 1 || h.OldLines != 4 || h.NewStart != 1 || h.NewLines != 5 {
		t.Fatalf("unexpected hunk header: %+v", h)
	}
	want := []Line{
		{Kind: LineContext, Content: "line1", OldLine: 1, NewLine: 1},
		{Kind: LineDelete, Content: "line2", OldLine: 2},
		{Kind: LineAdd, Content: "line2 changed", NewLine: 2},
		{Kind: LineAdd, Content: "inserted", NewLine: 3},
		{Kind: LineContext, Content: "line3", OldLine: 3, NewLine: 4},
		{Kind: LineContext, Content: "", OldLine: 4, NewLine: 5},
	}
	if !reflect.DeepEqual(h.Lines, want) {
		t.Fatalf("lines:\n got %+v\nwant %+v", h.Lines, want)
	}
	h2 := f.Hunks[1]
	if h2.OldLines != 1 || h2.NewLines != 1 || len(h2.Lines) != 2 || h2.Lines[1].NewLine != 11 {
		t.Fatalf("unexpected second hunk: %+v", h2)
	}
}

func TestParseAddedDeletedNoNewline(t *testing.T) {
	d := mustParse(t, `diff --git a/new.txt b/new.txt
new file mode 100644
index 0000000..3b18e51
--- /dev/null
+++ b/new.txt
@@ -0,0 +1,2 @@
+hello
+world
\ No newline at end of file
diff --git a/old.txt b/old.txt
deleted file mode 100755
index 3b18e51..0000000
--- a/old.txt
+++ /dev/null
@@ -1 +0,0 @@
-bye
`)
	if len(d.Files) != 2 {
		t.Fatalf("files = %d, want 2", len(d.Files))
	}
	added, deleted := d.Files[0], d.Files[1]
	if added.ChangeType != ChangeAdded || added.OldPath != "" || added.NewPath != "new.txt" || added.NewMode != "100644" {
		t.Fatalf("unexpected added file: %+v", added)
	}
	if !added.Hunks[0].Lines[1].NoNewline || added.Hunks[0].Lines[0].NoNewline {
		t.Fatalf("no-newline marker not attached to the last line")
	}
	if deleted.ChangeType != ChangeDeleted || deleted.NewPath != "" || deleted.OldPath != "old.txt" || deleted.OldMode != "100755" {
		t.Fatalf("unexpected deleted file: %+v", deleted)
	}
	if deleted.Path() != "old.txt" {
		t.Fatalf("Path() = %q", deleted.Path())
	}
}

func TestParseRenameCopyModeBinary(t *testing.T) {
	d := mustParse(t, `diff --git a/a b/b
similarity index 100%
rename from a
rename to b
diff --git a/src/x.go b/src/y.go
similarity index 90%
copy from src/x.go
copy to src/y.go
index 1..2 100644
--- a/src/x.go
+++ b/src/y.go
@@ -1 +1 @@
-x
+y
diff --git a/run.sh b/run.sh
old mode 100644
new mode 100755
diff --git a/img.png b/img.png
index 1..2 100644
Binary files a/img.png and b/img.png differ
diff --git a/bin.dat b/bin.dat
index 1..2 100644
GIT binary patch
literal 4
LcmZQzWMT#Y01f~L

literal 0
HcmV?d00001

`)
	if len(d.Files) != 5 {
		t.Fatalf("files = %d, want 5", len(d.Files))
	}
	r := d.Files[0]
	if r.ChangeType != ChangeRenamed || r.OldPath != "a" || r.NewPath != "b" || r.Similarity != 100 || len(r.Hunks) != 0 {
		t.Fatalf("unexpected rename: %+v", r)
	}
	c := d.Files[1]
	if c.ChangeType != ChangeCopied || c.OldPath != "src/x.go" || c.NewPath != "src/y.go" || len(c.Hunks) != 1 {
		t.Fatalf("unexpected copy: %+v", c)
	}
	m := d.Files[2]
	if m.ChangeType != ChangeModified || m.OldMode != "100644" || m.NewMode != "100755" {
		t.Fatalf("unexpected mode change: %+v", m)
	}
	if !d.Files[3].IsBinary || !d.Files[4].IsBinary || len(d.Files[4].Hunks) != 0 {
		t.Fatalf("binary files not detected")
	}
}

func TestParseQuotedAndSpacedPaths(t *testing.T) {
	d := mustParse(t, "diff --git a/my file.txt b/my file.txt\n"+
		"index 1..2 100644\n"+
		"--- a/my file.txt\t\n"+
		"+++ b/my file.txt\t\n"+
		"@@ -1 +1 @@\n-a\n+b\n"+
		"diff --git \"a/tab\\there.txt\" \"b/tab\\there.txt\"\n"+
		"index 1..2 100644\n"+
		"--- \"a/tab\\there.txt\"\n"+
		"+++ \"b/tab\\there.txt\"\n"+
		"@@ -1 +1 @@\n-a\n+b\n"+
		"diff --git a/caf\\303\\251 b/x\n"+
		"diff --git a/sp ace.bin b/sp ace.bin\n"+
		"Binary files a/sp ace.bin and b/sp ace.bin differ\n")
	if got := d.Files[0].NewPath; got != "my file.txt" {
		t.Fatalf("spaced path = %q", got)
	}
	if got := d.Files[1].NewPath; got != "tab\there.txt" {
		t.Fatalf("quoted path = %q", got)
	}
	if got := d.Files[3].NewPath; got != "sp ace.bin" {
		t.Fatalf("symmetric header path = %q", got)
	}
	if got, _ := readQuoted(`"caf\303\251"`); got != "café" {
		t.Fatalf("octal unquote = %q", got)
	}
}

func TestParseShowPreambleAndHeaderLikeContent(t *testing.T) {
	d := mustParse(t, `commit 0123456789abcdef
Author: Someone <someone@example.com>
Date:   Mon Jan 1 00:00:00 2024 +0000

    subject

diff --git a/x.md b/x.md
index 1..2 100644
--- a/x.md
+++ b/x.md
@@ -1,3 +1,3 @@
--- a/not-a-header
-diff --git a/fake b/fake
+++ b/still-content
+@@ -1 +1 @@
 tail
`)
	if len(d.Files) != 1 {
		t.Fatalf("files = %d, want 1", len(d.Files))
	}
	lines := d.Files[0].Hunks[0].Lines
	if len(lines) != 5 || lines[0].Content != "-- a/not-a-header" || lines[2].Content != "++ b/still-content" {
		t.Fatalf("header-like content lines mis-parsed: %+v", lines)
	}
}

func TestParseCombinedDiffRejected(t *testing.T) {
	_, err := Parse(strings.NewReader("diff --cc file.go\nindex 1,2..3\n"))
	if !errors.Is(err, ErrCombinedDiff) {
		t.Fatalf("err = %v, want ErrCombinedDiff", err)
	}
}

func TestParseEmpty(t *testing.T) {
	d := mustParse(t, "")
	if len(d.Files) != 0 {
		t.Fatalf("files = %d, want 0", len(d.Files))
	}
}

func TestHunkBodyRoundTrip(t *testing.T) {
	d := mustParse(t, `diff --git a/f b/f
--- a/f
+++ b/f
@@ -3,3 +3,3 @@ ctx
 a
-b
\ No newline at end of file
+c
\ No newline at end of file
`)
	h := d.Files[0].Hunks[0]
	got := ParseHunkBody(h.OldStart, h.NewStart, h.Body())
	if !reflect.DeepEqual(got, h.Lines) {
		t.Fatalf("round trip:\n got %+v\nwant %+v", got, h.Lines)
	}
	if ParseHunkBody(1, 1, "") != nil {
		t.Fatalf("empty body should give nil")
	}
}

// TestParseRealGit runs the parser on the output of the installed git binary.
func TestParseRealGit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	run := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return string(out)
	}
	write := func(name, content string) {
		t.Helper()
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run("init", "-q", "-b", "main")
	write("keep.txt", strings.Repeat("line\n", 20))
	write("gone.txt", "bye\n")
	write("move me.txt", strings.Repeat("same content\n", 10))
	write("bin.dat", "\x00\x01\x02")
	run("add", ".")
	run("commit", "-q", "-m", "init")
	write("keep.txt", "first\n"+strings.Repeat("line\n", 19)+"last")
	write("dir/new file.txt", "new\n")
	write("bin.dat", "\x00\x03")
	run("rm", "-q", "gone.txt")
	run("mv", "move me.txt", "moved.txt")
	run("add", ".")
	out := run("-c", "core.quotePath=false", "diff", "--cached", "-M", "--no-color", "--src-prefix=a/", "--dst-prefix=b/")

	d := mustParse(t, out)
	byPath := map[string]*File{}
	for _, f := range d.Files {
		byPath[f.Path()] = f
	}
	cases := map[string]ChangeType{
		"keep.txt":         ChangeModified,
		"dir/new file.txt": ChangeAdded,
		"bin.dat":          ChangeModified,
		"gone.txt":         ChangeDeleted,
		"moved.txt":        ChangeRenamed,
	}
	for path, ct := range cases {
		f, ok := byPath[path]
		if !ok {
			t.Fatalf("missing %q in parsed diff; got %v\n%s", path, byPath, out)
		}
		if f.ChangeType != ct {
			t.Errorf("%s: change type %s, want %s", path, f.ChangeType, ct)
		}
	}
	if !byPath["bin.dat"].IsBinary {
		t.Errorf("bin.dat should be binary")
	}
	if byPath["moved.txt"].OldPath != "move me.txt" {
		t.Errorf("rename old path = %q", byPath["moved.txt"].OldPath)
	}
	keep := byPath["keep.txt"]
	last := keep.Hunks[len(keep.Hunks)-1].Lines
	if !last[len(last)-1].NoNewline {
		t.Errorf("keep.txt: missing no-newline flag")
	}
}
