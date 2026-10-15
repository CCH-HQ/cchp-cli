# Safety model

## Credentials

- cchp signs in with a CCHP **management token** (`cch_auth_live_…`) and sends it
  as `Authorization: Bearer`. LLM tokens are refused.
- A token never appears in command-line arguments. It comes from `CCHP_TOKEN`,
  `CCHP_TOKEN_FILE` (refused when other users can read the file), standard
  input (`cchp login --token-stdin`), or the saved sign-in.
- Saved tokens live in the system keychain. On a computer without one, they go
  to `credentials.json`, mode 600. `CCHP_CREDENTIAL_STORE` chooses explicitly.
- Each token is saved for one server origin. `CCHP_BASE_URL` selects which saved
  token to use; a server without its own token gets none.
- Plain `http://` is accepted only for this computer (`localhost`, `127.0.0.1`,
  `::1`).

## The bound origin

- The token is attached only to requests whose scheme, host and port match the
  signed-in server.
- On a redirect to another origin the credential is dropped; cchp also checks
  every outgoing request and refuses to carry credentials elsewhere.
- cchp never sends an `X-CCH-Organization` header that differs from the token's
  own organization: the few operations that require it get the token's
  organization from the server.

## The policy layer

`spec/operation-policy.yaml` classifies every operation:

| Field | Meaning |
| --- | --- |
| `risk` | `read`, `write` or `destructive` |
| `retry` | whether cchp may retry it automatically (reads only) |
| `permission` | the permission the server checks, for plain-language errors |
| `secret_fields` | response fields that hold a one-time secret |
| `pagination` | how to walk its pages |

An operation missing from the file counts as `write`. In agent mode:

- reads run;
- writes need `--allow-write`;
- destructive operations also need `--confirm <operationId>`;
- operations that return a one-time secret need `--allow-write`,
  `--confirm <operationId>` and `--secret-out <new file>`.

People in a terminal confirm destructive and secret-producing operations in a
prompt instead; without a terminal they need `--confirm`.

## One-time secrets

With `--secret-out`, cchp writes the secret fields to a new file with mode 600
(it refuses to replace an existing file) and puts a placeholder in `data`, so
the secret never reaches stdout, stderr or logs. If the change succeeds but the
file cannot be written, cchp says so with `unknown_outcome` so the caller can
revoke what was created.

## Redaction

One redaction policy covers `--debug` traces, `--dry-run` output, error
messages and bodies, and diagnostics:

- `Authorization`, cookies, CSRF tokens and human-check proofs are masked;
- CCHP tokens in any text keep only their public prefix;
- JSON fields with credential names (`password`, `api_key`, `clientSecret`,
  `plaintext`, …) are masked at any depth.

## Retries and outcomes

Reads are retried on network failures and server errors. Changes are never
retried. When a change gets no answer, cchp reports `unknown_outcome` with the
idempotency key it sent, so the caller can check and, if needed, retry the same
change safely.

## What cchp will not do

- Operations that need a signed-in browser session are excluded and explained
  (`cchp commands --all`).
- When the server asks for a human check (HTTP 428), cchp reports it in plain
  words. It never solves or skips the check.
- `--dry-run` builds the full request locally and stops before the network: it
  sends nothing, needs no credential, and masks secrets.

## Isolation from other Restish setups

cchp runs Restish with its own private, empty configuration file and cache, so
user or project Restish configuration, plugins and `RSH_*` environment
variables cannot change its requests.
