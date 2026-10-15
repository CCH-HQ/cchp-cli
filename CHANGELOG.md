# Changelog

## v0.1.0

First release, built from the CCHP v1.0.6 control-plane spec with embedded
Restish v2.3.0.

### For people

- `cchp` opens a home screen: usage, providers, provider keys, LLM tokens,
  recent requests and guides, with search and detail views.
- `cchp login` asks for the server address (checked as you type) and the
  token (hidden), checks them with the server, and saves the token in the
  system keychain or a private file. `cchp status` and `cchp logout` complete
  the set.
- Every operation that accepts a management token is a command in a plain
  group, such as `cchp providers list` or `cchp requests get`.
- Errors say what happened and what to do, with the request ID in small print.
- Destructive and secret-producing commands ask before they run.
- `cchp guide` prints short guides.

### For agents

- Agent mode (`--json`, `CCHP_AGENT=1` or piped output) prints one JSON result
  object with the payload, operation, status, request ID and pagination, and
  exits nonzero on failure.
- `cchp commands --search`, `cchp commands show` and `cchp schema` work offline.
- `cchp api <operationId>` calls any of the 464 available operations.
- JSON bodies are checked against the schema with field paths, then sent byte
  for byte.
- Read-only by default; `--allow-write`, `--confirm <operationId>` and
  `--secret-out` gate changes, deletions and one-time secrets. `--dry-run` sends
  nothing.
- One page by default, with explicit cursors, `--page-limit`, `--max-items` and
  `--timeout`. Streams print NDJSON. Downloads need `--output`.
- Changes are never retried; an unanswered change is reported as
  `unknown_outcome` with its idempotency key.
- `cchp skill install` installs the Claude Code skill; `cchp skill show
  --agents-md` prints an AGENTS.md snippet.
- `cchp import run` previews, confirms, waits for and reports a CC Hub import.

### Spec and coverage

- 562 operations: 464 available, 98 excluded with reasons, 0 unsupported. See
  [docs/coverage.md](docs/coverage.md).
- The upstream spec is pinned by sha256 in `spec/source.lock.json`;
  `make sync-spec` re-exports it from a CCHP checkout or tag.

### Releases

- Static binaries for Linux, macOS and Windows on amd64 and arm64, with
  checksums. `install.sh` verifies the checksum before installing.
