package exclude

import "testing"

func TestMatch(t *testing.T) {
	cases := []struct {
		rule string
		path string
		want bool
	}{
		{"*.md", "README.md", true},
		{"*.md", "docs/guide/intro.md", true},
		{"*.md", "src/md.go", false},
		{"docs/", "docs/a.txt", true},
		{"docs/", "src/docs/a.txt", true},
		{"docs/", "docs.txt", false},
		{"src/gen/", "src/gen/x/y.go", true},
		{"src/gen/", "lib/src/gen/y.go", false},
		{"src/*.go", "src/a.go", true},
		{"src/*.go", "src/sub/a.go", false},
		{"**/*.snap", "a/b/c.snap", true},
		{"**/*.snap", "c.snap", true},
		{"src/**/test_*.py", "src/a/b/test_x.py", true},
		{"src/**/test_*.py", "src/test_x.py", true},
		{"package-lock.json", "web/package-lock.json", true},
		{"vendor/**", "vendor/x/y.go", true},
	}
	for _, c := range cases {
		n, err := Normalize(c.rule)
		if err != nil {
			t.Fatalf("Normalize(%q): %v", c.rule, err)
		}
		if got := New([]string{n}).Match(c.path); got != c.want {
			t.Errorf("rule %q path %q: got %v, want %v", c.rule, c.path, got, c.want)
		}
	}
}

func TestNormalize(t *testing.T) {
	for _, bad := range []string{"", "  ", "/", "!", "!!x", "a[.go"} {
		if _, err := Normalize(bad); err == nil {
			t.Errorf("Normalize(%q): expected error", bad)
		}
	}
	got, err := NormalizeAll([]string{" ./docs/ ", "*.md", "docs/"})
	if err != nil || len(got) != 2 || got[0] != "docs/" {
		t.Fatalf("NormalizeAll = %q, %v", got, err)
	}
	if !New(nil).Empty() || New(nil).Match("a") {
		t.Fatal("empty matcher should match nothing")
	}
}

func TestNegation(t *testing.T) {
	m := New([]string{"docs/", "*.md", "!docs/keep.md", "!README.md"})
	for path, want := range map[string]bool{
		"docs/a.txt":    true,
		"docs/keep.md":  false,
		"docs/other.md": true,
		"README.md":     false,
		"src/README.md": false,
		"src/guide.md":  true,
		"src/main.go":   false,
	} {
		if got := m.Match(path); got != want {
			t.Errorf("Match(%q) = %v, want %v", path, got, want)
		}
	}
	// Order matters: a later exclusion wins over an earlier re-inclusion.
	if !New([]string{"!a.md", "*.md"}).Match("a.md") {
		t.Error("last matching rule should win")
	}
	if n, err := Normalize(" !./docs/x.md "); err != nil || n != "!docs/x.md" {
		t.Fatalf("Normalize negation = %q, %v", n, err)
	}
}
