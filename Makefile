BINARY   := watchcat-agent
PKG      := ./cmd/watchcat-agent
VERSION  ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT   ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
DATE     ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS  := -s -w \
  -X github.com/aritaone/watchcat-agent/internal/buildinfo.Version=$(VERSION) \
  -X github.com/aritaone/watchcat-agent/internal/buildinfo.Commit=$(COMMIT) \
  -X github.com/aritaone/watchcat-agent/internal/buildinfo.BuildDate=$(DATE)

DIST := dist
# GOOS/GOARCH pairs shipped on every release.
PLATFORMS := \
  linux/amd64 linux/arm64 linux/arm \
  darwin/amd64 darwin/arm64 \
  windows/amd64 windows/arm64

.PHONY: all build test vet fmt lint run dist clean checksums help

all: fmt vet test build

## build: compile for the current platform into ./bin
build:
	@mkdir -p bin
	CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/$(BINARY) $(PKG)
	@echo "built bin/$(BINARY) ($(VERSION))"

## test: run the test suite with the race detector
test:
	go test -race -count=1 ./...

vet:
	go vet ./...

fmt:
	gofmt -l -w .

## run: run in the foreground against the sample config
run: build
	./bin/$(BINARY) run --config configs/watchcat-agent.yaml

## dist: cross-compile release binaries for every supported platform
dist: clean
	@mkdir -p $(DIST)
	@for p in $(PLATFORMS); do \
	  os=$${p%/*}; arch=$${p#*/}; \
	  ext=""; [ "$$os" = "windows" ] && ext=".exe"; \
	  out=$(DIST)/$(BINARY)_$(VERSION)_$${os}_$${arch}$$ext; \
	  echo "  -> $$out"; \
	  CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch \
	    go build -trimpath -ldflags "$(LDFLAGS)" -o $$out $(PKG) || exit 1; \
	done
	@$(MAKE) --no-print-directory checksums

## checksums: write SHA256SUMS for the built artifacts
checksums:
	@cd $(DIST) && (command -v sha256sum >/dev/null && sha256sum * > SHA256SUMS \
	  || shasum -a 256 * > SHA256SUMS)
	@echo "wrote $(DIST)/SHA256SUMS"

clean:
	rm -rf $(DIST) bin

help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/## //'
