# cchp

The command-line tool for [CCHP (Claude Code Hub Plus)](https://github.com/CCH-HQ/claude-code-hub-plus).
People get a home screen with a usage heatmap, wallet and package details,
providers, keys, LLM tokens and recent requests. Agents and scripts get every
control-plane operation behind one
predictable JSON contract, and the CCHP documentation in six languages.

![The cchp home screen](docs/media/home.gif)

## Install

With Homebrew (macOS or Linux):

```sh
brew install cch-hq/tap/cchp
```

Or with the install script:

```sh
curl -fsSL https://raw.githubusercontent.com/CCH-HQ/cchp-cli/main/install.sh | sh
```

Or with Go: `go install github.com/CCH-HQ/cchp-cli/cmd/cchp@latest`.
Details and other options: [docs/install.md](docs/install.md).

## Start in 30 seconds: people

1. In the CCHP console, open **Account › Security › Management tokens** and create a token.
2. Run `cchp login`, enter your server address and paste the token.
3. Run `cchp` and pick a task.

![Signing in](docs/media/login.gif)

## Start in 30 seconds: agents

```sh
export CCHP_BASE_URL=https://hub.example.com
export CCHP_TOKEN_FILE=~/.config/cchp/token        # a file only you can read
cchp commands --search "usage summary" --json       # find the operation
cchp schema getUsageSummary --json                  # its inputs and output
cchp api getUsageSummary --json                     # one JSON result on stdout
cchp skill install --project                        # teach Claude Code the loop
```

Agent mode reads only; changes need `--allow-write`, and deletions or one-time
secrets also need `--confirm <operationId>`.

## Look things up in the CCHP docs

`cchp docs` reads the documentation at [cch-plus.com](https://cch-plus.com/en/docs)
through its [llms.txt](https://llmstxt.org) files and full-text search, in
zh-CN, zh-TW, en, ja, ru and ko. It needs no sign-in, so an agent can answer a
user's question, or find how to configure something before it runs the
matching `cchp` command:

```sh
# "How do I deploy CCHP with Docker Compose?"
cchp docs search "docker compose" --lang en --read-top 1 --json

# 「怎么给供应商配置网络代理?」
cchp docs search 网络代理 --lang zh-CN --read-top 1 --json
cchp commands --search "provider network proxies" --json

cchp docs index --lang ja                             # every page by section
cchp docs read installation/docker-compose --lang ko  # one page as Markdown
cchp docs full --lang zh-TW > cchp-docs.md            # every page in one file
```

The language is `--lang`, then `CCHP_DOCS_LANG`, then the system locale, then
English. Details: [docs/docs-commands.md](docs/docs-commands.md).

## Documentation

- [Install](docs/install.md)
- [Guide for people](docs/human-guide.md)
- [Guide for agents](docs/agent-guide.md)
- [Reading the CCHP docs](docs/docs-commands.md)
- [Output contract](docs/output-contract.md)
- [Safety model](docs/safety-model.md)
- [Coverage](docs/coverage.md): every CCHP operation and whether cchp runs it
- [Updating the spec](docs/updating-the-spec.md)
- [How cchp is built](docs/architecture.md)
- [Changelog](CHANGELOG.md)
- [Release distribution](docs/releasing.md)

In the terminal: `cchp guide` lists short guides, and every command has `--help`.

## License

[MIT](LICENSE)
