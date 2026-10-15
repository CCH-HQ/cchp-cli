# Guide for agents

## Agent mode

cchp is in agent mode when any of these holds:

- `--json` is given;
- `CCHP_AGENT=1` is set;
- standard output is not a terminal (a pipe or a file).

In agent mode cchp prints exactly one JSON result object on stdout (a stream
prints one JSON line per event first), writes diagnostics to stderr, exits
nonzero on failure, and never prompts, pages or colors output, even on a
terminal. The full shape is in [output-contract.md](output-contract.md).

## Credentials

```sh
export CCHP_BASE_URL=https://hub.example.com
export CCHP_TOKEN_FILE=/path/to/token          # mode 600
# or: export CCHP_TOKEN=...                    # from a secret manager
# or: printf '%s' "$TOKEN" | cchp login --server "$CCHP_BASE_URL" --token-stdin --json
```

cchp never takes a token as an argument. `cchp status --json` checks the token
with the server.

## Discover, read the schema, call

```sh
cchp commands --search "provider keys" --json
cchp commands show listProviderKeys --json       # inputs, safety flags, examples
cchp schema listProviderKeys --json              # parameters and JSON Schemas
cchp api listProviderKeys --provider-id <id> --json
```

Discovery uses the spec built into cchp, so it works offline.

`cchp api <operationId>` is the stable entry point. Operation IDs come from the
CCHP spec and stay the same across cchp versions. The grouped commands
(`cchp keys list`) run the same code with friendlier names.

## Parameters and bodies

- Every parameter is a flag named after it in kebab case: `providerId` becomes
  `--provider-id`. Repeat a flag for array values.
- `orgId` defaults to the token's own organization.
- JSON bodies come from `--body-file <file>` or `--body-file -` (stdin). cchp
  checks the body against the operation's schema before anything is sent and
  reports each problem with its field path, such as `body.permissions[1]`. It
  then sends the bytes exactly as written: `null`, `false`, `0` and `""` arrive
  unchanged, and text such as `@file` inside JSON stays text.
- File uploads use `--form-file <field>=<path>`.

## Safety flags

| Operation | Needs in agent mode |
| --- | --- |
| reads data | nothing |
| changes data | `--allow-write` |
| deletes or replaces data | `--allow-write --confirm <operationId>` |
| returns a one-time secret | `--allow-write --confirm <operationId> --secret-out <new file>` |

An operation the policy has not classified counts as a change. `--dry-run`
prints the request cchp would send (secrets masked) and sends nothing; it needs
no other flag.

## Pagination

Lists return one page. `pagination.next_cursor` continues it:

```sh
cchp api listUsers --limit 100 --json
cchp api listUsers --limit 100 --cursor "<next_cursor>" --json
cchp api listUsageLogs --page-limit 10 --max-items 500 --json   # several pages
```

## Streams and downloads

- Streams print NDJSON event lines, then the result object. Agent mode stops
  after 20 events unless `--max-events N` says otherwise, or at `--timeout`.
- Operations that return files need `--output <new file>` (`-` for stdout).

## Failures

`error.kind` says what to do; [output-contract.md](output-contract.md) lists
every kind and exit code. Two need special care:

- `unknown_outcome` (exit 12): a change may or may not have happened. cchp never
  retries changes. Check the state first; if you retry, pass the printed
  `--idempotency-key`.
- `browser_session_required` and `human_verification_required` (exit 8): the
  user must do this in the console. Report it; there is no other way.

## Multi-step jobs

`cchp import run` previews, waits, confirms and reports a CC Hub import. See
`cchp import run --help` and the skill's workflows reference.

## Skill and AGENTS.md

```sh
cchp skill install --project          # .claude/skills/cchp in this project
cchp skill install --user             # ~/.claude/skills/cchp for every project
cchp skill show                       # print SKILL.md
cchp skill show --file references/errors.md
cchp skill show --agents-md           # a snippet for a project's AGENTS.md
```

The skill keeps SKILL.md short and links to references for authentication, the
output contract, pagination, errors and workflows, so an agent loads only what
it needs.
