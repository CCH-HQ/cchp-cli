# Updating the spec

cchp is built from a pinned copy of the CCHP control-plane spec. Everything the
CLI knows about operations is generated from four files in `spec/`:

| File | Edited by | Holds |
| --- | --- | --- |
| `spec/upstream/control-plane.yaml` | `make sync-spec` only | the spec exactly as CCHP exports it |
| `spec/source.lock.json` | `make sync-spec` only | upstream tag and commit, the spec's sha256, the Restish version |
| `spec/cli-overlay.yaml` | people | command names, groups, English text, exclusions (OpenAPI Overlay 1.0) |
| `spec/operation-policy.yaml` | people | risk, retry, permission, secret fields and pagination per operation |

`make generate` (`go run ./cmd/cchp-gen`) turns them into:

- `internal/assets/catalog.json.gz`: the operation index behind discovery,
  help and policy;
- `internal/assets/schemas.json.gz`: bundled request and response JSON Schemas;
- `internal/assets/engine-spec.json.gz`: the spec the embedded Restish engine
  loads;
- `docs/coverage.md`.

The generator also runs every available operation through Restish and marks
any it cannot build as unsupported, with Restish's message.

## Move to a new CCHP release

```sh
make sync-spec UPSTREAM=v1.0.7                 # clone that tag and export
make sync-spec UPSTREAM=../claude-code-hub-plus # or export from a checkout
```

`scripts/sync-spec.sh` runs `go run ./cmd/openapi-export` inside the CCHP tree,
writes `spec/upstream/control-plane.yaml`, records the tag, commit and sha256 in
`spec/source.lock.json`, and runs the generator.

The generator then lists what needs attention, for example:

```
cchp-gen: unexplained or invalid operations:
  getNewThing (GET /api/admin/new-thing): x-cchp-group is missing in spec/cli-overlay.yaml
  getNewThing (GET /api/admin/new-thing): x-cli-description must hold an English summary
```

For each new operation:

1. Add an action to `spec/cli-overlay.yaml` with `x-cchp-group`,
   `x-cli-description` and, if the default name reads badly, `x-cli-name`.
2. If it should not be in the CLI, set `x-cli-ignore: true` and
   `x-cchp-exclude-reason`. Operations without a management-token security
   option are excluded with a reason automatically. The overlay never adds
   management-token support the server lacks.
3. Give new parameters an English `x-cli-description`.
4. Classify it in `spec/operation-policy.yaml`. Until then it counts as a write.

Run `make generate`, review `docs/coverage.md`, and commit everything.

## Checks in CI

`make check-generated` (`cchp-gen -check`) fails when:

- `spec/upstream/control-plane.yaml` does not match the sha256 in the lock file;
- `go.mod` pins a different Restish version than the lock file;
- an operation has no outcome, no group, no English summary, or an excluded or
  unsupported operation has no reason;
- the policy names an operation, field or parameter the spec does not have;
- any generated file differs from what is committed.

`go test ./spec/` checks the lock and the embedded catalog the same way.

The CI workflow runs four jobs side by side on every pull request and every push
to `main`: build and test (`go vet`, `go test -race`), lint (golangci-lint),
spec lock and coverage (`cchp-gen -check`), and a release build of every target.
The `CI result` job passes only when all four pass.

`make e2e` runs locally. It starts a CCHP server from a source tree at the
pinned tag (`CCHP_SRC`), and the CCHP source is private, so CI does not run it.
Run it before every spec update and every release.

## Upgrade Restish

Update `github.com/rest-sh/restish/v2` in `go.mod`, run
`go run ./cmd/cchp-gen -lock -tag <tag> -commit <commit>` with the current
upstream values to record the new version, then `make generate` and the tests.
