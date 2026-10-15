# Errors

A failed run prints `"ok": false` with an `error` object and exits nonzero.

```json
{
  "schema_version": 1,
  "ok": false,
  "status": 403,
  "request_id": "6f0c…",
  "error": {
    "kind": "permission_denied",
    "message": "Your token can't do this. Ask an admin for the provider:update permission.",
    "required_permission": "provider:update",
    "retryable": false
  }
}
```

## Kinds and what to do

| `error.kind` | Exit | What to do |
| --- | --- | --- |
| `usage` | 2 | Fix the command; `hint` says how. |
| `invalid_input` | 7 | The body or a flag failed cchp's check before sending. Fix the fields in `error.fields`. |
| `validation` | 7 | The server rejected the input; `error.fields` locates it. |
| `policy` | 13 | Add the flag the message names (`--allow-write`, `--confirm`, `--secret-out`) only if the user wants the change. |
| `not_signed_in` | 14 | Ask the user to sign in or provide credentials. |
| `unauthorized` | 3 | The token was revoked, expired or mistyped. Ask for a new one. |
| `permission_denied` | 4 | Report the missing permission to the user. |
| `browser_session_required` | 8 | The user must do this in the console. |
| `human_verification_required` | 8 | The user must do this in the console; `challenge_action` names the check. |
| `not_found` | 5 | Check the ID. |
| `conflict` | 6 | Reload the current state, then decide. |
| `rate_limited` | 9 | Wait (`hint` may say how long) and retry. |
| `server_error` | 10 | Retry reads later; for changes, check the state first. |
| `not_sent` | 11 | Nothing reached the server; safe to retry after fixing the cause. |
| `unknown_outcome` | 12 | A change may or may not have happened. Check before retrying; reuse `idempotency_key`. |

`error.problem` holds the server's problem+json body with secrets masked, and
`error.code` its numeric code.

## Changes are never retried automatically

cchp retries reads on network failures. It never retries a change, because it
cannot know whether the first attempt was applied. `outcome` on the result
says what is known: `applied`, `not_applied` or `unknown`.
