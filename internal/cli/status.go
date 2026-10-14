package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newStatusCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show the current project and the progress of its reviews",
		Example: `  revboard status
  revboard status -P my-api`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			st, err := a.open()
			if err != nil {
				return err
			}
			ctx := cmd.Context()
			p, err := a.resolveProject(ctx, st)
			if err != nil {
				return err
			}
			sessions, err := st.ListSessions(ctx, p.ID, false)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "Project: %s (%s)\n", p.Name, p.RepoPath)
			if len(sessions) == 0 {
				fmt.Fprintln(out, "\nNo active review session. Start one with: revboard review start \"git diff <base> <branch>\"")
				return nil
			}
			for i, se := range sessions {
				marker := " "
				if i == 0 {
					marker = ">" // resumed by "revboard review resume"
				}
				pr := se.Progress
				units := reviewProgress(pr)
				fmt.Fprintf(out, "\n%s #%d %s  (%s)\n", marker, se.ID, se.Name, ago(se.LastOpenedAt))
				fmt.Fprintf(out, "    %s\n", quoteArgs(se.GitArgs))
				fmt.Fprintf(out, "    %s  %d/%d files reviewed", progressBar(units, 20), pr.ReviewedFiles, pr.TotalFiles)
				if pr.AttentionFiles > 0 {
					fmt.Fprintf(out, " · %d need attention", pr.AttentionFiles)
				}
				fmt.Fprintln(out)
			}
			fmt.Fprintln(out, "\n> = resumed by \"revboard review resume\"")
			return nil
		},
	}
}
