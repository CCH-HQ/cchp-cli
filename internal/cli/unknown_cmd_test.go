package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/CCH-HQ/cchp-cli/internal/contract"
)

func runUsage(t *testing.T, args ...string) (int, map[string]any) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("CCHP_CONFIG_DIR", dir+"/config")
	t.Setenv("CCHP_CACHE_DIR", dir+"/cache")
	t.Setenv("CCHP_CREDENTIAL_STORE", "file")
	t.Setenv("CCHP_TOKEN", "")
	t.Setenv("CCHP_TOKEN_FILE", "")
	t.Setenv("CCHP_BASE_URL", "")

	var out, errOut bytes.Buffer
	code := Run(args, IO{In: strings.NewReader(""), Out: &out, Err: &errOut}, BuildInfo{Version: "test"})
	var res map[string]any
	if err := json.Unmarshal(out.Bytes(), &res); err != nil {
		t.Fatalf("cchp %v printed %q (stderr %q): %v", args, out.String(), errOut.String(), err)
	}
	return code, res
}

func TestUnknownCommandWithFlagsReportsUnknownCommand(t *testing.T) {
	cases := []struct {
		args    []string
		message string
		hintSub string
	}{
		{
			args:    []string{"bogus", "--lang", "zh-CN"},
			message: `unknown command "bogus" for "cchp"`,
			hintSub: "cchp commands --search",
		},
		{
			args:    []string{"--json", "bogus", "--lang", "zh-CN"},
			message: `unknown command "bogus" for "cchp"`,
		},
		{
			args:    []string{"--timeout", "5s", "bogus", "--lang", "zh-CN"},
			message: `unknown command "bogus" for "cchp"`,
		},
		{
			args:    []string{"bogus", "-x"},
			message: `unknown command "bogus" for "cchp"`,
		},
		{
			args:    []string{"providers", "bogus", "--limit", "10"},
			message: `There is no command "bogus" in cchp providers`,
			hintSub: "cchp providers --help",
		},
		{
			args:    []string{"providers", "lis", "--limit", "10"},
			message: `There is no command "lis" in cchp providers`,
			hintSub: "Did you mean cchp providers list?",
		},
		{
			args:    []string{"api", "bogusOp", "--provider-id", "prov_1"},
			message: `there is no operation "bogusOp"`,
			hintSub: "cchp commands --search",
		},
		{
			args:    []string{"docs", "bogus", "--limit", "5"},
			message: `There is no command "bogus" in cchp docs`,
			hintSub: "cchp docs --help",
		},
		{
			args:    []string{"docs", "--lang", "zh-CN", "bogus", "--limit", "5"},
			message: `There is no command "bogus" in cchp docs`,
		},
		{
			args:    []string{"skill", "bogus", "--project"},
			message: `unknown command "bogus" for "cchp skill"`,
		},
		{
			args:    []string{"commands", "bogus", "--foo"},
			message: `unknown command "bogus" for "cchp commands"`,
		},
		{
			args:    []string{"completion", "bogus", "--foo"},
			message: `unknown command "bogus" for "cchp completion"`,
		},
	}

	for _, tc := range cases {
		code, res := runUsage(t, tc.args...)
		e, _ := res["error"].(map[string]any)
		if code != contract.ExitUsage || res["ok"] != false || e["kind"] != contract.KindUsage || e["message"] != tc.message {
			t.Errorf("cchp %v: got exit %d err=%v, want message %q", tc.args, code, e, tc.message)
		}
		if tc.hintSub != "" {
			hint, _ := e["hint"].(string)
			if !strings.Contains(hint, tc.hintSub) {
				t.Errorf("cchp %v: hint %q does not contain %q", tc.args, hint, tc.hintSub)
			}
		}
	}
}

func TestUnknownFlagStillReportsUnknownFlag(t *testing.T) {
	cases := []struct {
		args    []string
		message string
	}{
		{[]string{"--bogus"}, "unknown flag: --bogus"},
		{[]string{"--bogus", "providers"}, "unknown flag: --bogus"},
		{[]string{"-x", "bogus"}, "unknown shorthand flag: 'x' in -x"},
		{[]string{"providers", "--bogus", "list"}, "unknown flag: --bogus"},
		{[]string{"providers", "list", "--bogus"}, "unknown flag: --bogus"},
		{[]string{"docs", "search", "proxy", "--bogus"}, "unknown flag: --bogus"},
	}

	for _, tc := range cases {
		code, res := runUsage(t, tc.args...)
		e, _ := res["error"].(map[string]any)
		if code != contract.ExitUsage || res["ok"] != false || e["kind"] != contract.KindUsage || e["message"] != tc.message {
			t.Errorf("cchp %v: got exit %d err=%v, want message %q", tc.args, code, e, tc.message)
		}
	}
}
