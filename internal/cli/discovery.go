package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/CCH-HQ/cchp-cli/internal/assets"
	"github.com/CCH-HQ/cchp-cli/internal/catalog"
	"github.com/CCH-HQ/cchp-cli/internal/contract"
	"github.com/CCH-HQ/cchp-cli/internal/ui"
)

// summary is the compact form agents read from "cchp commands".
type summary struct {
	Operation string `json:"operation"`
	Command   string `json:"command"`
	Summary   string `json:"summary"`
	Method    string `json:"method"`
	Path      string `json:"path"`
	Risk      string `json:"risk"`
	Status    string `json:"status"`
	Reason    string `json:"reason,omitempty"`
}

func summarize(op *catalog.Operation) summary {
	s := summary{Operation: op.ID, Summary: op.Summary, Method: op.Method, Path: op.Path, Risk: op.Policy.Risk, Status: op.Status, Reason: op.Reason}
	if op.Status == catalog.StatusAvailable {
		s.Command = "cchp " + op.Command()
	}
	return s
}

func (a *app) commandsCommand() *cobra.Command {
	var search, group string
	var all bool
	cmd := &cobra.Command{
		Use:   "commands",
		Short: "List and search operations (works offline)",
		Long: `List and search every operation cchp knows, from the spec built into cchp.

  cchp commands --search "provider keys" --json
  cchp commands --group usage
  cchp commands show listOrgProviders`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			var ops []*catalog.Operation
			if search != "" {
				ops = a.cat.Search(search, all)
			} else {
				for _, op := range a.cat.Operations {
					if all || op.Status == catalog.StatusAvailable {
						ops = append(ops, op)
					}
				}
			}
			var list []summary
			for _, op := range ops {
				if group != "" && op.Group != group {
					continue
				}
				list = append(list, summarize(op))
			}
			if list == nil {
				list = []summary{}
			}
			if a.agent {
				return a.writeData(map[string]any{"count": len(list), "commands": list})
			}
			w := ui.Writer(a.io.Out)
			if len(list) == 0 {
				fmt.Fprintln(w, "Nothing matches. Try other words, or add --all to include operations cchp cannot run.")
				return nil
			}
			for _, s := range list {
				name := s.Command
				if name == "" {
					name = "(" + s.Status + ")"
				}
				fmt.Fprintf(w, "%s  %s\n", ui.Key.Render(s.Operation), name)
				fmt.Fprintf(w, "  %s %s\n", s.Summary, ui.Muted.Render("["+s.Risk+"]"))
			}
			fmt.Fprintln(w, ui.Muted.Render(fmt.Sprintf("%d operations. Details: cchp commands show <operation>", len(list))))
			return nil
		},
	}
	cmd.Flags().StringVar(&search, "search", "", "Words to look for in names, summaries and paths.")
	cmd.Flags().StringVar(&group, "group", "", "Only this command group.")
	cmd.Flags().BoolVar(&all, "all", false, "Include operations cchp cannot run, with the reason.")
	cmd.AddCommand(a.commandsShowCommand())
	return cmd
}

type opDetail struct {
	summary
	Group      string              `json:"group"`
	Permission string              `json:"permission,omitempty"`
	Needs      []string            `json:"needs"`
	Params     []catalog.Param     `json:"params"`
	Body       *catalog.Body       `json:"body,omitempty"`
	Response   catalog.Response    `json:"response"`
	Pagination *catalog.Pagination `json:"pagination,omitempty"`
	Secrets    []string            `json:"secret_fields,omitempty"`
	Examples   []string            `json:"examples,omitempty"`
}

func detail(op *catalog.Operation) opDetail {
	d := opDetail{summary: summarize(op), Group: op.Group, Permission: op.Policy.Permission, Params: op.Params,
		Body: op.Body, Response: op.Response, Pagination: op.Policy.Pagination, Secrets: op.Policy.SecretFields, Needs: []string{}}
	if d.Params == nil {
		d.Params = []catalog.Param{}
	}
	if op.Status != catalog.StatusAvailable {
		return d
	}
	var flags []string
	if op.Policy.Risk != catalog.RiskRead {
		d.Needs = append(d.Needs, "--allow-write")
		flags = append(flags, "--allow-write")
	}
	if op.Policy.Risk == catalog.RiskDestructive || op.Policy.SecretOutput() {
		d.Needs = append(d.Needs, "--confirm "+op.ID)
		flags = append(flags, "--confirm "+op.ID)
	}
	if op.Policy.SecretOutput() {
		d.Needs = append(d.Needs, "--secret-out <file>")
		flags = append(flags, "--secret-out secret.json")
	}
	if op.Response.Kind == catalog.ResponseBinary {
		d.Needs = append(d.Needs, "--output <file>")
		flags = append(flags, "--output download.bin")
	}
	var args []string
	for _, p := range op.Params {
		if !p.Required || p.Name == "orgId" || strings.EqualFold(p.Name, "Idempotency-Key") {
			continue
		}
		args = append(args, "--"+p.Flag+" <"+p.Flag+">")
	}
	if op.Body != nil {
		switch op.Body.Kind {
		case "json":
			args = append(args, "--body-file body.json")
		case "binary":
			args = append(args, "--body-file <file>")
		case "multipart":
			for _, f := range op.Body.FileFields {
				args = append(args, "--form-file "+f+"=<file>")
			}
		}
	}
	line := strings.TrimSpace("cchp api " + op.ID + " " + strings.Join(append(args, flags...), " ") + " --json")
	d.Examples = append(d.Examples, line)
	if op.Policy.Risk != catalog.RiskRead {
		d.Examples = append(d.Examples, strings.TrimSpace("cchp api "+op.ID+" "+strings.Join(args, " ")+" --dry-run --json"))
	}
	if op.Policy.Pagination != nil {
		d.Examples = append(d.Examples, "cchp api "+op.ID+" --page-limit 5 --max-items 200 --json")
	}
	return d
}

func (a *app) lookup(id string) (*catalog.Operation, error) {
	op, ok := a.cat.Get(id)
	if !ok {
		f := contract.Fail(contract.KindUsage, contract.ExitUsage, "there is no operation %q", id)
		if hits := a.cat.Search(id, true); len(hits) > 0 {
			f.Err.Hint = "Did you mean " + hits[0].ID + "?"
		} else {
			f.Err.Hint = "Find operations with cchp commands --search <words>."
		}
		return nil, f
	}
	return op, nil
}

func (a *app) commandsShowCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "show <operationId>",
		Short: "Show one operation: inputs, safety flags and examples",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			op, err := a.lookup(args[0])
			if err != nil {
				return err
			}
			d := detail(op)
			if a.agent {
				return a.writeData(d)
			}
			w := ui.Writer(a.io.Out)
			fmt.Fprintln(w, ui.Title.Render(op.Summary))
			fmt.Fprintf(w, "%s  %s %s\n", ui.Key.Render(op.ID), op.Method, op.Path)
			if op.Status != catalog.StatusAvailable {
				fmt.Fprintln(w, ui.Warning.Render("cchp cannot run this: ")+op.Reason)
				return nil
			}
			fmt.Fprintln(w, "Command: cchp "+op.Command())
			fmt.Fprintln(w, "Risk: "+op.Policy.Risk)
			if len(d.Needs) > 0 {
				fmt.Fprintln(w, "Agents need: "+strings.Join(d.Needs, " "))
			}
			if op.Policy.Permission != "" {
				fmt.Fprintln(w, "Permission: "+op.Policy.Permission)
			}
			if len(op.Params) > 0 {
				fmt.Fprintln(w, ui.Title.Render("Inputs"))
				for _, p := range op.Params {
					req := ""
					if p.Required && p.Name != "orgId" {
						req = " (required)"
					}
					fmt.Fprintln(w, "  "+ui.Wrap(fmt.Sprintf("--%s  %s%s", p.Flag, p.Description, req), a.io.Width-2, 4))
				}
			}
			if op.Body != nil {
				fmt.Fprintf(w, "Body: %s (%s). Fields: cchp schema %s\n", op.Body.Kind, op.Body.ContentType, op.ID)
			}
			fmt.Fprintln(w, ui.Title.Render("Examples"))
			for _, ex := range d.Examples {
				fmt.Fprintln(w, "  "+wrapCommand(ex, a.io.Width-2))
			}
			return nil
		},
	}
}

func (a *app) schemaCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "schema <operationId>",
		Short: "Print an operation's parameters and JSON Schemas (works offline)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			op, err := a.lookup(args[0])
			if err != nil {
				return err
			}
			s := assets.Schemas(op.ID)
			out := map[string]any{
				"operation": op.ID,
				"status":    op.Status,
				"method":    op.Method,
				"path":      op.Path,
				"params":    detail(op).Params,
				"body":      op.Body,
				"response":  op.Response,
			}
			if op.Status != catalog.StatusAvailable {
				out["reason"] = op.Reason
			}
			if len(s.Body) > 0 {
				out["body_schema"] = s.Body
			}
			if len(s.Response) > 0 {
				out["response_schema"] = s.Response
			}
			if a.agent {
				return a.writeData(out)
			}
			return writeJSONIndent(a.io.Out, out)
		},
	}
}

// wrapCommand splits a long example command before its flags, ending each broken
// line with a backslash so the result still pastes into a shell. A width of 0
// or less leaves the command on one line.
func wrapCommand(command string, width int) string {
	if width <= 0 || len(command) <= width {
		return command
	}
	const cont = " \\"
	var lines []string
	line := ""
	for _, part := range splitBeforeFlags(command) {
		if line != "" && len(line)+1+len(part)+len(cont) > width {
			lines = append(lines, line+cont)
			line = "   " + part
			continue
		}
		if line == "" {
			line = part
		} else {
			line += " " + part
		}
	}
	return strings.Join(append(lines, line), "\n  ")
}

// splitBeforeFlags cuts a command into the program words and one piece per flag
// with its value.
func splitBeforeFlags(command string) []string {
	var parts []string
	for _, word := range strings.Split(command, " ") {
		if len(parts) > 0 && !strings.HasPrefix(word, "--") {
			parts[len(parts)-1] += " " + word
			continue
		}
		parts = append(parts, word)
	}
	return parts
}

// writeData prints a discovery result in the standard result object.
func (a *app) writeData(v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return contract.Write(a.io.Out, &contract.Result{OK: true, Data: data})
}

func writeJSONIndent(w io.Writer, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	_, err = w.Write(append(data, '\n'))
	return err
}
