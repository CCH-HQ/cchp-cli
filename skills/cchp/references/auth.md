# Authentication

## Where the token comes from

cchp resolves the credential in this order:

1. `CCHP_TOKEN` with `CCHP_BASE_URL`.
2. `CCHP_TOKEN_FILE` (a file with mode 600) with `CCHP_BASE_URL`.
3. The token saved by `cchp login` for `CCHP_BASE_URL`, or for the current server
   when `CCHP_BASE_URL` is unset. Saved tokens live in the system keychain, or in
   a mode-600 file when the computer has none.

Each token is bound to one server. cchp sends it only to that server's origin and
drops it if a redirect leads elsewhere.

Sign in from a script without exposing the token in arguments:

```
printf '%s' "$TOKEN" | cchp login --server https://hub.example.com --token-stdin --json
```

## Management tokens

- Format: `cch_auth_live_<prefix>_<secret>`. LLM tokens (`cch_llm_live_…`) are
  rejected.
- A token belongs to one person in one organization.
- Effective permissions are the intersection of the person's role and the
  permissions granted to the token. `cchp api getMyPermissionSummary --json`
  shows them; `denied_reasons` says `token_scope` when the token is the limit.
- A permission error names the missing permission in `error.required_permission`
  when cchp knows it.

## What a management token cannot do

Operations listed as `excluded` by `cchp commands --all --json` need a browser
session or are part of browser flows: password and email changes, passkeys,
two-step verification, revealing saved secrets, rotating a management token,
sign-in and installation steps. Report this to the user; there is no
alternative path.

## Organization

`orgId` path parameters default to the token's organization. cchp sends the
`X-CCH-Organization` header only where an operation requires it, always with
the token's own organization.

## Check

```
cchp status --json          # server, token prefix, identity, and a live check
cchp whoami --json          # the server's view of the token
```
