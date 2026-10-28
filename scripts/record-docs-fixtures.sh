#!/usr/bin/env bash
# Record real answers of the documentation site for the docs package tests.
#
#   scripts/record-docs-fixtures.sh [site]     (default https://cch-plus.com)
#
# Each answer is saved gzipped in internal/sitedocs/testdata/site/ with its
# status and the headers cchp reads; manifest.json maps each request URI to it.
# The request URIs are exactly the ones cchp sends.
set -euo pipefail

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
site="${1:-https://cch-plus.com}"
out="$repo_root/internal/sitedocs/testdata/site"
rm -rf "$out"
mkdir -p "$out"

uris=(
  "/llms.txt"
  "/zh-CN/llms.txt" "/zh-TW/llms.txt" "/en/llms.txt" "/ja/llms.txt" "/ru/llms.txt" "/ko/llms.txt"
  "/en/docs/index.md"
  "/en/docs/installation/docker-compose.md"
  "/en/docs/installation/lan-testing.md"
  "/zh-CN/docs/installation/docker-compose.md"
  "/zh-CN/docs/providers/network-proxies.md"
  "/zh-CN/docs/providers/keys.md"
  "/ja/docs/guide/codex.md"
  "/en/docs/no-such-page.md"
  "/en/llms-full.txt"
  "/api/docs/search?lang=zh-CN&limit=5&q=%E7%BD%91%E7%BB%9C%E4%BB%A3%E7%90%86"
  "/api/docs/search?lang=en&q=docker+compose"
  "/api/docs/search?lang=ko&limit=3&q=%ED%8C%A8%EC%8A%A4%ED%82%A4"
  "/api/docs/search?lang=en&limit=5&q=zzzqqqxxyy"
  "/api/docs/search?lang=en&q=$(printf 'a%.0s' $(seq 1 201))"
)

manifest="{}"
for i in "${!uris[@]}"; do
  uri="${uris[$i]}"
  name="$(printf '%02d' "$i").gz"
  headers="$(mktemp -p "$out")"
  body="$(mktemp -p "$out")"
  status="$(curl -sS --max-time 120 -H 'User-Agent: cchp-fixture-recorder' -D "$headers" -o "$body" -w '%{http_code}' "$site$uri")"
  get() { grep -i "^$1:" "$headers" | tail -1 | cut -d' ' -f2- | tr -d '\r' || true; }
  gzip -n -9 -c "$body" > "$out/$name"
  manifest="$(jq --arg uri "$uri" --arg file "$name" --argjson status "$status" \
    --arg ct "$(get Content-Type)" --arg cc "$(get Cache-Control)" --arg etag "$(get ETag)" --arg lm "$(get Last-Modified)" \
    '.[$uri] = {status: $status, file: $file, headers: ({"Content-Type": $ct, "Cache-Control": $cc, "ETag": $etag, "Last-Modified": $lm} | with_entries(select(.value != "")))}' <<< "$manifest")"
  rm -f "$headers" "$body"
  echo "$status $uri"
done
jq --arg site "$site" --arg at "$(date -u +%Y-%m-%dT%H:%M:%SZ)" '{recorded_from: $site, recorded_at: $at, responses: .}' <<< "$manifest" > "$out/manifest.json"
du -sh "$out"
