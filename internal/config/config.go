// Package config resolves revboard file locations.
package config

import (
	"errors"
	"os"
	"path/filepath"
)

// EnvDB is the environment variable overriding the database path.
const EnvDB = "REVBOARD_DB"

// DefaultDBPath returns the database path: $REVBOARD_DB, else
// $XDG_CONFIG_HOME/revboard/revboard.db, else ~/.config/revboard/revboard.db.
func DefaultDBPath() (string, error) {
	if p := os.Getenv(EnvDB); p != "" {
		return p, nil
	}
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, "revboard", "revboard.db"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", errors.New("cannot determine the home directory; use --db or REVBOARD_DB")
	}
	return filepath.Join(home, ".config", "revboard", "revboard.db"), nil
}
