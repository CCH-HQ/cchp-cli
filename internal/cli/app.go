// Package cli is cchp's command tree. CCHP owns the Cobra root; operations run
// through the runner and its embedded Restish engine.
package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/CCH-HQ/cchp-cli/internal/auth"
	"github.com/CCH-HQ/cchp-cli/internal/catalog"
	"github.com/CCH-HQ/cchp-cli/internal/contract"
	"github.com/CCH-HQ/cchp-cli/internal/redact"
	"github.com/CCH-HQ/cchp-cli/internal/runner"
	"github.com/CCH-HQ/cchp-cli/internal/ui"
)

// BuildInfo is stamped at release time.
type BuildInfo struct {
	Version string
	Commit  string
	Date    string
}

// IO is where a run reads and writes.
type IO struct {
	In  io.Reader
	Out io.Writer
	Err io.Writer
	// OutTTY and InTTY tell whether stdout and stdin are terminals.
	OutTTY bool
	InTTY  bool
	// Width is the terminal width for wrapping messages; 0 leaves lines whole.
	Width int
}

// StdIO returns the process streams.
func StdIO() IO {
	return IO{
		In: os.Stdin, Out: os.Stdout, Err: os.Stderr,
		OutTTY: term.IsTerminal(int(os.Stdout.Fd())),
		InTTY:  term.IsTerminal(int(os.Stdin.Fd())),
		Width:  ui.TerminalWidth(os.Stderr),
	}
}

type app struct {
	io     IO
	build  BuildInfo
	agent  bool
	cat    *catalog.Catalog
	store  *auth.Store
	runner *runner.Runner

	jsonOut    bool
	dryRun     bool
	allowWrite bool
	confirm    string
	timeout    time.Duration
	debug      bool
}

// Run executes cchp with args (without the program name) and returns the exit code.
func Run(args []string, stdio IO, build BuildInfo) int {
	a := &app{io: stdio, build: build}
	a.agent = agentMode(args, stdio)
	dirs, err := auth.DefaultDirs()
	if err != nil {
		return a.report(contract.Fail(contract.KindInternal, contract.ExitInternal, "%v", err))
	}
	store, err := auth.OpenStore(dirs)
	if err != nil {
		return a.report(contract.Fail(contract.KindUsage, contract.ExitUsage, "%v", err))
	}
	a.store = store
	a.runner = runner.New(store, build.Version)
	a.cat = a.runner.Catalog
	// Restish reads RSH_* variables; cchp's engine must not inherit them.
	for _, kv := range os.Environ() {
		if name, _, _ := strings.Cut(kv, "="); strings.HasPrefix(name, "RSH_") {
			if err := os.Unsetenv(name); err != nil {
				return a.report(contract.Fail(contract.KindInternal, contract.ExitInternal, "%v", err))
			}
		}
	}

	root := a.rootCommand()
	root.SetArgs(args)
	root.SetIn(stdio.In)
	root.SetOut(stdio.Out)
	root.SetErr(stdio.Err)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	err = root.ExecuteContext(ctx)
	if err == nil {
		return contract.ExitOK
	}
	var f *contract.Failure
	if errors.As(err, &f) {
		return a.report(f)
	}
	return a.report(usageFailure(err))
}

// agentMode is on with --json, CCHP_AGENT=1, or when stdout is not a terminal.
func agentMode(args []string, stdio IO) bool {
	for _, arg := range args {
		if arg == "--" {
			break
		}
		if arg == "--json" || arg == "--json=true" {
			return true
		}
	}
	if os.Getenv("CCHP_AGENT") == "1" {
		return true
	}
	return !stdio.OutTTY
}

func usageFailure(err error) *contract.Failure {
	msg := err.Error()
	f := contract.Fail(contract.KindUsage, contract.ExitUsage, "%s", msg)
	switch {
	case strings.HasPrefix(msg, "unknown command"):
		f.Err.Hint = "Find commands with cchp commands --search <words>."
	case strings.HasPrefix(msg, "unknown flag"), strings.HasPrefix(msg, "unknown shorthand flag"):
		f.Err.Hint = "See the flags with --help."
	}
	return f
}

// report prints a failure in the current mode and returns its exit code.
func (a *app) report(f *contract.Failure) int {
	f.Err.Message = redact.String(f.Err.Message)
	f.Err.Hint = redact.String(f.Err.Hint)
	if a.agent {
		res := &contract.Result{OK: false, Status: f.Status, RequestID: f.RequestID, Outcome: f.Outcome, Error: &f.Err}
		if err := contract.Write(a.io.Out, res); err != nil {
			fmt.Fprintln(a.io.Err, "cchp:", err)
		}
		fmt.Fprintln(a.io.Err, "cchp:", f.Err.Message)
		return f.Exit
	}
	w := ui.Writer(a.io.Err)
	fmt.Fprintln(w, ui.Failure.Render("✗ ")+ui.Wrap(ui.Sentence(f.Err.Message), a.io.Width, 2))
	for _, fe := range f.Err.Fields {
		fmt.Fprintln(w, "  "+ui.Key.Render(fe.Location)+"  "+fe.Message)
	}
	if f.Err.Hint != "" {
		fmt.Fprintln(w, "  "+ui.Wrap(f.Err.Hint, a.io.Width, 2))
	}
	if f.Outcome == contract.OutcomeUnknown {
		fmt.Fprintln(w, "  "+ui.Warning.Render("The change may or may not have happened."))
	}
	if f.RequestID != "" {
		fmt.Fprintln(w, ui.Muted.Render("  request "+f.RequestID))
	}
	return f.Exit
}

// emit prints a successful result.
func (a *app) emit(op *catalog.Operation, res *contract.Result) error {
	if a.agent {
		return contract.Write(a.io.Out, res)
	}
	w := ui.Writer(a.io.Out)
	switch {
	case res.DryRun != nil:
		return a.humanDryRun(w, res.DryRun)
	case res.Stream != nil:
		fmt.Fprintln(w, ui.Muted.Render(fmt.Sprintf("%d events, ended: %s", res.Stream.Events, strings.ReplaceAll(res.Stream.Ended, "_", " "))))
		return nil
	case res.File != nil:
		if res.File.Path != "-" {
			fmt.Fprintln(w, ui.Success.Render("✓ ")+fmt.Sprintf("Saved %s to %s", humanBytes(res.File.Bytes), res.File.Path))
		}
	}
	if len(res.Data) > 0 {
		var buf bytes.Buffer
		if err := json.Indent(&buf, res.Data, "", "  "); err != nil {
			return err
		}
		fmt.Fprintln(w, buf.String())
	}
	if res.Secret != nil {
		fmt.Fprintln(w, ui.Success.Render("✓ ")+"Secret saved to "+res.Secret.Path+" (readable only by you).")
	} else if op != nil && op.Policy.SecretOutput() {
		fmt.Fprintln(w, ui.AlertBox.Render("This response holds a secret that is shown only once. Copy it now."))
	}
	if p := res.Pagination; p != nil && p.HasMore {
		next := "--cursor " + p.NextCursor
		if p.NextPage > 0 {
			next = fmt.Sprintf("--page %d", p.NextPage)
		}
		fmt.Fprintln(w, ui.Muted.Render(fmt.Sprintf("More results: add %s, or --page-limit N for several pages.", next)))
	}
	meta := []string{}
	if res.Status != 0 {
		meta = append(meta, fmt.Sprintf("%d", res.Status))
	}
	if res.RequestID != "" {
		meta = append(meta, "request "+res.RequestID)
	}
	if len(meta) > 0 {
		fmt.Fprintln(w, ui.Muted.Render(strings.Join(meta, " · ")))
	}
	return nil
}

func (a *app) humanDryRun(w io.Writer, d *contract.DryRun) error {
	fmt.Fprintln(w, ui.Title.Render("Dry run: nothing was sent."))
	fmt.Fprintln(w, ui.Key.Render(d.Method)+" "+d.URL)
	keys := make([]string, 0, len(d.Headers))
	for k := range d.Headers {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintln(w, ui.Muted.Render("  "+k+": "+d.Headers[k]))
	}
	if len(d.Body) > 0 {
		var buf bytes.Buffer
		if err := json.Indent(&buf, d.Body, "", "  "); err != nil {
			return err
		}
		fmt.Fprintln(w, buf.String())
	}
	if d.BodyText != "" {
		fmt.Fprintln(w, d.BodyText)
	}
	needs := []string{}
	if d.Policy.NeedsConfirm {
		needs = append(needs, "a confirmation")
	}
	if d.Policy.NeedsSecret {
		needs = append(needs, "a place to save its secret (--secret-out)")
	}
	if len(needs) > 0 {
		fmt.Fprintln(w, ui.Muted.Render("A real run needs "+strings.Join(needs, " and ")+"."))
	}
	return nil
}

func humanBytes(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d bytes", n)
}

func (a *app) addGlobalFlags(cmd *cobra.Command) {
	pf := cmd.PersistentFlags()
	pf.BoolVar(&a.jsonOut, "json", false, "Print one JSON result object (agent mode).")
	pf.BoolVar(&a.dryRun, "dry-run", false, "Show the request without sending it.")
	pf.BoolVar(&a.allowWrite, "allow-write", false, "Allow changes in agent mode (agent mode is read-only by default).")
	pf.StringVar(&a.confirm, "confirm", "", "Confirm a destructive or secret-producing operation by naming its operation ID.")
	pf.DurationVar(&a.timeout, "timeout", 60*time.Second, "Give up after this long.")
	pf.BoolVar(&a.debug, "debug", false, "Print a redacted trace of each request to stderr.")
}
