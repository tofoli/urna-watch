SHELL := /bin/bash
GO ?= go
BINARY := bin/urna-watch
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo 0.1.0)
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo dev)
BUILD_DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w \
	-X github.com/tofoli/urna-watch/internal/version.Version=$(VERSION) \
	-X github.com/tofoli/urna-watch/internal/version.Commit=$(COMMIT) \
	-X github.com/tofoli/urna-watch/internal/version.Date=$(BUILD_DATE)

.PHONY: all build test test-race cover lint fmt fmt-check check run once validate docker-build docker-up docker-down clean

all: check

build:
	@mkdir -p bin
	$(GO) build -trimpath -ldflags "$(LDFLAGS)" -o $(BINARY) ./cmd/urna-watch

test:
	$(GO) test ./...

test-race:
	$(GO) test -race ./...

cover:
	$(GO) test -coverprofile=coverage.out ./...
	$(GO) tool cover -func=coverage.out | tail -1

lint:
	$(GO) vet ./...
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run; \
	else \
		echo "golangci-lint não encontrado; go vet foi executado."; \
	fi

fmt:
	gofmt -w cmd internal tests

fmt-check:
	@out="$$(gofmt -l cmd internal tests)"; \
	if [ -n "$$out" ]; then echo "Arquivos sem gofmt:"; echo "$$out"; exit 1; fi

check: fmt-check lint test build

run:
	$(GO) run ./cmd/urna-watch

once:
	$(GO) run ./cmd/urna-watch --once --no-tui

validate:
	$(GO) run ./cmd/urna-watch --validate --no-tui

docker-build:
	docker build -t urna-watch:local .

docker-up:
	docker compose up -d --build

docker-down:
	docker compose down

clean:
	rm -rf bin coverage.out
