# Files Changed

- `internal/api/openapi.go` — added runtime OpenAPI route registry, route-to-operation catalog, schema generation, and YAML emission.
- `internal/api/server.go` plus route registration files — changed route registration to record OpenAPI patterns while still registering with `http.ServeMux`.
- `internal/api/system_handlers.go` — changed `/openapi.yaml` to serve generated YAML and added `/openapi` Scalar API reference page.
- `cmd/openapi-validate/main.go` — added Go-based OpenAPI sanity validator for files, stdin, or URLs.
- `internal/api/handlers_test.go`, `internal/api/openapi_validation_test.go` — added focused coverage for runtime spec generation, `/openapi`, metrics-aware route output, and unresolved `$ref` validation.
- `docs/openapi.yaml` — removed static maintained spec.
- `scripts/openapi_validate.rb` — removed Ruby validator.
- `Justfile`, `README.md`, `AGENTS.md`, `CLAUDE.md`, `docs/SMART_PLANNING.md`, `docs/CHANGELOG.md` — updated docs/tooling to point to runtime OpenAPI and added the `0.14.6` changelog entry.

# Behavior Implemented

- `/openapi.yaml` is now generated from routes registered by the running server.
- `/openapi` serves a Scalar interactive API reference backed by `/openapi.yaml`.
- The raw OpenAPI route remains available for tools and client generation.
- `just openapi-validate` now runs Go validation through the Nix toolchain instead of Ruby.
- `just openapi-validate-url` validates a running server's raw spec URL.
- Changelog guidance now explicitly forbids `Unreleased` sections in this repo.

# Deviations From Spec

- The original planner expected an offline `docs/openapi.yaml` snapshot. The user clarified that the static snapshot should be removed, so the implementation removed it and made the in-app spec authoritative.
- Scalar uses a pinned CDN script in the served HTML page. This keeps the PR small and avoids vendoring a frontend dependency, but it means the interactive page needs CDN access unless we later vendor the asset.

# Suggested Verification

- `env XDG_CACHE_HOME=/tmp/nix-cache nix develop -c go test ./...`
- `env XDG_CACHE_HOME=/tmp/nix-cache nix develop -c just openapi-validate`
- Start the server and validate the live URL with `env XDG_CACHE_HOME=/tmp/nix-cache nix develop -c go run ./cmd/openapi-validate http://127.0.0.1:18080/openapi.yaml`
