package export

import (
	"strings"
	"testing"

	"revboard/internal/diff"
	"revboard/internal/store"
)

func TestMarkdown(t *testing.T) {
	se := store.Session{Name: "feat", RepoPath: "/repo", GitArgs: []string{"diff", "dev", "feat", "--", ":(exclude)*.md"}, HeadSHA: "abc"}
	line := func(kind diff.LineKind, content string, old, new int, focus bool) store.ExcerptLine {
		return store.ExcerptLine{Line: diff.Line{Kind: kind, Content: content, OldLine: old, NewLine: new}, Focus: focus}
	}
	notes := []store.NoteContext{
		{Comment: store.Comment{Number: 4, Body: "Add a changelog entry", Export: true}},
		{Comment: store.Comment{Number: 1, FilePath: "src/a.go", Body: "skip me", Export: false}, FileID: 1, ChangeType: "modified"},
		{Comment: store.Comment{Number: 2, FilePath: "src/a.go", Side: "new", Line: 11, Body: "Use a constant\ninstead", Export: true},
			FileID: 1, ChangeType: "modified", HunkHeader: "@@ -10,2 +10,2 @@", HunkSection: "func f() {",
			Excerpt: []store.ExcerptLine{line(diff.LineDelete, "x := 1", 10, 0, false), line(diff.LineAdd, "x := 2", 0, 10, false), line(diff.LineContext, "`ctx`", 11, 11, true)}},
		{Comment: store.Comment{Number: 7, FilePath: "old/b.go", Side: "old", Line: 3, Body: "why removed?", Export: true},
			FileID: 2, ChangeType: "renamed", OldPath: "b.go", Outdated: true},
	}
	md := Markdown(se, notes, Options{Context: 1, Range: "dev..feat"})
	for _, want := range []string{
		"Repository root: `/repo`",
		"Reviewed change: `git diff dev feat -- ':(exclude)*.md'`",
		"Commits: `dev..feat`",
		"Notes: 3 (1 general, 2 on files)",
		"### Note #4 (general)\n\n> Add a changelog entry",
		"### File `src/a.go`",
		"#### Note #2: Line 11",
		"- Location: `src/a.go:11`",
		"- Noted line: `'ctx'`",
		"> Use a constant\n> instead",
		"```diff\n@@ -10,2 +10,2 @@ func f() {\n+x := 2\n `ctx`    ◀ note #2\n```",
		"renamed from `b.go`",
		"#### Note #7: Removed line 3",
		"no longer in the current diff",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("missing %q in:\n%s", want, md)
		}
	}
	if strings.Contains(md, "skip me") {
		t.Error("unmarked note exported")
	}
	if !strings.Contains(Markdown(se, notes, Options{All: true}), "skip me") {
		t.Error("All should include unmarked notes")
	}
	if !strings.Contains(Markdown(se, nil, Options{}), "No note selected") {
		t.Error("empty export")
	}
}
