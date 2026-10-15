// Command cchp is the command-line tool for CCHP (Claude Code Hub Plus).
package main

import (
	"os"

	"github.com/CCH-HQ/cchp-cli/internal/cli"
)

// Set by GoReleaser.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], cli.StdIO(), cli.BuildInfo{Version: version, Commit: commit, Date: date}))
}
