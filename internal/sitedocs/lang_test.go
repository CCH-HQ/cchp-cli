package sitedocs

import (
	"errors"
	"testing"

	"github.com/CCH-HQ/cchp-cli/internal/contract"
)

func TestFromLocaleMapsSystemLocales(t *testing.T) {
	cases := map[string]string{
		"zh_CN.UTF-8":      "zh-CN",
		"zh_CN":            "zh-CN",
		"zh_SG.GB2312":     "zh-CN",
		"zh":               "zh-CN",
		"zh_TW.UTF-8":      "zh-TW",
		"zh_HK.UTF-8":      "zh-TW",
		"zh_MO":            "zh-TW",
		"zh_Hant_HK":       "zh-TW",
		"en_US.UTF-8":      "en",
		"en_GB.UTF-8@euro": "en",
		"ja_JP.eucJP":      "ja",
		"ru_RU.KOI8-R":     "ru",
		"ko_KR.UTF-8":      "ko",
	}
	for locale, want := range cases {
		got, ok := FromLocale(locale)
		if !ok || got != want {
			t.Errorf("FromLocale(%q) = %q, %v; want %q", locale, got, ok, want)
		}
	}
	for _, locale := range []string{"", "C", "C.UTF-8", "POSIX", "de_DE.UTF-8", "pt_BR", "uk_UA.UTF-8", "not a locale"} {
		if got, ok := FromLocale(locale); ok {
			t.Errorf("FromLocale(%q) = %q; want no language", locale, got)
		}
	}
}

func env(vars map[string]string) func(string) string {
	return func(name string) string { return vars[name] }
}

func TestResolveOrder(t *testing.T) {
	all := map[string]string{LangEnv: "ja", "LC_ALL": "ko_KR.UTF-8", "LC_MESSAGES": "ru_RU.UTF-8", "LANG": "zh_TW.UTF-8"}
	cases := []struct {
		flag       string
		vars       map[string]string
		code, from string
	}{
		{"zh-cn", all, "zh-CN", SourceFlag},
		{"ZH_TW", nil, "zh-TW", SourceFlag},
		{"", all, "ja", LangEnv},
		{"", map[string]string{"LC_ALL": "ko_KR.UTF-8", "LC_MESSAGES": "ru_RU.UTF-8", "LANG": "zh_TW.UTF-8"}, "ko", "LC_ALL"},
		{"", map[string]string{"LC_MESSAGES": "ru_RU.UTF-8", "LANG": "zh_TW.UTF-8"}, "ru", "LC_MESSAGES"},
		{"", map[string]string{"LANG": "zh_HK.UTF-8"}, "zh-TW", "LANG"},
		// The first locale variable set is the locale in effect, even when the site has no language for it.
		{"", map[string]string{"LC_ALL": "C.UTF-8", "LANG": "zh_CN.UTF-8"}, "en", SourceDefault},
		{"", map[string]string{"LANG": "de_DE.UTF-8"}, "en", SourceDefault},
		{"", nil, "en", SourceDefault},
	}
	for _, c := range cases {
		code, from, err := Resolve(c.flag, env(c.vars))
		if err != nil || code != c.code || from != c.from {
			t.Errorf("Resolve(%q, %v) = %q, %q, %v; want %q, %q", c.flag, c.vars, code, from, err, c.code, c.from)
		}
	}
}

func TestResolveRejectsUnsupportedLanguages(t *testing.T) {
	for _, c := range []struct {
		flag string
		vars map[string]string
		msg  string
	}{
		{"de", nil, `--lang "de" is not a docs language`},
		{"", map[string]string{LangEnv: "zh"}, `CCHP_DOCS_LANG "zh" is not a docs language`},
	} {
		_, _, err := Resolve(c.flag, env(c.vars))
		f, ok := errors.AsType[*contract.Failure](err)
		if !ok || f.Exit != contract.ExitUsage || f.Err.Kind != contract.KindUsage || f.Err.Message != c.msg ||
			f.Err.Hint != "Use one of: zh-CN, zh-TW, en, ja, ru, ko." {
			t.Errorf("Resolve(%q, %v) error %#v", c.flag, c.vars, err)
		}
	}
}
