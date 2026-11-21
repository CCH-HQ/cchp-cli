---
name: cchp
description: Manage a CCHP (Claude Code Hub Plus) server with the cchp CLI - read usage, request logs, providers, provider keys, LLM tokens, members and settings, make changes safely, and look up how CCHP works in its documentation. Use when a task involves a CCHP gateway or its control plane, when the user asks how to install, configure or use CCHP, or when the user mentions cchp, CCHP or Claude Code Hub Plus.
---

# cchp

`cchp` calls every CCHP control-plane operation that accepts a management token.
It knows all operations offline, so look them up before calling. It also reads
the CCHP documentation, with no sign-in.

## When to use it

- Questions about a CCHP server: usage, costs, request logs, providers, keys,
  LLM tokens, members, quotas, settings.
- Changes to that server, when the user asked for them.
- Questions about CCHP itself: how to install it, what a feature does, how to
  configure it. Search the docs first (below).

## Look it up in the docs

```
cchp docs search "<words>" --read-top 1 --json   # best pages, with their Markdown
cchp docs read <slug> --json                     # one page, by the slug a result gives
cchp docs index --json                           # every page by section
```

Answer from the Markdown and cite the page's `url`. The language is `--lang`
(zh-CN, zh-TW, en, ja, ru, ko), then `CCHP_DOCS_LANG`, then the system locale;
answer in the user's language and search in it. Before changing a setting the
docs describe, find its operation with `cchp commands --search`. Recipes:
[references/workflows.md](references/workflows.md).

## Before you start

Check the sign-in: `cchp status --json`. If it fails with `not_signed_in`, ask
the user to run `cchp login`, or to provide `CCHP_BASE_URL` with `CCHP_TOKEN` or
`CCHP_TOKEN_FILE`. Never put a token in a command line. See
[references/auth.md](references/auth.md).

## The loop: discover, read the schema, call

```
cchp commands --search "<words>" --json     # find the operationId
cchp schema <operationId> --json            # parameters, body and response schemas
cchp api <operationId> [--flag value] --json
```

- Parameters are flags named after the parameter, for example `--provider-id`.
  `cchp commands show <operationId> --json` lists them with examples.
- `orgId` defaults to the token's organization.
- JSON bodies go in a file: `--body-file body.json`, or `--body-file -` for
  standard input. cchp validates the body against the schema first and reports
  each problem with its field path, then sends it unchanged.

## Output

One JSON object on stdout; diagnostics on stderr; nonzero exit on failure.
Read `ok`, `data`, `error.kind`, `error.message`, `request_id` and
`pagination`. Details: [references/output-contract.md](references/output-contract.md)
and [references/errors.md](references/errors.md).

Lists return one page. Continue with the cursor in `pagination.next_cursor`, or
fetch several pages with `--page-limit N --max-items M`. See
[references/pagination.md](references/pagination.md).

## Safety flags

- Agent mode only reads. Add `--allow-write` for changes the user asked for.
- Deletions, replacements and operations that return one-time secrets also need
  `--confirm <operationId>`.
- One-time secrets (new tokens, generated codes) need `--secret-out <new file>`;
  the value never appears on stdout. Tell the user where the file is.
- Use `--dry-run` to show the exact request first; nothing is sent.
- Changes are never retried automatically. On `unknown_outcome`, check the
  state before trying again; reuse `idempotency_key` when retrying.
- `human_verification_required` and `browser_session_required` mean the user
  must do the action in the console. Say so plainly; do not look for a way
  around it.

## Multi-step jobs

CC Hub imports have their own command, `cchp import run`. See
[references/workflows.md](references/workflows.md) for it and other recipes.
