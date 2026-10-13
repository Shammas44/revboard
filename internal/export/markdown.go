// Package export renders review notes as Markdown meant to be handed to a
// developer or an LLM to implement the requested changes.
package export

import (
	"fmt"
	"strings"

	"revboard/internal/store"
)

// Options control the export.
type Options struct {
	// All includes the notes not marked for export.
	All bool
	// Context is the number of hunk lines shown around a noted line (0 = whole hunk).
	Context int
	// Range describes the commits of the review (e.g. "dev..feature"), optional.
	Range string
}

// DefaultContext is the number of lines shown around a noted line.
const DefaultContext = 4

// Selected returns the notes included by the options.
func Selected(notes []store.NoteContext, opt Options) []store.NoteContext {
	if opt.All {
		return notes
	}
	out := make([]store.NoteContext, 0, len(notes))
	for _, n := range notes {
		if n.Export {
			out = append(out, n)
		}
	}
	return out
}

// Markdown renders the selected notes of a session.
func Markdown(se store.Session, notes []store.NoteContext, opt Options) string {
	notes = Selected(notes, opt)
	var b strings.Builder
	w := func(format string, args ...any) { fmt.Fprintf(&b, format, args...) }

	w("# Code review: requested changes\n\n")
	w("The notes below come from a code review. Each note describes a change to make in the\n")
	w("repository. Apply every note at the location it gives, then check the result builds and\n")
	w("its tests pass. Notes are numbered so that you can report on each of them.\n\n")

	w("## Review context\n\n")
	w("- Repository root: `%s`\n", se.RepoPath)
	w("- Reviewed change: `%s`\n", gitCommand(se.GitArgs))
	if opt.Range != "" {
		w("- Commits: `%s`\n", opt.Range)
	}
	if se.HeadSHA != "" {
		w("- HEAD when the diff was captured: `%s`\n", se.HeadSHA)
	}
	w("- File paths are relative to the repository root.\n")
	w("- Line numbers refer to the new version of a file (after the reviewed change), except\n")
	w("  for notes on a *removed line*, which refer to the previous version.\n")

	var general, files []store.NoteContext
	for _, n := range notes {
		if n.FilePath == "" {
			general = append(general, n)
		} else {
			files = append(files, n)
		}
	}
	w("- Notes: %d (%d general, %d on files)\n", len(notes), len(general), len(files))

	if len(notes) == 0 {
		w("\nNo note selected for export.\n")
		return b.String()
	}

	number := 0
	if len(general) > 0 {
		w("\n## General notes\n\nThese apply to the whole change.\n")
		for _, n := range general {
			number++
			w("\n### %d. General note\n\n%s\n", number, quote(n.Body))
		}
	}

	if len(files) > 0 {
		w("\n## Notes by file\n")
	}
	lastPath := ""
	for _, n := range files {
		if n.FilePath != lastPath {
			lastPath = n.FilePath
			w("\n### File `%s`\n\n", n.FilePath)
			w("%s\n", fileState(n))
		}
		number++
		w("\n#### %d. %s\n\n", number, title(n))
		w("- Location: %s\n", location(n))
		if n.Line > 0 && !n.Outdated {
			if l, ok := focusLine(n); ok {
				w("- Noted line: `%s`\n", inlineCode(l.Content))
			}
		}
		w("- Request:\n\n%s\n", quote(n.Body))
		if lines := window(n.Excerpt, opt.Context); len(lines) > 0 {
			header := n.HunkHeader
			if n.HunkSection != "" {
				header += " " + n.HunkSection
			}
			w("\nCode context (diff of the reviewed change, noted line marked with `◀ note %d`):\n\n", number)
			w("```diff\n%s\n", header)
			for _, l := range lines {
				line := sign(string(l.Kind)) + l.Content
				if l.Focus {
					line += "    ◀ note " + fmt.Sprint(number)
				}
				w("%s\n", line)
			}
			w("```\n")
		}
	}
	return b.String()
}

func gitCommand(args []string) string {
	parts := make([]string, len(args))
	for i, a := range args {
		if a == "" || strings.ContainsAny(a, " \t'\"*?()[]$`\\") {
			parts[i] = "'" + strings.ReplaceAll(a, "'", `'\''`) + "'"
		} else {
			parts[i] = a
		}
	}
	return "git " + strings.Join(parts, " ")
}

func fileState(n store.NoteContext) string {
	switch {
	case n.FileID == 0:
		return "This file is no longer part of the reviewed diff."
	case n.ChangeType == "added":
		return "This file is added by the change."
	case n.ChangeType == "deleted":
		return "This file is deleted by the change."
	case n.ChangeType == "renamed":
		return fmt.Sprintf("This file is renamed from `%s` by the change.", n.OldPath)
	case n.ChangeType == "copied":
		return fmt.Sprintf("This file is copied from `%s` by the change.", n.OldPath)
	}
	return "This file is modified by the change."
}

func title(n store.NoteContext) string {
	switch {
	case n.Line == 0:
		return "Whole file"
	case n.Side == "old":
		return fmt.Sprintf("Removed line %d", n.Line)
	}
	return fmt.Sprintf("Line %d", n.Line)
}

func location(n store.NoteContext) string {
	switch {
	case n.Line == 0:
		return fmt.Sprintf("the whole file `%s`", n.FilePath)
	case n.Outdated:
		return fmt.Sprintf("`%s:%d` (%s version; this line is no longer in the current diff, locate it from the note)",
			n.FilePath, n.Line, version(n.Side))
	case n.Side == "old":
		return fmt.Sprintf("`%s:%d` in the previous version (a line removed by the change)", n.FilePath, n.Line)
	}
	return fmt.Sprintf("`%s:%d`", n.FilePath, n.Line)
}

func version(side string) string {
	if side == "old" {
		return "previous"
	}
	return "new"
}

func focusLine(n store.NoteContext) (store.ExcerptLine, bool) {
	for _, l := range n.Excerpt {
		if l.Focus {
			return l, true
		}
	}
	return store.ExcerptLine{}, false
}

// window keeps the lines within context of the noted line (0 = all).
func window(lines []store.ExcerptLine, context int) []store.ExcerptLine {
	if context <= 0 {
		return lines
	}
	for i, l := range lines {
		if l.Focus {
			return lines[max(0, i-context):min(len(lines), i+context+1)]
		}
	}
	return lines
}

func sign(kind string) string {
	switch kind {
	case "add":
		return "+"
	case "delete":
		return "-"
	}
	return " "
}

func quote(body string) string {
	lines := strings.Split(strings.TrimRight(body, "\n"), "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight("> "+l, " ")
	}
	return strings.Join(lines, "\n")
}

// inlineCode makes content safe inside single backticks.
func inlineCode(s string) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\t", "    "))
	return strings.ReplaceAll(s, "`", "'")
}
