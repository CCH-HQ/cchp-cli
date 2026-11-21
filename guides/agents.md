# cchp for agents and scripts

## Agent mode

cchp switches to agent mode when you pass `--json`, set `CCHP_AGENT=1`, or
when standard output is not a terminal. In agent mode cchp:

- prints exactly one JSON result object on standard output (streams print one
  JSON line per event and then the result object);
- writes diagnostics to standard error;
- exits nonzero on any failure;
- never prompts, pages or colors output;
- only reads: changes need `--allow-write`.

## The loop

```
cchp commands --search "llm tokens" --json        # find the operation
cchp schema listLLMTokens --json                   # inputs and output schema
cchp api listLLMTokens --json                      # call it
```

`cchp api <operationId>` is the stable entry point: operation IDs come from the
CCHP spec and stay the same across cchp versions. `cchp commands show
<operationId>` prints the exact safety flags and examples.

## Credentials

Set `CCHP_BASE_URL` and either `CCHP_TOKEN` or `CCHP_TOKEN_FILE` (a file with
mode 600). A saved `cchp login` also works. Never put a token in arguments.

## Safety flags

- `--allow-write` for any change.
- `--confirm <operationId>` for deletions, replacements and anything that
  returns a one-time secret.
- `--secret-out <file>` for operations that return a one-time secret.
- `--dry-run` shows the exact request without sending it, secrets masked.

## Bodies

Pass JSON with `--body-file <file>` or `--body-file -` for standard input.
cchp checks it against the operation's schema first and sends it byte for byte.

## The CCHP docs

`cchp docs` reads the CCHP documentation without a sign-in, to answer a user's
question or to learn how a feature works before changing it:

```
cchp docs search "network proxy" --read-top 1 --json   # best pages and their Markdown
cchp docs read providers/network-proxies --json        # one page by its slug
cchp docs index --json                                 # every page by section
```

Pick the language with `--lang` (zh-CN, zh-TW, en, ja, ru, ko) or
`CCHP_DOCS_LANG`; otherwise cchp follows the system locale, then English.

Full reference: the cchp skill (`cchp skill show`) and the docs in the
repository.
