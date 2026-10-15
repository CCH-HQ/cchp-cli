# Pagination

List operations return **one page** by default. The result says where it stopped:

```json
"pagination": { "pages": 1, "items": 50, "has_more": true, "next_cursor": "o:NTA" }
```

## Continue explicitly

Pass the cursor to the operation's `--cursor` flag:

```
cchp api listUsers --limit 100 --json
cchp api listUsers --limit 100 --cursor o:NTA --json
```

Page-numbered lists (`listAnnouncements`, `listModels`, `searchAuditLogs`)
report `next_page`; pass it with `--page`.

## Fetch several pages

```
cchp api listUsageLogs --page-limit 10 --max-items 500 --json
```

- `--page-limit N` fetches up to N pages.
- `--max-items M` stops once M items arrived; `truncated` is then true.
- `--timeout` bounds the whole run; on a deadline cchp returns what it has with
  a warning and the cursor to continue.

With more than one page, `data` is the last page with its item list replaced by
every item fetched.

## Which operations page

`cchp commands show <operationId> --json` includes `pagination` with the cursor
parameter, the field holding the next cursor and the item list path. Lists
without it return everything in one response.

## Cursors

Cursors are opaque and valid only with the same filters. Don't build them.
