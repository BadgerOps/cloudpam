---
name: verify-pool-overlap
description: "Model-check CloudPAM's pool overlap guard with TLA+/TLC: proves whether concurrent POST /api/v1/pools requests can store two overlapping sibling pools. Use before and after touching createPool in internal/api/pool_handlers.go, store.CreatePool in any storage backend, the pools unique index, or when adding any new code path that creates pools."
---

# Verify pool overlap (TLA+ / TLC)

**Property.** No two pools under the same parent (or both at top level) ever share or overlap address space.
**Harm if broken.** CloudPAM hands the same IP range to two consumers, which is the one thing an IPAM exists to prevent.

**Status at the sync commit: the property is BROKEN.** Two findings, F1 and F2 below. Nothing in product code was changed; this skill records them so the next agent can rerun the check and tell "still broken" from "fixed" from "model drifted".

## Run it

```sh
.claude/skills/verify-pool-overlap/run.sh
```

Takes a few seconds after the first Nix fetch. It needs only `nix` and `git`; TLC comes from the nixpkgs rev pinned in `flake.lock` (TLC 2.19, tlaplus 1.7.4 at the sync commit). Exit 0 = results match the findings recorded here. Exit 3 = the race config now holds: the fix landed or the model drifted; confirm in the code before editing this file. Exit 1 = TLC or the model broke; fix the verification, not the product code.

Full TLC output lands in `out/<config>.out`. Read every trace with the pstack `/pstack:statechart` skill: it shows each invariant violation as its shortest path in plain words, and draws a chart of the states when the check passes. Point it at `PoolCreate.tla` with the config you ran.

To see the counterexample happen on the real server, run `.claude/skills/verify-pool-overlap/repro.sh` (builds inside `nix develop`, starts an in-memory server on port 18180, kills only what it started). It stops at the first parent that ends up with two or more overlapping children.

## What is checked

| Config | Stands for | Expected |
|---|---|---|
| `race.cfg` | the code at the sync commit: ListPools, overlap loop, then CreatePool, nothing held across them | `NoOverlappingSiblings` violated (F1) |
| `atomic.cfg` | the proposed fix: check and insert as one indivisible step | all rules hold |
| `memstore-dup.cfg` | in-memory store (`DEV_MODE=1`): no unique index, handler skips exact-equal CIDRs | `NoOverlappingSiblings` violated (F2) |

Rules in `PoolCreate.tla`:
- `NoOverlappingSiblings`: the property above, over what the store holds.
- `RejectionsAreJustified`: the guard never rejects a request that had no conflicting sibling.
- `TypeOK`.

Bounds: three concurrent requests at the top level, CIDRs `10.0.0.0/8`, `10.0.0.0/9`, `10.128.0.0/9` (the /8 and its two disjoint halves). The overlap relation is listed by hand in `MC.tla` and matches what `prefixesOverlapIPv4` returns for those three. Every interleaving of the three requests is explored: 28 states for the race config. Three requests at one scope is enough because the bug needs only two, and the third shows a rejection path so `RejectionsAreJustified` is not vacuous.

## How the model maps to the code

Each TLA+ action is one step the code cannot be interrupted inside:

| Action | Code | Why it is one step |
|---|---|---|
| `ListAndCheck(r)` | `createPool`, `internal/api/pool_handlers.go` lines 535-566: `s.store.ListPools(ctx)` then the overlap loop | `ListPools` copies the pool set under the store's read lock (`MemoryStore.ListPools`) or in one `SELECT` (sqlite/postgres); the loop reads only that copy. The loop skips an exact-equal CIDR (lines 550-552) and relies on "DB uniqueness". |
| `Insert(r)` | line 569: `s.store.CreatePool(ctx, in)` | One write under the store's write lock, or one `INSERT`. The only constraint it enforces is `uniq_pools_parent_cidr` on `(COALESCE(parent_id,-1), cidr)` from `migrations/0001_init.sql`. `MemoryStore.CreatePool` enforces nothing (`StoreRejectsDuplicate = FALSE`). |
| `AtomicCreate(r)` | not in the code | the fix shape: a handler-level mutex around lines 535-569, a serialized transaction, or a store-side overlap constraint. |

Between `ListAndCheck` and `Insert` nothing is held: there is no mutex in `Server` for pools (the only mutexes in `internal/api` are in rate limiting and `update_handlers.go`), and `net/http` runs each request on its own goroutine. That is the gap the model lets other requests use.

Pools are never deleted in the model. The parent-containment half of the promise (`validateChildCIDR`, lines 516-524) is not modelled: a child's CIDR is checked against a parent CIDR that `updatePool` cannot change, so it has no ordering question, but see "Not covered" for the delete race.

## F1: concurrent creates store overlapping siblings (race.cfg)

Counterexample TLC finds, in the code's words:

1. Request A `POST /api/v1/pools {"cidr":"10.0.0.0/8"}` calls `ListPools`: nothing there, overlap loop passes.
2. Request B `POST /api/v1/pools {"cidr":"10.0.0.0/9"}` calls `ListPools`: still nothing there, overlap loop passes.
3. A calls `store.CreatePool`: `10.0.0.0/8` stored, 201.
4. B calls `store.CreatePool`: `10.0.0.0/9` stored, 201. The unique index does not fire because the CIDRs differ.

The store now holds two top-level pools where one contains the other. The same four steps apply under any shared `parent_id`. `repro.sh` reproduces it on the real binary: it was observed at the sync commit (see the run record below).

Where the same shape appears but is **not** modelled here (same TOCTOU, same fix):
- `POST /api/v1/schema/apply` with `skip_conflicts:false` (`internal/api/schema_handlers.go` lines 208-230 then the create loop at 272).

## F2: in-memory store accepts the same CIDR twice, no race needed (memstore-dup.cfg)

With `DEV_MODE=1` the handler's loop skips an exact-equal sibling (line 550-552, "DB uniqueness should also catch") and `MemoryStore.CreatePool` has no uniqueness check, so two sequential `POST /api/v1/pools` with the same `cidr` and parent both return 201. The atomic fix alone does not close this; the in-memory store needs the `(parent_id, cidr)` check the SQL stores get from the index. Dev-mode only, but tests run against this store.

## Other writers that bypass the guard entirely (verified by reading, not modelled)

These call `store.CreatePool` with no overlap check at all, so the property can be broken by a single request with no concurrency:
- `POST /api/v1/schema/apply` with `skip_conflicts:true` (documented bypass, `schema_handlers.go`). Its check with `skip_conflicts:false` also compares against every pool rather than same-parent siblings, and never runs `validateChildCIDR`.
- `POST /api/v1/ai/sessions/{id}/apply-plan` (`internal/api/ai_handlers.go` ~line 306): no overlap check.
- `POST /api/v1/import/pools` CSV import (`internal/api/export_handlers.go` ~line 732): no overlap check.
- `POST /api/v1/recommendations/{id}/apply` (`internal/planning/recommendations.go` ~line 117): inserts the CIDR suggested at generation time without rechecking.
- Discovery import, `createDiscoveredPool` and the bulk path (`internal/api/discovery_handlers.go` ~lines 951, 1132): no overlap check against managed pools.

Excluded because they cannot change a CIDR: `PATCH /api/v1/pools/{id}` (`updatePool` accepts no `cidr` field).

## Not covered

- The ordering between a parent delete and a child create (`GetPool` parent, then `DeletePool`, then `CreatePool` child) is not modelled.
- IPv4 prefix arithmetic itself (`prefixesOverlapIPv4`, `lastAddr`, `validateChildCIDR`) is taken as correct; the model uses a hand-listed overlap relation for three CIDRs.
- More than three concurrent requests, more than one scope, deletes, soft-deleted pools, and the SQL stores' transaction isolation are outside the bound. A pass on `atomic.cfg` says the fix shape is right, not that a given implementation of it is.
- The bypass paths above are recorded, not model-checked.

## Coverage record

Files and functions this verification describes:
- `internal/api/pool_handlers.go`: `createPool` (overlap guard lines 527-566, `CreatePool` call line 569), `updatePool` (confirmed no CIDR field).
- `internal/api/cidr.go`: `prefixesOverlapIPv4`, `validateChildCIDR` (taken as the overlap relation).
- `internal/storage/store.go`: `MemoryStore.ListPools`, `MemoryStore.CreatePool` (no uniqueness check).
- `internal/storage/sqlite/sqlite.go` and `internal/storage/postgres/postgres.go`: `CreatePool` (plain INSERT, conflict only from the unique index).
- `migrations/0001_init.sql`: `uniq_pools_parent_cidr`.
- Bypass list: `schema_handlers.go`, `ai_handlers.go`, `export_handlers.go`, `discovery_handlers.go`, `internal/planning/recommendations.go`.

sync commit: 705c4b3069d915954acd0293775428843e9a61ba

`run.sh` prints DRIFT when any covered file changed after the sync commit. When that happens: re-read the lines cited above, fix the model if the code's steps changed (new lock, new constraint, new writer), rerun, and only then move the sync commit forward. If a change made `race.cfg` hold, confirm the lock or constraint really spans both the list and the insert before recording F1 as fixed. Never weaken a rule or widen a skip to make a run pass.

## Run record at the sync commit

- `race.cfg`: `NoOverlappingSiblings` violated at depth 5 (28 distinct states).
- `atomic.cfg`: no error (15 distinct states).
- `memstore-dup.cfg`: `NoOverlappingSiblings` violated at depth 3.
- `repro.sh` on the real binary (in-memory store, port 18180): attempt 1, eight overlapping sibling creates fired in parallel under parent #1, two returned 201; the store held both `10.1.0.0/17` and `10.1.0.0/18` under the same parent.
- F2 confirmed by hand on the same build: two sequential `POST /api/v1/pools` with `cidr` `10.0.0.0/8` at top level both returned 201 under `DEV_MODE=1`.

Note: `.gitignore` ignores `.claude/*` but re-includes `.claude/skills/`, so this folder is tracked; its `out/` directory is ignored by the local `.gitignore`.
