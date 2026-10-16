# cchp

The command-line tool for [CCHP (Claude Code Hub Plus)](https://github.com/CCH-HQ/claude-code-hub-plus).
People get a home screen with usage, providers, keys, LLM tokens and recent
requests. Agents and scripts get every control-plane operation behind one
predictable JSON contract.

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

## Documentation

- [Install](docs/install.md)
- [Guide for people](docs/human-guide.md)
- [Guide for agents](docs/agent-guide.md)
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
