#!/bin/sh
# Install postern from GitHub releases into ~/.local/bin and enable the agent.
#
#   curl -fsSL https://github.com/neitomic/postern/releases/latest/download/install.sh | sh
#   POSTERN_VERSION=0.1.0 sh install.sh
#
# Do not run as root. The agent is a per-user LaunchAgent / systemd --user unit.
set -eu

REPO=${POSTERN_REPO:-neitomic/postern}
PREFIX=${POSTERN_PREFIX:-}

die() {
	echo "postern-install: $*" >&2
	exit 1
}

if [ "$(id -u)" -eq 0 ]; then
	die "do not run as root; the agent is a user service"
fi

os=$(uname -s)
arch=$(uname -m)
case "$os" in
Darwin) goos=darwin ;;
Linux) goos=linux ;;
*) die "unsupported OS $os (need macOS or Linux)" ;;
esac
case "$arch" in
x86_64 | amd64) goarch=amd64 ;;
arm64 | aarch64) goarch=arm64 ;;
*) die "unsupported arch $arch (need amd64 or arm64)" ;;
esac

need() {
	command -v "$1" >/dev/null 2>&1 || die "need $1 on PATH"
}
need curl
need tar
need uname

version=${POSTERN_VERSION:-}
if [ -z "$version" ]; then
	version=$(curl -fsSI "https://github.com/${REPO}/releases/latest" | tr -d '\r' | grep -i '^location:' | tail -n 1)
	version=${version##*/}
	[ -n "$version" ] || die "could not resolve latest release tag"
fi
version=${version#v}

url="https://github.com/${REPO}/releases/download/v${version}/postern_${version}_${goos}_${goarch}.tar.gz"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

echo "postern-install: downloading $url"
curl -fsSL -o "$tmp/postern.tgz" "$url"
tar -xzf "$tmp/postern.tgz" -C "$tmp"
bin="$tmp/postern"
if [ ! -f "$bin" ]; then
	bin=$(find "$tmp" -name postern -type f | head -n 1)
fi
[ -n "$bin" ] && [ -f "$bin" ] || die "archive had no postern binary"
chmod 755 "$bin"
if [ "$(uname -s)" = Darwin ]; then
	xattr -d com.apple.quarantine "$bin" 2>/dev/null || true
fi

if [ -n "$PREFIX" ]; then
	"$bin" install --bin-dir "$PREFIX"
else
	"$bin" install
fi
