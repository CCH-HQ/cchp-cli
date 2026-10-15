#!/usr/bin/env bash
# Run the end-to-end tests against the server from up.sh and bootstrap.sh.
set -euo pipefail

repo_root="$(cd "$(dirname "$0")/../.." && pwd)"
E2E_DIR="${CCHP_E2E_DIR:-$repo_root/.local-e2e/e2e}"
set -a
# shellcheck disable=SC1091
. "$E2E_DIR/server.env"
set +a
export CCHP_E2E_ADMIN_TOKEN_FILE="$E2E_DIR/admin.token"
export CCHP_E2E_LIMITED_TOKEN_FILE="$E2E_DIR/limited.token"
mkdir -p "$E2E_DIR/tmp" "$E2E_DIR/gotmp"
export TMPDIR="$E2E_DIR/tmp" GOTMPDIR="$E2E_DIR/gotmp"
cd "$repo_root"
go test -tags e2e -count=1 "$@" ./test/e2e/
