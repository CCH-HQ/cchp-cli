# Workflows

## Answer a question from the docs

```
# "How do I deploy CCHP with Docker Compose?"
cchp docs search "docker compose" --lang en --read-top 1 --json
# 「怎么用 Docker Compose 部署 CCHP?」
cchp docs search "Docker Compose 部署" --lang zh-CN --read-top 1 --json
```

Answer from `data.results[0].markdown` and link `data.results[0].url`. If the
best result does not answer it, read the next ones with
`cchp docs read <slug> --json`, or list every page with `cchp docs index --json`.
A page missing in one language fails with `not_found` and names the languages
that have it.

## Find how to configure a feature, then do it

```
cchp docs search "network proxy" --lang en --read-top 1 --json   # how it works
cchp commands --search "provider network proxies" --json         # which command changes it
cchp schema saveProviderNetworkProxies --json                     # what it accepts
cchp providers set-network-proxies <providerId> --body-file proxies.json --dry-run --json
cchp providers set-network-proxies <providerId> --body-file proxies.json \
  --allow-write --confirm saveProviderNetworkProxies --json
```

```
cchp docs search 消费限额 --lang zh-CN --read-top 1 --json      # 限额怎么生效
cchp commands --search "quota policy" --json                     # 对应的命令
cchp billing create-quota-policy --body-file policy.json --dry-run --json
```

Only make the change when the user asked for it.

## Usage report

```
cchp api getUsageSummary --json                     # today, week, month, total
cchp api listUsageByModel --start 2026-10-01T00:00:00Z --json
cchp api getUsageTimeseries --granularity day --json
```

Cost fields ending in `Units` are credit units: 10,000,000,000 units = 1 credit.

## Find a failing request

```
cchp api listUsageLogs --final-status failed --limit 20 --json
cchp api getUsageLog --request-id <requestId> --json
cchp api listUsageLogAttempts --request-id <requestId> --json
```

## Disable a misbehaving provider key

```
cchp api listOrgProviders --json
cchp api listProviderKeyPage --provider-id <id> --json
cchp api disableProviderKey --provider-id <id> --key-id <keyId> --dry-run --json
cchp api disableProviderKey --provider-id <id> --key-id <keyId> --allow-write --json
```

Disabling can be undone with `enableProviderKey`. Revoking a key
(`revokeProviderKey`) cannot, so it also needs `--confirm revokeProviderKey`.

## Create an LLM token

```
cchp schema createLLMToken --json
cchp api createLLMToken --body-file token.json \
  --allow-write --confirm createLLMToken --secret-out new-token.json --json
```

Tell the user the secret is in `new-token.json` (mode 600). It is never printed.

## CC Hub import

`cchp import run` previews, waits, confirms and reports in one command.

```
# 1. Preview only, to read the counts, warnings and confirmation phrase.
cchp import run --source-url https://old-hub.example.com \
  --admin-token-file cchub-admin.token --preview-only --allow-write --json

# 2. Import in append mode, keeping what is already here.
cchp import run --job <jobId> --mode append --allow-write --json

# Overwrite mode replaces existing data and needs the phrase from step 1.
cchp import run --job <jobId> --mode overwrite --confirm-text "<phrase>" \
  --allow-write --confirm startCCHubImport --json
```

To import from an export file on disk, pass `--file export.zip`.
Choose what happens to conflicts with `--choice <type>=<choice>`, for example
`--choice provider=overwrite`; by default cchp keeps existing data.
Importing needs a platform administrator's token with `config:write`.

The final `data` is the job status, including `report`. On failure the error
names the job, and `retryable` says whether `cchp import retry <jobId>` can
continue it.
