// Package browser opens URLs in the user's default browser.
package browser

import (
	"errors"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// EnvBrowser overrides the command used to open URLs (the URL is appended as last argument).
const EnvBrowser = "REVBOARD_BROWSER"

// Open opens url in the default browser without waiting for it to exit.
func Open(url string) error {
	for _, argv := range candidates() {
		if _, err := exec.LookPath(argv[0]); err != nil {
			continue
		}
		cmd := exec.Command(argv[0], append(argv[1:], url)...)
		if err := cmd.Start(); err != nil {
			continue
		}
		go cmd.Wait() //nolint:errcheck // reap the child process
		return nil
	}
	return errors.New("no browser launcher found; set REVBOARD_BROWSER or open the URL manually")
}

func candidates() [][]string {
	if b := strings.Fields(os.Getenv(EnvBrowser)); len(b) > 0 {
		return [][]string{b}
	}
	switch runtime.GOOS {
	case "darwin":
		return [][]string{{"open"}}
	case "windows":
		return [][]string{{"rundll32", "url.dll,FileProtocolHandler"}}
	}
	var list [][]string
	if isWSL() {
		list = append(list, []string{"wslview"}, []string{"explorer.exe"})
	}
	return append(list, []string{"xdg-open"}, []string{"sensible-browser"}, []string{"x-www-browser"})
}

func isWSL() bool {
	if os.Getenv("WSL_DISTRO_NAME") != "" {
		return true
	}
	b, err := os.ReadFile("/proc/version")
	return err == nil && strings.Contains(strings.ToLower(string(b)), "microsoft")
}
