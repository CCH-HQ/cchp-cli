# How cchp is built

```mermaid
flowchart LR
  spec[spec/upstream + overlay + policy] -->|cchp-gen| assets[embedded catalog, schemas, engine spec]
  user[person or agent] --> root[cchp Cobra root]
  root --> tui[TUI: Bubble Tea, Lip Gloss, Huh]
  root --> discovery[commands, schema: catalog only, offline]
  root --> runner[runner: inputs, policy, pagination, secrets]
  root --> sitedocs[sitedocs: llms.txt, pages, search]
  tui --> runner
  runner --> engine[embedded Restish v2]
  engine -->|guarded transport| server[CCHP control plane]
  sitedocs -->|cache, retries, no credentials| site[cch-plus.com docs]
  assets --> discovery
  assets --> runner
  assets --> engine
```

## Parts

- **`cmd/cchp`** starts `internal/cli`, the Cobra root cchp owns: `login`,
  `status`, `commands`, `schema`, `api`, `guide`, `docs`, `skill`, `version`,
  and one command group per area (`providers`, `keys`, `usage`, …).
- **`internal/catalog`** describes operations, parameters, policy and coverage.
  **`internal/assets`** embeds the generated catalog, schemas and engine spec,
  so discovery and help need no network and no spec parsing at start-up.
- **`internal/runner`** runs one operation for both the CLI and the TUI: it
  checks inputs and the JSON body against the schema, applies the policy,
  resolves credentials, calls the engine, walks pages, moves secrets to
  `--secret-out` and maps failures to plain-language errors.
- **`internal/engine`** embeds Restish v2 through its supported embedding API:
  a branded CLI with bundled configuration and the operation promoted to the
  root, a custom auth handler that attaches the management token for the bound
  origin only, a JSON content type that sends bodies byte for byte, and a
  capture formatter that hands the response back to cchp. Restish loads the
  generated engine spec from a local file, never by discovery. A guard on the
  standard library transport Restish clones refuses credentials to other
  origins and stops dry runs before any connection.
- **`internal/sitedocs`** reads the documentation site for `cchp docs`: it
  picks the language, parses each language's `llms.txt` with the goldmark
  Markdown parser into sections and links, reads pages, `llms-full.txt` and the
  search API, and explains a page missing in one language by the languages
  whose `llms.txt` lists it. Its HTTP client never carries credentials; it
  retries reads with go-retryablehttp and keeps documents in an RFC 9111 disk
  cache (sandrolain/httpcache) whose freshness it caps at 5 minutes.
- **`internal/tui`** is the home screen and sign-in form.
- **`internal/auth`** stores the token (keychain or a mode-600 file) and binds
  it to the server origin.
- **`internal/redact`** is the one redaction policy.
- **`skills/`** and **`guides/`** are embedded and printed or installed by
  `cchp skill` and `cchp guide`.

## Tests

- Unit tests cover the policy, redaction, the output contract, body
  preservation, the origin and redirect guard, the spec lock and the TUI
  (golden screens with `teatest`).
- End-to-end tests (`go test -tags e2e ./test/e2e/`) run the built binary
  against a real CCHP server started with `scripts/e2e/up.sh`.
- The docs package's tests serve answers recorded from the documentation site
  (`internal/sitedocs/testdata`) from a local HTTP server. `make e2e-docs` runs
  the built binary against the real site, or a preview named by
  `CCHP_DOCS_URL`, for every language.
