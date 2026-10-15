#!/usr/bin/env bash
# Create the e2e administrator and management tokens on the server started by up.sh.
set -euo pipefail

repo_root="$(cd "$(dirname "$0")/../.." && pwd)"
E2E_DIR="${CCHP_E2E_DIR:-$repo_root/.local-e2e/e2e}"
set -a
# shellcheck disable=SC1091
. "$E2E_DIR/secrets.env"
# shellcheck disable=SC1091
. "$E2E_DIR/server.env"
set +a
export GOTMPDIR="$E2E_DIR/gotmp"
cd "$repo_root"
go run ./test/e2e/bootstrap
