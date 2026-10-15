#!/usr/bin/env bash
# Re-export the CCHP control-plane spec and refresh everything derived from it.
#
#   scripts/sync-spec.sh <path-to-CCHP-checkout>
#   scripts/sync-spec.sh <tag>            (clones CCH-HQ/claude-code-hub-plus at that tag)
#
# The export runs `go run ./cmd/openapi-export` inside the CCHP tree, exactly as
# CCHP's own `make openapi-export` does, so spec/upstream/control-plane.yaml is
# byte for byte what CCHP generates.
set -euo pipefail

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
upstream="${1:?usage: sync-spec.sh <path-or-tag>}"
work="$repo_root/.local-e2e/sync-spec"
mkdir -p "$work"
export GOTMPDIR="$work"

if [ -d "$upstream" ]; then
  src="$(cd "$upstream" && pwd)"
else
  src="$work/claude-code-hub-plus-$upstream"
  if [ ! -d "$src" ]; then
    git clone --quiet --depth 1 --branch "$upstream" https://github.com/CCH-HQ/claude-code-hub-plus.git "$src"
  fi
fi

commit="$(git -C "$src" rev-parse HEAD)"
tag="$(git -C "$src" describe --tags --exact-match HEAD)"

(cd "$src" && go run ./cmd/openapi-export -out "$repo_root/spec/upstream/control-plane.yaml")
cd "$repo_root"
go run ./cmd/cchp-gen -lock -tag "$tag" -commit "$commit"
go run ./cmd/cchp-gen
echo "Synced spec from $tag ($commit). Review spec/cli-overlay.yaml and spec/operation-policy.yaml for new operations."
