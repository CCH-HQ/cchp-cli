package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/CCH-HQ/cchp-cli/internal/contract"
)

// runDocs runs cchp in agent mode with an isolated home and no language or
// docs site from the environment.
func runDocs(t *testing.T, env map[string]string, args ...string) (int, map[string]any) {
	t.Helper()
	dir := t.TempDir()
	for k, v := range map[string]string{
		"CCHP_CONFIG_DIR": dir + "/config", "CCHP_CACHE_DIR": dir + "/cache", "CCHP_CREDENTIAL_STORE": "file",
		"CCHP_DOCS_LANG": "", "CCHP_DOCS_URL": "", "LC_ALL": "", "LC_MESSAGES": "", "LANG": "",
		"CCHP_TOKEN": "", "CCHP_TOKEN_FILE": "", "CCHP_BASE_URL": "",
	} {
		t.Setenv(k, v)
	}
	for k, v := range env {
		t.Setenv(k, v)
	}
	var out, errOut bytes.Buffer
	code := Run(args, IO{In: strings.NewReader(""), Out: &out, Err: &errOut}, BuildInfo{Version: "test"})
	var res map[string]any
	if err := json.Unmarshal(out.Bytes(), &res); err != nil {
		t.Fatalf("cchp %v printed %q: %v", args, out.String(), err)
	}
	return code, res
}

func TestDocsDryRunsShowTheRequest(t *testing.T) {
	cases := []struct {
		args []string
		env  map[string]string
		url  string
	}{
		{[]string{"docs", "index", "--lang", "zh-cn"}, nil, "https://cch-plus.com/zh-CN/llms.txt"},
		{[]string{"docs", "full"}, map[string]string{"LANG": "zh_HK.UTF-8"}, "https://cch-plus.com/zh-TW/llms-full.txt"},
		{[]string{"docs", "read", "installation/docker-compose"}, map[string]string{"CCHP_DOCS_LANG": "ja", "LANG": "ko_KR.UTF-8"}, "https://cch-plus.com/ja/docs/installation/docker-compose.md"},
		{[]string{"docs", "read", "https://cch-plus.com/ru/docs/guide/codex"}, map[string]string{"CCHP_DOCS_URL": "http://127.0.0.1:8787"}, "http://127.0.0.1:8787/ru/docs/guide/codex.md"},
		{[]string{"docs", "search", "网络", "代理", "--limit", "5"}, map[string]string{"LC_ALL": "zh_CN.UTF-8"}, "https://cch-plus.com/api/docs/search?lang=zh-CN&limit=5&q=%E7%BD%91%E7%BB%9C+%E4%BB%A3%E7%90%86"},
		{[]string{"docs", "search", "proxy", "--docs-url", "https://preview.example.dev"}, nil, "https://preview.example.dev/api/docs/search?lang=en&q=proxy"},
	}
	for _, c := range cases {
		code, res := runDocs(t, c.env, append(c.args, "--dry-run", "--json")...)
		dry, _ := res["dry_run"].(map[string]any)
		headers, _ := dry["headers"].(map[string]any)
		if code != 0 || dry["method"] != "GET" || dry["url"] != c.url || headers["User-Agent"] != "cchp/test" || headers["Authorization"] != nil {
			t.Errorf("%v: exit %d %v", c.args, code, res)
		}
	}
}

func TestDocsUsageFailures(t *testing.T) {
	cases := []struct {
		args    []string
		env     map[string]string
		message string
	}{
		{[]string{"docs", "index", "--lang", "de"}, nil, `--lang "de" is not a docs language`},
		{[]string{"docs", "full"}, map[string]string{"CCHP_DOCS_LANG": "fr"}, `CCHP_DOCS_LANG "fr" is not a docs language`},
		{[]string{"docs", "read", "https://cch-plus.com/en/docs/installation", "--lang", "ja"}, nil, "the URL is a page in en, but --lang asks for ja"},
		{[]string{"docs", "read", "https://example.com/en/docs/installation"}, nil, `cannot read "https://example.com/en/docs/installation": cchp reads docs pages of https://cch-plus.com and cch-plus.com only`},
		{[]string{"docs", "search", "x", "--limit", "0"}, nil, "--limit must be between 1 and 50"},
		{[]string{"docs", "search", "x", "--read-top", "51"}, nil, "--read-top must be between 0 and 50"},
		{[]string{"docs", "search", " "}, nil, "search needs words to look for"},
		{[]string{"docs", "index", "--docs-url", "http://docs.example.com"}, nil, `the docs site "http://docs.example.com" needs https://; plain http:// is accepted only for this computer`},
		{[]string{"docs", "nope"}, nil, `There is no command "nope" in cchp docs`},
	}
	for _, c := range cases {
		code, res := runDocs(t, c.env, append(c.args, "--json")...)
		e, _ := res["error"].(map[string]any)
		if code != contract.ExitUsage || res["ok"] != false || e["kind"] != contract.KindUsage || e["message"] != c.message {
			t.Errorf("%v: exit %d %v", c.args, code, res)
		}
	}
}
