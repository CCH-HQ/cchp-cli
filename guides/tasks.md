# Common tasks

The home screen (`cchp`) covers these with lists you can search with `/`.
The commands below do the same from the prompt.

## Usage

```
cchp usage summary                        # today, this week, this month
cchp usage by-model --start 2026-10-01T00:00:00Z
cchp usage trend --granularity day
```

Costs are in credits. Add `--json` to any command for exact numbers.

In the home screen, **See usage** opens a dashboard with a daily heatmap.
Use `Tab` for requests/tokens/cost, the arrows to select a day, `1`/`2`/`3`
for today/week/month, and `PgUp`/`PgDn` to scroll. Calendar days use UTC.

## Balance and packages

```
cchp billing wallet                      # your balance and owned packages
```

Choose **My account** from the home screen, or press `a` on the usage
dashboard, to see remaining package credits, periodic allowances and expiry.
Your wallet balance and package allowances are separate. Viewing them requires
`wallet:read`; these screens do not make purchases or change your account.

## Providers

```
cchp providers list
cchp providers get <provider-id>
cchp providers test <provider-id>         # checks the provider answers; changes nothing
```

## Provider keys

```
cchp keys list <provider-id>
cchp keys disable <provider-id> <key-id>  # stop using a key for now
cchp keys enable <provider-id> <key-id>
cchp keys health                          # recent health of every key
```

## LLM tokens

```
cchp llm-tokens list
cchp llm-tokens create --body-file token.json --secret-out token-secret.json
```

The new token's secret is shown once. With `--secret-out` cchp saves it in a
file only you can read; without it, cchp shows it on screen once.

## Recent requests

```
cchp requests list --limit 20
cchp requests get <request-id>
cchp requests attempts <request-id>       # each upstream try, in order
```

Lists show one page. Add `--page-limit 5` for more pages, or pass the cursor
cchp prints to continue.

## Changes need a confirmation

Commands that delete or replace something ask before they run. Scripts confirm
with `--confirm <operationId>`; `cchp commands show <operationId>` prints the
exact flags.
