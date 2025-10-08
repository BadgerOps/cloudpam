.DEFAULT_GOAL := help

# If `just` is installed, Make will delegate targets to it.
# Otherwise, Make runs the fallback recipes defined below.

GO_CACHE_DIR := $(CURDIR)/tmp/go-cache
export GOCACHE := $(GO_CACHE_DIR)

FALLBACK_TARGETS := dev build sqlite-build sqlite-run fmt lint test test-race cover tidy

# Mark only help and fallback targets as phony. Do not mark
# the top-level targets themselves as phony, so the catch‑all
# pattern rule (%:) can delegate to Just or fallback recipes.
.PHONY: help $(addprefix .fallback-,$(FALLBACK_TARGETS))
.PHONY: .go-cache-dir

help:
	@echo "Targets: $(FALLBACK_TARGETS)"
	@echo "If 'just' is installed, 'make <target>' delegates to 'just <target>'"

%:
	@if command -v just >/dev/null 2>&1; then \
		echo "delegating to: just $@"; \
		just $@; \
	else \
		$(MAKE) .fallback-$@; \
	fi

.go-cache-dir:
	@mkdir -p $(GO_CACHE_DIR)

.fallback-dev: .go-cache-dir
	go run ./cmd/cloudpam

.fallback-build: .go-cache-dir
	go build -o cloudpam ./cmd/cloudpam

.fallback-sqlite-build: .go-cache-dir
	go build -tags sqlite -o cloudpam ./cmd/cloudpam

.fallback-sqlite-run: .fallback-sqlite-build
	SQLITE_DSN="file:cloudpam.db?cache=shared&_fk=1" ./cloudpam

.fallback-fmt: .go-cache-dir
	go fmt ./...

.fallback-lint:
	@command -v golangci-lint >/dev/null || { echo "golangci-lint not found. Install from https://golangci-lint.run/ to use 'make lint'"; exit 1; }
	@v=$$(golangci-lint --version 2>/dev/null | awk '{print $$4}' | sed 's/^v//'); req=1.61.0; \
	if [ -n "$$v" ] && [ "$$req" != "$$(printf '%s\n' "$$req" "$$v" | sort -V | head -n1)" ]; then \
		echo "golangci-lint $$v is too old; need >= $$req for Go 1.24"; \
		exit 1; \
	fi
	golangci-lint run

.fallback-test: .go-cache-dir
	go test ./...

.fallback-test-race: .go-cache-dir
	go test -race ./...

.fallback-cover: .go-cache-dir
	set -euo pipefail; \
	go test ./... -covermode=atomic -coverprofile=coverage.out; \
	go tool cover -func=coverage.out | tee coverage.txt; \
	go tool cover -html=coverage.out -o coverage.html; \
	echo "wrote coverage.out and coverage.html"

.fallback-tidy: .go-cache-dir
	go mod tidy
