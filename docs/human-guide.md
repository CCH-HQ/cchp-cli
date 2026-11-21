# Guide for people

## The home screen

Run `cchp` in a terminal. Move with the arrow keys and press Enter.

![The home screen](media/home-menu.png)

| Task | What you see |
| --- | --- |
| See usage | A usage dashboard with daily activity, period metrics, your wallet summary and the busiest models |
| Providers | Your upstream providers; press `/` to search, Enter for details |
| Provider keys | Pick a provider, then see its keys |
| LLM tokens | The keys your apps use to call models |
| Recent requests | The latest calls through the gateway; Enter shows one request |
| My account | Your wallet balance, owned packages, remaining allowances and expiry dates |
| Guides | Short how-tos |

Details show the most useful fields first; press `a` for every field. `Esc`
goes back, `q` leaves.

### Usage dashboard

The dashboard shows requests, input/output tokens, cost and success rate. Press
`1`, `2` or `3` for today, this week or this month.

The activity heatmap covers the past 365 days, including today. Each column is
a week starting on Monday. Press `Tab` to switch between requests, tokens and
cost, `←`/`→` to move a week, and `↑`/`↓` to move a day. The selected day's exact
counts and cost appear below the grid. `Home` and `End` jump to the oldest day
and today. Dates and calendar boundaries use UTC, as the dashboard labels show.

Narrow terminals show the most recent weeks first; moving to an older day pans
the calendar. `PgUp`/`PgDn` scroll the page to the model rankings. Activity levels
use both color and different glyphs, so the grid is readable without color.

Your personal wallet balance and active package names appear above the metrics.
Press `a`, or choose **My account** from the home menu, for package details:

- Usage packages show remaining and total credits.
- Time packages show the periodic allowance, current usage and remaining credits,
  the current period's end, and the package's expiry when provided by the server.
- Expired and other inactive packages retain their status and do not appear in
  the active-package summary.

Wallet balance and package allowances are separate. These screens only read
data; they do not purchase, renew or recharge anything. Wallet data needs
`wallet:read`, and usage data needs `usage:read`. An unavailable widget shows
its own error while the other widgets remain usable. Press `r` to refresh and
`Esc` to go back.

## Signing in

```sh
cchp login
```

cchp asks for two things:

1. **Server address.** `https://` is already filled in. cchp checks the address
   as you type.
2. **Management token.** Paste it; the screen hides it.

cchp checks the token with the server before saving it, then shows who you are
signed in as. `cchp status` shows it again later, and `cchp logout` removes the
token from this computer.

![Signing in](media/login.gif)

## Commands

Everything on the home screen, and much more, is a command. Commands come in
groups:

```sh
cchp usage summary
cchp providers list
cchp keys list --provider-id <provider-id>
cchp llm-tokens list
cchp requests list --limit 20
cchp requests get <request-id>
```

`cchp --help` lists the groups, `cchp <group> --help` lists a group's commands,
and `cchp commands --search "<words>"` finds a command by what it does.

![Finding commands](media/discover.gif)

## The CCHP docs

`cchp docs` reads the CCHP documentation from cch-plus.com in your language,
without signing in:

```sh
cchp docs search "network proxy"          # the best matching pages
cchp docs read providers/network-proxies  # one page, rendered in the terminal
cchp docs index                           # every page by section
cchp docs full > cchp-docs.md             # every page in one Markdown file
```

cchp follows your system language; `--lang zh-CN` (or zh-TW, en, ja, ru, ko)
picks another. More: [docs-commands.md](docs-commands.md).

## Changes and confirmations

Commands that add or change things run straight away. Commands that delete or
replace something, or that create a secret shown only once, ask first and
explain why, with **Go ahead** and **Cancel** to choose from.

Secrets such as a new LLM token appear once. Save them with `--secret-out
<file>`, or copy them from the screen straight away.

## When something goes wrong

cchp says what happened in plain words and what to do next. The request ID in
small print helps an administrator find the request on the server.

![Errors](media/errors.gif)

| Message | What to do |
| --- | --- |
| Your token can't do this. Ask an admin for the X permission. | Ask for a token that includes that permission. |
| This server needs you to sign in from the browser for this action. | Use the CCHP console for it. |
| This server needs you to complete a human check in the browser for this action. | Use the CCHP console for it. |
| The server didn't accept your token. | Create a new token and run `cchp login`. |

`cchp guide errors` lists every message.

## Colors

cchp uses your terminal's own colors, so it reads well on light and dark
backgrounds. Set `NO_COLOR=1` to turn color off.
