package cli

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/CCH-HQ/cchp-cli/internal/selfupdate"
	"github.com/CCH-HQ/cchp-cli/internal/ui"
)

const updateLong = `Check for a newer release of cchp and update the installed binary safely.

cchp automatically detects how it was installed:
  - Homebrew Cellar: runs brew upgrade cch-hq/tap/cchp
  - Go install ($GOBIN or $GOPATH/bin): runs go install github.com/CCH-HQ/cchp-cli/cmd/cchp@<version>
  - Direct binary / install.sh: downloads the release archive and checksums.txt,
    verifies SHA-256 (fail-closed), extracts the binary, and replaces it atomically
  - Development build (dev / non-SemVer): refuses to overwrite unless --force is given

This command reads public GitHub Releases only; it needs no sign-in and never
sends your CCHP management token.`

func (a *app) updateCommand() *cobra.Command {
	var check, force bool
	var version string

	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update cchp to the latest release (no sign-in needed)",
		Long:  updateLong,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			var debug io.Writer
			if a.debug {
				debug = a.io.Err
			}
			cfg := selfupdate.Config{
				CurrentVersion: a.build.Version,
				UserAgent:      a.runner.UserAgent,
				Debug:          debug,
			}
			if !a.agent {
				errOut := ui.Writer(a.io.Err)
				cfg.OnProgress = func(msg string) {
					fmt.Fprintln(errOut, ui.Muted.Render("  "+msg))
				}
			}
			if a.configureUpdater != nil {
				a.configureUpdater(&cfg)
			}

			ctx, cancel := context.WithTimeout(cmd.Context(), a.timeout)
			defer cancel()

			res, err := selfupdate.New(cfg).Run(ctx, selfupdate.RunOptions{
				Check:   check,
				Force:   force,
				DryRun:  a.dryRun,
				Version: version,
			})
			if err != nil {
				return err
			}
			if a.agent {
				return a.writeData(res)
			}
			a.renderUpdateResult(res)
			return nil
		},
	}

	f := cmd.Flags()
	f.BoolVar(&check, "check", false, "Check for the latest release without updating.")
	f.BoolVar(&force, "force", false, "Reinstall even if already up to date or running a development build.")
	f.StringVar(&version, "version", "", "Install a specific release version (for example v0.2.0) for direct or Go installs.")
	return cmd
}

func (a *app) renderUpdateResult(res *selfupdate.Result) {
	w := ui.Writer(a.io.Out)
	methodLine := formatInstallMethod(res)

	switch {
	case res.DryRun:
		fmt.Fprintln(w, ui.Title.Render("Dry run: nothing was changed."))
		fmt.Fprintf(w, "  %s  %s\n", ui.Key.Render("Current version:"), res.CurrentVersion)
		if res.LatestVersion != "" {
			fmt.Fprintf(w, "  %s  %s\n", ui.Key.Render("Target version: "), res.TargetVersion)
		}
		fmt.Fprintf(w, "  %s  %s\n", ui.Key.Render("Install method: "), methodLine)
		if res.Message != "" {
			fmt.Fprintf(w, "  %s  %s\n", ui.Key.Render("Planned action: "), res.Message)
		}
		if res.DownloadURL != "" {
			fmt.Fprintf(w, "  %s  %s\n", ui.Muted.Render("Archive URL:    "), ui.Muted.Render(res.DownloadURL))
		}
		if res.NeedsForce {
			fmt.Fprintln(w, "  "+ui.Warning.Render("A real run needs --force (development build)."))
			fmt.Fprintln(w, "  "+ui.Muted.Render("Update guide: "+res.DocsURL))
		}

	case res.CheckOnly:
		switch {
		case res.DevBuild:
			fmt.Fprintln(w, ui.Warning.Render("! ")+fmt.Sprintf("Current build is a development version (%s); latest release is %s.", res.CurrentVersion, res.LatestVersion))
			fmt.Fprintf(w, "  %s  %s\n", ui.Key.Render("Install method:"), methodLine)
			fmt.Fprintln(w, "  "+ui.Muted.Render("Run cchp update --force to replace it, or see "+res.DocsURL))
		case res.UpdateAvailable:
			fmt.Fprintf(w, "%s %s -> %s\n", ui.Title.Render("Update available:"), res.CurrentVersion, ui.Success.Render(res.LatestVersion))
			fmt.Fprintf(w, "  %s  %s\n", ui.Key.Render("Install method:"), methodLine)
			fmt.Fprintln(w, "  "+ui.Muted.Render("Run cchp update to install "+res.TargetVersion+"."))
		default:
			fmt.Fprintln(w, ui.Success.Render("✓ ")+fmt.Sprintf("cchp %s is up to date.", res.CurrentVersion))
			fmt.Fprintf(w, "  %s  %s\n", ui.Key.Render("Install method:"), methodLine)
		}

	case res.Updated:
		fmt.Fprintln(w, ui.Success.Render("✓ ")+fmt.Sprintf("Updated cchp %s -> %s", res.CurrentVersion, res.TargetVersion))
		fmt.Fprintf(w, "  %s  %s\n", ui.Key.Render("Install method:"), methodLine)
		if len(res.Command) > 0 {
			fmt.Fprintf(w, "  %s  %s\n", ui.Muted.Render("Command:"), ui.Muted.Render(strings.Join(res.Command, " ")))
		}
		if res.SHA256 != "" {
			fmt.Fprintf(w, "  %s  %s\n", ui.Muted.Render("Verified SHA-256:"), ui.Muted.Render(res.SHA256))
		}

	default:
		fmt.Fprintln(w, ui.Success.Render("✓ ")+res.Message)
		fmt.Fprintf(w, "  %s  %s\n", ui.Key.Render("Install method:"), methodLine)
		if res.UpdateAvailable && res.DocsURL != "" {
			fmt.Fprintln(w, "  "+ui.Muted.Render("Update guide: "+res.DocsURL))
		}
	}
}

func formatInstallMethod(res *selfupdate.Result) string {
	method := string(res.InstallMethod)
	if res.EffectiveMethod != "" && res.EffectiveMethod != res.InstallMethod {
		method += " -> " + string(res.EffectiveMethod)
	}
	if res.ExecutablePath != "" {
		return fmt.Sprintf("%s (%s)", method, res.ExecutablePath)
	}
	return method
}
