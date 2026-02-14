set shell := ["bash", "-cu"]

cache := justfile_directory() + "/tmp/go-cache"
go-env := "GOCACHE=" + cache

openapi_generator := "openapi-generator-cli"
openapi_spec := justfile_directory() + "/docs/openapi.yaml"
openapi_html_dir := justfile_directory() + "/docs/openapi-html"

default:
    @just --list

ensure-cache:
    mkdir -p "{{cache}}"

dev: ensure-cache
    {{go-env}} go run ./cmd/cloudpam

build: ensure-cache
    {{go-env}} go build -o cloudpam ./cmd/cloudpam

agent-build: ensure-cache
    {{go-env}} go build -o cloudpam-agent ./cmd/cloudpam-agent

agent-run config="agent.yaml": agent-build
    ./cloudpam-agent -config {{config}}

sqlite-build: ensure-cache
    {{go-env}} go build -tags sqlite -o cloudpam ./cmd/cloudpam

sqlite-run: sqlite-build
    SQLITE_DSN="file:cloudpam.db?cache=shared&_fk=1" ./cloudpam

postgres-build: ensure-cache
    {{go-env}} go build -tags postgres -o cloudpam ./cmd/cloudpam

postgres-run: postgres-build
    DATABASE_URL="postgres://cloudpam:cloudpam@localhost:5432/cloudpam?sslmode=disable" ./cloudpam

postgres-up:
    docker compose up -d postgres

postgres-down:
    docker compose down

postgres-test: ensure-cache
    {{go-env}} go test -tags postgres ./...

fmt: ensure-cache
    {{go-env}} go fmt ./...

lint:
    #!/usr/bin/env bash
    set -euo pipefail
    # Find golangci-lint: PATH, ~/go/bin, or GOPATH/bin
    LINT=""
    for candidate in golangci-lint "$HOME/go/bin/golangci-lint" "$(go env GOPATH 2>/dev/null)/bin/golangci-lint"; do
      if command -v "$candidate" >/dev/null 2>&1; then LINT="$candidate"; break; fi
    done
    if [ -z "$LINT" ]; then
      echo "golangci-lint not found. Install: curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/HEAD/install.sh | sh -s -- -b \$(go env GOPATH)/bin v2.1.6"
      exit 1
    fi
    echo "Using $($LINT --version 2>&1)"
    $LINT run --timeout=5m

test: ensure-cache
    {{go-env}} go test ./...

test-race: ensure-cache
    {{go-env}} go test -race ./...

cover: ensure-cache
    set -euo pipefail
    {{go-env}} go test ./... -covermode=atomic -coverprofile=coverage.out -v
    {{go-env}} go tool cover -func=coverage.out | tee coverage.txt
    {{go-env}} go tool cover -html=coverage.out -o coverage.html
    @echo "wrote coverage.out and coverage.html"

cover-threshold thr="0": ensure-cache
    set -euo pipefail
    {{go-env}} go test ./... -covermode=atomic -coverprofile=coverage.out -v
    total=$({{go-env}} go tool cover -func=coverage.out | grep total: | awk '{print substr($3, 1, length($3)-1)}')
    awk -v t="$total" -v thr="{{thr}}" 'BEGIN{ if (t+0 < thr+0) { printf("coverage %.2f%% is below threshold %.2f%%\n", t, thr); exit 1 } else { printf("coverage %.2f%% meets threshold %.2f%%\n", t, thr); } }'

tidy: ensure-cache
    {{go-env}} go mod tidy

docker-build tag="cloudpam:latest":
    docker build -t {{tag}} .

docker-run tag="cloudpam:latest": docker-build
    docker run --rm -p 8080:8080 {{tag}}

ui-install:
    cd ui && npm install

ui-build: ui-install
    cd ui && npm run build

ui-dev:
    cd ui && npm run dev

ui-test:
    cd ui && npm test

build-full: ui-build build

# Run Go backend and Vite dev server concurrently (Ctrl-C stops both)
dev-all: ensure-cache ui-install
    trap 'kill 0' EXIT; \
    {{go-env}} go run ./cmd/cloudpam & \
    cd ui && npm run dev & \
    wait

# Install git hooks (pre-commit lint check)
install-hooks:
    cp scripts/pre-commit .git/hooks/pre-commit
    chmod +x .git/hooks/pre-commit
    @echo "pre-commit hook installed"

openapi-validate:
    ruby "{{justfile_directory()}}/scripts/openapi_validate.rb" "{{openapi_spec}}"

openapi-html:
    if ! command -v {{openapi_generator}} >/dev/null 2>&1; then \
      echo "openapi-generator-cli not found. Install: https://openapi-generator.tech/docs/installation/"; \
      exit 1; \
    fi
    rm -rf "{{openapi_html_dir}}"
    {{openapi_generator}} generate -g html2 -i "{{openapi_spec}}" -o "{{openapi_html_dir}}"
