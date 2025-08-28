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
    golangci-lint run

test:
    go test ./...

tidy:
    go mod tidy

