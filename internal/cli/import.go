package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"sort"
	"strings"
	"time"

	"charm.land/huh/v2"
	"github.com/spf13/cobra"

	"github.com/CCH-HQ/cchp-cli/internal/catalog"
	"github.com/CCH-HQ/cchp-cli/internal/contract"
	"github.com/CCH-HQ/cchp-cli/internal/runner"
	"github.com/CCH-HQ/cchp-cli/internal/ui"
)

// importStatus is the part of the CC Hub import job cchp reads.
type importStatus struct {
	JobID       string          `json:"jobId"`
	Status      string          `json:"status"`
	Phase       string          `json:"phase"`
	Progress    int             `json:"progress"`
	ErrorCode   int64           `json:"errorCode"`
	ErrorDetail string          `json:"errorDetail"`
	Retryable   bool            `json:"retryable"`
	Preview     *importPreview  `json:"preview"`
	Report      json.RawMessage `json:"report"`
}

type importPreview struct {
	ConfirmPhrase          string   `json:"confirm_phrase"`
	RequiresSourceTimezone bool     `json:"requires_source_timezone"`
	RequiresFiveHourReset  bool     `json:"requires_five_hour_reset"`
	Warnings               []string `json:"warnings"`
	UserCount              int64    `json:"user_count"`
	ProviderCount          int64    `json:"provider_count"`
	ProviderKeyCount       int64    `json:"provider_key_count"`
	UserKeyCount           int64    `json:"user_key_count"`
	ConflictCounts         []struct {
		Mode  string `json:"mode"`
		Type  string `json:"type"`
		Count int64  `json:"count"`
	} `json:"conflict_counts"`
	Conflicts []struct {
		Type    string   `json:"type"`
		Allowed []string `json:"allowed"`
	} `json:"conflicts"`
}

func (a *app) importRunCommand() *cobra.Command {
	var sourceURL, adminTokenFile, file, jobID, mode, confirmText, sourceTZ string
	var choices []string
	var restartWindow, previewOnly bool
	var poll, wait time.Duration
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Import from CC Hub: preview, confirm, wait and report",
		Long: `Import users, providers and keys from a CC Hub installation in one go.

1. Preview: fetch the export from the CC Hub server (--source-url with
   --admin-token-file) or upload an export file (--file).
2. Wait until the preview is ready and show what would change.
3. Confirm with your choices and wait until the import finishes.
4. Print the result.

Agents: this changes data, so it needs --allow-write. Overwrite mode replaces
existing data and also needs --confirm startCCHubImport and --confirm-text with
the phrase from the preview (run once with --preview-only to read it).
Continue from an existing preview with --job <jobId>.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if mode != "append" && mode != "overwrite" {
				return contract.Fail(contract.KindUsage, contract.ExitUsage, "--mode must be append or overwrite")
			}
			if a.agent && !a.allowWrite && !a.dryRun {
				return contract.Fail(contract.KindPolicy, contract.ExitPolicy, "importing changes data; add --allow-write").WithHint("Agent mode is read-only by default.")
			}
			if mode == "overwrite" && a.agent && !previewOnly && a.confirm != "startCCHubImport" {
				return contract.Fail(contract.KindPolicy, contract.ExitPolicy, "overwrite mode replaces existing data; add --confirm startCCHubImport")
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), wait)
			defer cancel()
			var err error
			if jobID == "" {
				jobID, err = a.importStart(ctx, sourceURL, adminTokenFile, file)
				if err != nil {
					return err
				}
			}
			st, err := a.importWait(ctx, jobID, poll, "preview")
			if err != nil {
				return err
			}
			if st.Status != "preview" {
				return importFailure(st)
			}
			if !a.agent {
				a.showPreview(st)
			}
			if previewOnly {
				return a.importResult(st, "preview ready; nothing imported yet")
			}
			defaults, err := importDefaults(st.Preview, mode, choices)
			if err != nil {
				return err
			}
			if mode == "overwrite" && confirmText == "" && !a.agent {
				if err := huh.NewInput().Title("Overwrite replaces existing data.").
					Description("Type the confirmation phrase: " + st.Preview.ConfirmPhrase).
					Value(&confirmText).Run(); err != nil {
					return err
				}
			}
			if !a.agent && mode == "append" {
				ok := false
				if err := huh.NewConfirm().Title("Start the import?").Affirmative("Import").Negative("Cancel").Value(&ok).Run(); err != nil {
					return err
				}
				if !ok {
					fmt.Fprintln(ui.Writer(a.io.Err), "Nothing was imported. The preview stays available as job "+jobID+".")
					return nil
				}
			}
			body := map[string]any{"jobId": jobID, "mode": mode, "choices": map[string]any{"defaults": defaults}}
			if confirmText != "" {
				body["confirmText"] = confirmText
			}
			if sourceTZ != "" {
				body["sourceTimezone"] = sourceTZ
			}
			if restartWindow {
				body["restartFiveHourWindow"] = true
			}
			if _, err := a.importCall(ctx, "startCCHubImport", nil, body, nil); err != nil {
				return err
			}
			st, err = a.importWait(ctx, jobID, poll, "completed")
			if err != nil {
				return err
			}
			if st.Status != "completed" {
				return importFailure(st)
			}
			return a.importResult(st, "import completed")
		},
	}
	f := cmd.Flags()
	f.StringVar(&sourceURL, "source-url", "", "CC Hub server address to fetch the export from.")
	f.StringVar(&adminTokenFile, "admin-token-file", "", "File holding the CC Hub admin token (or set CCHP_CCHUB_ADMIN_TOKEN).")
	f.StringVar(&file, "file", "", "CC Hub export file to upload instead of fetching it.")
	f.StringVar(&jobID, "job", "", "Continue from this preview job.")
	f.StringVar(&mode, "mode", "append", "append adds to what is here; overwrite replaces it.")
	f.StringArrayVar(&choices, "choice", nil, "What to do with one kind of conflict, as type=choice, for example provider=skip.")
	f.StringVar(&confirmText, "confirm-text", "", "The preview's confirmation phrase (overwrite mode).")
	f.StringVar(&sourceTZ, "source-timezone", "", "IANA time zone of the CC Hub server, when the preview asks for it.")
	f.BoolVar(&restartWindow, "restart-five-hour-window", false, "Restart the five-hour usage window, when the preview asks for it.")
	f.BoolVar(&previewOnly, "preview-only", false, "Stop after the preview.")
	f.DurationVar(&poll, "poll", 2*time.Second, "How often to check the job.")
	f.DurationVar(&wait, "wait", 30*time.Minute, "Give up waiting after this long.")
	return cmd
}

func (a *app) importStart(ctx context.Context, sourceURL, adminTokenFile, file string) (string, error) {
	var raw json.RawMessage
	var err error
	switch {
	case file != "" && sourceURL != "":
		return "", contract.Fail(contract.KindUsage, contract.ExitUsage, "use --file or --source-url, not both")
	case file != "":
		raw, err = a.importCall(ctx, "uploadCCHubImport", map[string]string{"file": file}, nil, nil)
	case sourceURL != "":
		token := os.Getenv("CCHP_CCHUB_ADMIN_TOKEN")
		if adminTokenFile != "" {
			data, rerr := os.ReadFile(adminTokenFile)
			if rerr != nil {
				return "", contract.Fail(contract.KindUsage, contract.ExitUsage, "reading --admin-token-file: %v", rerr)
			}
			token = strings.TrimSpace(string(data))
		}
		if token == "" {
			return "", contract.Fail(contract.KindUsage, contract.ExitUsage, "--source-url needs the CC Hub admin token in --admin-token-file or CCHP_CCHUB_ADMIN_TOKEN")
		}
		raw, err = a.importCall(ctx, "previewCCHubImport", nil, map[string]any{"sourceUrl": sourceURL, "adminToken": token}, nil)
	default:
		return "", contract.Fail(contract.KindUsage, contract.ExitUsage, "give --source-url (with --admin-token-file), --file, or --job")
	}
	if err != nil {
		return "", err
	}
	var st importStatus
	if err := json.Unmarshal(raw, &st); err != nil {
		return "", err
	}
	return st.JobID, nil
}

// importCall runs one import operation through the normal runner path.
func (a *app) importCall(ctx context.Context, id string, files map[string]string, body any, params map[string][]string) (json.RawMessage, error) {
	op, err := a.lookup(id)
	if err != nil {
		return nil, err
	}
	opts := runner.Options{Op: op, Params: params, FormFiles: files, Agent: a.agent, AllowWrite: true, HumanConfirmed: true, Confirm: a.confirm, Timeout: a.timeout}
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		opts.Body = data
	}
	if op.Policy.Risk == catalog.RiskDestructive {
		opts.Confirm = op.ID
	}
	res, err := a.runner.Run(ctx, opts)
	if err != nil {
		return nil, err
	}
	return res.Data, nil
}

func (a *app) importWait(ctx context.Context, jobID string, poll time.Duration, want string) (*importStatus, error) {
	lastPhase := ""
	for {
		raw, err := a.importCall(ctx, "getCCHubImportStatus", nil, nil, map[string][]string{"job-id": {jobID}})
		if err != nil {
			return nil, err
		}
		var st importStatus
		if err := json.Unmarshal(raw, &st); err != nil {
			return nil, err
		}
		if !a.agent && st.Phase != lastPhase {
			fmt.Fprintln(ui.Writer(a.io.Err), ui.Muted.Render(fmt.Sprintf("  %s… %d%%", st.Phase, st.Progress)))
			lastPhase = st.Phase
		}
		switch st.Status {
		case want, "failed", "cancelled", "completed":
			return &st, nil
		}
		select {
		case <-ctx.Done():
			return nil, contract.Fail(contract.KindUnknownOutcome, contract.ExitUnknown, "stopped waiting for import job %s; it keeps running on the server", jobID).
				WithHint("Check it later with: cchp import status %s", jobID)
		case <-time.After(poll):
		}
	}
}

func importDefaults(p *importPreview, mode string, choices []string) (map[string]string, error) {
	defaults := map[string]string{}
	allowed := map[string][]string{}
	if p != nil {
		for _, c := range p.Conflicts {
			allowed[c.Type] = c.Allowed
		}
		for _, cc := range p.ConflictCounts {
			if cc.Mode != mode || cc.Count == 0 {
				continue
			}
			// Leave existing data alone unless the caller chose otherwise.
			if contains(allowed[cc.Type], "keep") || cc.Type == "quota" {
				defaults[cc.Type] = "keep"
			} else {
				defaults[cc.Type] = "skip"
			}
		}
	}
	for _, c := range choices {
		t, v, ok := strings.Cut(c, "=")
		if !ok || t == "" || v == "" {
			return nil, contract.Fail(contract.KindUsage, contract.ExitUsage, "--choice takes type=choice, got %q", c)
		}
		defaults[t] = v
	}
	return defaults, nil
}

func contains(list []string, s string) bool {
	return slices.Contains(list, s)
}

func (a *app) showPreview(st *importStatus) {
	w := ui.Writer(a.io.Err)
	p := st.Preview
	if p == nil {
		return
	}
	fmt.Fprintln(w, ui.Title.Render("Preview of import "+st.JobID))
	fmt.Fprintf(w, "  %d users, %d user keys, %d providers, %d provider keys\n", p.UserCount, p.UserKeyCount, p.ProviderCount, p.ProviderKeyCount)
	var lines []string
	for _, cc := range p.ConflictCounts {
		if cc.Count > 0 {
			lines = append(lines, fmt.Sprintf("  %s (%s): %d already here", cc.Type, cc.Mode, cc.Count))
		}
	}
	sort.Strings(lines)
	for _, l := range lines {
		fmt.Fprintln(w, l)
	}
	for _, warning := range p.Warnings {
		fmt.Fprintln(w, ui.Warning.Render("  ! ")+warning)
	}
}

func importFailure(st *importStatus) error {
	f := contract.Fail(contract.KindServer, contract.ExitServer, "import job %s ended as %s: %s", st.JobID, st.Status, nonEmptyText(st.ErrorDetail, "no detail"))
	f.Err.Code = st.ErrorCode
	f.Err.Retryable = st.Retryable
	if st.Retryable {
		f.Err.Hint = "Retry it with: cchp import retry " + st.JobID + " --allow-write"
	}
	return f
}

func nonEmptyText(s, alt string) string {
	if strings.TrimSpace(s) == "" {
		return alt
	}
	return s
}

func (a *app) importResult(st *importStatus, message string) error {
	data, err := json.Marshal(st)
	if err != nil {
		return err
	}
	if a.agent {
		res := &contract.Result{OK: true, Operation: "startCCHubImport", Data: data}
		if st.Status == "completed" {
			res.Outcome = contract.OutcomeApplied
		}
		return contract.Write(a.io.Out, res)
	}
	fmt.Fprintln(ui.Writer(a.io.Out), ui.Success.Render("✓ ")+message+" (job "+st.JobID+")")
	return nil
}
