// Package exclude matches file paths against session exclusion rules.
//
// Rules use a small gitignore-like syntax:
//
//	*.md            no slash: matches the file name at any depth
//	docs/           trailing slash: everything below a directory (at any depth when no other slash)
//	src/gen/**      slash inside: matched against the full path from the repository root
//	**/*.snap       "**" matches any number of directories
//	!keep.md        not supported (rules only exclude)
package exclude

import (
	"errors"
	"fmt"
	"path"
	"strings"
)

// MaxRules limits the number of rules per session.
const MaxRules = 200

// Normalize trims a rule and checks its syntax.
func Normalize(rule string) (string, error) {
	r := strings.TrimSpace(rule)
	r = strings.TrimPrefix(r, "./")
	switch {
	case r == "" || r == "/":
		return "", errors.New("empty exclusion rule")
	case strings.HasPrefix(r, "!"):
		return "", fmt.Errorf("negated rules are not supported: %q", rule)
	case strings.ContainsAny(r, "\n\r\x00"):
		return "", fmt.Errorf("invalid characters in rule %q", rule)
	}
	for _, seg := range strings.Split(strings.Trim(r, "/"), "/") {
		if seg == "**" {
			continue
		}
		if _, err := path.Match(seg, ""); err != nil {
			return "", fmt.Errorf("invalid pattern %q: %v", rule, err)
		}
	}
	return r, nil
}

// NormalizeAll normalizes rules, removing duplicates while keeping their order.
func NormalizeAll(rules []string) ([]string, error) {
	if len(rules) > MaxRules {
		return nil, fmt.Errorf("too many exclusion rules (max %d)", MaxRules)
	}
	out := make([]string, 0, len(rules))
	seen := map[string]bool{}
	for _, r := range rules {
		n, err := Normalize(r)
		if err != nil {
			return nil, err
		}
		if !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	return out, nil
}

// Matcher tests paths against a list of rules.
type Matcher struct {
	rules [][]string // each rule split into segments; a trailing "**" marks a directory rule
}

// New compiles rules (assumed normalized). Invalid rules are ignored.
func New(rules []string) *Matcher {
	m := &Matcher{}
	for _, r := range rules {
		dir := strings.HasSuffix(r, "/")
		r = strings.Trim(r, "/")
		if r == "" {
			continue
		}
		segs := strings.Split(r, "/")
		if len(segs) == 1 && segs[0] != "**" {
			// No slash: the rule may match at any depth.
			segs = append([]string{"**"}, segs...)
		}
		if dir {
			segs = append(segs, "**")
		}
		m.rules = append(m.rules, segs)
	}
	return m
}

// Empty reports whether the matcher has no rule.
func (m *Matcher) Empty() bool { return m == nil || len(m.rules) == 0 }

// Match reports whether a repository-relative path is excluded.
func (m *Matcher) Match(p string) bool {
	if m.Empty() || p == "" {
		return false
	}
	parts := strings.Split(p, "/")
	for _, r := range m.rules {
		if matchSegments(r, parts) {
			return true
		}
	}
	return false
}

// matchSegments matches path segments against pattern segments where "**"
// matches zero or more segments.
func matchSegments(pat, parts []string) bool {
	if len(pat) == 0 {
		return len(parts) == 0
	}
	if pat[0] == "**" {
		for i := 0; i <= len(parts); i++ {
			if matchSegments(pat[1:], parts[i:]) {
				return true
			}
		}
		return false
	}
	if len(parts) == 0 {
		return false
	}
	ok, _ := path.Match(pat[0], parts[0])
	return ok && matchSegments(pat[1:], parts[1:])
}
