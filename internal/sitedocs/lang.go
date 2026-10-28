package sitedocs

import (
	"strings"

	"golang.org/x/text/language"

	"github.com/CCH-HQ/cchp-cli/internal/contract"
)

// Language is one language of the documentation site.
type Language struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

// Languages are the site's languages in the order the site lists them.
var Languages = []Language{
	{Code: "zh-CN", Name: "简体中文"},
	{Code: "zh-TW", Name: "繁體中文"},
	{Code: "en", Name: "English"},
	{Code: "ja", Name: "日本語"},
	{Code: "ru", Name: "Русский"},
	{Code: "ko", Name: "한국어"},
}

// DefaultLanguage is used when nothing else names a language.
const DefaultLanguage = "en"

// LangEnv names the environment variable that sets the docs language.
const LangEnv = "CCHP_DOCS_LANG"

// Where the language came from, reported as lang_source. The environment
// variables report their own names.
const (
	SourceFlag    = "flag"
	SourceURL     = "url"
	SourceDefault = "default"
)

// localeEnvs are read in POSIX order: the first one set is the locale in effect.
var localeEnvs = []string{"LC_ALL", "LC_MESSAGES", "LANG"}

var matcher = func() language.Matcher {
	tags := make([]language.Tag, len(Languages))
	for i, l := range Languages {
		tags[i] = language.MustParse(l.Code)
	}
	return language.NewMatcher(tags)
}()

// Codes lists the language codes.
func Codes() []string {
	out := make([]string, len(Languages))
	for i, l := range Languages {
		out[i] = l.Code
	}
	return out
}

// Canonical returns the code that s names, ignoring case and accepting _ for -.
func Canonical(s string) (string, bool) {
	s = strings.ReplaceAll(strings.TrimSpace(s), "_", "-")
	for _, l := range Languages {
		if strings.EqualFold(l.Code, s) {
			return l.Code, true
		}
	}
	return "", false
}

// Resolve picks the docs language: the --lang flag, then CCHP_DOCS_LANG, then
// the system locale (LC_ALL, LC_MESSAGES or LANG, whichever is set first),
// then English. An unsupported flag or CCHP_DOCS_LANG value is an error; a
// system locale the site has no language for falls through to English.
func Resolve(flag string, getenv func(string) string) (code, source string, err error) {
	if flag != "" {
		code, ok := Canonical(flag)
		if !ok {
			return "", "", unsupported(flag, "--lang")
		}
		return code, SourceFlag, nil
	}
	if v := getenv(LangEnv); v != "" {
		code, ok := Canonical(v)
		if !ok {
			return "", "", unsupported(v, LangEnv)
		}
		return code, LangEnv, nil
	}
	for _, name := range localeEnvs {
		v := getenv(name)
		if v == "" {
			continue
		}
		if code, ok := FromLocale(v); ok {
			return code, name, nil
		}
		break
	}
	return DefaultLanguage, SourceDefault, nil
}

// FromLocale maps a POSIX locale such as zh_CN.UTF-8 or zh_HK to the site
// language that serves it: zh_HK and zh_MO read zh-TW, zh_SG reads zh-CN.
func FromLocale(locale string) (string, bool) {
	if i := strings.IndexAny(locale, ".@"); i >= 0 {
		locale = locale[:i]
	}
	if locale == "" || locale == "C" || locale == "POSIX" {
		return "", false
	}
	tag, err := language.Parse(strings.ReplaceAll(locale, "_", "-"))
	if err != nil {
		return "", false
	}
	_, i, confidence := matcher.Match(tag)
	if confidence < language.High {
		return "", false
	}
	return Languages[i].Code, true
}

func unsupported(value, where string) *contract.Failure {
	return contract.Fail(contract.KindUsage, contract.ExitUsage, "%s %q is not a docs language", where, value).
		WithHint("Use one of: %s.", strings.Join(Codes(), ", "))
}
