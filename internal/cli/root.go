// Package cli implements the revboard command line interface.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"revboard/internal/config"
	"revboard/internal/store"
)

// usageError marks errors caused by invalid arguments (exit code 2).
type usageError struct{ err error }

func (e usageError) Error() string { return e.err.Error() }
func (e usageError) Unwrap() error { return e.err }

// app holds the state shared by all commands.
type app struct {
	version    string
	dbPath     string
	projectRef string
	store      *store.Store
	out        io.Writer
}

// open opens the database on first use.
func (a *app) open() (*store.Store, error) {
	if a.store != nil {
		return a.store, nil
	}
	path := a.dbPath
	if path == "" {
		var err error
		if path, err = config.DefaultDBPath(); err != nil {
			return nil, err
		}
	}
	st, err := store.Open(path)
	if err != nil {
		return nil, err
	}
	a.store = st
	return st, nil
}

func (a *app) close() {
	if a.store != nil {
		a.store.Close()
	}
}

// Execute runs the CLI and returns the process exit code.
func Execute(version string) int {
	a := &app{version: version, out: os.Stdout}
	defer a.close()
	root := newRootCmd(a)
	err := root.ExecuteContext(context.Background())
	if err == nil {
		return 0
	}
	fmt.Fprintln(os.Stderr, "error:", err)
	var ue usageError
	if errors.As(err, &ue) {
		fmt.Fprintf(os.Stderr, "Run '%s --help' for usage.\n", root.Name())
		return 2
	}
	return 1
}

func newRootCmd(a *app) *cobra.Command {
	root := &cobra.Command{
		Use:   "revboard",
		Short: "Git-Review-Board: persistent, navigable code reviews of git diffs",
		Long: `Git-Review-Board (revboard) captures a git diff, stores it in a central SQLite
database and opens a local web application to review it: navigate the file
tree, mark hunks and files as reviewed or needing attention, pin files and keep
private notes. Review sessions survive restarts and can be resumed at any time,
across all your repositories.

Database: $REVBOARD_DB, else $XDG_CONFIG_HOME/revboard/revboard.db, else ~/.config/revboard/revboard.db`,
		Example: `  # Start reviewing a feature branch, ignoring markdown files
  revboard review start "git diff dev feat/oai-article-16 -- ':(exclude)*.md'"

  # Come back later and continue where you left off
  revboard review resume

  # See every project and switch between them
  revboard project list
  revboard project switch my-api`,
		Version:       a.version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error { return usageError{err} })
	root.PersistentFlags().StringVar(&a.dbPath, "db", "", "database file (default $REVBOARD_DB or ~/.config/revboard/revboard.db)")
	root.PersistentFlags().StringVarP(&a.projectRef, "project", "P", "", "project name or id (default: current project)")

	root.AddCommand(newReviewCmd(a), newProjectCmd(a), newStatusCmd(a), newServeCmd(a), newVersionCmd(a))
	return root
}

func newVersionCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the revboard version",
		Args:  noArgs,
		Run: func(cmd *cobra.Command, _ []string) {
			fmt.Fprintln(cmd.OutOrStdout(), "revboard", a.version)
		},
	}
}

// Argument validators returning usage errors.

func noArgs(cmd *cobra.Command, args []string) error {
	return wrapUsage(cobra.NoArgs(cmd, args))
}

func exactArgs(n int) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error { return wrapUsage(cobra.ExactArgs(n)(cmd, args)) }
}

func maxArgs(n int) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error { return wrapUsage(cobra.MaximumNArgs(n)(cmd, args)) }
}

func minArgs(n int) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error { return wrapUsage(cobra.MinimumNArgs(n)(cmd, args)) }
}

func wrapUsage(err error) error {
	if err != nil {
		return usageError{err}
	}
	return nil
}
