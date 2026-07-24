.PHONY: help build run run-only dev test lint fmt tidy clean migrate dev-secrets setup

BINARY := bin/doc-registry
PKG    := ./...
AIR    := $(shell command -v air 2>/dev/null || echo "$(HOME)/go/bin/air")

help:
	@echo "Targets:"
	@echo "  build        Build server binary to $(BINARY)"
	@echo "  run          Run the server (one-shot dev mode)"
	@echo "  run-only     Run the server without touching docker (expects deps already up)"
	@echo "  dev          Hot reload with Air (go install github.com/air-verse/air@latest)"
	@echo "  test         Run tests"
	@echo "  lint         Run go vet"
	@echo "  fmt          Run gofmt"
	@echo "  tidy         go mod tidy"
	@echo "  migrate      Apply PostgreSQL migrations"
	@echo "  setup        First-time setup: generate all required secrets into .env (idempotent)"
	@echo "  dev-secrets  Generate SETTINGS_ENCRYPTION_KEY into .env if missing (idempotent)"

# One-time setup for a fresh checkout: creates .env and generates all required
# secrets (SETTINGS_ENCRYPTION_KEY). Idempotent — safe to re-run.
setup:
	@./scripts/setup.sh

build:
	go build -o $(BINARY) ./cmd/doc-registry

# Generate the local dev encryption key into .env so a fresh checkout boots
# without manual setup. Idempotent.
dev-secrets:
	@./scripts/dev-secrets.sh

# `make run` starts Doc Registry in the foreground. PostgreSQL remains required
# through POSTGRES_DSN; use the root single-container appliance for normal
# repository-level development.
run: dev-secrets
	@echo "==> doc-registry ready."
	go run ./cmd/doc-registry

run-only: dev-secrets
	go run ./cmd/doc-registry

dev: dev-secrets
	@$(AIR) -c .air.toml

test:
	go test -race -count=1 $(PKG)

lint:
	go vet $(PKG)

fmt:
	gofmt -s -w .

tidy:
	go mod tidy

clean:
	rm -rf bin dist coverage.out

migrate:
	go run ./cmd/doc-registry --migrate-only
