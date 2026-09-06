#!/bin/sh
# Pack dist/<goos>-<goarch>/ into versioned tarballs + SHA256SUMS.
# Usage: VERSION=0.1.0 ./scripts/pack-release.sh
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
VERSION=${VERSION:-dev}
VERSION=${VERSION#v}
DIST="$ROOT/dist"

if [ ! -d "$DIST" ]; then
	echo "pack-release: $DIST missing; run make dist first" >&2
	exit 1
fi

pack_one() {
	osarch=$1
	dir="$DIST/$osarch"
	if [ ! -x "$dir/postern" ]; then
		echo "pack-release: missing $dir/postern" >&2
		exit 1
	fi
	os=${osarch%-*}
	arch=${osarch#*-}
	stage="$DIST/.stage-$os-$arch"
	rm -rf "$stage"
	mkdir -p "$stage"
	cp "$dir/postern" "$stage/postern"
	chmod 755 "$stage/postern"
	if [ -x "$dir/posternd" ]; then
		cp "$dir/posternd" "$stage/posternd"
		chmod 755 "$stage/posternd"
		ln -sfn posternd "$stage/posternd-shell"
		mkdir -p "$stage/scripts" "$stage/contrib/sshd" "$stage/contrib/systemd"
		cp "$ROOT/scripts/vps-bootstrap.sh" "$stage/scripts/"
		chmod 755 "$stage/scripts/vps-bootstrap.sh"
		cp "$ROOT/contrib/sshd/50-postern.conf" "$stage/contrib/sshd/"
		cp "$ROOT/contrib/systemd/posternd.service" "$stage/contrib/systemd/"
	fi
	cp "$ROOT/LICENSE" "$ROOT/README.md" "$stage/"
	tar -C "$stage" -czf "$DIST/postern_${VERSION}_${os}_${arch}.tar.gz" .
	rm -rf "$stage"
}

pack_one linux-amd64
pack_one linux-arm64
pack_one darwin-amd64
pack_one darwin-arm64

cp "$ROOT/scripts/install.sh" "$DIST/install.sh"

cd "$DIST"
if command -v sha256sum >/dev/null 2>&1; then
	sha256sum postern_${VERSION}_*.tar.gz >SHA256SUMS
else
	shasum -a 256 postern_${VERSION}_*.tar.gz >SHA256SUMS
fi
echo "pack-release: wrote tarballs for $VERSION"
ls -l postern_${VERSION}_*.tar.gz SHA256SUMS install.sh
