GO ?= go
GOLANGCI_LINT ?= golangci-lint
VERSION ?= dev
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: build generate check-generated test lint e2e e2e-docs sync-spec clean

build:
	$(GO) build -trimpath -ldflags '$(LDFLAGS)' -o cchp ./cmd/cchp

# Rebuild the catalog, schemas, engine spec and docs/coverage.md from spec/.
generate:
	$(GO) run ./cmd/cchp-gen

# Fail when generated files are stale or an operation has no explained outcome.
check-generated:
	$(GO) run ./cmd/cchp-gen -check

test:
	$(GO) test ./...

lint:
	$(GO) vet ./...
	$(GOLANGCI_LINT) run ./...

# End-to-end tests against a real CCHP server. CCHP_SRC points at a CCHP source
# tree at the pinned tag; see docs/updating-the-spec.md.
e2e:
	scripts/e2e/up.sh
	scripts/e2e/bootstrap.sh
	scripts/e2e/test.sh

# End-to-end tests of cchp docs in every language against the documentation
# site: https://cch-plus.com, or a preview build named by CCHP_DOCS_URL.
e2e-docs:
	$(GO) test -tags e2e -count=1 ./test/docs/

# Re-export the control-plane spec from a CCHP checkout path or a release tag,
# then refresh the lock file and everything generated from it.
#   make sync-spec UPSTREAM=../claude-code-hub-plus
#   make sync-spec UPSTREAM=v1.0.7
sync-spec:
	@test -n "$(UPSTREAM)" || { echo "usage: make sync-spec UPSTREAM=<path-or-tag>"; exit 2; }
	scripts/sync-spec.sh "$(UPSTREAM)"

clean:
	rm -f cchp
	rm -rf dist
