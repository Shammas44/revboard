// Package gitx validates user supplied git commands and runs git.
package gitx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// allowedSubcommands are the git subcommands that produce a unified diff revboard can review.
var allowedSubcommands = map[string]bool{"diff": true, "show": true}

// refusedFlags change the output format so that it is no longer a parsable unified diff.
var refusedFlags = []string{
	"--stat", "--numstat", "--shortstat", "--name-only", "--name-status", "--raw",
	"--word-diff", "--color-words", "--cc", "-c", "--combined", "--summary",
	"--patch-with-stat", "--patch-with-raw", "--compact-summary", "--dirstat",
	"--no-patch", "-s", "--check", "--output",
}

// SplitCommand turns the command given on the CLI into git arguments.
// A single argument is split using POSIX shell-word rules; several arguments are
// taken verbatim. A leading "git" is removed.
func SplitCommand(args []string) ([]string, error) {
	var words []string
	if len(args) == 1 {
		w, err := SplitWords(args[0])
		if err != nil {
			return nil, err
		}
		words = w
	} else {
		words = append(words, args...)
	}
	if len(words) > 0 && words[0] == "git" {
		words = words[1:]
	}
	if len(words) == 0 {
		return nil, errors.New("empty git command")
	}
	return words, nil
}

// SplitWords splits s like a POSIX shell would (quotes and backslash escapes,
// no expansion).
func SplitWords(s string) ([]string, error) {
	var (
		words   []string
		cur     strings.Builder
		inWord  bool
		quote   rune
		escaped bool
	)
	for _, r := range s {
		switch {
		case escaped:
			cur.WriteRune(r)
			escaped = false
		case quote == '\'':
			if r == '\'' {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case quote == '"':
			switch r {
			case '"':
				quote = 0
			case '\\':
				escaped = true
			default:
				cur.WriteRune(r)
			}
		case r == '\\':
			escaped, inWord = true, true
		case r == '\'' || r == '"':
			quote, inWord = r, true
		case r == ' ' || r == '\t' || r == '\n':
			if inWord {
				words = append(words, cur.String())
				cur.Reset()
				inWord = false
			}
		default:
			cur.WriteRune(r)
			inWord = true
		}
	}
	if quote != 0 {
		return nil, fmt.Errorf("unterminated %c quote in command", quote)
	}
	if escaped {
		return nil, errors.New("trailing backslash in command")
	}
	if inWord {
		words = append(words, cur.String())
	}
	return words, nil
}

// Validate checks that args (without the leading "git") is a reviewable command.
func Validate(args []string) error {
	if len(args) == 0 {
		return errors.New("empty git command")
	}
	sub := args[0]
	if strings.HasPrefix(sub, "-") {
		return fmt.Errorf("git options before the subcommand are not supported (%s); use \"git diff ...\" or \"git show ...\"", sub)
	}
	if !allowedSubcommands[sub] {
		return fmt.Errorf("unsupported git subcommand %q: only \"diff\" and \"show\" are supported", sub)
	}
	for _, a := range args[1:] {
		if a == "--" {
			break
		}
		for _, f := range refusedFlags {
			if a == f || strings.HasPrefix(a, f+"=") || (strings.HasPrefix(f, "--") && strings.HasPrefix(a, f+"-")) {
				return fmt.Errorf("option %s is not supported: revboard needs a unified diff (patch) output", a)
			}
		}
	}
	return nil
}

// BuildArgs returns the full argv passed to git: normalising options are
// injected so that the output does not depend on the user's git configuration.
func BuildArgs(args []string) []string {
	out := []string{"-c", "core.quotePath=false", args[0],
		"--no-color", "--no-ext-diff", "--src-prefix=a/", "--dst-prefix=b/"}
	return append(out, args[1:]...)
}

// Error is returned when git exits with a non-zero status.
type Error struct {
	Args   []string
	Stderr string
	Err    error
}

func (e *Error) Error() string {
	msg := strings.TrimSpace(e.Stderr)
	if msg == "" {
		msg = e.Err.Error()
	}
	return fmt.Sprintf("git %s: %s", strings.Join(e.Args, " "), msg)
}

func (e *Error) Unwrap() error { return e.Err }

// Run executes git with argv in dir and returns its stdout.
func Run(ctx context.Context, dir string, argv ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", argv...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, &Error{Args: argv, Stderr: stderr.String(), Err: err}
	}
	return stdout.Bytes(), nil
}

// RepoRoot returns the absolute top-level directory of the repository containing dir.
func RepoRoot(ctx context.Context, dir string) (string, error) {
	out, err := Run(ctx, dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", fmt.Errorf("not a git repository (run revboard inside the repository you want to review): %w", err)
	}
	return filepath.Clean(strings.TrimSpace(string(out))), nil
}

// HeadSHA returns the commit id of HEAD, or "" when it cannot be resolved.
func HeadSHA(ctx context.Context, dir string) string {
	out, err := Run(ctx, dir, "rev-parse", "HEAD")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
