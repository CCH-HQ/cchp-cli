// Command cchp is the command-line tool for CCHP (Claude Code Hub Plus).
package main

import (
	"os"
	"runtime/debug"

	"github.com/CCH-HQ/cchp-cli/internal/cli"
	"github.com/CCH-HQ/cchp-cli/internal/selfupdate"
)

// Set by GoReleaser.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func resolveVersion(v string) string {
	if v != "" && v != "dev" {
		return v
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		if norm, ok := selfupdate.NormalizeVersion(info.Main.Version); ok {
			return norm
		}
	}
	return v
}

func main() {
	os.Exit(cli.Run(os.Args[1:], cli.StdIO(), cli.BuildInfo{Version: resolveVersion(version), Commit: commit, Date: date}))
}
