# Output contract

Agent mode is on with `--json`, `CCHP_AGENT=1`, or when stdout is not a terminal.

## Result object

Every run prints one object on stdout:

```json
{
  "schema_version": 1,
  "ok": true,
  "operation": "listOrgProviders",
  "status": 200,
  "request_id": "6f0c…",
  "data": { "providers": [ … ], "nextCursor": "…" },
  "pagination": { "pages": 1, "items": 50, "has_more": true, "next_cursor": "…" }
}
```

| Field | Present when | Meaning |
| --- | --- | --- |
| `ok` | always | `true` on success |
| `operation` | operation calls | the operationId |
| `status` | the server answered | HTTP status |
| `request_id` | the server answered | quote it when reporting problems |
| `data` | a JSON response | the response body, numbers exact |
| `pagination` | list operations | where the list stopped and how to continue |
| `outcome` | changes | `applied`, `not_applied` or `unknown` |
| `idempotency_key` | the operation takes one | reuse it to retry safely |
| `secret` | `--secret-out` was used | file path and the fields saved there |
| `file` | downloads | path, bytes, sha256 and content type |
| `stream` | streams | events delivered and why it ended |
| `dry_run` | `--dry-run` | the request cchp would send, secrets masked |
| `error` | `ok` is false | see [errors.md](errors.md) |
| `warnings` | sometimes | things worth knowing that did not fail the run |

Discovery commands (`commands`, `schema`, `version`, `guide`) use the same
object with their content in `data`.

## Streams

Streaming operations print one JSON line per event, then the result object:

```json
{"type":"event","operation":"streamUsageRealtime","seq":1,"event":"usage","data":{…}}
```

In agent mode a stream stops after 20 events unless `--max-events` says
otherwise, or at `--timeout`.

## Downloads

Operations that return files need `--output <new file>` (`-` writes to stdout).
cchp refuses to replace an existing file. The file holds the response body byte
for byte whatever its Content-Type (server-sent events and NDJSON included), and
`file.bytes` and `file.sha256` describe it. A request archive part that arrives
empty although the archive lists it as not empty fails with `server_error`.

## Exit codes

| Code | Meaning |
| --- | --- |
| 0 | success |
| 1 | unexpected error |
| 2 | usage: unknown command or flag, missing input |
| 3 | the server rejected the token (401) |
| 4 | permission denied (403) |
| 5 | not found (404) |
| 6 | conflict with the current state (409, 412) |
| 7 | invalid input (local schema check, 400, 422) |
| 8 | needs the browser or a human check (428) |
| 9 | rate limited (429) |
| 10 | server error (5xx) |
| 11 | not sent: nothing reached the server |
| 12 | unknown outcome: the request may have reached the server |
| 13 | refused by cchp's safety policy |
| 14 | not signed in |

## Input

- Parameters are flags; repeat a flag for array values.
- JSON bodies: `--body-file <file>` or `--body-file -`. cchp sends the bytes
  as written, so `null`, `false`, `0` and `""` arrive exactly; `@file` text
  inside JSON is plain text.
- Multipart uploads: `--form-file <field>=<path>`.
- `cchp skill show` prints raw Markdown unless `--json` is given.
