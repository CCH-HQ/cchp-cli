# Install

## Homebrew

On macOS or Linux, with Apple Silicon/ARM64 or Intel/AMD64:

```sh
brew install cch-hq/tap/cchp
```

This adds [CCH-HQ's tap](https://github.com/CCH-HQ/homebrew-tap), installs
the release binary, and sets up Bash, Zsh and Fish completions. To update:

```sh
brew update
brew upgrade cchp
```

## Install script

```sh
curl -fsSL https://raw.githubusercontent.com/CCH-HQ/cchp-cli/main/install.sh | sh
```

The script picks the archive for your system (Linux or macOS, amd64 or arm64),
downloads it from the latest GitHub release, checks it against the release's
`checksums.txt`, and installs `cchp` into `~/.local/bin`.

Options, as environment variables:

| Variable | Meaning | Default |
| --- | --- | --- |
| `CCHP_VERSION` | Release to install, for example `v0.1.0` | the latest release |
| `CCHP_INSTALL_DIR` | Where to put `cchp` | `~/.local/bin` |

```sh
curl -fsSL https://raw.githubusercontent.com/CCH-HQ/cchp-cli/main/install.sh | CCHP_VERSION=v0.1.0 sh
```

## Go

```sh
go install github.com/CCH-HQ/cchp-cli/cmd/cchp@latest
```

This needs Go 1.27 or newer. The binary reports its version as `dev`; release
builds report the tag.

## Manual download

Every release on the [releases page](https://github.com/CCH-HQ/cchp-cli/releases)
has archives for Linux, macOS and Windows on amd64 and arm64, and a
`checksums.txt`. Check an archive before you unpack it:

```sh
sha256sum --ignore-missing -c checksums.txt
```

## Check the install

```sh
cchp version
```

It prints the cchp version and the CCHP spec it was built from.

## Where cchp keeps things

| What | Where |
| --- | --- |
| Settings and the signed-in server | `~/.config/cchp/config.json` (Linux), `~/Library/Application Support/cchp` (macOS), `%AppData%\cchp` (Windows) |
| The token | the system keychain (macOS Keychain, Windows Credential Manager, Secret Service on Linux) |
| The token, when there is no keychain | `credentials.json` next to `config.json`, readable only by you |
| Cache | the user cache directory, `cchp/`: the engine's spec and, in `docs/`, docs pages read in the last 5 minutes |

`CCHP_CONFIG_DIR` and `CCHP_CACHE_DIR` move these. `CCHP_CREDENTIAL_STORE=file`
or `keychain` picks the token store explicitly.

## Shell completion

```sh
cchp completion bash > ~/.local/share/bash-completion/completions/cchp
cchp completion zsh > "${fpath[1]}/_cchp"
cchp completion fish > ~/.config/fish/completions/cchp.fish
```

## Uninstall

Run `cchp logout`. For Homebrew installs, run `brew uninstall cchp`; otherwise,
delete the `cchp` binary. Delete the directories above to remove local data.
