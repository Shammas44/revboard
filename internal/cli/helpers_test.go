package cli

import (
	"bytes"
	"errors"
	"net"
	"strings"
	"testing"

	"revboard/internal/config"
)

func TestResolvePort(t *testing.T) {
	cases := []struct {
		flagSet  bool
		flag     int
		env      string
		port     int
		explicit bool
	}{
		{false, config.DefaultPort, "", config.DefaultPort, false},
		{false, config.DefaultPort, "5555", 5555, true},
		{true, 6666, "5555", 6666, true}, // the flag wins over the environment
		{true, 0, "", 0, true},           // 0 = any free port
	}
	for _, c := range cases {
		port, explicit, err := resolvePort(c.flagSet, c.flag, c.env)
		if err != nil || port != c.port || explicit != c.explicit {
			t.Errorf("resolvePort(%v, %d, %q) = %d, %v, %v", c.flagSet, c.flag, c.env, port, explicit, err)
		}
	}
	for _, env := range []string{"abc", "70000", "-1"} {
		var ue usageError
		if _, _, err := resolvePort(false, 0, env); !errors.As(err, &ue) {
			t.Errorf("env %q: expected a usage error, got %v", env, err)
		}
	}
}

func TestListenFallsBackOnlyForTheDefaultPort(t *testing.T) {
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()
	port := busy.Addr().(*net.TCPAddr).Port

	var warn bytes.Buffer
	ln, err := listen("127.0.0.1", port, false, &warn)
	if err != nil {
		t.Fatalf("default port busy: %v", err)
	}
	defer ln.Close()
	if ln.Addr().(*net.TCPAddr).Port == port || !strings.Contains(warn.String(), "already in use") {
		t.Fatalf("expected a fallback with a warning, got %s / %q", ln.Addr(), warn.String())
	}

	if _, err := listen("127.0.0.1", port, true, &warn); err == nil || !strings.Contains(err.Error(), "already in use") {
		t.Fatalf("explicit busy port should fail, got %v", err)
	}
}

func TestProgressBarCountsAttentionAsDone(t *testing.T) {
	cases := []struct {
		u    progressUnits
		want string
	}{
		{progressUnits{0, 0, 0}, "[··········]   -"},
		{progressUnits{5, 0, 10}, "[█████·····]  50%"},
		{progressUnits{6, 4, 10}, "[██████▒▒▒▒] 100%"},
		{progressUnits{3, 2, 10}, "[███▒▒·····]  50%"},
	}
	for _, c := range cases {
		if got := progressBar(c.u, 10); got != c.want {
			t.Errorf("progressBar(%+v) = %q, want %q", c.u, got, c.want)
		}
	}
}
