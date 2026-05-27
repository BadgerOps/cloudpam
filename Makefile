.DEFAULT_GOAL := help

# If `just` is installed, Make will delegate targets to it.
# Otherwise, Make runs the fallback recipes defined below.

FALLBACK_TARGETS := dev run build sqlite-build sqlite-run fmt lint test test-race cover tidy

.PHONY: help $(FALLBACK_TARGETS) cover-threshold

help:
	@echo "Targets: $(FALLBACK_TARGETS)"
	@echo "If 'just' is installed, 'make <target>' delegates to 'just <target>'"

$(FALLBACK_TARGETS):
	@if command -v just >/dev/null 2>&1; then \
		echo "delegating to: just $@"; \
		just $@; \
	else \
		$(MAKE) .fallback-$@; \
	fi

cover-threshold:
	@if command -v just >/dev/null 2>&1; then \
		echo "delegating to: just cover-threshold thr=$(thr)"; \
		just cover-threshold thr=$(thr); \
	else \
		$(MAKE) .fallback-cover-threshold thr=$(thr); \
	fi

.fallback-dev:
	DEV_MODE=1 go run ./cmd/cloudpam

.fallback-run: .fallback-dev

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
	@v=$$(golangci-lint --version 2>/dev/null | awk '{print $$4}' | sed 's/^v//'); req=1.61.0; \
	if [ -n "$$v" ] && [ "$$req" != "$$(printf '%s\n' "$$req" "$$v" | sort -V | head -n1)" ]; then \
		echo "golangci-lint $$v is too old; need >= $$req for Go 1.24"; \
		exit 1; \
	fi
	golangci-lint run

.fallback-test:
	go test ./...

.fallback-test-race:
	go test -race ./...

.fallback-cover:
	go test ./... -covermode=atomic -coverprofile=coverage.out -v
	go tool cover -func=coverage.out | tee coverage.txt
	go tool cover -html=coverage.out -o coverage.html
	@echo "wrote coverage.out and coverage.html"

.fallback-cover-threshold:
	go test ./... -covermode=atomic -coverprofile=coverage.out -v
	@total=$$(go tool cover -func=coverage.out | grep total: | awk '{print substr($$3, 1, length($$3)-1)}'); \
	awk -v t="$$total" -v thr="$(thr)" 'BEGIN{ if (t+0 < thr+0) { printf("coverage %.2f%% is below threshold %.2f%%\n", t, thr); exit 1 } else { printf("coverage %.2f%% meets threshold %.2f%%\n", t, thr); } }'

.fallback-tidy:
	go mod tidy
