package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/CCH-HQ/cchp-cli/internal/auth"
	"github.com/CCH-HQ/cchp-cli/internal/contract"
	"github.com/CCH-HQ/cchp-cli/internal/runner"
	"github.com/CCH-HQ/cchp-cli/internal/tui"
	"github.com/CCH-HQ/cchp-cli/internal/ui"
)

func (a *app) loginCommand() *cobra.Command {
	var server, tokenFile string
	var tokenStdin bool
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Sign in to a CCHP server with a management token",
		Long: `Sign in to a CCHP server with a management token.

In a terminal, cchp asks for the server address and the token.
Scripts pass the token on standard input or in a file, never as an argument:

  printf '%s' "$TOKEN" | cchp login --server https://hub.example.com --token-stdin

Create a management token in the console under Account, Security, Management tokens.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			interactive := !a.agent && a.io.InTTY && a.io.OutTTY && !tokenStdin && tokenFile == ""
			if interactive {
				return tui.Login(cmd.Context(), tui.Deps{Runner: a.runner, Store: a.store, Version: a.build.Version}, server)
			}
			if server == "" {
				server = os.Getenv("CCHP_BASE_URL")
			}
			if server == "" {
				return contract.Fail(contract.KindUsage, contract.ExitUsage, "login needs --server <address>")
			}
			var token string
			switch {
			case tokenStdin:
				data, err := io.ReadAll(io.LimitReader(a.io.In, 64<<10))
				if err != nil {
					return err
				}
				token = string(data)
			case tokenFile != "":
				data, err := os.ReadFile(tokenFile)
				if err != nil {
					return contract.Fail(contract.KindUsage, contract.ExitUsage, "reading --token-file: %v", err)
				}
				token = string(data)
			case os.Getenv("CCHP_TOKEN") != "":
				token = os.Getenv("CCHP_TOKEN")
			default:
				return contract.Fail(contract.KindUsage, contract.ExitUsage, "login needs the token on standard input (--token-stdin), in a file (--token-file) or in CCHP_TOKEN")
			}
			profile, err := a.runner.SignIn(cmd.Context(), server, strings.TrimSpace(token))
			if err != nil {
				return err
			}
			if a.agent {
				return a.writeData(profile)
			}
			fmt.Fprintln(ui.Writer(a.io.Out), ui.Success.Render("✓ ")+"Signed in as "+auth.DisplayName(profile.Identity)+" to "+profile.Server)
			return nil
		},
	}
	cmd.Flags().StringVar(&server, "server", "", "Server address, for example https://hub.example.com.")
	cmd.Flags().BoolVar(&tokenStdin, "token-stdin", false, "Read the token from standard input.")
	cmd.Flags().StringVar(&tokenFile, "token-file", "", "Read the token from this file.")
	return cmd
}

func (a *app) logoutCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Forget the saved token for the current server",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			server, err := a.store.Server()
			if errors.Is(err, auth.ErrNotSignedIn) {
				return notSignedInFailure()
			}
			if err != nil {
				return err
			}
			if err := a.store.Delete(server); err != nil {
				if errors.Is(err, auth.ErrNotSignedIn) {
					return notSignedInFailure()
				}
				return err
			}
			if a.agent {
				return a.writeData(map[string]any{"signed_out": server})
			}
			fmt.Fprintln(ui.Writer(a.io.Out), ui.Success.Render("✓ ")+"Signed out of "+server+". The token still works until you revoke it in the console.")
			return nil
		},
	}
}

func (a *app) statusCommand() *cobra.Command {
	var offline bool
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show which server you're signed in to and whether the token works",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cred, err := a.runner.Credential()
			if errors.Is(err, auth.ErrNotSignedIn) {
				return notSignedInFailure()
			}
			if err != nil {
				return contract.Fail(contract.KindNotSignedIn, contract.ExitNotSignedIn, "%v", err)
			}
			out := map[string]any{"server": cred.Server, "source": cred.Source, "token_prefix": auth.TokenPrefix(cred.Token)}
			var id *auth.Identity
			if offline {
				id = cred.Identity
			} else {
				id, err = a.runner.Whoami(cmd.Context(), cred)
				if err != nil {
					return err
				}
				out["token_works"] = true
			}
			if id != nil {
				out["identity"] = id
			}
			if a.agent {
				return a.writeData(out)
			}
			w := ui.Writer(a.io.Out)
			who := "your token"
			if id != nil {
				who = auth.DisplayName(*id)
			}
			fmt.Fprintln(w, ui.Success.Render("✓ ")+"Signed in as "+who+" to "+cred.Server)
			if id != nil && id.OrganizationName != "" {
				fmt.Fprintln(w, "  Organization: "+id.OrganizationName)
			}
			if id != nil && id.OrgRole != "" {
				fmt.Fprintln(w, "  Role: "+strings.ReplaceAll(id.OrgRole, "_", " "))
			}
			fmt.Fprintln(w, ui.Muted.Render("  token "+auth.TokenPrefix(cred.Token)+"…, saved in "+sourceName(cred.Source)))
			return nil
		},
	}
	cmd.Flags().BoolVar(&offline, "offline", false, "Don't contact the server.")
	return cmd
}

func sourceName(s string) string {
	switch s {
	case "keychain":
		return "the system keychain"
	case "file":
		return "a private file"
	case "env":
		return "CCHP_TOKEN"
	case "token-file":
		return "CCHP_TOKEN_FILE"
	}
	return s
}

func (a *app) whoamiCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "whoami",
		Short: "Show who the current token belongs to",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			op, err := a.lookup("getWhoami")
			if err != nil {
				return err
			}
			res, err := a.runner.Run(cmd.Context(), runner.Options{Op: op, Agent: a.agent, Timeout: a.timeout, DryRun: a.dryRun})
			if err != nil {
				return err
			}
			return a.emit(op, res)
		},
	}
}
