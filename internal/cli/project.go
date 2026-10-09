package cli

import (
	"fmt"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

func newProjectCmd(a *app) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "project",
		Aliases: []string{"p"},
		Short:   "List projects and switch the current project",
		Long: `A project is a git repository in which a review was started. The current
project is used by "revboard review resume", "revboard review list", "revboard status", ...
when --project is not given. "revboard review start" makes its repository current.`,
	}
	cmd.AddCommand(newProjectListCmd(a), newProjectSwitchCmd(a), newProjectRenameCmd(a), newProjectRemoveCmd(a))
	return cmd
}

func newProjectListCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List projects with their number of active reviews",
		Args:    noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			st, err := a.open()
			if err != nil {
				return err
			}
			projects, err := st.ListProjects(cmd.Context())
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if len(projects) == 0 {
				fmt.Fprintln(out, "No projects yet. Run \"revboard review start <git command>\" inside a repository.")
				return nil
			}
			tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, "\tID\tNAME\tACTIVE REVIEWS\tLAST USED\tPATH")
			for _, p := range projects {
				marker := ""
				if p.IsCurrent {
					marker = "*"
				}
				fmt.Fprintf(tw, "%s\t%d\t%s\t%d\t%s\t%s\n", marker, p.ID, p.Name, p.ActiveSessions, ago(p.LastUsedAt), p.RepoPath)
			}
			return tw.Flush()
		},
	}
}

func newProjectSwitchCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:     "switch <name|id>",
		Aliases: []string{"use"},
		Short:   "Make a project the current one",
		Example: `  revboard project switch my-api
  revboard project switch 3`,
		Args: exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := a.open()
			if err != nil {
				return err
			}
			p, err := st.FindProject(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			if err := st.SetCurrentProject(cmd.Context(), p.ID); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Switched to project %q (%s), %d active review(s)\n", p.Name, p.RepoPath, p.ActiveSessions)
			return nil
		},
	}
}

func newProjectRenameCmd(a *app) *cobra.Command {
	return &cobra.Command{
		Use:     "rename <name|id> <new-name>",
		Short:   "Rename a project",
		Example: `  revboard project rename app-2 billing-app`,
		Args:    exactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := a.open()
			if err != nil {
				return err
			}
			p, err := st.FindProject(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			if err := st.RenameProject(cmd.Context(), p.ID, args[1]); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Renamed project %q to %q\n", p.Name, args[1])
			return nil
		},
	}
}

func newProjectRemoveCmd(a *app) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:     "remove <name|id>",
		Aliases: []string{"rm"},
		Short:   "Remove a project and all of its review sessions from revboard",
		Long:    "Remove a project and all of its review sessions from the revboard database. The git repository itself is not touched.",
		Example: `  revboard project remove old-prototype --yes`,
		Args:    exactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := a.open()
			if err != nil {
				return err
			}
			p, err := st.FindProject(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			if !yes && !confirm(cmd, fmt.Sprintf("Remove project %q and all its review sessions?", p.Name)) {
				fmt.Fprintln(cmd.OutOrStdout(), "Aborted.")
				return nil
			}
			if err := st.DeleteProject(cmd.Context(), p.ID); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Removed project %q\n", p.Name)
			return nil
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "do not ask for confirmation")
	return cmd
}
