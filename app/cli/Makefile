BIN := bin/specgate
VERSION ?= dev
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -X github.com/specgate/specgate/app/cli/internal/buildinfo.Version=$(VERSION) \
           -X github.com/specgate/specgate/app/cli/internal/buildinfo.Commit=$(COMMIT) \
           -X github.com/specgate/specgate/app/cli/internal/buildinfo.Date=$(DATE)

.PHONY: build test lint fmt tidy clean

build:
	go build -ldflags "$(LDFLAGS)" -o $(BIN) ./cmd/specgate

test:
	go test -race -count=1 ./...
	node --test internal/command/local_ui_assets_test.cjs internal/command/local_ui_evidence.test.cjs internal/command/local_ui_navigation.test.cjs

lint:
	go vet ./...

fmt:
	gofmt -s -w .

tidy:
	go mod tidy

clean:
	rm -f $(BIN)
