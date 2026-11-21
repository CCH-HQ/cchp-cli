# Reading the CCHP docs

`cchp docs` reads the CCHP documentation at [cch-plus.com](https://cch-plus.com/en/docs)
through the [llms.txt](https://llmstxt.org) files the site publishes. Agents use
it to answer a user's question about CCHP, and to find how to configure
something before they run the matching `cchp` command.

| Command | Reads | Gives |
| --- | --- | --- |
| `cchp docs index` | `/<lang>/llms.txt` | the pages by section: title, slug, URL, description |
| `cchp docs read <slug\|url>` | `/<lang>/docs/<slug>.md` | one page as Markdown |
| `cchp docs search <words>` | `/api/docs/search` | the best matching sections of pages: title, section, URL, slug, snippet, score |
| `cchp docs full` | `/<lang>/llms-full.txt` | every page of the language as one Markdown document |

The docs commands read public pages only. They need no sign-in and never send
a token, so they work before `cchp login` and on machines without credentials.

## Languages

The docs are in `zh-CN`, `zh-TW`, `en`, `ja`, `ru` and `ko`. cchp picks the
language in this order:

1. `--lang`;
2. `CCHP_DOCS_LANG`;
3. the system locale: the first of `LC_ALL`, `LC_MESSAGES` and `LANG` that is
   set (`zh_CN.UTF-8` reads `zh-CN`; `zh_TW`, `zh_HK` and `zh_MO` read `zh-TW`);
   a locale the docs have no language for, such as `C` or `de_DE`, reads `en`;
4. `en`.

`--lang` and `CCHP_DOCS_LANG` must name one of the six languages. Results say
which language they are in (`lang`) and where it came from (`lang_source`:
`flag`, `url`, `CCHP_DOCS_LANG`, `LC_ALL`, `LC_MESSAGES`, `LANG` or `default`).

When a page is missing in the chosen language, `cchp docs read` fails with
`not_found` and names the languages that have it, from their `llms.txt`.

## Agent loop

Search with `--read-top` to get the best pages' Markdown in the same call,
answer from it, and cite the page's `url`:

```sh
cchp docs search "docker compose" --lang en --read-top 1 --json
```

```json
{
  "schema_version": 1,
  "ok": true,
  "data": {
    "lang": "en",
    "query": "docker compose",
    "url": "https://cch-plus.com/api/docs/search?lang=en&q=docker+compose",
    "count": 10,
    "results": [
      {
        "title": "Docker Compose",
        "section": "Installation",
        "slug": "installation/docker-compose",
        "url": "https://cch-plus.com/en/docs/installation/docker-compose",
        "md_url": "https://cch-plus.com/en/docs/installation/docker-compose.md",
        "snippet": "…",
        "score": 12.5,
        "markdown": "# Docker Compose\n\n…"
      }
    ],
    "lang_source": "flag"
  }
}
```

Read more pages by their `slug` with `cchp docs read <slug> --json`; list the
whole table of contents with `cchp docs index --json`.

### Answer a user's question

> How do I deploy CCHP with Docker Compose?

```sh
cchp docs search "docker compose" --lang en --read-top 1 --json
# answer from data.results[0].markdown; link data.results[0].url
```

> 怎么用 Docker Compose 部署 CCHP?

```sh
cchp docs search "Docker Compose 部署" --lang zh-CN --read-top 1 --json
# 用 data.results[0].markdown 回答，并附上 data.results[0].url
```

Chinese, Japanese and Korean words need no spaces between them.

### Find how to configure a feature, then run it

> Route provider `prov_123` through our office proxy.

```sh
cchp docs search "network proxy" --lang en --read-top 1 --json   # what a proxy list is, how it is chosen
cchp commands --search "provider network proxies" --json         # the matching command
cchp schema saveProviderNetworkProxies --json                     # its body
cchp providers set-network-proxies prov_123 --body-file proxies.json --dry-run --json
cchp providers set-network-proxies prov_123 --body-file proxies.json \
  --allow-write --confirm saveProviderNetworkProxies --json
```

> 给团队设一个每月消费限额。

```sh
cchp docs search 消费限额 --lang zh-CN --read-top 1 --json    # 限额的层级、窗口与生效方式
cchp commands --search "quota policy" --json                   # 对应的命令
cchp schema createQuotaPolicy --json                           # 请求体字段
cchp billing create-quota-policy --body-file policy.json --dry-run --json
cchp billing create-quota-policy --body-file policy.json --allow-write --json
```

The docs explain what a setting does; `cchp commands` and `cchp schema` say
which operation changes it and what it accepts. Changes still need
`--allow-write`, and `--confirm` where the policy asks for it.

## Output

The docs commands follow the [output contract](output-contract.md).

- `index` and `search` print one JSON result object in agent mode, and a list
  for people.
- `read` and `full` print the JSON result object with `--json`. Without it they
  print the Markdown as it is when standard output is not a terminal, so
  `cchp docs full --lang ja > cchp-docs-ja.md` saves the document, and render it
  for reading on a terminal.

| Command | `data` fields |
| --- | --- |
| `index` | `lang`, `url`, `title`, `summary`, `details`, `sections[]` with `title`, `optional` and `pages[]` (`title`, `slug`, `url`, `md_url`, `description`), `count`, `lang_source` |
| `read` | `lang`, `slug`, `title`, `url`, `md_url`, `markdown`, `lang_source` |
| `search` | `lang`, `query`, `url`, `count`, `results[]` (`title`, `section`, `slug`, `url`, `md_url`, `snippet`, `score`, and `markdown` on the first result of each `--read-top` page), `lang_source` |
| `full` | `lang`, `url`, `bytes`, `markdown`, `lang_source` |

Failures use the contract's kinds and exit codes:

| Kind | When |
| --- | --- |
| `usage` (2) | an unknown `--lang` or `CCHP_DOCS_LANG`, a bad `--docs-url`, a page reference that is not a docs page of the site |
| `not_found` (5) | the page does not exist in the language; the message names the languages that have it |
| `validation` (7) | the site rejected the search, for example a query that is too long |
| `rate_limited` (9), `server_error` (10) | the site is limiting requests or failing; retried twice first |
| `server_error` (10) | the site sent an `llms.txt`, page or search answer that does not follow its format |
| `not_sent` (11), `unknown_outcome` (12) | the site could not be reached, or did not answer before `--timeout` |

## Search options

- `--limit N` returns at most N results, 1 to 50 (the site's default is 10).
- `--read-top N` adds the Markdown of the N best pages. Results are sections of
  pages and a page can rank more than once; its Markdown goes on its first
  result, and each result's `url` leads to its section.

## The site, the cache and previews

- The site is `--docs-url`, then `CCHP_DOCS_URL`, then `https://cch-plus.com`.
  Point it at a preview build to read unreleased docs, for example
  `--docs-url http://127.0.0.1:8787`. Plain `http://` is accepted only for this
  computer.
- A slug, a page URL (`https://cch-plus.com/zh-CN/docs/providers/network-proxies`),
  its `.md` URL or a path (`/zh-CN/docs/providers/network-proxies`) all name a
  page. A URL on cch-plus.com is read from the site in use, so links from a
  preview's `llms.txt` keep working.
- `llms.txt` files, pages and `llms-full.txt` are cached in cchp's cache
  directory (`CCHP_CACHE_DIR`) for as long as the site allows and at most 5
  minutes. After that cchp checks them with the site using `ETag` and
  `Last-Modified`. Search answers are never cached. `--no-cache` fetches
  everything again.
- Reads are retried twice on network failures and on 408, 429 and 5xx answers,
  like every other read; `--timeout` bounds the whole command.
- `--dry-run` prints the first request the command would send and sends nothing.
- `--debug` prints each request and whether the answer came from the site or
  the cache.
