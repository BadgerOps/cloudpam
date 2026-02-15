# syntax=docker/dockerfile:1

# ---------- build stage ----------
FROM golang:1.24-alpine AS builder

RUN apk add --no-cache git

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG VERSION=dev
RUN CGO_ENABLED=0 go build \
    -trimpath \
    -tags 'sqlite postgres' \
    -ldflags "-s -w -X main.version=${VERSION}" \
    -o /cloudpam ./cmd/cloudpam

# ---------- runtime stage ----------
FROM alpine:3.21

RUN apk add --no-cache ca-certificates tzdata curl \
    && addgroup -S cloudpam && adduser -S cloudpam -G cloudpam

COPY --from=builder /cloudpam /usr/local/bin/cloudpam

USER cloudpam
EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD curl -f http://localhost:8080/healthz || exit 1

ENTRYPOINT ["cloudpam"]
