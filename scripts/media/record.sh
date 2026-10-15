#!/usr/bin/env bash
# Record the terminal demos in docs/media with vhs (charmbracelet/vhs).
#
#   scripts/media/record.sh [home|login|errors|discover ...]
#
# Needs vhs, ttyd, ffmpeg and a Chromium on PATH, and the e2e server from
# scripts/e2e/up.sh with the tokens from scripts/e2e/bootstrap.sh. The tapes in
# docs/media/tapes are templates: {{ENV}} becomes a generated environment file
# and {{SERVER}} the server address. The login demo types a throwaway token
# that the screen masks; the rendered tape stays under .local-e2e.
set -euo pipefail

repo_root="$(cd "$(dirname "$0")/../.." && pwd)"
E2E_DIR="${CCHP_E2E_DIR:-$repo_root/.local-e2e/e2e}"
W="$repo_root/.local-e2e/media"
mkdir -p "$W/bin" "$W/tapes" "$W/tmp"
export GOTMPDIR="$W/tmp" TMPDIR="$W/tmp"
cd "$repo_root"

set -a
# shellcheck disable=SC1091
. "$E2E_DIR/server.env"
set +a
server="$CCHP_E2E_BASE_URL"
go build -o "$W/bin/cchp" ./cmd/cchp
export PATH="$W/bin:$PATH"

write_env() { # name config-dir [extra lines...]
  local name=$1 dir=$2
  shift 2
  {
    echo "export PATH=$W/bin:\$PATH"
    echo "export CCHP_CONFIG_DIR=$dir/config CCHP_CACHE_DIR=$W/cache CCHP_CREDENTIAL_STORE=file"
    echo "unset CCHP_TOKEN CCHP_TOKEN_FILE CCHP_BASE_URL CCHP_AGENT"
    echo "export PS1='\$ '"
    for line in "$@"; do echo "$line"; done
  } > "$W/$name.env"
}

# Signed in as the e2e administrator.
rm -rf "$W/state-admin"
write_env admin "$W/state-admin"
CCHP_CONFIG_DIR="$W/state-admin/config" CCHP_CACHE_DIR="$W/cache" CCHP_CREDENTIAL_STORE=file \
  "$W/bin/cchp" login --server "$server" --token-stdin --json < "$E2E_DIR/admin.token" > /dev/null
# Not signed in yet.
write_env fresh "$W/state-fresh"
# The limited token from the environment.
write_env limited "$W/state-limited" "export CCHP_BASE_URL=$server CCHP_TOKEN_FILE=$E2E_DIR/limited.token"

render() { # tape env-name [token]
  local name=$1 env=$2 token=${3:-}
  local text
  text="$(cat "docs/media/tapes/$name.tape")"
  text="${text//\{\{ENV\}\}/$W/$env.env}"
  text="${text//\{\{SERVER\}\}/$server}"
  text="${text//\{\{TOKEN\}\}/$token}"
  printf '%s\n' "$text" > "$W/tapes/$name.tape"
}

tapes=("$@")
if [ ${#tapes[@]} -eq 0 ]; then
  tapes=(login home errors discover)
fi
for tape in "${tapes[@]}"; do
  case "$tape" in
    login)
      rm -rf "$W/state-fresh" "$W/login-token.json"
      printf '%s' '{"name":"vhs login demo","permissions":["usage:read"]}' |
        CCHP_CONFIG_DIR="$W/state-admin/config" CCHP_CACHE_DIR="$W/cache" CCHP_CREDENTIAL_STORE=file \
          "$W/bin/cchp" api createAPIKey --body-file - --allow-write --confirm createAPIKey \
          --secret-out "$W/login-token.json" --json > /dev/null
      plaintext="$(jq -r '.values.plaintext' "$W/login-token.json")"
      rm -f "$W/login-token.json"
      render login fresh "$plaintext"
      ;;
    errors) render errors limited ;;
    *) render "$tape" admin ;;
  esac
  vhs "$W/tapes/$tape.tape"
done
