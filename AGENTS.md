# Repository Guidelines

## Project Structure & Module Organization
- `cmd/cloudpam/`: main entrypoint and storage selection.
- `internal/api/`: HTTP server, routes, and handlers.
- `internal/storage/`: storage interface, in‑memory impl; `internal/storage/sqlite/` for SQLite.
- `internal/domain/`: core types.
- `migrations/`: SQL migrations (embedded in SQLite builds).
- `web/`: static UI (served at `/`).
- `docs/`: project plan, changelog.
- `scripts/` and `photos/`: Playwright screenshot tooling and outputs (LFS‑tracked).

## Build, Test, and Development Commands
- `make dev` or `just dev`: run server on `:8080` (in‑memory store).
- `make build` or `just build`: build `cloudpam` binary.
- `make sqlite-build` / `make sqlite-run`: build with `-tags sqlite` and run with `SQLITE_DSN` (e.g., `file:cloudpam.db?cache=shared&_fk=1`).
- `make test` / `make test-race`: run tests (optionally with `-race`).
- `make cover`: generate `coverage.out|.html`.
- `make fmt` / `make lint`: format and lint (golangci‑lint 1.61+).

Examples:
- `SQLITE_DSN='file:cloudpam.db?cache=shared&_fk=1' ./cloudpam`
- `./cloudpam -migrate up | -migrate status` (SQLite builds only)

## Coding Style & Naming Conventions
- Use `go fmt` and pass `golangci-lint` (see `.golangci.yml`).
- Go 1.24+. Packages are lower‑case; exported identifiers `CamelCase`.
- Keep errors lowercase and actionable; prefer small, focused files (e.g., `server_test.go`).

## Testing Guidelines
- Framework: Go `testing`. Tests live alongside code as `*_test.go`.
- Run: `go test ./...` (CI also runs `-race`).
- Coverage: `just cover` and optional threshold `just cover-threshold thr=80`.
- Name tests `TestXxx` and exercise API via `httptest` helpers (see `internal/api/handlers_test.go`).

## Commit & Pull Request Guidelines
- Commits: short imperative subject with scope prefix when useful (e.g., `UI: …`, `chore: …`, `CI: …`); reference issues (`closes #25`).
- PRs: include clear description, linked issues, and before/after screenshots for UI changes.
  - Generate screenshots: `npm install && npx playwright install chromium && APP_URL=http://localhost:8080 npm run screenshots` (outputs to `photos/`).
- Ensure `make lint test` (and SQLite path if applicable) are green before requesting review.

## Security & Configuration Tips
- Env: `ADDR` or `PORT` (listen), `SQLITE_DSN` (SQLite DSN), `APP_VERSION` (migration stamp).
- Default store is in‑memory unless built with `-tags sqlite`. Migrations apply automatically on startup in SQLite builds.

## Documentation
- Start with `README.md`, `docs/PROJECT_PLAN.md`, and `docs/CHANGELOG.md` for roadmap and changes.
