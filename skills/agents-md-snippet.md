## CCHP server (cchp)

Use the `cchp` CLI for anything about the CCHP gateway server.

- Find operations offline: `cchp commands --search "<words>" --json`, then
  `cchp schema <operationId> --json`.
- Call them with `cchp api <operationId> [flags] --json`; read `ok`, `data`,
  `error` and `request_id` from the single JSON object on stdout.
- Credentials come from `CCHP_BASE_URL` with `CCHP_TOKEN` or `CCHP_TOKEN_FILE`,
  or from `cchp login`. Never pass a token as an argument.
- cchp only reads by default. Changes need `--allow-write`; deletions,
  replacements and one-time secrets also need `--confirm <operationId>`, and
  secrets need `--secret-out <file>`. Try `--dry-run` first.
- Lists return one page; continue with `pagination.next_cursor`.
- If the result says `browser_session_required` or
  `human_verification_required`, ask the user to do it in the console.
- More: `cchp skill show` and `cchp guide agents`.
