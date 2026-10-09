package cli

import (
	"github.com/spf13/cobra"
)

func newServeCmd(a *app) *cobra.Command {
	var so serveOptions
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the web UI on the dashboard of all projects and sessions",
		Example: `  revboard serve
  revboard serve --port 7777 --no-open`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			st, err := a.open()
			if err != nil {
				return err
			}
			return a.serve(cmd, st, so, "/")
		},
	}
	so.register(cmd)
	return cmd
}
