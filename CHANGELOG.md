# Changelog

## [v0.2.2] - 2026-11-26

Built from the CCHP v1.0.17 control-plane spec with embedded Restish v2.3.0.

### Added

- Commands for the operations CCHP v1.0.17 added:
  - a new `cchp provider-tests` group for scheduled and manual provider key
    tests, which replace the key health checks: `overview`, `tasks`, `task`,
    `create-task`, `update-task`, `delete-task`, `set-task-enabled`,
    `run-task`, `task-runs`, `task-executions`, `task-history`, `task-series`,
    `runs`, `run`, `start-run`, `cancel-run`, `execution`, `key-contexts`,
    `key-states`, `target-candidates`, `estimate-cost`, `evaluate-rule` and
    `response-catalog`;
  - client request recording and request templates in `cchp provider-tests`:
    `recordings`, `recording`, `create-recording`, `cancel-recording`,
    `recording-exchange`, `infer-recording`, `rotate-recording-key`,
    `save-recording`, `templates`, `template`, `update-template`,
    `delete-template`, `export-template` and `preview-template`; the temporary
    recording key that `create-recording` and `rotate-recording-key` return is
    a one-time secret and needs `--secret-out`;
  - the redesigned plugin system in `cchp plugins`: organization installs
    (`catalog`, `list`, `overview`, `resources`, `install`, `get`, `update`,
    `uninstall`, `config`, `set-config`, `cards`, `invoke`, `runs`, `run`,
    `operations`, `registrations`, `preview`, `suspend`, `resume`,
    `rotate-token`, `narrow-grant`, `evaluate-upgrade`, `apply-upgrade`,
    `reject-upgrade`, `retry-subscription`, `skip-subscription`,
    `release-suppression`, `lookup-pairing`, `preview-pairing` and
    `reject-pairing`), the same commands for platform installs with a
    `platform-` prefix (for example `platform-list` and `platform-install`),
    and plugin publishers, packages, sources and settings for platform
    administrators (`publishers`, `register-publisher`, `revoke-publisher`,
    `add-publisher-key`, `revoke-publisher-key`, `packages`, `upload-package`,
    `confirm-signing-key`, `withdraw-version`, `sources`, `save-source`,
    `sync-source`, `runners`, `spend`, `settings`, `update-settings` and
    `release-all-suppressions`);
  - the provider report in `cchp providers report` and `key-report`, and
    `cchp providers group-routing`;
  - image input limits in `cchp admin image-input-limits`.
- CCHP answers the plugin commands with not found (404, code 31400) while its
  experimental `feature.plugin_system` flag is off; a platform administrator
  turns it on with `cchp admin set-feature-flag feature.plugin_system`.
- `--routing-entry` on `cchp keys list`, and its `--sort` also takes
  `multiplier` and `probability`.

### Changed

- `cchp plugins list`, `get`, `config` and `uninstall` act on installs of the
  redesigned plugin system and take `--install-id`.
- `cchp providers run-new-api-group-sync` and `start-new-api-group-sync` send
  the body CCHP v1.0.17 expects, whose `adopt_existing_keys` takes over
  matching local keys instead of creating new upstream tokens.

### Removed

- The key health check commands, removed in CCHP v1.0.17 in favor of provider
  tests (`cchp provider-tests`): `cchp keys check`, `check-many`,
  `check-results` and `check-defaults`, and `cchp providers check-new-key`,
  `check-endpoints`, `check-schedules`, `create-check-schedule`,
  `update-check-schedule`, `delete-check-schedule`, `run-check-schedule` and
  `check-schedule-runs`.
- The commands of the retired plugin system: `cchp plugins upload`, `inspect`,
  `enable`, `disable`, `save-config`, `save-targets`, `logs` and `metrics`, and
  the plugin upstream control commands `upstream-bindings`,
  `create-upstream-binding`, `update-upstream-binding`,
  `override-upstream-guard`, `clear-upstream-guard-override` and
  `rotate-upstream-webhook-secret`.
- `cchp providers group-scheduling` and `group-key-scheduling`, replaced by
  `cchp providers group-routing`.

### Spec and coverage

- The pinned spec is CCHP v1.0.17 (`b0579fa14a`): 844 operations, 715
  available, 128 excluded with reasons, 1 unsupported. See
  [docs/coverage.md](docs/coverage.md).
- Eight new plugin approval operations are excluded with their reason:
  approving a plugin install, an upgrade or an external pairing, and approving
  an install again, for organization and platform installs. They need a recent
  browser sign-in and human verification, and the server rejects management
  tokens for them.
- One new operation is unsupported: testing image input preprocessing
  (`testImagePreprocess`) sends a multipart form with text fields and a list of
  images, which cchp cannot build yet.

## [v0.2.1] - 2026-11-23

Built from the CCHP v1.0.16 control-plane spec with embedded Restish v2.3.0.

### Added

- Commands for the operations CCHP v1.0.16 added:
  - New API group auto-sync in `cchp providers`: `new-api-group-sync`,
    `update-new-api-group-sync`, `preview-new-api-group-sync`,
    `run-new-api-group-sync`, `start-new-api-group-sync` and
    `new-api-group-sync-run`, plus upstream token metadata lookup in
    `cchp keys lookup-upstream`;
  - organization member referral management in `cchp org`: `member-referral`,
    `create-member-referral`, `update-member-referral` and
    `delete-member-referral`, plus `cchp org team-provider-groups`;
  - member quota window reset in `cchp billing reset-member-quota-usage` and
    stateless client icon preview in `cchp safety preview-client-icon`.
- Flags for parameters CCHP v1.0.16 added to existing operations:
  `--include-empty-model` on `cchp requests list` and `export`, and `--locale`
  on `cchp admin check-updates`.
- `cchp update` checks GitHub Releases and updates `cchp` automatically without
  needing a sign-in or sending any CCHP management token
  ([#12](https://github.com/CCH-HQ/cchp-cli/pull/12)):
  - detects how `cchp` was installed from the executable path: Homebrew Cellar
    (`brew upgrade cch-hq/tap/cchp`, refreshing with `brew update` when the tap
    is stale), Go install in `$GOBIN` or `$GOPATH/bin` (`go install
    github.com/CCH-HQ/cchp-cli/cmd/cchp@<version>`), direct binary /
    `install.sh` (downloads the matching `.tar.gz`/`.zip` release archive and
    `checksums.txt`, verifies SHA-256 fail-closed, validates against archive
    path traversal, and replaces the binary atomically), and development builds
    (`dev` / non-SemVer, refused unless `--force` is given);
  - supports `--check`, `--force`, `--version <vX.Y.Z>`, and inherited global
    `--json`, `--dry-run`, `--timeout`, and `--debug` flags;
  - on any failure, reports a structured error whose hint prints concrete manual
    upgrade steps and the locale-appropriate official update guide URL
    (`https://cch-plus.com/<lang>/docs/cli#install`).
- Tagged `go install` builds now resolve their release version from embedded Go
  module build info instead of reporting `dev`
  ([#12](https://github.com/CCH-HQ/cchp-cli/pull/12)).

### Changed

- Redaction also masks `api_keys` arrays of raw upstream API keys in dry runs,
  traces and error bodies (for example `cchp keys lookup-upstream`), while
  keeping object arrays such as `apiKeys` intact.

### Fixed

- Passing an unknown subcommand together with flags meant for it (such as
  `cchp providers bogus --limit 10`) now reports the unknown command instead of
  the unknown flag, and dry runs without a saved sign-in no longer suggest
  `--org-id` on operations that do not define that flag
  ([#11](https://github.com/CCH-HQ/cchp-cli/pull/11)).

### Spec and coverage

- The pinned spec is CCHP v1.0.16 (`b7d7bc1518`): 754 operations, 634
  available, 120 excluded with reasons, 0 unsupported. See
  [docs/coverage.md](docs/coverage.md).
- One new browser-only operation is excluded with its reason: creating a local
  member account directly (`createOrgLocalMember`), which requires a browser
  session, human verification and recent sign-in and rejects management tokens.

## [v0.2.0] - 2026-11-21

Built from the CCHP v1.0.15 control-plane spec with embedded Restish v2.3.0.

### Added

- `cchp docs` reads the CCHP documentation at cch-plus.com through its
  [llms.txt](https://llmstxt.org) files, with no sign-in, so agents can answer
  users and find how to configure something before running the matching
  command:
  - `cchp docs index` lists the pages by section with their title, slug, URL
    and description, from `/<lang>/llms.txt`;
  - `cchp docs read <slug|url>` prints a page as Markdown, from
    `/<lang>/docs/<slug>.md`; it takes a slug, a docs URL or a `.md` URL, and a
    page missing in the chosen language fails with `not_found` and names the
    languages that have it;
  - `cchp docs search <words>` uses the site's full-text search and lists the
    best matching sections with their page title, section, URL, slug, snippet
    and score; `--limit` (1 to 50) and `--read-top N`, which adds the Markdown
    of the N best pages;
  - `cchp docs full` prints every page of a language from
    `/<lang>/llms-full.txt`.
  - Languages zh-CN, zh-TW, en, ja, ru and ko, picked by `--lang`, then
    `CCHP_DOCS_LANG`, then the system locale (`LC_ALL`, `LC_MESSAGES`, `LANG`;
    `zh_HK` and `zh_MO` read zh-TW), then en.
  - `--docs-url` or `CCHP_DOCS_URL` points at a preview build; plain `http://`
    only for this computer.
  - Documents are cached in cchp's cache directory for at most 5 minutes, then
    revalidated with `ETag` and `Last-Modified`; `--no-cache` fetches them
    again. Reads are retried like other reads. The docs commands never send
    credentials.
  - `read` and `full` print raw Markdown unless `--json` is given; on a
    terminal they render it. See [docs/docs-commands.md](docs/docs-commands.md).

## [v0.1.4] - 2026-11-20

Built from the CCHP v1.0.15 control-plane spec with embedded Restish v2.3.0.

### Added

- Commands for the operations CCHP v1.0.13 through v1.0.15 added
  ([#9](https://github.com/CCH-HQ/cchp-cli/pull/9)):
  - client definitions and client access control in `cchp safety`:
    `client-definitions`, `create-client-definition`,
    `update-client-definition`, `delete-client-definition`,
    `reorder-client-definitions`, `identify-client`, `fetch-client-icon`,
    `restore-client-preset`, `restore-missing-client-presets`, `client-access`,
    `save-client-access`, `set-client-access-rule`, `evaluate-client-access`,
    `client-access-revisions`, `client-access-revision`,
    `restore-client-access-revision`, `llm-token-client-access` and
    `save-llm-token-client-access`, plus `cchp account client-definitions` and
    `cchp safety preview-privacy-import`;
  - outbound network proxies: `cchp admin network-proxy`, `set-network-proxy`
    and `test-network-proxy`; `cchp providers network-proxies`,
    `set-network-proxies` and `test-network-proxies`; and
    `cchp keys network-proxies`, `set-network-proxies` and
    `test-network-proxies`;
  - GeoIP and client IP inspection: `cchp admin geoip`, `update-geoip`,
    `geoip-update-run`, `lookup-geoip`, `geoip-lookup`, `inspect-client-ip` and
    `cloudflare-proxy-ranges`;
  - diagnostics and profiling: `cchp admin web-vitals` and
    `cchp admin download-pgo`;
  - account, models and requests: `cchp account bootstrap`, `sidebar` and
    `login-history`, `cchp usage batch`, `cchp models catalog-model`,
    `price-table-sync` and `price-table-sync-run`, `cchp public model`, and
    `cchp requests archive-langfuse`;
  - organizations, providers and operations: `cchp org test-storage`,
    `request-content-access`, `set-request-content-access`, `member-team-orgs`
    and `move-member`, `cchp providers detect-endpoint-types`, and
    `cchp operations condition-entities` and `sync-campaign-notifications`.
- Flags for parameters CCHP v1.0.13 through v1.0.15 added to existing
  operations: `--exclude-provider-key-ids`, `--client-id` and
  `--include-unmatched-get` on `cchp requests list` and `export`,
  `--exclude-provider-key-ids` and `--provider-key-ids` on
  `cchp requests filter-options`, `--variant` on `cchp requests archive`,
  `context`, `download-part` and `inspect-bundle`, `--scope` on
  `cchp safety error-rules`, `create-error-rule`, `update-error-rule`,
  `delete-error-rule` and `test-error-rule`, `--trigger` on
  `cchp admin profiling-captures`, and `--if-none-match` on
  `cchp models catalog` and `list`
  ([#9](https://github.com/CCH-HQ/cchp-cli/pull/9)).
- `cchp account login-history` and `cchp operations condition-entities` report
  pagination and follow `--page-limit` and `--max-items`
  ([#9](https://github.com/CCH-HQ/cchp-cli/pull/9)).

### Changed

- Redaction masks more request-body credentials in dry runs, traces and error
  bodies, including S3 secret access keys, payment and Langfuse secret keys,
  new and temporary passwords, and invitation, session and management tokens
  ([#9](https://github.com/CCH-HQ/cchp-cli/pull/9)).
- `cchp billing org-billing` now checks `billing:read` and reflects the
  organization-wide monthly credit limit
  ([#9](https://github.com/CCH-HQ/cchp-cli/pull/9)).

### Removed

- `cchp billing member-overdraft-limit`, `set-member-overdraft-limit`,
  `team-overdraft-limit` and `set-team-overdraft-limit`, removed in CCHP
  v1.0.13 in favor of organization-level credit limits via
  `cchp admin set-org-billing`
  ([#9](https://github.com/CCH-HQ/cchp-cli/pull/9)).
- `cchp account client-rules`, `cchp safety client-rules` and
  `cchp safety replace-client-rules`, replaced by client definitions and client
  access rule sets (`cchp safety client-definitions` and
  `cchp safety client-access`)
  ([#9](https://github.com/CCH-HQ/cchp-cli/pull/9)).
- `cchp safety privacy-ruleset`, `set-privacy-auto-update` and
  `update-privacy-ruleset`, removed when CCHP moved to bundled privacy filter
  rules ([#9](https://github.com/CCH-HQ/cchp-cli/pull/9)).

### Spec and coverage

- The pinned spec is CCHP v1.0.15 (`96ed124df5`): 739 operations, 620
  available, 119 excluded with reasons, 0 unsupported. See
  [docs/coverage.md](docs/coverage.md)
  ([#9](https://github.com/CCH-HQ/cchp-cli/pull/9)).
- Thirteen new browser-only or session-only operations are excluded with their
  reasons: cross-domain sign-in handovers (`begin`, `inspect`, `approve`,
  `decline`, `redeem`, `targets`, `origins`), browser Web Vitals reporting,
  browser HTTP cache clearing, installation client-IP setup (`getSetupClientIP`,
  `saveSetupClientIP`), read-only LLM token quota status (use
  `cchp billing quota-status --scope-type user_key`) and admin user login
  history (`listAdminUserLoginHistory`, browser session only)
  ([#9](https://github.com/CCH-HQ/cchp-cli/pull/9)).

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
- `cchp commands --search` ranks commands by the words of their command path,
  their group's title and their English summary, in any grammatical form
  (`keys` matches `key`), so a command keeps its place when CCHP renames the
  operation behind it: `cchp commands --search "provider keys"` finds
  `cchp keys list` first. Among equal matches the list comes first. An unknown
  operation ID suggests the operation whose words match it, for example
  `listProviderKeyPage` for `cchp api listProviderKeys`
  ([#7](https://github.com/CCH-HQ/cchp-cli/pull/7)).

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

[Unreleased]: https://github.com/CCH-HQ/cchp-cli/compare/v0.2.1...HEAD
[v0.2.1]: https://github.com/CCH-HQ/cchp-cli/compare/v0.2.0...v0.2.1
[v0.2.0]: https://github.com/CCH-HQ/cchp-cli/compare/v0.1.4...v0.2.0
[v0.1.4]: https://github.com/CCH-HQ/cchp-cli/compare/v0.1.3...v0.1.4
[v0.1.3]: https://github.com/CCH-HQ/cchp-cli/compare/v0.1.2...v0.1.3
[v0.1.2]: https://github.com/CCH-HQ/cchp-cli/compare/v0.1.1...v0.1.2
[v0.1.1]: https://github.com/CCH-HQ/cchp-cli/compare/v0.1.0...v0.1.1
[v0.1.0]: https://github.com/CCH-HQ/cchp-cli/releases/tag/v0.1.0
