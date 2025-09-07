set shell := ["bash", "-cu"]

default:
    @just --list

dev:
    go run ./cmd/cloudpam

build:
    go build -o cloudpam ./cmd/cloudpam

sqlite-build:
    go build -tags sqlite -o cloudpam ./cmd/cloudpam

sqlite-run: sqlite-build
    SQLITE_DSN="file:cloudpam.db?cache=shared&_fk=1" ./cloudpam

fmt:
    go fmt ./...

lint:
    # Ensure golangci-lint is installed and new enough for Go 1.24
    if ! command -v golangci-lint >/dev/null 2>&1; then \
      echo "golangci-lint not found. Install from https://golangci-lint.run/ or use Docker image golangci/golangci-lint"; \
      exit 1; \
    fi; \
    v=$(golangci-lint --version 2>/dev/null | awk '{print $4}' | sed 's/^v//'); \
    req=1.61.0; \
    if [ -n "$v" ] && [ "$req" != "$(printf '%s\n' "$req" "$v" | sort -V | head -n1)" ]; then \
      echo "golangci-lint $v is too old; need >= $req for Go 1.24. Upgrade: https://golangci-lint.run/welcome/install/"; \
      exit 1; \
    fi; \
    golangci-lint run

test:
    go test ./...

test-race:
    go test -race ./...

cover:
    set -euo pipefail
    go test ./... -covermode=atomic -coverprofile=coverage.out -v
    go tool cover -func=coverage.out | tee coverage.txt
    go tool cover -html=coverage.out -o coverage.html
    @echo "wrote coverage.out and coverage.html"

cover-threshold thr="0":
    set -euo pipefail
    go test ./... -covermode=atomic -coverprofile=coverage.out -v
    total=$(go tool cover -func=coverage.out | grep total: | awk '{print substr($3, 1, length($3)-1)}')
    awk -v t="$total" -v thr="{{thr}}" 'BEGIN{ if (t+0 < thr+0) { printf("coverage %.2f%% is below threshold %.2f%%\n", t, thr); exit 1 } else { printf("coverage %.2f%% meets threshold %.2f%%\n", t, thr); } }'

tidy:
    go mod tidy
