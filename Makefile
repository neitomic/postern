VERSION ?= dev

.PHONY: test build dist

# VPS install: scripts/vps-bootstrap.sh copies dist/linux-amd64/posternd (or
# POSTERND_BIN) to /usr/bin/posternd and ln -s posternd /usr/bin/posternd-shell
# (argv0 dispatcher; not a cobra command). sshd -t hard-fails before reload.
# Merge gate: CGO_ENABLED=0 go test -tags=integration ./...  (skip only if sshd is absent)

test:
	CGO_ENABLED=0 go test ./...

build:
	mkdir -p dist
	CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X github.com/neitomic/postern/internal/version.Version=${VERSION}" -o dist/posternd ./cmd/posternd
	CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X github.com/neitomic/postern/internal/version.Version=${VERSION}" -o dist/postern  ./cmd/postern
	ln -sfn posternd dist/posternd-shell

# Cross-compile into dist/<goos>-<goarch>/. CGO_ENABLED=0 (modernc sqlite).
dist:
	mkdir -p dist/linux-amd64 dist/darwin-arm64 dist/darwin-amd64
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "-s -w -X github.com/neitomic/postern/internal/version.Version=${VERSION}" -o dist/linux-amd64/posternd ./cmd/posternd
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "-s -w -X github.com/neitomic/postern/internal/version.Version=${VERSION}" -o dist/linux-amd64/postern ./cmd/postern
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags "-s -w -X github.com/neitomic/postern/internal/version.Version=${VERSION}" -o dist/darwin-arm64/posternd ./cmd/posternd
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags "-s -w -X github.com/neitomic/postern/internal/version.Version=${VERSION}" -o dist/darwin-arm64/postern ./cmd/postern
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -trimpath -ldflags "-s -w -X github.com/neitomic/postern/internal/version.Version=${VERSION}" -o dist/darwin-amd64/posternd ./cmd/posternd
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -trimpath -ldflags "-s -w -X github.com/neitomic/postern/internal/version.Version=${VERSION}" -o dist/darwin-amd64/postern ./cmd/postern
	ln -sfn posternd dist/linux-amd64/posternd-shell
