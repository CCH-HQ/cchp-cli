#!/usr/bin/env bash
# Start a private CCHP server for the end-to-end tests.
#
# Usage: CCHP_SRC=<path to a CCHP source tree at the pinned tag> scripts/e2e/up.sh
#
# Everything this script creates lives under CCHP_E2E_DIR (default .local-e2e/e2e):
# its own Postgres and Dragonfly containers (compose project cchp-cli-e2e), its own
# gateway binary, logs and generated secrets. It never touches another database.
set -euo pipefail

repo_root="$(cd "$(dirname "$0")/../.." && pwd)"
: "${CCHP_SRC:?set CCHP_SRC to a CCHP source tree}"
CCHP_SRC="$(cd "$CCHP_SRC" && pwd)"
E2E_DIR="${CCHP_E2E_DIR:-$repo_root/.local-e2e/e2e}"
PROJECT="${CCHP_E2E_PROJECT:-cchp-cli-e2e}"
PG_PORT="${CCHP_E2E_PG_PORT:-55730}"
DF_PORT="${CCHP_E2E_DF_PORT:-16730}"
GATEWAY_PORT="${CCHP_E2E_GATEWAY_PORT:-18730}"
CONTROL_PORT="${CCHP_E2E_CONTROL_PORT:-18731}"
ARCHIVE_PORT="${CCHP_E2E_ARCHIVE_PORT:-18732}"
NODE_PORT="${CCHP_E2E_NODE_PORT:-18733}"

mkdir -p "$E2E_DIR/bin" "$E2E_DIR/compose" "$E2E_DIR/data" "$E2E_DIR/node" "$E2E_DIR/scratch" "$E2E_DIR/gotmp"
export GOTMPDIR="$E2E_DIR/gotmp"

secrets="$E2E_DIR/secrets.env"
if [ ! -f "$secrets" ]; then
  umask 077
  {
    echo "CCH_TOKEN_PEPPER=$(openssl rand -hex 32)"
    echo "CCH_MASTER_KEY=$(openssl rand -hex 32)"
    echo "CCHP_DATABASE_RUNTIME_PASSWORD=$(openssl rand -hex 16)"
    echo "DRAGONFLY_KEY_SECRET=$(openssl rand -hex 32)"
  } > "$secrets"
  umask 022
fi
chmod 600 "$secrets"
set -a
# shellcheck disable=SC1090
. "$secrets"
set +a

# The compose file mounts ./postgres/init; copy it so the init scripts are readable
# by the container user without changing the source tree.
cp "$CCHP_SRC/deployments/dev/compose.yml" "$E2E_DIR/compose/compose.yml"
rm -rf "$E2E_DIR/compose/postgres"
cp -R "$CCHP_SRC/deployments/dev/postgres" "$E2E_DIR/compose/postgres"
chmod -R o+rX "$E2E_DIR/compose/postgres"

export CCH_DEV_PROJECT_NAME="$PROJECT" POSTGRES_PORT="$PG_PORT" DRAGONFLY_PORT="$DF_PORT"
export POSTGRES_USER=hub POSTGRES_PASSWORD=hub POSTGRES_DB=app
docker compose -p "$PROJECT" -f "$E2E_DIR/compose/compose.yml" up -d --wait postgres dragonfly

if [ ! -x "$E2E_DIR/bin/gateway" ] || [ "${CCHP_E2E_REBUILD:-0}" = "1" ]; then
  (cd "$CCHP_SRC" && go build -o "$E2E_DIR/bin/gateway" ./cmd/gateway)
fi

if [ -z "${ATLAS_BIN:-}" ]; then
  ATLAS_BIN="$E2E_DIR/bin/atlas"
  if [ ! -x "$ATLAS_BIN" ]; then
    (cd "$CCHP_SRC" && ATLAS_INSTALL_PATH="$ATLAS_BIN" ./scripts/install_atlas.sh)
  fi
fi
export ATLAS_BIN

export APP_ENV=development
export CCHP_DATABASE_RUNTIME_ROLE=cchp_runtime
export ATLAS_DATABASE_URL="postgres://hub:hub@127.0.0.1:$PG_PORT/app?sslmode=disable"
export DATABASE_URL="postgres://cchp_runtime:$CCHP_DATABASE_RUNTIME_PASSWORD@127.0.0.1:$PG_PORT/app?sslmode=disable"
export DRAGONFLY_URL="redis://127.0.0.1:$DF_PORT/0"
export CCH_DATA_DIR="$E2E_DIR/data" CCH_NODE_DIR="$E2E_DIR/node" CCH_SCRATCH_DIR="$E2E_DIR/scratch"
# Passkeys need a host name as relying party ID, so the public origin uses localhost.
export CCH_PUBLIC_BASE_URL="http://localhost:$CONTROL_PORT"
export CCH_CONTROL_PLANE_ALLOWED_ORIGINS="http://localhost:$CONTROL_PORT"
export CCH_ARCHIVE_INTERNAL_ADDR="127.0.0.1:$ARCHIVE_PORT" CCH_ARCHIVE_INTERNAL_URL="http://127.0.0.1:$ARCHIVE_PORT"
export CCH_NODE_INTERNAL_ADDR="127.0.0.1:$NODE_PORT" CCH_NODE_INTERNAL_URL="http://127.0.0.1:$NODE_PORT"
export LOG_LEVEL=info

(cd "$CCHP_SRC" && go run ./cmd/migrate apply) > "$E2E_DIR/migrate.log" 2>&1

if [ -f "$E2E_DIR/gateway.pid" ] && kill -0 "$(cat "$E2E_DIR/gateway.pid")" 2>/dev/null; then
  kill "$(cat "$E2E_DIR/gateway.pid")"
  while kill -0 "$(cat "$E2E_DIR/gateway.pid")" 2>/dev/null; do sleep 0.2; done
fi
# The gateway reads configs/ relative to its working directory.
(cd "$CCHP_SRC" && exec setsid nohup "$E2E_DIR/bin/gateway" api --role=all --addr="127.0.0.1:$GATEWAY_PORT" --control-addr="127.0.0.1:$CONTROL_PORT" \
  > "$E2E_DIR/gateway.log" 2>&1 < /dev/null) &
echo $! > "$E2E_DIR/gateway.pid"

for _ in $(seq 1 120); do
  if curl -fsS "http://127.0.0.1:$CONTROL_PORT/api/health" > /dev/null 2>&1; then
    break
  fi
  sleep 0.5
done
curl -fsS "http://127.0.0.1:$CONTROL_PORT/api/health" > /dev/null

# The environment the bootstrap and the tests read.
umask 077
{
  echo "CCHP_E2E_BASE_URL=http://127.0.0.1:$CONTROL_PORT"
  echo "CCHP_E2E_PUBLIC_ORIGIN=http://localhost:$CONTROL_PORT"
  echo "CCHP_E2E_SRC=$CCHP_SRC"
  echo "CCHP_E2E_DIR=$E2E_DIR"
  echo "CCHP_E2E_GATEWAY_BIN=$E2E_DIR/bin/gateway"
  echo "CCHP_E2E_DATABASE_URL=$DATABASE_URL"
  echo "CCHP_E2E_ADMIN_DATABASE_URL=$ATLAS_DATABASE_URL"
} > "$E2E_DIR/server.env"
echo "CCHP server ready at http://127.0.0.1:$CONTROL_PORT"
