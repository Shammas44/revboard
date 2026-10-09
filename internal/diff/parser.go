package diff

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
)

// ErrCombinedDiff is returned when the input contains a combined diff (merge commit).
var ErrCombinedDiff = errors.New("combined diffs (merge commits) are not supported; diff against a parent instead")

var hunkHeaderRe = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@ ?(.*)$`)

// parser holds the state machine used by Parse.
type parser struct {
	diff *Diff
	cur  *File
	hunk *Hunk

	// Remaining line counts of the hunk being read.
	remOld, remNew int
	// Next line numbers inside the hunk being read.
	oldNo, newNo int

	// Header flags of the current file.
	isNew, isDeleted, isRename, isCopy bool
	// skipBinary is set while reading a "GIT binary patch" payload.
	skipBinary bool
}

// Parse reads a git unified diff and returns its AST.
// Lines before the first "diff --git" header (e.g. the commit header printed by
// "git show") are ignored.
func Parse(r io.Reader) (*Diff, error) {
	p := &parser{diff: &Diff{}}
	br := bufio.NewReaderSize(r, 64*1024)
	for {
		line, err := br.ReadString('\n')
		if len(line) > 0 {
			line = strings.TrimSuffix(line, "\n")
			if perr := p.feed(line); perr != nil {
				return nil, perr
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
	}
	p.finishFile()
	return p.diff, nil
}

func (p *parser) inHunk() bool {
	return p.hunk != nil && (p.remOld > 0 || p.remNew > 0)
}

func (p *parser) feed(line string) error {
	if p.inHunk() {
		if p.feedHunkLine(line) {
			return nil
		}
		// Malformed hunk: stop reading it and treat the line as a header.
		p.remOld, p.remNew = 0, 0
	}

	// "\ No newline at end of file" right after the last line of a hunk.
	if strings.HasPrefix(line, `\`) {
		if p.hunk != nil && len(p.hunk.Lines) > 0 {
			p.hunk.Lines[len(p.hunk.Lines)-1].NoNewline = true
		}
		return nil
	}

	header := strings.TrimSuffix(line, "\r")
	switch {
	case strings.HasPrefix(header, "diff --git "):
		p.startFile(header)
		return nil
	case strings.HasPrefix(header, "diff --cc ") || strings.HasPrefix(header, "diff --combined "):
		return ErrCombinedDiff
	}

	if p.cur == nil || p.skipBinary {
		return nil
	}

	if strings.HasPrefix(header, "@@ ") {
		return p.startHunk(header)
	}
	if len(p.cur.Hunks) == 0 {
		p.parseExtendedHeader(header)
	}
	// Anything else after the hunks of a file is ignored.
	return nil
}

// feedHunkLine consumes one line of the current hunk. It returns false when the
// line cannot belong to a hunk.
func (p *parser) feedHunkLine(line string) bool {
	h := p.hunk
	if strings.HasPrefix(line, `\`) {
		if len(h.Lines) > 0 {
			h.Lines[len(h.Lines)-1].NoNewline = true
		}
		return true
	}
	if line == "" {
		// Some tools strip the leading space of empty context lines.
		line = " "
	}
	switch line[0] {
	case '+':
		h.Lines = append(h.Lines, Line{Kind: LineAdd, Content: line[1:], NewLine: p.newNo})
		p.newNo++
		p.remNew--
		p.cur.Additions++
	case '-':
		h.Lines = append(h.Lines, Line{Kind: LineDelete, Content: line[1:], OldLine: p.oldNo})
		p.oldNo++
		p.remOld--
		p.cur.Deletions++
	case ' ':
		h.Lines = append(h.Lines, Line{Kind: LineContext, Content: line[1:], OldLine: p.oldNo, NewLine: p.newNo})
		p.oldNo++
		p.newNo++
		p.remOld--
		p.remNew--
	default:
		return false
	}
	return true
}

func (p *parser) startFile(header string) {
	p.finishFile()
	oldPath, newPath := parseGitHeaderPaths(strings.TrimPrefix(header, "diff --git "))
	p.cur = &File{OldPath: oldPath, NewPath: newPath}
	p.hunk = nil
	p.remOld, p.remNew = 0, 0
	p.isNew, p.isDeleted, p.isRename, p.isCopy = false, false, false, false
	p.skipBinary = false
}

func (p *parser) finishFile() {
	f := p.cur
	if f == nil {
		return
	}
	switch {
	case p.isNew:
		f.ChangeType = ChangeAdded
		f.OldPath = ""
	case p.isDeleted:
		f.ChangeType = ChangeDeleted
		f.NewPath = ""
	case p.isRename:
		f.ChangeType = ChangeRenamed
	case p.isCopy:
		f.ChangeType = ChangeCopied
	case f.OldPath == "":
		f.ChangeType = ChangeAdded
	case f.NewPath == "":
		f.ChangeType = ChangeDeleted
	default:
		f.ChangeType = ChangeModified
	}
	p.diff.Files = append(p.diff.Files, f)
	p.cur = nil
	p.hunk = nil
}

func (p *parser) startHunk(header string) error {
	m := hunkHeaderRe.FindStringSubmatch(header)
	if m == nil {
		return fmt.Errorf("invalid hunk header %q in %s", header, p.cur.Path())
	}
	h := &Hunk{
		OldStart: atoi(m[1]),
		OldLines: atoiDefault(m[2], 1),
		NewStart: atoi(m[3]),
		NewLines: atoiDefault(m[4], 1),
		Section:  m[5],
	}
	p.cur.Hunks = append(p.cur.Hunks, h)
	p.hunk = h
	p.remOld, p.remNew = h.OldLines, h.NewLines
	p.oldNo, p.newNo = h.OldStart, h.NewStart
	return nil
}

func (p *parser) parseExtendedHeader(header string) {
	f := p.cur
	cut := func(prefix string) (string, bool) {
		if strings.HasPrefix(header, prefix) {
			return header[len(prefix):], true
		}
		return "", false
	}
	if v, ok := cut("new file mode "); ok {
		p.isNew = true
		f.NewMode = v
	} else if v, ok := cut("deleted file mode "); ok {
		p.isDeleted = true
		f.OldMode = v
	} else if v, ok := cut("old mode "); ok {
		f.OldMode = v
	} else if v, ok := cut("new mode "); ok {
		f.NewMode = v
	} else if v, ok := cut("similarity index "); ok {
		f.Similarity = atoi(strings.TrimSuffix(v, "%"))
	} else if v, ok := cut("rename from "); ok {
		p.isRename = true
		f.OldPath = unquotePath(v)
	} else if v, ok := cut("rename to "); ok {
		p.isRename = true
		f.NewPath = unquotePath(v)
	} else if v, ok := cut("copy from "); ok {
		p.isCopy = true
		f.OldPath = unquotePath(v)
	} else if v, ok := cut("copy to "); ok {
		p.isCopy = true
		f.NewPath = unquotePath(v)
	} else if v, ok := cut("--- "); ok {
		f.OldPath = stripPrefix(unquoteHeaderPath(v), "a/")
	} else if v, ok := cut("+++ "); ok {
		f.NewPath = stripPrefix(unquoteHeaderPath(v), "b/")
	} else if strings.HasPrefix(header, "Binary files ") {
		f.IsBinary = true
	} else if header == "GIT binary patch" {
		f.IsBinary = true
		p.skipBinary = true
	}
}

// parseGitHeaderPaths extracts both paths from the part of a "diff --git" line
// following the command. Paths may be C-quoted. Unquoted paths containing spaces
// are ambiguous; the symmetric "a/X b/X" form is detected, otherwise the first
// " b/" separator is used (rename headers later provide the exact paths).
func parseGitHeaderPaths(rest string) (string, string) {
	var a, b string
	if strings.HasPrefix(rest, `"`) {
		q, remain := readQuoted(rest)
		a = q
		remain = strings.TrimPrefix(remain, " ")
		if strings.HasPrefix(remain, `"`) {
			b, _ = readQuoted(remain)
		} else {
			b = remain
		}
	} else if i := strings.LastIndex(rest, ` "`); i >= 0 && strings.HasSuffix(rest, `"`) {
		a = rest[:i]
		b, _ = readQuoted(rest[i+1:])
	} else {
		// Symmetric form: "a/" + P + " b/" + P.
		if n := len(rest) - 5; n > 0 && n%2 == 0 {
			l := n / 2
			if rest[:2] == "a/" && rest[2+l:2+l+3] == " b/" && rest[2:2+l] == rest[2+l+3:] {
				return rest[2 : 2+l], rest[2+l+3:]
			}
		}
		if i := strings.Index(rest, " b/"); i >= 0 {
			a, b = rest[:i], rest[i+1:]
		} else if i := strings.Index(rest, " "); i >= 0 {
			a, b = rest[:i], rest[i+1:]
		} else {
			a, b = rest, rest
		}
	}
	return stripPrefix(a, "a/"), stripPrefix(b, "b/")
}

// unquoteHeaderPath handles the path of a ---/+++ line: "/dev/null" becomes "",
// quoted names are unquoted and the trailing tab git adds to names with spaces is removed.
func unquoteHeaderPath(v string) string {
	v = strings.TrimSuffix(v, "\r")
	if v == "/dev/null" {
		return ""
	}
	if strings.HasPrefix(v, `"`) {
		q, _ := readQuoted(v)
		return q
	}
	return strings.TrimSuffix(v, "\t")
}

func unquotePath(v string) string {
	if strings.HasPrefix(v, `"`) {
		q, _ := readQuoted(v)
		return q
	}
	return v
}

func stripPrefix(path, prefix string) string {
	if path == "" {
		return ""
	}
	return strings.TrimPrefix(path, prefix)
}

// readQuoted decodes a C-style quoted string as produced by git (s starts with a
// double quote). It returns the decoded value and the remainder after the closing quote.
func readQuoted(s string) (string, string) {
	var b strings.Builder
	i := 1
	for i < len(s) {
		c := s[i]
		switch {
		case c == '"':
			return b.String(), s[i+1:]
		case c == '\\' && i+1 < len(s):
			i++
			switch e := s[i]; e {
			case 'a':
				b.WriteByte('\a')
			case 'b':
				b.WriteByte('\b')
			case 't':
				b.WriteByte('\t')
			case 'n':
				b.WriteByte('\n')
			case 'v':
				b.WriteByte('\v')
			case 'f':
				b.WriteByte('\f')
			case 'r':
				b.WriteByte('\r')
			case '0', '1', '2', '3':
				if i+2 < len(s) {
					if n, err := strconv.ParseUint(s[i:i+3], 8, 8); err == nil {
						b.WriteByte(byte(n))
						i += 2
						break
					}
				}
				b.WriteByte(e)
			default:
				b.WriteByte(e)
			}
		default:
			b.WriteByte(c)
		}
		i++
	}
	return b.String(), ""
}

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

func atoiDefault(s string, def int) int {
	if s == "" {
		return def
	}
	return atoi(s)
}
