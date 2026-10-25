# Changelog

## [Unreleased]

## [v0.1.3] - 2026-10-25

Built from the CCHP v1.0.12 control-plane spec with embedded Restish v2.3.0.

### Added

- Commands for the operations CCHP v1.0.12 added
  ([#7](https://github.com/CCH-HQ/cchp-cli/pull/7)):
  - request and response overrides, in the new `cchp overrides` group: `rules`,
    `save`, `set-rule`, `apply-preset`, `validate`, `catalog`, `revisions`,
    `revision` and `restore-revision`, and for an LLM token's own rules
    `llm-token-rules`, `save-llm-token-rules` and `validate-llm-token-rules`;
  - member billing: `cchp billing member-billing`, `member-billing-actions`,
    `member-plans`, `member-quota`, `adjust-member-balance`,
    `adjust-member-quota`, `revoke-member-plan` and `redeem-for-member`, each
    change with a preview command (`preview-member-balance-adjustment`,
    `preview-member-quota-adjustment`, `preview-revoke-member-plan`,
    `preview-redeem-for-member`);
  - provider keys: `cchp keys get`, `get-many`, `summary`, `revoked`,
    `provider-totals`, `new-key-ratio-defaults`, `reorder` and `undo-reorder`,
    and bulk jobs over every key that matches a filter: `start-bulk-job`,
    `bulk-jobs`, `bulk-job` and `cancel-bulk-job`;
  - provider groups and routing: `cchp providers group-model-rules`,
    `set-group-model-rules`, `reset-group-model-rules`, `group-member-models`,
    `counts` and `preview-route`;
  - model aliases: `cchp models aliases`, `create-alias` and `delete-alias`;
  - invite code join requests: `cchp org join-requests`,
    `approve-join-request` and `reject-join-request`, and
    `cchp org mail-readiness`;
  - quotas and usage: `cchp billing quota-pools` and `quota-status`,
    `cchp usage view-settings` and `cchp requests get-own`;
  - platform administration: `cchp admin feature-flags`, `set-feature-flag`,
    `blocked-ips`, `unblock-ip`, `usage-dead-letters`, `usage-dead-letter`,
    `replay-usage-dead-letter`, `discard-usage-dead-letter`,
    `request-body-budget`, `mail-readiness`, `preview-mail-template`,
    `set-cron-schedule` and `backfill-archive-destination`;
  - `cchp account feature-flags` and
    `cchp plugins clear-upstream-guard-override`.
- Flags for the parameters CCHP v1.0.12 added to existing operations:
  `--archived` and `--batch-id` on `cchp requests list` and `export`,
  `--cost-basis`, `--scope-type` and `--scope-id` on `cchp usage leaderboard`,
  `--scope` on `cchp models list`, `--q`, `--ids`, `--limit` and `--cursor` on
  `cchp providers groups`, and `--key-ids` on `cchp keys check-results`
  ([#7](https://github.com/CCH-HQ/cchp-cli/pull/7)).
- `cchp keys list`, `cchp keys revoked`, `cchp providers groups`,
  `cchp providers group-member-models` and
  `cchp billing member-billing-actions` report pagination and follow
  `--page-limit` and `--max-items`
  ([#7](https://github.com/CCH-HQ/cchp-cli/pull/7)).

### Changed

- `cchp keys list` lists the organization's provider keys a page at a time,
  filtered by provider, group, state or search text, because CCHP v1.0.12
  removed the list of all keys of one provider. Name the provider with
  `--provider-id <provider-id>`; it was a positional argument.
  `cchp api listProviderKeys` is now `cchp api listProviderKeyPage`, and the
  home screen's provider keys list reads its first 100 keys of the provider
  through it ([#7](https://github.com/CCH-HQ/cchp-cli/pull/7)).

### Removed

- `cchp safety request-filters`, `save-request-filters` and
  `test-request-filter`, together with the request filter operations that
  CCHP v1.0.12 replaced with request and response overrides: use
  `cchp overrides rules`, `save` and `validate`
  ([#7](https://github.com/CCH-HQ/cchp-cli/pull/7)).
- `cchp providers model-redirects`, `create-model-redirect`,
  `update-model-redirect` and `delete-model-redirect`, together with the model
  redirect operations CCHP v1.0.12 removed. Model redirects are override rules
  now: `cchp overrides apply-preset` with the `model_redirect` preset adds one,
  `cchp overrides rules --scope provider:<provider-id>` lists them, and
  `cchp overrides save` and `set-rule` change or remove them
  ([#7](https://github.com/CCH-HQ/cchp-cli/pull/7)).

### Fixed

- Permission errors name the permission the server checks for the member LLM
  key commands (`member_llm_key:read`, `create`, `update`, `rotate` and
  `revoke`), pausing, resuming and cancelling a repricing job
  (`billing:reprice`) and the usage breakdowns `cchp usage by-*`
  (`usage:read`) ([#7](https://github.com/CCH-HQ/cchp-cli/pull/7)).

### Spec and coverage

- The pinned spec is CCHP v1.0.12 (`2a00e7460`): 681 operations, 575
  available, 106 excluded with reasons, 0 unsupported. See
  [docs/coverage.md](docs/coverage.md)
  ([#7](https://github.com/CCH-HQ/cchp-cli/pull/7)).
- One new operation is excluded with its reason: listing your own requests to
  join with invite codes, which the server accepts only from a signed-in
  browser session. CCHP v1.0.12 also removed two installation steps cchp
  already excluded, `completeSetup` and `createSetupProvider`
  ([#7](https://github.com/CCH-HQ/cchp-cli/pull/7)).

## [v0.1.2] - 2026-10-21

Built from the CCHP v1.0.10 control-plane spec with embedded Restish v2.3.0.

### Added

- Commands for the operations CCHP v1.0.10 added
  ([#6](https://github.com/CCH-HQ/cchp-cli/pull/6)):
  - content moderation: `cchp safety moderation-events`, `moderation-event`,
    `moderation-event-stats`, `moderation-summary`, `moderation-sanctions` and
    `lift-moderation-sanction`;
  - postpaid billing: `cchp billing org-billing`, `member-overdraft-limit`,
    `set-member-overdraft-limit`, `team-overdraft-limit` and
    `set-team-overdraft-limit`, and for platform administrators
    `cchp admin org-billing` and `set-org-billing`;
  - promotional grant adjustments: `cchp billing grant-adjustments`,
    `grant-adjustment`, `grant-adjustment-items`, `member-grants`,
    `compensation-plans`, `grant-adjustment-subjects`,
    `preview-grant-adjustment`, `create-grant-adjustment` and
    `retry-grant-adjustment`;
  - `cchp billing renew-plan`, and `cchp account inviter`, `bind-inviter` and
    `unbind-inviter`;
  - `cchp llm-tokens disable`, `enable`, `disable-member-key` and
    `enable-member-key`;
  - `cchp org member-tags`, `member-account` and `notification-categories`, and
    `cchp admin member-tags` and `teams`;
  - `cchp operations redeem-codes`, `redeem-code-batch`,
    `update-redeem-code-batch` and `update-redeem-code`;
  - `cchp plugins upstream-bindings`, `create-upstream-binding`,
    `update-upstream-binding`, `override-upstream-guard` and
    `rotate-upstream-webhook-secret`;
  - `cchp providers group-key-scheduling` and `save-split-endpoints`, and
    `cchp keys task-callbacks`, `update-task-callbacks` and
    `preview-draft-model-sync`;
  - `cchp tasks webhook-secret`, `reveal-webhook-secret` and
    `rotate-webhook-secret`.
- Flags for the parameters CCHP v1.0.10 added to existing operations, such as
  search, filter and sort flags on `cchp org members`, `cchp org teams`,
  `cchp llm-tokens list`, `cchp requests list` and `cchp usage leaderboard`,
  and `--started-at` on `cchp requests get`, `attempts`, `archive`, `context`,
  `download-part` and `bundle-link`
  ([#6](https://github.com/CCH-HQ/cchp-cli/pull/6)).
- `cchp org members`, `cchp org invitations`, `cchp llm-tokens list`,
  `cchp llm-tokens member-keys` and `cchp billing referrals` report pagination
  and follow `--page-limit` and `--max-items`
  ([#6](https://github.com/CCH-HQ/cchp-cli/pull/6)).

### Changed

- `cchp org members`, `cchp org invitations` and `cchp billing referrals`
  return the first page of 20 entries unless `--page-size` or `--limit` asks
  for another size, because CCHP v1.0.10 pages them
  ([#6](https://github.com/CCH-HQ/cchp-cli/pull/6)).
- `cchp llm-tokens list` and `cchp llm-tokens member-keys` leave revoked keys
  out unless `--status revoked` or `--status all` asks for them, as CCHP
  v1.0.10 does ([#6](https://github.com/CCH-HQ/cchp-cli/pull/6)).
- `cchp providers overview` calls the operation by its CCHP v1.0.10 ID:
  `cchp api listProviders` is now `cchp api listAdminProviders`
  ([#6](https://github.com/CCH-HQ/cchp-cli/pull/6)).

### Removed

- `cchp safety violations` and `cchp safety watch-violations`, together with
  the moderation violation list and stream that CCHP v1.0.10 removed;
  `cchp safety moderation-events` lists moderation events instead
  ([#6](https://github.com/CCH-HQ/cchp-cli/pull/6)).

### Fixed

- Downloads save the response body byte for byte whatever its Content-Type.
  `cchp requests download-part` saved request archive parts served as
  `text/event-stream` (response frames) or NDJSON as empty files and reported
  `ok: true`; it now saves every part exactly as `curl` receives it, and the
  reported `bytes` and `sha256` describe the saved file
  ([#5](https://github.com/CCH-HQ/cchp-cli/pull/5)).
- A download that arrives empty while the request archive lists the part as
  not empty fails with `server_error` (exit 10) instead of reporting success
  ([#5](https://github.com/CCH-HQ/cchp-cli/pull/5)).
- `cchp requests download-bundle` is a download: it needs `--output` and saves
  the bundle's bytes instead of printing them as data
  ([#5](https://github.com/CCH-HQ/cchp-cli/pull/5)).

### Spec and coverage

- The pinned spec is CCHP v1.0.10 (`b0bf159de`): 621 operations, 514
  available, 107 excluded with reasons, 0 unsupported. See
  [docs/coverage.md](docs/coverage.md)
  ([#6](https://github.com/CCH-HQ/cchp-cli/pull/6)).
- Nine of the new operations are excluded with reasons: your moderation
  cooldown and personal notifications, setting a member's email or temporary
  password, the WeChat in-app payment step, and three user administration
  operations that accept only a browser session
  ([#6](https://github.com/CCH-HQ/cchp-cli/pull/6)).

## [v0.1.1] - 2026-10-16

### Added

- A terminal usage dashboard with period metrics, a navigable 365-day activity
  heatmap for requests, tokens and cost, and responsive layouts. Calendar days
  use UTC; each widget reports loading and errors independently.
- A personal account view with wallet balance, owned packages, current-period
  allowances and expiry dates, plus a wallet summary on the usage dashboard.
- Homebrew installation through `cch-hq/tap/cchp` for macOS and Linux on
  amd64 and arm64, including shell completions. Stable releases automatically
  update the tap after verifying the published archives and checksums.

### Changed

- The project is MIT-licensed, and release archives include `LICENSE`.

### Fixed

- The install script explains when no release has been published instead of
  trying to download an archive from an invalid release URL.

## [v0.1.0]

First release, built from the CCHP v1.0.6 control-plane spec with embedded
Restish v2.3.0.

### For people

- `cchp` opens a home screen: usage, providers, provider keys, LLM tokens,
  recent requests and guides, with search and detail views.
- `cchp login` asks for the server address (checked as you type) and the
  token (hidden), checks them with the server, and saves the token in the
  system keychain or a private file. `cchp status` and `cchp logout` complete
  the set.
- Every operation that accepts a management token is a command in a plain
  group, such as `cchp providers list` or `cchp requests get`.
- Errors say what happened and what to do, with the request ID in small print.
- Destructive and secret-producing commands ask before they run.
- `cchp guide` prints short guides.

### For agents

- Agent mode (`--json`, `CCHP_AGENT=1` or piped output) prints one JSON result
  object with the payload, operation, status, request ID and pagination, and
  exits nonzero on failure.
- `cchp commands --search`, `cchp commands show` and `cchp schema` work offline.
- `cchp api <operationId>` calls any of the 464 available operations.
- JSON bodies are checked against the schema with field paths, then sent byte
  for byte.
- Read-only by default; `--allow-write`, `--confirm <operationId>` and
  `--secret-out` gate changes, deletions and one-time secrets. `--dry-run` sends
  nothing.
- One page by default, with explicit cursors, `--page-limit`, `--max-items` and
  `--timeout`. Streams print NDJSON. Downloads need `--output`.
- Changes are never retried; an unanswered change is reported as
  `unknown_outcome` with its idempotency key.
- `cchp skill install` installs the Claude Code skill; `cchp skill show
  --agents-md` prints an AGENTS.md snippet.
- `cchp import run` previews, confirms, waits for and reports a CC Hub import.

### Spec and coverage

- 562 operations: 464 available, 98 excluded with reasons, 0 unsupported. See
  [docs/coverage.md](docs/coverage.md).
- The upstream spec is pinned by sha256 in `spec/source.lock.json`;
  `make sync-spec` re-exports it from a CCHP checkout or tag.

### Releases

- Static binaries for Linux, macOS and Windows on amd64 and arm64, with
  checksums. `install.sh` verifies the checksum before installing.

[Unreleased]: https://github.com/CCH-HQ/cchp-cli/compare/v0.1.3...HEAD
[v0.1.3]: https://github.com/CCH-HQ/cchp-cli/compare/v0.1.2...v0.1.3
[v0.1.2]: https://github.com/CCH-HQ/cchp-cli/compare/v0.1.1...v0.1.2
[v0.1.1]: https://github.com/CCH-HQ/cchp-cli/compare/v0.1.0...v0.1.1
[v0.1.0]: https://github.com/CCH-HQ/cchp-cli/releases/tag/v0.1.0
