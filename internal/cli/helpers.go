package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"revboard/internal/browser"
	"revboard/internal/config"
	"revboard/internal/server"
	"revboard/internal/store"
)

// resolveProject applies the project resolution order: --project flag,
// current project, project containing the working directory.
func (a *app) resolveProject(ctx context.Context, st *store.Store) (store.Project, error) {
	if a.projectRef != "" {
		return st.FindProject(ctx, a.projectRef)
	}
	p, err := st.CurrentProject(ctx)
	if err == nil {
		return p, nil
	}
	if !errors.Is(err, store.ErrNotFound) {
		return p, err
	}
	if wd, werr := os.Getwd(); werr == nil {
		if p, err := st.ProjectContaining(ctx, wd); err == nil {
			return p, nil
		}
	}
	return store.Project{}, errors.New(`no project selected; run "revboard project switch <name>" or "revboard review start <git command>"`)
}

// resolveSession returns the session given as argument, or the most recently
// opened active session of the resolved project.
func (a *app) resolveSession(ctx context.Context, st *store.Store, args []string) (store.Session, error) {
	if len(args) > 0 {
		id, err := parseSessionID(args[0])
		if err != nil {
			return store.Session{}, err
		}
		return st.GetSession(ctx, id)
	}
	p, err := a.resolveProject(ctx, st)
	if err != nil {
		return store.Session{}, err
	}
	se, err := st.LatestSession(ctx, p.ID)
	if errors.Is(err, store.ErrNotFound) {
		return se, fmt.Errorf("project %q has no active review session; start one with \"revboard review start\"", p.Name)
	}
	return se, err
}

func parseSessionID(s string) (int64, error) {
	id, err := strconv.ParseInt(strings.TrimPrefix(s, "#"), 10, 64)
	if err != nil || id <= 0 {
		return 0, usageError{fmt.Errorf("invalid session id %q", s)}
	}
	return id, nil
}

// serveOptions are the flags shared by the commands that start the web server.
type serveOptions struct {
	addr   string
	port   int
	noOpen bool
}

func (o *serveOptions) register(cmd *cobra.Command) {
	cmd.Flags().StringVar(&o.addr, "addr", "127.0.0.1", "loopback address to listen on")
	cmd.Flags().IntVar(&o.port, "port", config.DefaultPort,
		"port to listen on, overrides $"+config.EnvPort+" (0 = any free port)")
	cmd.Flags().BoolVar(&o.noOpen, "no-open", false, "do not open the browser")
}

// serve starts the web server, opens the browser on urlPath and blocks until
// Ctrl+C is pressed.
// resolvePort returns the port to listen on: the --port flag when given, else
// $REVBOARD_PORT, else the default port. explicit is false for the default,
// which may be replaced by a free port when it is busy.
func resolvePort(flagSet bool, flagPort int, env string) (port int, explicit bool, err error) {
	switch {
	case flagSet:
		port, explicit = flagPort, true
	case env != "":
		p, err := strconv.Atoi(strings.TrimSpace(env))
		if err != nil {
			return 0, false, usageError{fmt.Errorf("invalid %s=%q: expected a port number", config.EnvPort, env)}
		}
		port, explicit = p, true
	default:
		return config.DefaultPort, false, nil
	}
	if port < 0 || port > 65535 {
		return 0, false, usageError{fmt.Errorf("invalid port %d: expected 0-65535", port)}
	}
	return port, explicit, nil
}

// listen opens the server socket. A busy default port falls back to a free
// port (with a warning); a port chosen by the user must be available.
func listen(addr string, port int, explicit bool, warn io.Writer) (net.Listener, error) {
	ln, err := server.Listen(net.JoinHostPort(addr, strconv.Itoa(port)))
	if err == nil || !errors.Is(err, syscall.EADDRINUSE) {
		return ln, err
	}
	if explicit {
		return nil, fmt.Errorf("port %d is already in use (another \"revboard serve\"?); stop it or choose another port with --port or %s", port, config.EnvPort)
	}
	fmt.Fprintf(warn, "warning: port %d is already in use (another \"revboard serve\"?); using a free port instead\n", port)
	return server.Listen(net.JoinHostPort(addr, "0"))
}

func (a *app) serve(cmd *cobra.Command, st *store.Store, o serveOptions, urlPath string) error {
	port, explicit, err := resolvePort(cmd.Flags().Changed("port"), o.port, os.Getenv(config.EnvPort))
	if err != nil {
		return err
	}
	ln, err := listen(o.addr, port, explicit, cmd.ErrOrStderr())
	if err != nil {
		return err
	}
	url := "http://" + ln.Addr().String() + urlPath
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "\nrevboard is running at %s\nPress Ctrl+C to stop.\n", url)
	if !o.noOpen {
		if err := browser.Open(url); err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "warning: could not open the browser: %v\n", err)
		}
	}
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	err = server.New(st, a.version, nil).Serve(ctx, ln)
	fmt.Fprintln(out, "\nStopped. Your review state is saved; resume with \"revboard review resume\".")
	return err
}

// confirm asks a yes/no question on the command input.
func confirm(cmd *cobra.Command, question string) bool {
	fmt.Fprintf(cmd.OutOrStdout(), "%s [y/N] ", question)
	line, err := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes"
}

// progressBar renders a fixed-width textual progress bar: "█" for reviewed
// units, "▒" for units needing attention. Both count as done in the percentage.
func progressBar(u progressUnits, width int) string {
	if u.total == 0 {
		return "[" + strings.Repeat("·", width) + "]   -"
	}
	done := u.reviewed + u.attention
	filled := done * width / u.total
	flagged := min(filled, u.attention*width/u.total)
	return fmt.Sprintf("[%s%s%s] %3d%%", strings.Repeat("█", filled-flagged), strings.Repeat("▒", flagged),
		strings.Repeat("·", width-filled), done*100/u.total)
}

// ago renders a duration in a compact human form.
func ago(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
	return t.Local().Format("2006-01-02")
}

func sessionSummary(se store.Session) string {
	p := se.Progress
	s := fmt.Sprintf("%d files, %d hunks — %d/%d hunks reviewed", p.TotalFiles, p.TotalHunks, p.ReviewedHunks, p.TotalHunks)
	if p.AttentionHunks > 0 || p.AttentionFiles > 0 {
		s += fmt.Sprintf(", %d need attention", max(p.AttentionHunks, p.AttentionFiles))
	}
	return s
}
