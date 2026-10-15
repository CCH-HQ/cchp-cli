# Output contract

This page is the contract between cchp and the programs that call it.
`schema_version` changes only when an existing field changes meaning; new
fields can appear at any time.

## When it applies

Agent mode is on when `--json` is given, `CCHP_AGENT=1` is set, or standard
output is not a terminal. Then:

- stdout carries exactly one JSON result object, ending with a newline;
- streams first print one JSON line per event (NDJSON), then the result object;
- stderr carries diagnostics, including a one-line message on failure;
- the exit code is 0 on success and nonzero on any failure;
- there are no prompts, pagers or colors, even on a terminal.

`cchp skill show` prints raw Markdown unless `--json` is given, so it can be
redirected into a file.

## The result object

```json
{
  "schema_version": 1,
  "ok": true,
  "operation": "listOrgProviders",
  "status": 200,
  "request_id": "6f0c2b0e-…",
  "data": { "providers": [ … ], "nextCursor": "…" },
  "pagination": { "pages": 1, "items": 50, "has_more": true, "next_cursor": "…" }
}
```

| Field | Present when | Meaning |
| --- | --- | --- |
| `schema_version` | always | `1` |
| `ok` | always | `true` on success |
| `operation` | an operation ran | its operationId |
| `status` | the server answered | the final HTTP status |
| `request_id` | the server answered | from `X-Request-Id` or the error body |
| `data` | the response had a body | JSON bodies as sent by the server, numbers exact; text bodies as a JSON string |
| `pagination` | list operations | see below |
| `outcome` | the operation changes data | `applied`, `not_applied` or `unknown` |
| `idempotency_key` | the operation takes one | the key sent; cchp generates one when you give none |
| `secret` | `--secret-out` was used | `path` and the `fields` saved there |
| `file` | a download | `path`, `bytes`, `sha256`, `content_type` |
| `stream` | a stream | `events` delivered and `ended`: `max_events`, `deadline` or `server_closed` |
| `dry_run` | `--dry-run` | `method`, `url`, `headers`, `body` or `body_text`, and `policy` |
| `error` | `ok` is false | see below |
| `warnings` | sometimes | notes that did not fail the run |

Discovery and support commands (`commands`, `schema`, `version`, `guide`,
`status`, `login`, `logout`, `skill install`) use the same object with their
content in `data`.

### Pagination

```json
{ "pages": 2, "items": 150, "has_more": true, "next_cursor": "…", "total": 412, "truncated": false }
```

- `has_more` with `next_cursor` (cursor lists) or `next_page` (numbered lists)
  says how to continue.
- With more than one page, `data` is the last page with its item list holding
  every item fetched.
- `truncated` is true when `--max-items` cut a page short.

### Streams

```json
{"type":"event","operation":"streamSystemHealth","seq":1,"event":"metrics","id":"42","data":{"rpm":12}}
```

`event` and `id` appear when the server sent them.

## Errors

```json
{
  "schema_version": 1,
  "ok": false,
  "status": 403,
  "request_id": "…",
  "error": {
    "kind": "permission_denied",
    "message": "Your token can't do this. Ask an admin for the member:read permission.",
    "hint": "…",
    "code": 10004,
    "required_permission": "member:read",
    "retryable": false,
    "problem": { … }
  }
}
```

| `error` field | Meaning |
| --- | --- |
| `kind` | stable category, below |
| `message` | one plain sentence |
| `hint` | what to do next |
| `code` | CCHP's numeric error code |
| `detail` | the server's own detail text |
| `required_permission` | the permission a 403 needed, when known |
| `challenge_action` | the action a 428 human check guards |
| `fields` | `location` and `message` per bad input, such as `body.name` |
| `retryable` | whether running the same command again can succeed |
| `problem` | the server's problem+json body, secrets masked |

### Kinds and exit codes

| Exit | `kind` | Meaning |
| --- | --- | --- |
| 0 | | success |
| 1 | `internal`, `http_error` | unexpected failure or an unusual HTTP status |
| 2 | `usage` | unknown command or flag, missing input, operation cchp does not run |
| 3 | `unauthorized` | the server rejected the token (401) |
| 4 | `permission_denied` | the token lacks a permission (403) |
| 5 | `not_found` | 404 |
| 6 | `conflict` | the current state does not allow it (409, 412) |
| 7 | `invalid_input`, `validation` | cchp's schema check failed, or the server rejected the input (400, 422) |
| 8 | `browser_session_required`, `human_verification_required` | needs the browser, or a human check (428) |
| 9 | `rate_limited` | 429 |
| 10 | `server_error` | 5xx |
| 11 | `not_sent` | nothing reached the server |
| 12 | `unknown_outcome` | the request may have reached the server |
| 13 | `policy` | refused by cchp's safety policy |
| 14 | `not_signed_in` | no credential |

## Input

- Parameters are flags in kebab case (`providerId` → `--provider-id`).
  Positional arguments fill path parameters in order.
- Booleans accept `--flag`, `--flag=true` and `--flag=false`.
- Repeat a flag for array values.
- JSON bodies: `--body-file <file>` or `--body-file -`. The body must be one
  JSON document. cchp checks it against the operation's schema and sends the
  bytes unchanged.
- Multipart uploads: `--form-file <field>=<path>`. Binary uploads:
  `--body-file <path>`.

## Time limits

`--timeout` (default 60s) bounds the whole run, including several pages. Reads
are retried twice on network failures and 5xx answers; changes never are.

When the time runs out before the request leaves cchp, the result is
`not_sent` and cchp sends nothing afterwards. When it runs out after the request
left and before the answer came, a change reports `unknown_outcome`.
