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

sqlite-build: ensure-cache
    {{go-env}} go build -tags sqlite -o cloudpam ./cmd/cloudpam

sqlite-run: sqlite-build
    SQLITE_DSN="file:cloudpam.db?cache=shared&_fk=1" ./cloudpam

fmt: ensure-cache
    {{go-env}} go fmt ./...

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

openapi-validate:
    ruby "{{justfile_directory()}}/scripts/openapi_validate.rb" "{{openapi_spec}}"

openapi-html:
    if ! command -v {{openapi_generator}} >/dev/null 2>&1; then \
      echo "openapi-generator-cli not found. Install: https://openapi-generator.tech/docs/installation/"; \
      exit 1; \
    fi
    rm -rf "{{openapi_html_dir}}"
    {{openapi_generator}} generate -g html2 -i "{{openapi_spec}}" -o "{{openapi_html_dir}}"
