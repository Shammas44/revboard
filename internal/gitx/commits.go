package gitx

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// MaxCommits limits the number of commits returned by Log.
const MaxCommits = 500

// Commit describes one commit of a reviewed range.
type Commit struct {
	SHA      string       `json:"sha"`
	ShortSHA string       `json:"shortSha"`
	Author   string       `json:"author"`
	Email    string       `json:"email"`
	Date     time.Time    `json:"date"`
	Parents  []string     `json:"parents"`
	Subject  string       `json:"subject"`
	Body     string       `json:"body"`
	Files    []CommitFile `json:"files"`
}

// CommitFile is one entry of "git log --numstat".
type CommitFile struct {
	Path      string `json:"path"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
	Binary    bool   `json:"binary"`
}

// CommitRange returns the "git log" revision arguments listing the commits
// covered by a reviewed command (args without the leading "git"), and a
// human readable description. It returns nil revisions when the command
// compares the working tree or the index only.
func CommitRange(ctx context.Context, dir string, args []string) ([]string, string, error) {
	if len(args) == 0 {
		return nil, "", errors.New("empty git command")
	}
	sub := args[0]
	var revs []string
	ranged := ""
	for _, a := range args[1:] {
		if a == "--" {
			break
		}
		if strings.HasPrefix(a, "-") {
			continue
		}
		if i := strings.Index(a, "..."); i >= 0 {
			// "git diff A...B" compares merge-base(A, B) with B: the commits of A..B.
			ranged = orHead(a[:i]) + ".." + orHead(a[i+3:])
			continue
		}
		if strings.Contains(a, "..") {
			ranged = a
			continue
		}
		if isCommit(ctx, dir, a) {
			revs = append(revs, a)
		}
	}

	switch sub {
	case "show":
		if ranged != "" {
			return []string{ranged}, ranged, nil
		}
		if len(revs) == 0 {
			revs = []string{"HEAD"}
		}
		return append([]string{"--no-walk"}, revs...), strings.Join(revs, " "), nil
	case "diff":
		switch {
		case ranged != "":
			return []string{ranged}, ranged, nil
		case len(revs) == 0:
			return nil, "working tree", nil
		case len(revs) == 1:
			// Compares a commit with the working tree or the index.
			r := revs[0] + "..HEAD"
			return []string{r}, r + " (plus uncommitted changes)", nil
		case len(revs) == 2:
			r := revs[0] + ".." + revs[1]
			return []string{r}, r, nil
		}
		return nil, "", fmt.Errorf("cannot derive a commit range from %d revisions", len(revs))
	}
	return nil, "", fmt.Errorf("unsupported git subcommand %q", sub)
}

func orHead(rev string) string {
	if rev == "" {
		return "HEAD"
	}
	return rev
}

func isCommit(ctx context.Context, dir, rev string) bool {
	_, err := Run(ctx, dir, "rev-parse", "--verify", "--quiet", "--end-of-options", rev+"^{commit}")
	return err == nil
}

// Log lists the commits of revs (most recent first), optionally restricted to
// a repository-relative path.
func Log(ctx context.Context, dir string, revs []string, path string) ([]Commit, error) {
	if len(revs) == 0 {
		return []Commit{}, nil
	}
	argv := []string{"-c", "core.quotePath=false", "log", "--no-color", "--numstat",
		"--max-count=" + strconv.Itoa(MaxCommits),
		"--format=%x1e%H%x1f%h%x1f%an%x1f%ae%x1f%aI%x1f%P%x1f%s%x1f%b%x1f"}
	argv = append(argv, revs...)
	argv = append(argv, "--")
	if path != "" {
		// Pathspecs are relative to dir; ":(top)" anchors them at the repository root.
		argv = append(argv, ":(top,literal)"+path)
	}
	out, err := Run(ctx, dir, argv...)
	if err != nil {
		return nil, err
	}
	return parseLog(string(out))
}

func parseLog(out string) ([]Commit, error) {
	commits := []Commit{}
	for _, rec := range strings.Split(out, "\x1e") {
		if strings.TrimSpace(rec) == "" {
			continue
		}
		f := strings.SplitN(rec, "\x1f", 9)
		if len(f) != 9 {
			return nil, fmt.Errorf("unexpected git log output")
		}
		date, _ := time.Parse(time.RFC3339, f[4])
		c := Commit{
			SHA: f[0], ShortSHA: f[1], Author: f[2], Email: f[3], Date: date,
			Parents: strings.Fields(f[5]), Subject: f[6], Body: strings.TrimSpace(f[7]),
			Files: []CommitFile{},
		}
		for _, line := range strings.Split(f[8], "\n") {
			parts := strings.SplitN(line, "\t", 3)
			if len(parts) != 3 {
				continue
			}
			cf := CommitFile{Path: numstatPath(parts[2])}
			if parts[0] == "-" {
				cf.Binary = true
			} else {
				cf.Additions, _ = strconv.Atoi(parts[0])
				cf.Deletions, _ = strconv.Atoi(parts[1])
			}
			c.Files = append(c.Files, cf)
		}
		commits = append(commits, c)
	}
	return commits, nil
}

// numstatPath returns the new path of a numstat entry, resolving the rename
// notations "old => new" and "dir/{old => new}/file".
func numstatPath(p string) string {
	if i := strings.Index(p, "{"); i >= 0 {
		if j := strings.Index(p[i:], "}"); j >= 0 {
			inner := p[i+1 : i+j]
			if k := strings.Index(inner, " => "); k >= 0 {
				p = p[:i] + inner[k+4:] + p[i+j+1:]
				return strings.ReplaceAll(p, "//", "/")
			}
		}
	}
	if k := strings.Index(p, " => "); k >= 0 {
		return p[k+4:]
	}
	return p
}
