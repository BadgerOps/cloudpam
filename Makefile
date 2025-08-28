.DEFAULT_GOAL := help

# If `just` is installed, Make will delegate targets to it.
# Otherwise, Make runs the fallback recipes defined below.

FALLBACK_TARGETS := dev build sqlite-build sqlite-run fmt lint test tidy

.PHONY: help $(FALLBACK_TARGETS)

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

.fallback-dev:
	go run ./cmd/cloudpam

.fallback-build:
	go build -o cloudpam ./cmd/cloudpam

.fallback-sqlite-build:
	go build -tags sqlite -o cloudpam ./cmd/cloudpam

.fallback-sqlite-run: .fallback-sqlite-build
	SQLITE_DSN="file:cloudpam.db?cache=shared&_fk=1" ./cloudpam

.fallback-fmt:
	go fmt ./...

.fallback-lint:
	@command -v golangci-lint >/dev/null || { echo "golangci-lint not found. Install from https://golangci-lint.run/ to use 'make lint'"; exit 1; }
	golangci-lint run

.fallback-test:
	go test ./...

.fallback-tidy:
	go mod tidy

