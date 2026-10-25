package catalog_test

import (
	"strings"
	"testing"

	"github.com/CCH-HQ/cchp-cli/internal/assets"
	"github.com/CCH-HQ/cchp-cli/internal/catalog"
)

// Common searches by people and agents, with the command they are looking for.
var commonSearches = []struct {
	query, operation, command string
}{
	{"provider keys", "listProviderKeyPage", "keys list"},
	{"usage summary", "getUsageSummary", "usage summary"},
	{"override rules", "getRequestOverrideRuleSets", "overrides rules"},
	{"blocked ips", "listIPBlocklist", "admin blocked-ips"},
	{"member balance", "adjustMemberBalance", "billing adjust-member-balance"},
	{"providers", "listOrgProviders", "providers list"},
	{"keys", "listProviderKeyPage", "keys list"},
	{"llm tokens", "listLLMTokens", "llm-tokens list"},
	{"revoke llm token", "revokeLLMToken", "llm-tokens revoke"},
	{"moderation events", "listModerationEvents", "safety moderation-events"},
	{"model aliases", "listModelAliases", "models aliases"},
	{"Provider Keys", "listProviderKeyPage", "keys list"},
}

func TestSearchPutsTheCommandPeopleMeanFirst(t *testing.T) {
	cat := assets.Catalog()
	for _, all := range []bool{false, true} {
		for _, c := range commonSearches {
			hits := cat.Search(c.query, all)
			if len(hits) == 0 {
				t.Errorf("%q (all=%v) found nothing", c.query, all)
				continue
			}
			if hits[0].ID != c.operation || hits[0].Command() != c.command {
				t.Errorf("%q (all=%v) first hit is %s (%s), want %s (%s)", c.query, all, hits[0].ID, hits[0].Command(), c.operation, c.command)
			}
		}
	}
}

// The ranking rests on the command path and the English summary, so a command
// keeps its place when CCHP renames the operation behind it.
func TestSearchRankingSurvivesOperationRenames(t *testing.T) {
	orig := assets.Catalog()
	renamed := &catalog.Catalog{Groups: orig.Groups}
	for _, op := range orig.Operations {
		cp := *op
		cp.ID = "renamedOperation" + op.Method + op.Path
		renamed.Operations = append(renamed.Operations, &cp)
	}
	for _, c := range commonSearches {
		hits := renamed.Search(c.query, false)
		if len(hits) == 0 || hits[0].Command() != c.command {
			got := "nothing"
			if len(hits) > 0 {
				got = hits[0].Command()
			}
			t.Errorf("after renames %q finds %s first, want %s", c.query, got, c.command)
		}
	}
}

func TestSearchFindsAnOperationByItsID(t *testing.T) {
	cat := assets.Catalog()
	for _, q := range []string{"listOrgProviders", "listorgproviders", "getUsageSummary"} {
		hits := cat.Search(q, false)
		if len(hits) == 0 || !strings.EqualFold(hits[0].ID, q) {
			t.Errorf("%q does not find its own operation first", q)
		}
	}
}

func TestSearchSuggestsTheOperationThatReplacedAnOldID(t *testing.T) {
	hits := assets.Catalog().Search("listProviderKeys", true)
	if len(hits) == 0 || hits[0].ID != "listProviderKeyPage" {
		t.Fatalf("listProviderKeys suggests %v, want listProviderKeyPage first", ids(hits))
	}
}

func TestSearchNeedsEveryWord(t *testing.T) {
	cat := assets.Catalog()
	if hits := cat.Search("provider zebra", true); len(hits) != 0 {
		t.Fatalf("a word that matches nothing still found %v", ids(hits))
	}
	if hits := cat.Search("  ", true); len(hits) != 0 {
		t.Fatalf("an empty search found %d operations", len(hits))
	}
}

func TestSearchMatchesPartsOfWords(t *testing.T) {
	hits := assets.Catalog().Search("prov keys", false)
	if len(hits) == 0 || hits[0].ID != "listProviderKeyPage" {
		t.Fatalf("prov keys finds %v first", ids(hits))
	}
}

func ids(ops []*catalog.Operation) []string {
	out := make([]string, 0, 3)
	for i, op := range ops {
		if i == 3 {
			break
		}
		out = append(out, op.ID)
	}
	return out
}
