#!/bin/sh
# Install cchp from GitHub releases, checking the archive against checksums.txt.
#
#   curl -fsSL https://raw.githubusercontent.com/CCH-HQ/cchp-cli/main/install.sh | sh
#
# CCHP_VERSION    release to install, for example v0.1.0 (default: latest)
# CCHP_INSTALL_DIR where to install (default: ~/.local/bin)
set -eu

repo="CCH-HQ/cchp-cli"
install_dir="${CCHP_INSTALL_DIR:-$HOME/.local/bin}"

fail() {
  echo "cchp install: $*" >&2
  exit 1
}

need() {
  command -v "$1" > /dev/null 2>&1 || fail "this script needs $1"
}

need curl
need tar
need uname

case "$(uname -s)" in
  Linux) os=linux ;;
  Darwin) os=darwin ;;
  *) fail "unsupported system $(uname -s); download a release from https://github.com/$repo/releases" ;;
esac

case "$(uname -m)" in
  x86_64 | amd64) arch=amd64 ;;
  aarch64 | arm64) arch=arm64 ;;
  *) fail "unsupported processor $(uname -m)" ;;
esac

if command -v sha256sum > /dev/null 2>&1; then
  sha256() { sha256sum "$1" | cut -d' ' -f1; }
elif command -v shasum > /dev/null 2>&1; then
  sha256() { shasum -a 256 "$1" | cut -d' ' -f1; }
else
  fail "this script needs sha256sum or shasum"
fi

tag="${CCHP_VERSION:-}"
if [ -z "$tag" ]; then
  # The latest-release page redirects to .../releases/tag/<tag>.
  latest="$(curl -fsSLI -o /dev/null -w '%{url_effective}' "https://github.com/$repo/releases/latest")"
  tag="${latest##*/}"
fi
case "$tag" in
  v*) ;;
  *) fail "could not determine the release to install (got '$tag')" ;;
esac
version="${tag#v}"

archive="cchp_${version}_${os}_${arch}.tar.gz"
base="https://github.com/$repo/releases/download/$tag"

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

echo "Downloading cchp $tag for $os/$arch"
curl -fsSL -o "$work/$archive" "$base/$archive"
curl -fsSL -o "$work/checksums.txt" "$base/checksums.txt"

expected="$(grep "  $archive\$" "$work/checksums.txt" | cut -d' ' -f1)"
[ -n "$expected" ] || fail "checksums.txt has no entry for $archive"
actual="$(sha256 "$work/$archive")"
[ "$expected" = "$actual" ] || fail "checksum mismatch for $archive: expected $expected, got $actual"

tar -xzf "$work/$archive" -C "$work" cchp
mkdir -p "$install_dir"
install -m 0755 "$work/cchp" "$install_dir/cchp"

echo "Installed cchp $tag to $install_dir/cchp"
case ":$PATH:" in
  *":$install_dir:"*) ;;
  *) echo "Add $install_dir to your PATH to run cchp." ;;
esac
echo "Next: cchp login"
