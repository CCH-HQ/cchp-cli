package cli

import (
	"fmt"
	"strings"
	"sync"

	"charm.land/huh/v2"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/CCH-HQ/cchp-cli/internal/catalog"
	"github.com/CCH-HQ/cchp-cli/internal/contract"
	"github.com/CCH-HQ/cchp-cli/internal/runner"
	"github.com/CCH-HQ/cchp-cli/internal/ui"
)

// opFlags holds the per-command flags that are not operation parameters.
type opFlags struct {
	bodyFile  string
	formFiles []string
	secretOut string
	output    string
	pageLimit int
	maxItems  int
	maxEvents int
}

// boolParam is a boolean operation parameter. It keeps the exact text the user
// gave so an unset flag sends nothing and --flag=false sends false.
type boolParam struct{ v string }

func (b *boolParam) String() string { return b.v }
func (b *boolParam) Type() string   { return "bool" }
func (b *boolParam) Set(s string) error {
	switch strings.ToLower(s) {
	case "true", "1":
		b.v = "true"
	case "false", "0":
		b.v = "false"
	default:
		return fmt.Errorf("use true or false, not %q", s)
	}
	return nil
}

// opCommand builds one operation command. The same code path serves
// "cchp <group> <name>" and "cchp api <operationId>".
func (a *app) opCommand(op *catalog.Operation, use string, hidden bool) *cobra.Command {
	var pathParams []catalog.Param
	for _, p := range op.Params {
		if p.In == "path" {
			pathParams = append(pathParams, p)
		}
	}
	var usage strings.Builder
	usage.WriteString(use)
	for _, p := range pathParams {
		if p.Name == "orgId" {
			continue
		}
		usage.WriteString(" [" + p.Flag + "]")
	}
	cmd := &cobra.Command{
		Use:     usage.String(),
		Short:   op.Summary,
		Long:    opLong(op),
		Hidden:  hidden,
		Aliases: op.Aliases,
		Args:    cobra.MaximumNArgs(len(pathParams)),
		Annotations: map[string]string{
			"operation": op.ID,
		},
	}
	if hidden {
		cmd.Aliases = nil
	}
	if op.Status != catalog.StatusAvailable {
		cmd.RunE = func(cmd *cobra.Command, args []string) error {
			_, err := a.runner.Run(cmd.Context(), runner.Options{Op: op})
			return err
		}
		return cmd
	}
	of := &opFlags{}
	flags := cmd.Flags()
	for _, p := range op.Params {
		desc := p.Description
		if p.Required && p.In != "path" && !strings.EqualFold(p.Name, "Idempotency-Key") {
			desc += " (required)"
		}
		if len(p.Enum) > 0 {
			desc += " One of: " + strings.Join(p.Enum, ", ") + "."
		}
		switch p.Type {
		case "array":
			flags.StringArray(p.Flag, nil, desc+" Repeat the flag for more values.")
		case "boolean":
			flags.Var(&boolParam{}, p.Flag, desc)
			flags.Lookup(p.Flag).NoOptDefVal = "true"
		default:
			flags.String(p.Flag, "", desc)
		}
	}
	if op.Body != nil {
		switch op.Body.Kind {
		case "json":
			flags.StringVar(&of.bodyFile, "body-file", "", "JSON request body file; - reads standard input. Sent exactly as written.")
		case "binary":
			flags.StringVar(&of.bodyFile, "body-file", "", "File to upload as the request body.")
		case "multipart":
			flags.StringArrayVar(&of.formFiles, "form-file", nil, "File to upload, as field=path (fields: "+strings.Join(op.Body.FileFields, ", ")+").")
		}
	}
	if op.Policy.SecretOutput() {
		flags.StringVar(&of.secretOut, "secret-out", "", "Save the one-time secret to this new file (mode 600) instead of printing it.")
	}
	if op.Response.Kind == catalog.ResponseBinary {
		flags.StringVarP(&of.output, "output", "o", "", "Save the downloaded file here; - writes to standard output.")
	}
	if op.Policy.Pagination != nil {
		flags.IntVar(&of.pageLimit, "page-limit", 1, "Fetch up to this many pages.")
		flags.IntVar(&of.maxItems, "max-items", 0, "Stop after this many items (0 for no limit).")
	}
	if op.Response.Kind == catalog.ResponseStream {
		flags.IntVar(&of.maxEvents, "max-events", 0, "Stop after this many events (agent mode default 20).")
	}
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		return a.runOperation(cmd, op, of, pathParams, args)
	}
	return cmd
}

func opLong(op *catalog.Operation) string {
	var b strings.Builder
	b.WriteString(op.Summary)
	b.WriteString("\n\n")
	if op.Status != catalog.StatusAvailable {
		b.WriteString("cchp cannot run this operation: " + op.Reason + "\n")
		return b.String()
	}
	fmt.Fprintf(&b, "Operation: %s (%s %s)\n", op.ID, op.Method, op.Path)
	switch op.Policy.Risk {
	case catalog.RiskRead:
		b.WriteString("Reads data only.\n")
	case catalog.RiskDestructive:
		fmt.Fprintf(&b, "Deletes or replaces data. Agents need --allow-write --confirm %s.\n", op.ID)
	default:
		b.WriteString("Changes data. Agents need --allow-write.\n")
	}
	if op.Policy.SecretOutput() {
		fmt.Fprintf(&b, "Returns a secret shown only once (%s). Agents need --secret-out and --confirm %s.\n", strings.Join(op.Policy.SecretFields, ", "), op.ID)
	}
	if op.Policy.Permission != "" {
		fmt.Fprintf(&b, "Needs the %s permission.\n", op.Policy.Permission)
	}
	if op.Body != nil && op.Body.Kind == "json" {
		fmt.Fprintf(&b, "Body: JSON via --body-file. See the fields with: cchp schema %s\n", op.ID)
	}
	return b.String()
}

func (a *app) runOperation(cmd *cobra.Command, op *catalog.Operation, of *opFlags, pathParams []catalog.Param, args []string) error {
	params := map[string][]string{}
	var flagErr error
	cmd.Flags().Visit(func(f *pflag.Flag) {
		p, ok := op.Param(f.Name)
		if !ok {
			return
		}
		if p.Type == "array" {
			vals, err := cmd.Flags().GetStringArray(f.Name)
			if err != nil {
				flagErr = err
				return
			}
			params[f.Name] = vals
			return
		}
		params[f.Name] = []string{f.Value.String()}
	})
	if flagErr != nil {
		return flagErr
	}
	positional := pathParams
	if len(args) > 0 && len(pathParams) > 0 && pathParams[0].Name == "orgId" && len(args) < len(pathParams) {
		// The organization defaults to the token's own; positional values fill the rest.
		positional = pathParams[1:]
	}
	for i, v := range args {
		p := positional[i]
		if _, set := params[p.Flag]; set {
			return contract.Fail(contract.KindUsage, contract.ExitUsage, "%s was given twice, as an argument and as --%s", p.Flag, p.Flag)
		}
		params[p.Flag] = []string{v}
	}
	formFiles := map[string]string{}
	for _, ff := range of.formFiles {
		name, path, ok := strings.Cut(ff, "=")
		if !ok || name == "" || path == "" {
			return contract.Fail(contract.KindUsage, contract.ExitUsage, "--form-file takes field=path, got %q", ff)
		}
		formFiles[name] = path
	}
	maxEvents := of.maxEvents
	if maxEvents == 0 && a.agent && op.Response.Kind == catalog.ResponseStream {
		maxEvents = 20
	}
	opts := runner.Options{
		Op: op, Params: params, BodyFile: of.bodyFile, Stdin: a.io.In, FormFiles: formFiles,
		SecretOut: of.secretOut, Output: of.output, Stdout: a.io.Out,
		PageLimit: of.pageLimit, MaxItems: of.maxItems, MaxEvents: maxEvents,
		Timeout: a.timeout, Agent: a.agent, AllowWrite: a.allowWrite, Confirm: a.confirm, DryRun: a.dryRun,
	}
	if !a.agent && !a.dryRun && a.confirm != op.ID && (op.Policy.Risk == catalog.RiskDestructive || op.Policy.SecretOutput()) {
		ok, err := a.humanConfirm(op)
		if err != nil {
			return err
		}
		if !ok {
			fmt.Fprintln(ui.Writer(a.io.Err), "Nothing was changed.")
			return nil
		}
		opts.HumanConfirmed = true
	}
	var mu sync.Mutex
	closed := false
	if op.Response.Kind == catalog.ResponseStream {
		opts.OnEvent = func(ev *contract.Event) error {
			mu.Lock()
			defer mu.Unlock()
			if closed {
				return nil
			}
			if a.agent {
				return contract.WriteEvent(a.io.Out, ev)
			}
			name := ev.Event
			if name == "" {
				name = "event"
			}
			fmt.Fprintln(ui.Writer(a.io.Out), ui.Key.Render(name)+" "+string(ev.Data))
			return nil
		}
	}
	if a.debug {
		a.runner.Debug = a.io.Err
	}
	res, err := a.runner.Run(cmd.Context(), opts)
	mu.Lock()
	closed = true
	mu.Unlock()
	if err != nil {
		return err
	}
	return a.emit(op, res)
}

// humanConfirm asks before a destructive or secret-producing call. Without a
// terminal it refuses, so nothing changes by accident.
func (a *app) humanConfirm(op *catalog.Operation) (bool, error) {
	if !a.io.InTTY || !a.io.OutTTY {
		return false, contract.Fail(contract.KindPolicy, contract.ExitPolicy, "%s needs confirmation; add --confirm %s", op.ID, op.ID)
	}
	title := op.Summary
	desc := "This deletes or replaces data and may not be undone."
	if op.Policy.SecretOutput() {
		desc = "The response holds a secret that is shown only once."
	}
	ok := false
	err := huh.NewConfirm().Title(title).Description(desc).Affirmative("Go ahead").Negative("Cancel").Value(&ok).Run()
	if err != nil {
		return false, err
	}
	return ok, nil
}
