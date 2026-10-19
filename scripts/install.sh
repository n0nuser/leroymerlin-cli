#!/bin/sh
# Installs the leroymerlin release binary into ~/.local/bin
# (or $LEROYMERLIN_INSTALL_DIR, or $PREFIX/bin).
#
#   curl -fsSL https://raw.githubusercontent.com/n0nuser/leroymerlin-cli/main/scripts/install.sh | sh
#
# Pin a version with VERSION=0.1.0, or a directory with LEROYMERLIN_INSTALL_DIR.
set -eu

repo="n0nuser/leroymerlin-cli"
binary="leroymerlin"
dir="${LEROYMERLIN_INSTALL_DIR:-${PREFIX:-$HOME/.local}/bin}"

command -v curl >/dev/null 2>&1 || { echo "curl is required" >&2; exit 1; }

os=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$os" in
  linux|darwin) ;;
  *) echo "unsupported os: $os. Download the archive from https://github.com/$repo/releases" >&2; exit 1 ;;
esac

arch=$(uname -m)
case "$arch" in
  x86_64|amd64) arch=amd64 ;;
  arm64|aarch64) arch=arm64 ;;
  *) echo "unsupported arch: $arch. Download the archive from https://github.com/$repo/releases" >&2; exit 1 ;;
esac

if [ -n "${VERSION:-}" ]; then
  tag="v${VERSION#v}"
else
  tag=$(curl -fsSL "https://api.github.com/repos/$repo/releases/latest" |
    sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p')
  [ -n "$tag" ] || { echo "could not find the latest release of $repo" >&2; exit 1; }
fi
version=${tag#v}
asset="${binary}_${version}_${os}_${arch}.tar.gz"

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
curl -fsSL "https://github.com/$repo/releases/download/$tag/$asset" -o "$tmp/$asset"
curl -fsSL "https://github.com/$repo/releases/download/$tag/checksums.txt" -o "$tmp/checksums.txt"

# macOS ships shasum, most Linux distributions ship sha256sum.
if command -v sha256sum >/dev/null 2>&1; then
  checksum_cmd="sha256sum"
elif command -v shasum >/dev/null 2>&1; then
  checksum_cmd="shasum -a 256"
else
  echo "sha256sum or shasum is required to verify the download" >&2
  exit 1
fi
(cd "$tmp" && grep " $asset\$" checksums.txt | $checksum_cmd -c - >/dev/null)

tar -xzf "$tmp/$asset" -C "$tmp" "$binary"
mkdir -p "$dir"
if command -v install >/dev/null 2>&1; then
  install -m 0755 "$tmp/$binary" "$dir/$binary"
else
  cp "$tmp/$binary" "$dir/$binary"
  chmod 0755 "$dir/$binary"
fi

echo "installed $binary $version to $dir/$binary"
case ":$PATH:" in
  *":$dir:"*) ;;
  *) echo "add $dir to your PATH to run it from anywhere" ;;
esac
