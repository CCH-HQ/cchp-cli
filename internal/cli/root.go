package cli

import (
	"fmt"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	"github.com/CCH-HQ/cchp-cli/internal/catalog"
	"github.com/CCH-HQ/cchp-cli/internal/contract"
	"github.com/CCH-HQ/cchp-cli/internal/tui"
)

const rootLong = `cchp manages a CCHP (Claude Code Hub Plus) server from your terminal.

Run cchp on its own for the interactive home screen. Sign in once with
cchp login; after that every command uses your saved sign-in.

Agents and scripts: cchp prints one JSON result object when you pass --json,
set CCHP_AGENT=1, or pipe its output. Start with: cchp guide agents`

func (a *app) rootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:           "cchp",
		Short:         "Manage a CCHP server from the terminal",
		Long:          rootLong,
		SilenceErrors: true,
		SilenceUsage:  true,
		Version:       a.build.Version,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if a.agent || !a.io.InTTY {
				return cmd.Help()
			}
			return tui.Run(cmd.Context(), tui.Deps{Runner: a.runner, Store: a.store, Version: a.build.Version})
		},
	}
	root.SetVersionTemplate("cchp {{.Version}}\n")
	a.addGlobalFlags(root)
	root.AddGroup(
		&cobra.Group{ID: "start", Title: "Get started:"},
		&cobra.Group{ID: "data", Title: "Work with your server:"},
		&cobra.Group{ID: "agent", Title: "For agents and scripts:"},
	)

	for _, c := range []*cobra.Command{a.loginCommand(), a.logoutCommand(), a.statusCommand(), a.whoamiCommand(), a.guideCommand(), a.docsCommand()} {
		c.GroupID = "start"
		root.AddCommand(c)
	}
	for _, g := range a.cat.Groups {
		cmd := a.groupCommand(g)
		if cmd == nil {
			continue
		}
		cmd.GroupID = "data"
		root.AddCommand(cmd)
	}
	for _, c := range []*cobra.Command{a.apiCommand(), a.commandsCommand(), a.schemaCommand(), a.skillCommand(), a.versionCommand()} {
		c.GroupID = "agent"
		root.AddCommand(c)
	}
	root.SetHelpCommandGroupID("start")
	root.SetCompletionCommandGroupID("agent")
	return root
}

func (a *app) groupCommand(g catalog.Group) *cobra.Command {
	ops := a.cat.InGroup(g.Name)
	if len(ops) == 0 {
		return nil
	}
	cmd := &cobra.Command{
		Use:   g.Name,
		Short: strings.TrimSuffix(g.Summary, "."),
		Long:  g.Title + ": " + g.Summary,
		Args:  suggestSubcommand,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	for _, op := range ops {
		cmd.AddCommand(a.opCommand(op, op.Name, false))
	}
	if g.Name == "import" {
		cmd.AddCommand(a.importRunCommand())
	}
	return cmd
}

// suggestSubcommand rejects an unknown subcommand and names the closest ones.
func suggestSubcommand(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return nil
	}
	f := contract.Fail(contract.KindUsage, contract.ExitUsage, "There is no command %q in %s", args[0], cmd.CommandPath())
	cmd.SuggestionsMinimumDistance = 2
	if s := cmd.SuggestionsFor(args[0]); len(s) > 0 {
		f.Err.Hint = "Did you mean " + cmd.CommandPath() + " " + s[0] + "?"
	} else {
		f.Err.Hint = "See the commands with " + cmd.CommandPath() + " --help."
	}
	return f
}

func (a *app) apiCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "api <operationId>",
		Short: "Call any operation by its stable operation ID",
		Long: `Call any CCHP operation by its operation ID. This is the stable entry point
for agents: names here never change with CLI wording.

Find operation IDs with: cchp commands --search <words>
See inputs and outputs with: cchp schema <operationId>`,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return nil
			}
			_, err := a.lookup(args[0])
			return err
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	for _, op := range a.cat.Operations {
		cmd.AddCommand(a.opCommand(op, op.ID, true))
	}
	return cmd
}

func (a *app) versionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Show the cchp version and the CCHP spec it was built from",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			src := a.cat.Source
			info := map[string]any{
				"version":  a.build.Version,
				"commit":   a.build.Commit,
				"date":     a.build.Date,
				"go":       runtime.Version(),
				"platform": runtime.GOOS + "/" + runtime.GOARCH,
				"spec": map[string]any{
					"repository": src.Repository,
					"tag":        src.Tag,
					"commit":     src.Commit,
					"path":       src.SpecPath,
					"sha256":     src.SpecSHA256,
					"exporter":   src.Exporter,
				},
				"restish": map[string]any{"module": src.RestishModule, "version": src.RestishVersion},
			}
			if a.agent {
				return a.writeData(info)
			}
			fmt.Fprintf(a.io.Out, "cchp %s (%s, %s)\n", a.build.Version, a.build.Commit, a.build.Date)
			fmt.Fprintf(a.io.Out, "CCHP spec %s, commit %.12s, sha256 %.12s\n", src.Tag, src.Commit, src.SpecSHA256)
			fmt.Fprintf(a.io.Out, "Restish %s\n", src.RestishVersion)
			return nil
		},
	}
}

func notSignedInFailure() *contract.Failure {
	return contract.Fail(contract.KindNotSignedIn, contract.ExitNotSignedIn, "You're not signed in.").
		WithHint("Run cchp login, or set CCHP_BASE_URL with CCHP_TOKEN or CCHP_TOKEN_FILE.")
}
