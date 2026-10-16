# Release distribution

Pushing a `v*` tag runs `.github/workflows/release.yml`. GoReleaser builds and
publishes the six platform archives and `checksums.txt` to GitHub Releases.

After a stable release succeeds, the Homebrew job downloads the four macOS/Linux
archives, verifies every SHA256 against `checksums.txt`, and updates
[`Formula/cchp.rb` in CCH-HQ/homebrew-tap](https://github.com/CCH-HQ/homebrew-tap).
Prereleases do not update the tap. The updater requires the latest stable
release, so rerunning an older release cannot downgrade the formula.

## Homebrew publisher App

Register a private, organization-owned GitHub App named
`cchp-homebrew-publisher` with only **Contents: read and write** (Metadata read
is automatic). Disable webhooks and event subscriptions. Install it on CCH-HQ
with **Only select repositories: homebrew-tap**.

In **cchp-cli > Settings > Secrets and variables > Actions**, configure:

| Type | Name | Value |
| --- | --- | --- |
| Variable | `HOMEBREW_APP_CLIENT_ID` | The App's Client ID |
| Secret | `HOMEBREW_APP_PRIVATE_KEY` | The full PEM private key |

The regular `GITHUB_TOKEN` reads release assets. The Homebrew job creates a
short-lived installation token restricted to `homebrew-tap` and Contents write,
pushes as the App's bot, and automatically revokes the token when the job ends.
The App token lets the tap update trigger its own CI. No personal access token
or SSH deploy key is required.

For key rotation, generate a new private key in the App settings, replace
`HOMEBREW_APP_PRIVATE_KEY`, verify an update, then revoke the old key. Do not
reuse the GitOps source-reader or release-writer identities for this tap.

## Retry or bootstrap Homebrew

To retry only the tap update without rebuilding or republishing a release:

```sh
gh workflow run release.yml --repo CCH-HQ/cchp-cli -f tag=v0.1.0
```

For a local preview (requires Python 3.11+ and authenticated `gh`):

```sh
python3 scripts/update-homebrew.py v0.1.0 --output /tmp/cchp.rb
python3 -B -m unittest discover -s scripts -p 'test_*.py' -v
```

The command rejects drafts, prereleases, older releases, missing archives and
checksum mismatches before replacing the formula. An unchanged formula produces
no commit. The tap's CI installs and tests the formula on macOS and Linux.
