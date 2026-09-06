VERSION ?= dev
V := $(patsubst v%,%,$(VERSION))
LDFLAGS := -s -w -X github.com/neitomic/postern/internal/version.Version=$(V)

.PHONY: test build dist pack

# VPS install: scripts/vps-bootstrap.sh copies dist/linux-amd64/posternd (or
# POSTERND_BIN) to /usr/bin/posternd and ln -s posternd /usr/bin/posternd-shell
# (argv0 dispatcher; not a cobra command). sshd -t hard-fails before reload.
# Merge gate: CGO_ENABLED=0 go test -tags=integration ./...  (skip only if sshd is absent)

test:
	CGO_ENABLED=0 go test ./...

build:
	mkdir -p dist
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o dist/posternd ./cmd/posternd
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o dist/postern  ./cmd/postern
	ln -sfn posternd dist/posternd-shell

# Cross-compile into dist/<goos>-<goarch>/. CGO_ENABLED=0 (modernc sqlite).
# posternd is Linux-only at runtime (serve fatals elsewhere); still ship it
# on linux-amd64 and linux-arm64. Darwin archives are the agent/client only.
dist:
	mkdir -p dist/linux-amd64 dist/linux-arm64 dist/darwin-amd64 dist/darwin-arm64
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "$(LDFLAGS)" -o dist/linux-amd64/posternd ./cmd/posternd
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "$(LDFLAGS)" -o dist/linux-amd64/postern ./cmd/postern
	ln -sfn posternd dist/linux-amd64/posternd-shell
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags "$(LDFLAGS)" -o dist/linux-arm64/posternd ./cmd/posternd
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags "$(LDFLAGS)" -o dist/linux-arm64/postern ./cmd/postern
	ln -sfn posternd dist/linux-arm64/posternd-shell
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags "$(LDFLAGS)" -o dist/darwin-arm64/postern ./cmd/postern
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -trimpath -ldflags "$(LDFLAGS)" -o dist/darwin-amd64/postern ./cmd/postern

pack: dist
	VERSION=$(V) ./scripts/pack-release.sh
