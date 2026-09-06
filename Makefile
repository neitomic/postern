VERSION ?= dev

.PHONY: test build

test:
	CGO_ENABLED=0 go test ./...

build:
	mkdir -p dist
	CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X github.com/neitomic/postern/internal/version.Version=${VERSION}" -o dist/posternd ./cmd/posternd
	CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X github.com/neitomic/postern/internal/version.Version=${VERSION}" -o dist/postern  ./cmd/postern
