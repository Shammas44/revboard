// Command revboard is Git-Review-Board: persistent, navigable code reviews of git diffs.
package main

import (
	"os"

	"revboard/internal/cli"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	os.Exit(cli.Execute(version))
}
