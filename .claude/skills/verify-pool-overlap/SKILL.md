---
name: verify-pool-overlap
description: "Model-check CloudPAM's pool overlap guard with TLA+/TLC: proves whether concurrent POST /api/v1/pools requests can store two overlapping sibling pools. Use before and after touching createPool in internal/api/pool_handlers.go, placePools or placeMu in internal/api/pool_placement.go, store.CreatePool in any storage backend, the pools unique indexes, or when adding any new code path that creates pools."
---

# Verify pool overlap (TLA+ / TLC)

**Property.** No two pools under the same parent (or both at top level) ever share or overlap address space.
**Harm if broken.** CloudPAM hands the same IP range to two consumers, which is the one thing an IPAM exists to prevent.

**Status at the sync commit: the property HOLDS for one server process.** Commit 6b01164 (#262) serialized pool placement behind `Server.placeMu` and made `MemoryStore.CreatePool` refuse an exact duplicate, which fixed the two findings this skill recorded before (F1 concurrent overlap, F2 in-memory duplicate). One known limitation stands, L1 below: several CloudPAM processes on one database are not serialized. Nothing in product code was changed by this skill.

## Run it

```sh
.claude/skills/verify-pool-overlap/run.sh
```

Takes a few seconds after the first Nix fetch. It needs only `nix` and `git` (a full clone: the drift check reads the sync commit). TLC comes from the nixpkgs rev pinned in `flake.lock` (TLC 2.19, tlaplus 1.7.4 at the sync commit).

| Exit | Meaning |
|---|---|
| 0 | Every config matched the table below and the covered code is unchanged since the sync commit. |
| 4 | Every config matched, but covered code changed since the sync commit, committed or not (or the commit is missing). The pass may describe code that no longer exists: re-read the diff it prints, update the model, then move the sync commit. |
| 3 | `multi-process.cfg` now holds: a cross-process guard landed, or the model lost the ability to fail. Confirm in the code before editing this file. |
| 1 | Any other mismatch, or TLC failed. A rule broken on one process is a bug candidate: replay the trace against the code. Never weaken a rule to get 0. |

Full TLC output lands in `out/<config>.out` (gitignored). Read every trace with the pstack `/pstack:statechart` skill: it shows each invariant violation as its shortest path in plain words, and draws a chart of the states when the check passes. Point it at `PoolCreate.tla` with the config you ran.

To see the guard on the real server, run `.claude/skills/verify-pool-overlap/repro.sh` (builds inside `nix develop`, starts an in-memory server on port 18180, kills only what it started). It fires eight overlapping sibling creates at once, 40 times, and expects exactly one `201` and seven `400` each time, then posts one CIDR twice and expects `201` then `409`. It refuses to start if the port already answers `/healthz`. Exit 0 means both held; exit 1 prints the overlapping pools it stored.

## What is checked

| Config | Stands for | Expected |
|---|---|---|
| `one-process.cfg` | the code, every request on one process: `placePools` holds `placeMu` across `ListPools`, the check and `CreatePool` | all rules hold |
| `duplicate.cfg` | the same, with two requests for the same CIDR: the check skips an exact-equal sibling, so the store's `(parent, cidr)` refusal is the only guard | all rules hold |
| `multi-process.cfg` | the code, each request on a different process sharing one database: each `placeMu` serializes nothing | `NoOverlappingSiblings` violated (L1) |

`multi-process.cfg` also keeps the model honest on every run: its interleaving is exactly what one process would allow without `placeMu`, so if the model ever loses the ability to fail, `run.sh` exits 3.

Rules in `PoolCreate.tla`:
- `NoOverlappingSiblings`: the property above, over what the store holds.
- `RejectionsAreJustified`: the guard never rejects a request that had no conflicting sibling.
- `TypeOK`.

Bounds: three concurrent requests at the top level, CIDRs `10.0.0.0/8`, `10.0.0.0/9`, `10.128.0.0/9` (the /8 and its two disjoint halves), or `10.0.0.0/9` twice in `duplicate.cfg`. The overlap relation is listed by hand in `MC.tla` and matches what `prefixesOverlapIPv4` returns for those CIDRs. Every interleaving is explored (18 distinct states for one process, 20 for the duplicate run). Three requests at one scope is enough because the violation needs only two, and the third gives a rejection path so `RejectionsAreJustified` is not vacuous.

## How the model maps to the code

`createPool` (`internal/api/pool_handlers.go`) validates the body, checks the parent with `validateChildCIDR`, then calls `s.placePools(ctx, siblingsOnly, ...)` (`internal/api/pool_placement.go`). Each TLA+ action is one step that no other request on the same process can enter:

| Action | Code | Why it is one step |
|---|---|---|
| `ListAndCheck(r)` | `placePools`: `s.placeMu.Lock()`, `s.store.ListPools(ctx)`, then `checkPlacement` | `ListPools` copies the pool set under the store's read lock (`MemoryStore.ListPools`) or in one `SELECT`; `checkPlacement` reads only that copy. Under `siblingsOnly` it compares pools with the same parent and skips an exact-equal CIDR (`overlapPolicy.compares`). A conflict returns `400 cidr overlaps with existing block`, and the deferred `Unlock` releases the lock in the same step. |
| `Insert(r)` | `placePools`: `s.store.CreatePool(ctx, in)`, then the deferred `placeMu.Unlock()` | One write. Every store refuses an exact `(parent, cidr)` duplicate with `storage.ErrConflict`, which `createPool` maps to `409`: SQLite through `uniq_pools_parent_cidr` (`migrations/0001_init.sql`), Postgres through `idx_pools_org_cidr` on live `(organization_id, cidr)` rows (`migrations/postgres/001_core_schema.up.sql`, stricter: it also refuses an equal CIDR under another parent), and `MemoryStore.CreatePool` by scanning live pools. |

`holder` in the model is `placeMu`, one per process (`ReqProcess`). It is a field of `Server`, so it serializes requests inside one process only; `net/http` runs each request on its own goroutine, and nothing else orders them. `POST /api/v1/schema/apply` takes the same lock through `placePools` (policy `anyLivePool`, or `overlapAllowed` with `skip_conflicts:true`), so it cannot interleave with `createPool` on one process; it is read, not modelled.

Pools are never deleted in the model. The parent-containment half of the promise (`validateChildCIDR`) runs before the lock and is not modelled: `updatePool` cannot change a CIDR, so it has no ordering question, but see "Not covered" for the delete race. `schema/apply` (and `schema/check`) run it too, through `validateSchemaContainment`, on each `parent_ref` entry against the CIDR of the entry it names in the same request body, before the lock and whatever `skip_conflicts` says; that reads no stored pool, so it adds no ordering question either.

## L1: several processes on one database (multi-process.cfg)

Commit 6b01164 says it: "placeMu serializes one process only. CloudPAM instances that share one Postgres database are not serialized by it." The Helm chart allows `replicaCount` above 1 with Postgres. Counterexample TLC finds, in the code's words:

1. Process 1 serves request A `POST /api/v1/pools {"cidr":"10.0.0.0/8"}`: `placePools` locks its own `placeMu`, `ListPools` returns nothing, `checkPlacement` passes.
2. Process 2 serves request B `POST /api/v1/pools {"cidr":"10.0.0.0/9"}`: its own `placeMu` is free, `ListPools` returns nothing, `checkPlacement` passes.
3. A calls `store.CreatePool`: `10.0.0.0/8` stored, 201.
4. B calls `store.CreatePool`: `10.0.0.0/9` stored, 201. No unique index fires because the CIDRs differ.

Not reproduced live: `repro.sh` runs one process. A fix needs a guard the database enforces (a serializing transaction, an advisory lock, or an exclusion constraint on overlapping CIDRs).

## History

- F1 (concurrent creates stored overlapping siblings) and F2 (the in-memory store accepted the same CIDR twice) were found at 705c4b3 and fixed by 6b01164 (#262). `one-process.cfg` and `duplicate.cfg` are the checks that now hold; `repro.sh` shows both on the binary.

## Other writers that bypass the guard entirely (verified by reading, not modelled)

These store pools with no overlap check, so one request can break the property with no concurrency. All but the first also skip `placeMu`, so they can insert between another request's check and insert:
- `POST /api/v1/schema/apply` with `skip_conflicts:true` (documented bypass: `placePools` with `overlapAllowed` takes the lock but skips the check). With `skip_conflicts:false` it compares against every live pool, not only siblings, and also refuses overlaps inside the batch. Either way it refuses a `parent_ref` entry outside its parent with `400` before the lock (`validateSchemaContainment`).
- `POST /api/v1/ai/sessions/{id}/apply-plan` (`internal/api/ai_handlers.go` line 306): no overlap check.
- `POST /api/v1/import/pools` CSV import (`internal/api/export_handlers.go` line 732): no overlap check.
- `POST /api/v1/recommendations/{id}/apply` (`internal/planning/recommendations.go` line 117): inserts the CIDR suggested at generation time without rechecking.
- Discovery import, `createDiscoveredPool` and the bulk path (`internal/api/discovery_handlers.go` lines 951, 1132): no overlap check against managed pools.

Excluded because they cannot change a CIDR: `PATCH /api/v1/pools/{id}` (`updatePool` accepts no `cidr` field).

## Not covered

- The ordering between a parent delete and a child create (`GetPool` parent before the lock, then `DeletePool`, then `CreatePool` child) is not modelled.
- Soft deletes: on SQLite a deleted pool still holds its `(parent, cidr)` in `uniq_pools_parent_cidr` (issue #268); the model has no deletes.
- IPv4 prefix arithmetic itself (`prefixesOverlapIPv4`, `validateChildCIDR`, `cidrsOverlap`) is taken as correct; the model uses a hand-listed overlap relation.
- More than three concurrent requests, more than one scope, mixed placements of requests across processes, `schema/apply` as a modelled actor, and the SQL stores' transaction isolation are outside the bound.
- The bypass paths above are recorded, not model-checked.

## Coverage record

Files and functions this verification describes:
- `internal/api/pool_handlers.go`: `createPool` (validation, `validateChildCIDR`, the `placePools` call and its 400/409 mapping), `updatePool` (confirmed no CIDR field).
- `internal/api/pool_placement.go`: `placePools`, `checkPlacement`, `overlapPolicy.compares`, `cidrsOverlap`, `sameParent`; `Server.placeMu` (`internal/api/server.go`).
- `internal/api/schema_handlers.go`: `handleSchemaApply` (same lock; read, not modelled), `validateSchemaContainment` (pre-lock parent containment for `parent_ref` entries; read, not modelled).
- `internal/api/cidr.go`: `prefixesOverlapIPv4`, `validateChildCIDR` (taken as the overlap relation).
- `internal/storage/store.go`: `MemoryStore.ListPools`, `MemoryStore.CreatePool` (live `(parent, cidr)` duplicate refused with `ErrConflict`).
- `internal/storage/sqlite/sqlite.go` and `internal/storage/postgres/postgres.go`: `CreatePool` (plain INSERT, conflict only from the unique index).
- `migrations/0001_init.sql`: `uniq_pools_parent_cidr`; `migrations/postgres/001_core_schema.up.sql`: `idx_pools_org_cidr`.
- Bypass list: `ai_handlers.go`, `export_handlers.go`, `discovery_handlers.go`, `internal/planning/recommendations.go`.

sync commit: acd7a34ddffb694e9c5913c50d530ab368f82f2b

`run.sh` prints DRIFT and exits 4 when any covered file in the working tree differs from the sync commit. The covered files are the model's sources above plus `internal/api/server.go` (`placeMu`) and the bypass writers (`ai_handlers.go`, `export_handlers.go`, `discovery_handlers.go`, `internal/planning/recommendations.go`). When that happens: re-read the code cited above, fix the model if the code's steps changed (new lock, new constraint, new writer), rerun, and only then move the sync commit to a commit on master. A drift on a bypass file means re-reading that writer and updating its entry in the bypass list, not the model: those paths are recorded, not modelled, and the question is whether the writer still skips the check or the lock. If a change made `multi-process.cfg` hold, confirm the guard really spans processes before recording L1 as fixed. Never weaken a rule or widen a skip to make a run pass.

## Run record at the sync commit

- `one-process.cfg`: no error (18 distinct states, depth 6).
- `duplicate.cfg`: no error (20 distinct states, depth 6).
- `multi-process.cfg`: `NoOverlappingSiblings` violated at depth 5 (26 distinct states), the four steps of L1.
- Breaks that prove the checks can fail: removing the `holder` guard from `ListAndCheck` (modelling `placeMu` as absent) makes `one-process.cfg` violate `NoOverlappingSiblings` at depth 5; setting `StoreRejectsDuplicate = FALSE` in `duplicate.cfg` (the in-memory store before 6b01164) makes it violate `NoOverlappingSiblings` at depth 5.
- `repro.sh` on the real binary (in-memory store, port 18180): 40 attempts, each with exactly one `201` and seven `400` among eight overlapping sibling creates; the same top-level CIDR twice gave `201` then `409`.

Note: `.gitignore` ignores `.claude/*` but re-includes `.claude/skills/`, so this folder is tracked; its `out/` directory is ignored by the local `.gitignore`.
