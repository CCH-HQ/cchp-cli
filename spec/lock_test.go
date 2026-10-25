package spec_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"golang.org/x/mod/modfile"

	"github.com/CCH-HQ/cchp-cli/internal/assets"
	"github.com/CCH-HQ/cchp-cli/internal/catalog"
)

type lock struct {
	Upstream struct {
		Tag    string `json:"tag"`
		Commit string `json:"commit"`
	} `json:"upstream"`
	Spec struct {
		Path   string `json:"path"`
		SHA256 string `json:"sha256"`
	} `json:"spec"`
	Restish struct {
		Module  string `json:"module"`
		Version string `json:"version"`
	} `json:"restish"`
}

func readLock(t *testing.T) lock {
	t.Helper()
	data, err := os.ReadFile("source.lock.json")
	if err != nil {
		t.Fatal(err)
	}
	var l lock
	if err := json.Unmarshal(data, &l); err != nil {
		t.Fatal(err)
	}
	return l
}

func TestUpstreamSpecMatchesTheLock(t *testing.T) {
	l := readLock(t)
	data, err := os.ReadFile("../" + l.Spec.Path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	if got := hex.EncodeToString(sum[:]); got != l.Spec.SHA256 {
		t.Fatalf("%s changed: sha256 %s, lock says %s. The upstream spec stays exactly as exported; use make sync-spec.", l.Spec.Path, got, l.Spec.SHA256)
	}
	if !strings.HasPrefix(l.Upstream.Tag, "v") || len(l.Upstream.Commit) != 40 {
		t.Fatalf("lock upstream %+v", l.Upstream)
	}
}

func TestRestishVersionMatchesGoMod(t *testing.T) {
	l := readLock(t)
	data, err := os.ReadFile("../go.mod")
	if err != nil {
		t.Fatal(err)
	}
	mf, err := modfile.Parse("go.mod", data, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range mf.Require {
		if r.Mod.Path == l.Restish.Module {
			if r.Mod.Version != l.Restish.Version {
				t.Fatalf("go.mod has %s %s, lock says %s", r.Mod.Path, r.Mod.Version, l.Restish.Version)
			}
			return
		}
	}
	t.Fatalf("go.mod does not require %s", l.Restish.Module)
}

func TestEmbeddedCatalogComesFromTheLockedSpec(t *testing.T) {
	l := readLock(t)
	src := assets.Catalog().Source
	if src.SpecSHA256 != l.Spec.SHA256 || src.Tag != l.Upstream.Tag || src.RestishVersion != l.Restish.Version {
		t.Fatalf("embedded catalog source %+v does not match the lock; run make generate", src)
	}
}

func TestEveryOperationHasAnExplainedOutcome(t *testing.T) {
	c := assets.Catalog()
	if len(c.Operations) != 681 {
		t.Fatalf("catalog has %d operations; the v1.0.12 spec has 681", len(c.Operations))
	}
	for _, op := range c.Operations {
		switch op.Status {
		case catalog.StatusAvailable:
			if op.Summary == "" || op.Group == "" || op.Name == "" {
				t.Errorf("%s lacks a summary or command name", op.ID)
			}
		case catalog.StatusExcluded, catalog.StatusUnsupported:
			if op.Reason == "" {
				t.Errorf("%s is %s without a reason", op.ID, op.Status)
			}
		default:
			t.Errorf("%s has status %q", op.ID, op.Status)
		}
	}
}
