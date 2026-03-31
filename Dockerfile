# syntax=docker/dockerfile:1

# ---------- build stage ----------
FROM golang:1.25-alpine AS builder

RUN apk add --no-cache git nodejs npm

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY ui/package.json ui/package-lock.json ./ui/
RUN cd ui && npm ci

COPY . .
RUN cd ui && npm run build

ARG VERSION=dev
RUN CGO_ENABLED=0 go build \
    -trimpath \
    -tags 'sqlite postgres' \
    -ldflags "-s -w -X main.version=${VERSION}" \
    -o /cloudpam ./cmd/cloudpam

# ---------- runtime stage ----------
FROM cgr.dev/chainguard/static:latest

COPY --from=builder /cloudpam /usr/local/bin/cloudpam

USER nonroot
EXPOSE 8080

ENTRYPOINT ["cloudpam"]
