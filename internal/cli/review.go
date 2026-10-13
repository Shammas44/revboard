package cli

import (
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"revboard/internal/export"
	"revboard/internal/gitx"
	"revboard/internal/review"
	"revboard/internal/store"
)

func newReviewCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "review",
		Aliases: []string{"r"},
		Short:   "Start, resume and manage review sessions",
	}
	cmd.AddCommand(
		newReviewStartCmd(a),
		newReviewResumeCmd(a),
		newReviewListCmd(a),
		newReviewRefreshCmd(a),
		newReviewRenameCmd(a),
		newReviewExcludeCmd(a),
		newReviewCommitsCmd(a),
		newReviewNotesCmd(a),
		newReviewArchiveCmd(a),
		newReviewDeleteCmd(a),
	)
	return cmd
}

// reviewProgress returns the done/total units shown in progress bars:
// hunks when the session has any, files otherwise.
func reviewProgress(p store.Progress) (int, int) {
	if p.TotalHunks > 0 {
		return p.ReviewedHunks, p.TotalHunks
	}
	return p.ReviewedFiles, p.TotalFiles
}

func newReviewStartCmd(a *app) *cobra.Command {
	var (
		name    string
		noServe bool
		so      serveOptions
	)
	cmd := &cobra.Command{
		Use:   "start <git command>",
		Short: "Capture a git diff as a new review session and open it",
		Long: `Run a "git diff" or "git show" command in the current directory, store its
output as a new review session of the current repository, then start the web
UI and open it in the browser.

The command can be given as a single quoted string (split like a shell would)
or as separate arguments after "--". The leading "git" is optional. revboard adds
--no-color --no-ext-diff and fixed a/ b/ prefixes so that your git
configuration does not affect parsing.`,
		Example: `  revboard review start "git diff dev feat/oai-article-16 -- ':(exclude)*.md'"
  revboard review start "git diff main...HEAD" --name "Payment refactoring"
  revboard review start "git show HEAD~1"
  revboard review start -- git diff --cached
  revboard review start "diff v1.2.0 v1.3.0 -- src/" --no-serve`,
		Args: minArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			gitArgs, err := gitx.SplitCommand(args)
			if err != nil {
				return usageError{err}
			}
			if err := gitx.Validate(gitArgs); err != nil {
				return usageError{err}
			}
			st, err := a.open()
			if err != nil {
				return err
			}
			wd, err := os.Getwd()
			if err != nil {
				return err
			}
			svc := &review.Service{Store: st}
			res, err := svc.Start(cmd.Context(), wd, gitArgs, name)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "Created review session #%d %q in project %q\n", res.Session.ID, res.Session.Name, res.Project.Name)
			fmt.Fprintf(out, "  %s\n", sessionSummary(res.Session))
			if noServe {
				fmt.Fprintf(out, "Open it later with: revboard review resume %d\n", res.Session.ID)
				return nil
			}
			return a.serve(cmd, st, so, fmt.Sprintf("/sessions/%d", res.Session.ID))
		},
	}
	cmd.Flags().StringVarP(&name, "name", "n", "", "session name (default: derived from the git command)")
	cmd.Flags().BoolVar(&noServe, "no-serve", false, "only create the session, do not start the web UI")
	so.register(cmd)
	return cmd
}

func newReviewResumeCmd(a *app) *cobra.Command {
	var so serveOptions
	cmd := &cobra.Command{
		Use:   "resume [session-id]",
		Short: "Re-open a review session in the web UI",
		Long: `Re-open a review session exactly as you left it (progress, notes, pins,
selected file). Without an id, the most recently opened active session of the
current project is resumed. Resuming an archived session re-activates it.`,
		Example: `  revboard review resume
  revboard review resume 12
  revboard review resume -P my-api --port 8080 --no-open`,
		Args: maxArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := a.open()
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			se, err := a.resolveSession(ctx, st, args)
			if err != nil {
				return err
			}
			if se.State == store.SessionArchived {
				active := store.SessionActive
				if se, err = st.UpdateSession(ctx, se.ID, store.SessionPatch{State: &active}); err != nil {
					return err
				}
			}
			if err := st.TouchSession(ctx, se.ID); err != nil {
				return err
			}
			if err := st.SetCurrentProject(ctx, se.ProjectID); err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "Resuming review session #%d %q (project %q)\n", se.ID, se.Name, se.ProjectName)
			fmt.Fprintf(out, "  %s\n", sessionSummary(se))
			return a.serve(cmd, st, so, fmt.Sprintf("/sessions/%d", se.ID))
		},
	}
	so.register(cmd)
	return cmd
}

func newReviewListCmd(a *app) *cobra.Command {
	var allProjects, archived bool
	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List review sessions of the current project",
		Example: `  revboard review list
  revboard review list --all-projects --archived`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			st, err := a.open()
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			var projectID int64
			if !allProjects {
				p, err := a.resolveProject(ctx, st)
				if err != nil {
					return err
				}
				projectID = p.ID
			}
			sessions, err := st.ListSessions(ctx, projectID, archived)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if len(sessions) == 0 {
				fmt.Fprintln(out, "No review sessions. Start one with: revboard review start \"git diff <base> <branch>\"")
				return nil
			}
			tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, "ID\tPROJECT\tNAME\tPROGRESS\tATTENTION\tFILES\tLAST OPENED\tSTATE")
			for _, se := range sessions {
				done, total := reviewProgress(se.Progress)
				fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%d\t%d\t%s\t%s\n", se.ID, se.ProjectName, se.Name,
					progressBar(done, total, 10), se.Progress.AttentionHunks, se.Progress.TotalFiles,
					ago(se.LastOpenedAt), se.State)
			}
			return tw.Flush()
		},
	}
	cmd.Flags().BoolVarP(&allProjects, "all-projects", "a", false, "list sessions of every project")
	cmd.Flags().BoolVar(&archived, "archived", false, "include archived sessions")
	return cmd
}

func newReviewRefreshCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "refresh [session-id]",
		Short: "Re-run the git command of a session and update its diff",
		Long: `Re-run the stored git command (e.g. after new commits were pushed on the
branch) and replace the diff of the session. Review statuses of hunks whose
content did not change are kept, as well as pins and notes.`,
		Example: `  revboard review refresh
  revboard review refresh 12`,
		Args: maxArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := a.open()
			if err != nil {
				return err
			}
			se, err := a.resolveSession(cmd.Context(), st, args)
			if err != nil {
				return err
			}
			svc := &review.Service{Store: st}
			updated, err := svc.Refresh(cmd.Context(), se.ID)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Refreshed review session #%d %q\n  before: %s\n  after:  %s\n",
				se.ID, se.Name, sessionSummary(se), sessionSummary(updated))
			return nil
		},
	}
}

func newReviewArchiveCmd(a *app) *cobra.Command {
	var unarchive bool
	cmd := &cobra.Command{
		Use:   "archive <session-id>",
		Short: "Archive a finished review session (hidden from lists)",
		Example: `  revboard review archive 12
  revboard review archive 12 --undo`,
		Args: exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := parseSessionID(args[0])
			if err != nil {
				return err
			}
			st, err := a.open()
			if err != nil {
				return err
			}
			state := store.SessionArchived
			if unarchive {
				state = store.SessionActive
			}
			se, err := st.UpdateSession(cmd.Context(), id, store.SessionPatch{State: &state})
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Review session #%d %q is now %s\n", se.ID, se.Name, se.State)
			return nil
		},
	}
	cmd.Flags().BoolVar(&unarchive, "undo", false, "re-activate an archived session")
	return cmd
}

func newReviewDeleteCmd(a *app) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:     "delete <session-id>",
		Aliases: []string{"rm"},
		Short:   "Delete a review session with all its statuses and notes",
		Example: `  revboard review delete 12 --yes`,
		Args:    exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := parseSessionID(args[0])
			if err != nil {
				return err
			}
			st, err := a.open()
			if err != nil {
				return err
			}
			se, err := st.GetSession(cmd.Context(), id)
			if err != nil {
				return err
			}
			if !yes && !confirm(cmd, fmt.Sprintf("Delete review session #%d %q and all its notes?", se.ID, se.Name)) {
				fmt.Fprintln(cmd.OutOrStdout(), "Aborted.")
				return nil
			}
			if err := st.DeleteSession(cmd.Context(), id); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Deleted review session #%d\n", id)
			return nil
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "do not ask for confirmation")
	return cmd
}

func newReviewRenameCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:     "rename <session-id> <new-name>",
		Short:   "Rename a review session",
		Example: `  revboard review rename 12 "Payment refactoring, 2nd pass"`,
		Args:    exactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := parseSessionID(args[0])
			if err != nil {
				return err
			}
			st, err := a.open()
			if err != nil {
				return err
			}
			name := args[1]
			se, err := st.UpdateSession(cmd.Context(), id, store.SessionPatch{Name: &name})
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Review session #%d renamed to %q\n", se.ID, se.Name)
			return nil
		},
	}
}

func newReviewExcludeCmd(a *app) *cobra.Command {
	var (
		sessionRef    string
		remove, clear bool
	)
	cmd := &cobra.Command{
		Use:   "exclude [rule...]",
		Short: "List, add or remove the file exclusion rules of a session",
		Long: `Excluded files are hidden from the review: they are not listed in the file
tree and do not count in the progress. Rules are stored on the session and can
be changed at any time; review statuses of excluded files are kept.

Rule syntax (gitignore-like):
  *.md          file name pattern, at any depth
  docs/         a directory, at any depth
  src/gen/      a path from the repository root (contains a slash)
  **/*.snap     "**" matches any number of directories

Without rules, the current rules are listed.`,
		Example: `  revboard review exclude
  revboard review exclude '*.md' 'docs/' 'package-lock.json'
  revboard review exclude --remove 'docs/'
  revboard review exclude --clear -s 12`,
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := a.open()
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			var target []string
			if sessionRef != "" {
				target = []string{sessionRef}
			}
			se, err := a.resolveSession(ctx, st, target)
			if err != nil {
				return err
			}
			rules := se.Exclusions
			switch {
			case clear:
				rules = []string{}
			case remove:
				drop := map[string]bool{}
				for _, r := range args {
					drop[strings.TrimSpace(r)] = true
				}
				kept := []string{}
				for _, r := range rules {
					if !drop[r] {
						kept = append(kept, r)
					}
				}
				rules = kept
			case len(args) > 0:
				rules = append(append([]string{}, rules...), args...)
			}
			if clear || remove || len(args) > 0 {
				if se, err = st.UpdateSession(ctx, se.ID, store.SessionPatch{Exclusions: &rules}); err != nil {
					return err
				}
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "Exclusion rules of review session #%d %q:\n", se.ID, se.Name)
			if len(se.Exclusions) == 0 {
				fmt.Fprintln(out, "  (none)")
			}
			for _, r := range se.Exclusions {
				fmt.Fprintf(out, "  %s\n", r)
			}
			fmt.Fprintf(out, "%d file(s) excluded, %d file(s) to review\n", se.Progress.ExcludedFiles, se.Progress.TotalFiles)
			return nil
		},
	}
	cmd.Flags().StringVarP(&sessionRef, "session", "s", "", "session id (default: last session of the current project)")
	cmd.Flags().BoolVar(&remove, "remove", false, "remove the given rules instead of adding them")
	cmd.Flags().BoolVar(&clear, "clear", false, "remove all rules")
	return cmd
}

func newReviewCommitsCmd(a *app) *cobra.Command {
	var path string
	cmd := &cobra.Command{
		Use:   "commits [session-id]",
		Short: "List the commits covered by a review session",
		Long: `List the commits covered by the git command of a session (e.g. dev..feature
for "git diff dev feature"), read live from the repository. Use --path to only
list the commits touching one file.`,
		Example: `  revboard review commits
  revboard review commits 12 --path src/api/handler.go`,
		Args: maxArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := a.open()
			if err != nil {
				return err
			}
			se, err := a.resolveSession(cmd.Context(), st, args)
			if err != nil {
				return err
			}
			list, err := (&review.Service{Store: st}).Commits(cmd.Context(), se.ID, path)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "Commits of review session #%d %q (%s): %d\n", se.ID, se.Name, list.Range, len(list.Commits))
			tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
			for _, c := range list.Commits {
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%d files\n", c.ShortSHA, c.Date.Local().Format("2006-01-02 15:04"), c.Author, c.Subject, len(c.Files))
			}
			return tw.Flush()
		},
	}
	cmd.Flags().StringVar(&path, "path", "", "only commits touching this repository-relative path")
	return cmd
}

func newReviewNotesCmd(a *app) *cobra.Command {
	var (
		markdown, all bool
		context       int
	)
	cmd := &cobra.Command{
		Use:   "notes [session-id]",
		Short: "Print the notes of a session with the code they refer to",
		Long: `Print every note of a session: session notes, file notes and line notes.
Each line note is shown with the lines of its hunk around the noted line (see
--context), the noted line marked with ">".

--markdown prints the notes marked for export (all by default; use --all to
ignore the marks) as a structured document meant for a developer or an LLM to
implement the requested changes: repository, git command, and for each note
the file path, line, request and code context.`,
		Example: `  revboard review notes
  revboard review notes --context 0          # whole hunks
  revboard review notes 12 --markdown > review-notes.md
  revboard review notes --markdown | xclip -selection clipboard`,
		Args: maxArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := a.open()
			if err != nil {
				return err
			}
			se, err := a.resolveSession(cmd.Context(), st, args)
			if err != nil {
				return err
			}
			notes, err := st.ListNotesWithContext(cmd.Context(), se.ID)
			if err != nil {
				return err
			}
			if context < 0 {
				return usageError{fmt.Errorf("--context must be >= 0")}
			}
			if markdown {
				opt := export.Options{All: all, Context: context}
				if !cmd.Flags().Changed("context") {
					opt.Context = export.DefaultContext
				}
				if revs, desc, err := gitx.CommitRange(cmd.Context(), se.WorkDir, se.GitArgs); err == nil && len(revs) > 0 {
					opt.Range = desc
				}
				fmt.Fprint(cmd.OutOrStdout(), export.Markdown(se, notes, opt))
				return nil
			}
			printNotes(cmd.OutOrStdout(), se, notes, context)
			return nil
		},
	}
	cmd.Flags().BoolVar(&markdown, "markdown", false, "print the Markdown export (for a developer or an LLM)")
	cmd.Flags().BoolVar(&all, "all", false, "with --markdown: include the notes not marked for export")
	cmd.Flags().IntVarP(&context, "context", "C", 3, "lines shown around a noted line (0 = whole hunk)")
	return cmd
}

// printNotes renders notes as plain text.
func printNotes(out io.Writer, se store.Session, notes []store.NoteContext, context int) {
	fmt.Fprintf(out, "Notes of review session #%d %q: %d\n", se.ID, se.Name, len(notes))
	if len(notes) == 0 {
		fmt.Fprintln(out, "\nNo notes.")
		return
	}
	lastPath := "\x00"
	for _, n := range notes {
		if n.FilePath != lastPath {
			lastPath = n.FilePath
			title := n.FilePath
			if title == "" {
				title = "Session"
			}
			fmt.Fprintf(out, "\n== %s\n", title)
		}
		where := "file"
		switch {
		case n.FilePath == "":
			where = "session"
		case n.Line > 0:
			where = fmt.Sprintf("line %d (%s)", n.Line, n.Side)
			if n.Outdated {
				where += ", no longer in the diff"
			}
		}
		if !n.Export {
			where += ", not exported"
		}
		fmt.Fprintf(out, "\n  #%d [%s]\n", n.Number, where)
		if excerpt := excerptWindow(n.Excerpt, context); len(excerpt) > 0 {
			fmt.Fprintf(out, "    %s\n", n.HunkHeader)
			for _, l := range excerpt {
				marker := " "
				if l.Focus {
					marker = ">"
				}
				fmt.Fprintf(out, "  %s %4s %4s %s\n", marker, lineNumber(l.OldLine), lineNumber(l.NewLine), excerptLine(l))
			}
		}
		for _, line := range strings.Split(n.Body, "\n") {
			fmt.Fprintf(out, "  │ %s\n", line)
		}
	}
}

// excerptWindow keeps the lines within context of the noted line (0 = all).
func excerptWindow(lines []store.ExcerptLine, context int) []store.ExcerptLine {
	if context == 0 {
		return lines
	}
	for i, l := range lines {
		if l.Focus {
			return lines[max(0, i-context):min(len(lines), i+context+1)]
		}
	}
	return lines
}

func excerptLine(l store.ExcerptLine) string {
	switch l.Kind {
	case "add":
		return "+" + l.Content
	case "delete":
		return "-" + l.Content
	}
	return " " + l.Content
}

func lineNumber(n int) string {
	if n == 0 {
		return ""
	}
	return fmt.Sprint(n)
}

// quoteArgs renders git arguments for display.
func quoteArgs(args []string) string {
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
