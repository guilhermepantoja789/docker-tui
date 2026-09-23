#!/usr/bin/env bash
# Install or update docker-tui from GitHub Releases.
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/guilhermepantoja789/docker-tui/main/scripts/install.sh | bash
# Optional env:
#   VERSION=v0.1.0   pin a release tag (default: latest)
#   BINDIR=/path     install directory (default: /usr/local/bin or ~/.local/bin)
set -euo pipefail

REPO="guilhermepantoja789/docker-tui"
BINARY="docker-tui"
API="https://api.github.com/repos/${REPO}/releases"

need() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "docker-tui install: missing required command: $1" >&2
    exit 1
  }
}

need curl
need tar
need uname
need mktemp

os="$(uname -s)"
arch="$(uname -m)"

case "$os" in
  Linux)  GOOS="Linux" ;;
  Darwin) GOOS="Darwin" ;;
  *)
    echo "docker-tui install: unsupported OS: $os (linux and macOS only)" >&2
    exit 1
    ;;
esac

case "$arch" in
  x86_64|amd64) GOARCH="x86_64" ;;
  arm64|aarch64) GOARCH="arm64" ;;
  *)
    echo "docker-tui install: unsupported architecture: $arch" >&2
    exit 1
    ;;
esac

if [[ -z "${BINDIR:-}" ]]; then
  if [[ -w /usr/local/bin ]] || [[ "$(id -u)" -eq 0 ]]; then
    BINDIR="/usr/local/bin"
  else
    BINDIR="${HOME}/.local/bin"
  fi
fi

tmpdir="$(mktemp -d)"
cleanup() { rm -rf "$tmpdir"; }
trap cleanup EXIT

if [[ -n "${VERSION:-}" ]]; then
  tag="$VERSION"
  [[ "$tag" == v* ]] || tag="v${tag}"
  asset_url="https://github.com/${REPO}/releases/download/${tag}/${BINARY}_${GOOS}_${GOARCH}.tar.gz"
  checksum_url="https://github.com/${REPO}/releases/download/${tag}/checksums.txt"
else
  need grep
  need sed
  json="$(curl -fsSL "${API}/latest")"
  tag="$(printf '%s' "$json" | sed -n 's/.*"tag_name":[[:space:]]*"\([^"]*\)".*/\1/p' | head -n1)"
  if [[ -z "$tag" ]]; then
    echo "docker-tui install: could not resolve latest release tag" >&2
    exit 1
  fi
  asset_url="https://github.com/${REPO}/releases/download/${tag}/${BINARY}_${GOOS}_${GOARCH}.tar.gz"
  checksum_url="https://github.com/${REPO}/releases/download/${tag}/checksums.txt"
fi

archive="${tmpdir}/${BINARY}.tar.gz"
checksums="${tmpdir}/checksums.txt"
asset_name="${BINARY}_${GOOS}_${GOARCH}.tar.gz"

echo "Installing ${BINARY} ${tag} (${GOOS}/${GOARCH}) → ${BINDIR}"

curl -fsSL -o "$archive" "$asset_url"
curl -fsSL -o "$checksums" "$checksum_url"

if command -v sha256sum >/dev/null 2>&1; then
  expected="$(grep -E "[[:space:]]${asset_name}$" "$checksums" | awk '{print $1}' | head -n1)"
  actual="$(sha256sum "$archive" | awk '{print $1}')"
elif command -v shasum >/dev/null 2>&1; then
  expected="$(grep -E "[[:space:]]${asset_name}$" "$checksums" | awk '{print $1}' | head -n1)"
  actual="$(shasum -a 256 "$archive" | awk '{print $1}')"
else
  echo "docker-tui install: need sha256sum or shasum to verify checksums" >&2
  exit 1
fi

if [[ -z "$expected" ]]; then
  echo "docker-tui install: checksum entry not found for ${asset_name}" >&2
  exit 1
fi
if [[ "$expected" != "$actual" ]]; then
  echo "docker-tui install: checksum mismatch for ${asset_name}" >&2
  echo "  expected: $expected" >&2
  echo "  actual:   $actual" >&2
  exit 1
fi

tar -xzf "$archive" -C "$tmpdir"
mkdir -p "$BINDIR"
install -m 755 "${tmpdir}/${BINARY}" "${BINDIR}/${BINARY}"

echo "Installed ${BINDIR}/${BINARY}"
if ! command -v "$BINARY" >/dev/null 2>&1; then
  echo "Note: add ${BINDIR} to your PATH to run '${BINARY}'" >&2
fi
"${BINDIR}/${BINARY}" --version || true
