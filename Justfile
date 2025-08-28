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

tidy:
    go mod tidy
