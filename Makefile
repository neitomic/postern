VERSION ?= dev

.PHONY: test build

# VPS install: copy dist/posternd to /usr/bin/posternd and
# ln -s posternd /usr/bin/posternd-shell (argv0 dispatcher; not a cobra command).
# Merge gate: CGO_ENABLED=0 go test -tags=integration ./...  (skip only if sshd is absent)

test:
	CGO_ENABLED=0 go test ./...

build:
	mkdir -p dist
	CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X github.com/neitomic/postern/internal/version.Version=${VERSION}" -o dist/posternd ./cmd/posternd
	CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X github.com/neitomic/postern/internal/version.Version=${VERSION}" -o dist/postern  ./cmd/postern
	ln -sfn posternd dist/posternd-shell
