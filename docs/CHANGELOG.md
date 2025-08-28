# Changelog – Phase 1

## Scope
- Add hierarchical pools (top-level and sub-pools).
- Compute selectable IPv4 blocks within a pool and display hosts per block.
- Create sub-pools by clicking a free block in the UI.
- Optional SQLite backend (build tag) with in-memory default.
- Minor cleanup (unused import fix) and basic validation.

## What Changed

- Domain model:
  - `internal/domain/types.go`: `Pool` now includes `parent_id`; `CreatePool` accepts optional `parent_id`.

- Storage:
  - `internal/storage/store.go`: added `GetPool`; `CreatePool` stores `parent_id`; in-memory store updated.
  - `internal/storage/sqlite/sqlite.go`: schema migration includes `parent_id`; implemented `ListPools`, `CreatePool`, `GetPool` with `parent_id`.

- API:
  - `internal/http/server.go`:
    - New route `GET /api/v1/pools/{id}/blocks?new_prefix_len=<n>` returns IPv4 subnets inside the pool CIDR, each with hosts per block and Used/Free flag.
    - `POST /api/v1/pools` accepts `parent_id` and validates child CIDR is within parent (IPv4).
    - Added IPv4 helpers (address math, usable host counts).

- UI (Alpine.js):
  - `web/index.html`:
    - Top-level pools table with “View Blocks”.
    - Block browser: choose prefix size, see hosts, Used/Free, and create a sub-pool.

- Server/Build:
  - `cmd/cloudpam/main.go`: select storage via helper; removed unused `fmt` import.
  - `cmd/cloudpam/store_default.go`: default in-memory store; warns if `SQLITE_DSN` set without `-tags sqlite`.
  - `cmd/cloudpam/store_sqlite.go` (build tag `sqlite`): initializes SQLite store from `SQLITE_DSN` (or default), falls back to memory on error.

## How to Use (Dev)
- Start (memory store): `go run ./cmd/cloudpam`
- Open UI: http://localhost:8080
- Create a top-level pool (e.g., Name: `Prod`, CIDR: `10.0.0.0/16`).
- Click “View Blocks”, choose `Block size (prefix)` (e.g., 24), click “List Blocks”.
- Click “Create Sub-pool” on a Free block and provide a name.

## SQLite Support
- Build: `go build -tags sqlite -o cloudpam ./cmd/cloudpam`
- Run: `SQLITE_DSN='file:cloudpam.db?cache=shared&_fk=1' ./cloudpam`
- Note: add dependency if not present: `go get modernc.org/sqlite@latest`.

## Notes
- Blocks and validation currently target IPv4 only.
- Used block detection marks blocks that exactly match existing sub-pool CIDRs; partial overlap handling can be added later.
- Next candidates: delete endpoints, list sub-pools under a parent, IPv6 support, allocator service, and policy hooks.
