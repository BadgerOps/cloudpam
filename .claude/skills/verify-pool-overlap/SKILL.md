---
name: verify-pool-overlap
description: "Model-check CloudPAM's pool overlap guard with TLA+/TLC: proves whether concurrent POST /api/v1/pools requests, on one process or on several replicas sharing PostgreSQL, can store two overlapping sibling pools. Use before and after touching createPool in internal/api/pool_handlers.go, placePools, place or placeGate in internal/api/pool_placement.go, storage.PlacementLocker (internal/storage/placement.go) or Store.WithPlacementLock (internal/storage/postgres/placement.go), store.CreatePool in any storage backend, the pools unique indexes, or when adding any new code path that creates pools."
---

# Verify pool overlap (TLA+ / TLC)

**Property.** No two pools under the same parent (or both at top level) ever share or overlap address space.
**Harm if broken.** CloudPAM hands the same IP range to two consumers, which is the one thing an IPAM exists to prevent.

**Status at the sync commit: the property HOLDS for one server process on any store, and for several replicas on one PostgreSQL database.** Commit 6b01164 (#262) serialized pool placement behind `Server.placeGate` and made `MemoryStore.CreatePool` refuse an exact duplicate, which fixed findings F1 and F2. Commit 47df5c4 (#267) resolved limitation L1 for PostgreSQL: placement now runs inside one transaction that holds a per-organization `pg_advisory_xact_lock`, and that lock spans every replica. L1 stays open for several processes on one SQLite file. That deployment is not supported, and the Helm chart refuses it. Nothing in product code was changed by this skill.

## Run it

```sh
.claude/skills/verify-pool-overlap/run.sh
```

It takes a few seconds after the first Nix fetch. It needs only `nix` and `git` (a full clone: the drift check reads the sync commit). TLC comes from the nixpkgs rev pinned in `flake.lock` (TLC 2.19, tlaplus 1.7.4 at the sync commit).

| Exit | Meaning |
|---|---|
| 0 | Every config matched the table below and the covered code is unchanged since the sync commit. |
| 4 | Every config matched, but covered code changed since the sync commit, committed or not (or the commit is missing). The pass may describe code that no longer exists: re-read the diff it prints, update the model, then move the sync commit. |
| 3 | `multi-process-local.cfg` now holds. It is the negative control, so either the model lost the ability to fail, or stores without `PlacementLocker` now serialize across processes. Confirm in the code before editing this file. |
| 1 | Any other mismatch, or TLC failed. A rule broken in a config expected to hold is a bug candidate: replay the trace against the code. Never weaken a rule to get 0. |

Full TLC output lands in `out/<config>.out` (gitignored). Read every trace with the pstack `/pstack:statechart` skill: it shows each invariant violation as its shortest path in plain words, and draws a chart of the states when the check passes. Point it at `PoolCreate.tla` with the config you ran.

To see the guard on the real server, run `.claude/skills/verify-pool-overlap/repro.sh` (builds inside `nix develop`, starts an in-memory server on port 18180, kills only what it started). It fires eight overlapping sibling creates at once, 40 times, and expects exactly one `201` and seven `400` each time, then posts one CIDR twice and expects `201` then `409`. It refuses to start if the port already answers `/healthz`. Exit 0 means both held; exit 1 prints the overlapping pools it stored.

To see the guard across processes on PostgreSQL, run:

```sh
DATABASE_URL='postgres://user:pass@host:5432/db?sslmode=disable' .claude/skills/verify-pool-overlap/repro-postgres.sh [PORT_A [PORT_B]]
```

The user in `DATABASE_URL` needs `CREATEDB`. The script creates a scratch database on that server, builds `cmd/cloudpam` with `-tags postgres`, and starts two processes on it (ports 18181 and 18182 by default) one after the other. It requires each process to log `placement guard: postgres advisory lock`. It then runs the same 40 attempts as `repro.sh` with the eight creates split four and four across the two processes, and posts one top-level CIDR on both processes at once, expecting one `201` and one `409`. Last, it scans `GET /api/v1/pools` for overlapping siblings. It stops only its own processes and drops only its own database. Exit 0 means `multi-process.cfg` and `multi-process-duplicate.cfg` hold on the binary. Exit 1 prints the codes and the overlapping pools. It uses `psql` from `PATH`, or from the pinned nixpkgs when `psql` is missing, plus `curl` and `python3`.

## What is checked

| Config | Stands for | Expected |
|---|---|---|
| `one-process.cfg` | one process on a store without `PlacementLocker` (MemoryStore, SQLite): `placePools` holds `placeGate` across `ListPools`, the check and `CreatePool` | all rules hold |
| `duplicate.cfg` | the same, with two requests for the same CIDR: the check skips an exact-equal sibling, so the store's `(parent, cidr)` refusal is the only guard | all rules hold |
| `multi-process.cfg` | three replicas on one PostgreSQL database, one request each: `placeGate` serializes nothing across them, and the advisory lock in `WithPlacementLock` covers the list, the check, the insert and the commit; any holder may lose its session before `COMMIT` | all rules hold (#267) |
| `multi-process-duplicate.cfg` | the same, with two requests for the same CIDR: `idx_pools_org_cidr` fires inside the transaction, the savepoint turns it into a `409`, and the transaction still commits | all rules hold |
| `multi-process-local.cfg` | three processes sharing one store without `PlacementLocker` (one SQLite file, unsupported): only the per-process `placeGate` | `NoOverlappingSiblings` violated (L1) |

`multi-process-local.cfg` keeps the model honest on every run. It is `multi-process.cfg` with `DatabaseLock = FALSE`, and its interleaving is what one process would allow without `placeGate`. If the model ever loses the ability to fail, `run.sh` exits 3.

Rules in `PoolCreate.tla`:
- `NoOverlappingSiblings`: the property above, over the committed pools. A row inside an open placement transaction is invisible to every other session, so it is not yet stored.
- `RejectionsAreJustified`: the guard never rejects a request that had no conflicting sibling.
- `TypeOK`.

Bounds: three concurrent requests at the top level, CIDRs `10.0.0.0/8`, `10.0.0.0/9`, `10.128.0.0/9` (the /8 and its two disjoint halves), or `10.0.0.0/9` twice in the duplicate configs. The overlap relation is listed by hand in `MC.tla` and matches what `prefixesOverlapIPv4` returns for those CIDRs. Every interleaving is explored (18 distinct states for one process, 20 for its duplicate run, 68 for three PostgreSQL replicas, 71 for their duplicate run). Three requests at one scope is enough because the violation needs only two, and the third gives a rejection path so `RejectionsAreJustified` is not vacuous.

## How the model maps to the code

`createPool` (`internal/api/pool_handlers.go`) validates the body, checks the parent with `validateChildCIDR`, then calls `s.placePools(ctx, siblingsOnly, ...)` (`internal/api/pool_placement.go`). `placePools` takes `placeGate`. If the store implements `storage.PlacementLocker` it calls `WithPlacementLock` and runs `place` on the `PlacementStore` it is given; otherwise it runs `place` on the store itself. `DatabaseLock` in the model is that branch: only the PostgreSQL store implements the interface (`internal/storage/postgres/placement.go`). Each TLA+ action is one step that no other placement can enter:

| Action | Code | Why it is one step |
|---|---|---|
| `ListAndCheck(r)` | `placePools`: `s.placeGate.acquire(work)`. With `DatabaseLock`, `WithPlacementLock` then opens a READ COMMITTED transaction, sets `lock_timeout` and `idle_in_transaction_session_timeout`, and takes `pg_advisory_xact_lock(hashtextextended('cloudpam/pool-placement/' \|\| orgID, 0))`. Then `place`: `ListPools` (`placementTx.ListPools` on PostgreSQL), then `checkPlacement` | Every placement takes `placeGate` first, and a request that waits on either lock has read nothing. `ListPools` copies the pool set under the store's read lock (`MemoryStore.ListPools`) or in one `SELECT`; under READ COMMITTED that `SELECT` takes a new snapshot after the lock is granted, so it sees every pool the previous holder committed. `checkPlacement` reads only that copy. Under `siblingsOnly` it compares pools with the same parent and skips an exact-equal CIDR (`overlapPolicy.compares`). A conflict returns `400 cidr overlaps with existing block`: the deferred `endTx(ctx, tx.Rollback)` frees the advisory lock and the deferred `release` frees `placeGate` in the same step. |
| `Insert(r)` (`DatabaseLock = FALSE`) | `place`: `s.store.CreatePool(ctx, in)`, then the deferred `placeGate.release()` | One write that commits on its own. SQLite refuses an exact `(parent, cidr)` duplicate through `uniq_pools_parent_cidr` (`migrations/0001_init.sql`) and `MemoryStore.CreatePool` by scanning live pools, both with `storage.ErrConflict`, which `createPool` maps to `409`. |
| `TxInsert(r)` (`DatabaseLock = TRUE`) | `placementTx.CreatePool`: `createPool` (`internal/storage/postgres/postgres.go`) inside a savepoint (`t.tx.Begin`), `sp.Rollback` on error | One INSERT that no other session sees before `COMMIT`. A duplicate fails `idx_pools_org_cidr` on live `(organization_id, cidr)` rows (`migrations/postgres/001_core_schema.up.sql`, stricter than SQLite: it also refuses an equal CIDR under another parent). The savepoint rollback keeps the transaction usable, and the `409` is reported after the commit. |
| `Commit(r)` | `WithPlacementLock`: `fn` returned nil and `ctx.Err()` is nil, so `endTx(ctx, tx.Commit)` on a context detached from the request; then the deferred `placeGate.release()` | PostgreSQL marks a transaction committed before it frees the transaction's advisory lock, so the next holder's `ListPools` sees the rows. Nothing happens on the request between the commit and the `release`, so the two are one step. |
| `SessionLost(r)` | The placement transaction ends without `COMMIT`: the request context is cancelled (the `ctx.Err()` check, or a statement fails on it), `idle_in_transaction_session_timeout` (15s) or `pg_terminate_backend` ends the session, or the process dies. The deferred `endTx(ctx, tx.Rollback)`, or the server, rolls it back | The lock is transaction-scoped and the inserts run in the same transaction, so whatever ends the transaction frees the lock and discards the inserts together. `TestWithPlacementLock_CancelledInsideFnStoresNothing`, `TestWithPlacementLock_TerminatedHolderStoresNothingAndFreesLock` and `TestSchemaApply_CancelledMidBatch_StoresNothing` show this on a real database. |

`holder` in the model is `placeGate` (a one-slot channel that a waiter abandons when its 10s `placementBudget` ends, formerly the mutex `placeMu`), one per process (`ReqProcess`). It is a field of `Server`, so it serializes requests inside one process only; `net/http` runs each request on its own goroutine, and nothing else orders them. `dbHolder` is the advisory lock. Its key depends only on the store's organization (`s.orgID`), so it is one lock for every process on the database. `uncommitted` holds the rows inserted inside a placement transaction that has not committed, and `pools` is what every other session's `ListPools` sees. `POST /api/v1/schema/apply` takes the same locks through `placePools` (policy `anyLivePool`, or `overlapAllowed` with `skip_conflicts:true`), so it cannot interleave with `createPool`; it is read, not modelled.

Pools are never deleted in the model. The parent-containment half of the promise (`validateChildCIDR`) runs before the lock and is not modelled: `updatePool` cannot change a CIDR, so it has no ordering question, but see "Not covered" for the delete race. `schema/apply` (and `schema/check`) run it too, through `validateSchemaContainment`, on each `parent_ref` entry against the CIDR of the entry it names in the same request body, before the lock and whatever `skip_conflicts` says; that reads no stored pool, so it adds no ordering question either.

## L1: several processes on one database (resolved for PostgreSQL)

Recorded since this skill was added, and resolved for PostgreSQL by 47df5c4 (#267). Commit 6b01164 had said it: "placeGate serializes one process only. CloudPAM instances that share one Postgres database are not serialized by it." The Helm chart allowed `replicaCount` above 1 with Postgres. The counterexample TLC found, in the code's words:

1. Process 1 serves request A `POST /api/v1/pools {"cidr":"10.0.0.0/8"}`: `placePools` locks its own `placeGate`, `ListPools` returns nothing, `checkPlacement` passes.
2. Process 2 serves request B `POST /api/v1/pools {"cidr":"10.0.0.0/9"}`: its own `placeGate` is free, `ListPools` returns nothing, `checkPlacement` passes.
3. A calls `store.CreatePool`: `10.0.0.0/8` stored, 201.
4. B calls `store.CreatePool`: `10.0.0.0/9` stored, 201. No unique index fires because the CIDRs differ.

The fix is a guard the database enforces: the advisory lock in `WithPlacementLock`. `multi-process.cfg` now models it and holds. The tests in 1184847 (`internal/api/pool_placement_postgres_test.go`) reproduced the race with two `api.Server` values on one database, and they pass after 47df5c4. `repro-postgres.sh` shows the same on two real processes.

L1 is still open for stores without `PlacementLocker`: several processes on one SQLite file are serialized by nothing. That deployment is unsupported, and the Helm chart fails with `storage.backend=sqlite runs one replica` when `replicaCount` is above 1. `multi-process-local.cfg` is that case, and TLC still finds the four steps above in it.

## History

- F1 (concurrent creates stored overlapping siblings) and F2 (the in-memory store accepted the same CIDR twice) were found at 705c4b3 and fixed by 6b01164 (#262). `one-process.cfg` and `duplicate.cfg` are the checks that now hold; `repro.sh` shows both on the binary.
- L1 (replicas on one database), which 6b01164 itself stated, was fixed for PostgreSQL by 47df5c4 (#267). `multi-process.cfg` and `multi-process-duplicate.cfg` are the checks that now hold.

## Other writers that bypass the guard entirely (verified by reading, not modelled)

These store pools with no overlap check, so one request can break the property with no concurrency. All but the first also skip `placeGate` and, on PostgreSQL, the advisory lock, so they can insert between another request's `ListPools` and its insert or `COMMIT`, on any replica:
- `POST /api/v1/schema/apply` with `skip_conflicts:true` (documented bypass: `placePools` with `overlapAllowed` takes the locks but skips the check). With `skip_conflicts:false` it compares against every live pool, not only siblings, and also refuses overlaps inside the batch. Either way it refuses a `parent_ref` entry outside its parent with `400` before the locks (`validateSchemaContainment`).
- `POST /api/v1/ai/sessions/{id}/apply-plan` (`internal/api/ai_handlers.go` line 306): no overlap check.
- `POST /api/v1/import/pools` CSV import (`internal/api/export_handlers.go` line 732): no overlap check.
- `POST /api/v1/recommendations/{id}/apply` (`internal/planning/recommendations.go` line 117): inserts the CIDR suggested at generation time without rechecking.
- Discovery import, `createDiscoveredPool` and the bulk path (`internal/api/discovery_handlers.go` lines 951, 1132): no overlap check against managed pools.

Excluded because they cannot change a CIDR: `PATCH /api/v1/pools/{id}` (`updatePool` accepts no `cidr` field).

## Not covered

- Isolation levels other than READ COMMITTED. `ListAndCheck` reads `pools` at the moment it holds both locks, which is what READ COMMITTED gives. The model cannot express REPEATABLE READ or SERIALIZABLE, where the snapshot comes from the transaction's first statement (`set_config`), before the lock wait ends. `WithPlacementLock` pins READ COMMITTED for that reason, and `TestWithPlacementLock_WaiterSeesHolderPoolsUnderRepeatableReadDefault` guards the pin on a database whose default is REPEATABLE READ.
- The time budgets. A request that waits longer than `placementLockWait` (5s) for the advisory lock, or whose whole placement runs past `placementBudget` (10s, counted from the moment it queues on `placeGate`; the store's own `placementDeadline` is a second copy of that bound), gets `storage.ErrBusy` and a `503` with `Retry-After: 1`; its transaction rolls back, so nothing it inserted stays. A `lock_timeout` on a later INSERT (a bypass writer's uncommitted duplicate) is mapped the same way and stops the batch.
- A `COMMIT` whose reply is lost after the server committed: the pools exist, but `createPool` answers `500`. The model's `Commit` always reports `created` or `rejected`, so this case changes only the reported status, not what the store holds.
- The organization. The model has one; the lock key and `listPools` are both per organization. Two organizations whose keys collide under `hashtextextended` share a lock, which only adds waiting.
- The ordering between a parent delete and a child create (`GetPool` parent before the lock, then `DeletePool`, then `CreatePool` child) is not modelled.
- Soft deletes: on SQLite a deleted pool still holds its `(parent, cidr)` in `uniq_pools_parent_cidr` (issue #268); the model has no deletes.
- IPv4 prefix arithmetic itself (`prefixesOverlapIPv4`, `validateChildCIDR`, `cidrsOverlap`) is taken as correct; the model uses a hand-listed overlap relation.
- More than three concurrent requests, more than one scope, two requests on one replica beside a third on another, `schema/apply` batches (several inserts, some refused by their savepoint) as a modelled actor, and a process crash that ends every request on that process at once are outside the bound.
- The bypass paths above are recorded, not model-checked.

## Coverage record

Files and functions this verification describes:
- `internal/api/pool_handlers.go`: `createPool` (validation, `validateChildCIDR`, the `placePools` call and its 400/409 mapping), `updatePool` (confirmed no CIDR field).
- `internal/api/pool_placement.go`: `placePools`, `place`, `checkPlacement`, `overlapPolicy.compares`, `cidrsOverlap`, `sameParent`.
- `internal/api/server.go`: `Server.placeGate`, `writeStoreErr` (`409` for `ErrConflict`, `503` for `ErrBusy`).
- `internal/api/schema_handlers.go`: `handleSchemaApply` (same locks; read, not modelled), `validateSchemaContainment` (pre-lock parent containment for `parent_ref` entries; read, not modelled).
- `internal/api/cidr.go`: `prefixesOverlapIPv4`, `validateChildCIDR` (taken as the overlap relation).
- `internal/storage/placement.go`: `PlacementStore`, `PlacementLocker` and its contract.
- `internal/storage/postgres/placement.go`: `WithPlacementLock`, `endTx`, `placementTx.ListPools`, `placementTx.CreatePool`.
- `internal/storage/store.go`: `MemoryStore.ListPools`, `MemoryStore.CreatePool` (live `(parent, cidr)` duplicate refused with `ErrConflict`).
- `internal/storage/sqlite/sqlite.go` and `internal/storage/postgres/postgres.go`: `CreatePool` (plain INSERT, conflict only from the unique index); in `postgres.go` also `listPools` and `createPool`, which the transaction and the pool share.
- `migrations/0001_init.sql`: `uniq_pools_parent_cidr`; `migrations/postgres/001_core_schema.up.sql`: `idx_pools_org_cidr`.
- Bypass list: `ai_handlers.go`, `export_handlers.go`, `discovery_handlers.go`, `internal/planning/recommendations.go`.

sync commit: 8e15a9f30ac6ce03b444285f4cf61835f8f5dff7

`run.sh` prints DRIFT and exits 4 when any covered file in the working tree differs from the sync commit. The covered files are the model's sources above plus the bypass writers (`ai_handlers.go`, `export_handlers.go`, `discovery_handlers.go`, `internal/planning/recommendations.go`). When that happens: re-read the code cited above, fix the model if the code's steps changed (new lock, new transaction boundary, new constraint, new writer), rerun, and only then move the sync commit to a commit on master. A drift on a bypass file means re-reading that writer and updating its entry in the bypass list, not the model: those paths are recorded, not modelled, and the question is whether the writer still skips the check or the locks. If a change made `multi-process-local.cfg` hold, confirm whether the model can still fail before recording anything. Never weaken a rule or widen a skip to make a run pass.

## Run record at the sync commit

- `one-process.cfg`: no error (18 distinct states, depth 6).
- `duplicate.cfg`: no error (20 distinct states, depth 6).
- `multi-process.cfg`: no error (68 distinct states, depth 9).
- `multi-process-duplicate.cfg`: no error (71 distinct states, depth 9).
- `multi-process-local.cfg`: `NoOverlappingSiblings` violated at depth 5 (26 distinct states), the four steps of L1.
- TLC reaches each new path, checked with throwaway invariants: an uncommitted insert, a `SessionLost` after `TxInsert` followed by an overlapping `201` on another replica, and the savepoint `409` in `multi-process-duplicate.cfg`.
- Breaks that prove the checks can fail:
  - Removing the `holder` guard from `ListAndCheck` (modelling `placeGate` as absent) makes `one-process.cfg` violate `NoOverlappingSiblings` at depth 5.
  - `StoreRejectsDuplicate = FALSE` in `duplicate.cfg` (the in-memory store before 6b01164) makes it violate `NoOverlappingSiblings` at depth 5.
  - Removing `DatabaseLock => dbHolder = NoHolder` from `ListAndCheck` (the advisory lock gone, `placeGate` kept) makes `multi-process.cfg` violate `NoOverlappingSiblings` at depth 7.
  - Making `SessionLost` free only the advisory lock and leave the request running with its uncommitted insert makes `multi-process.cfg` violate `NoOverlappingSiblings` at depth 8. This is a session-level lock on a connection apart from the inserts: request A inserts `10.0.0.0/8`, loses the lock, B lists on another replica without seeing A's row, inserts `10.0.0.0/9`, and both commit.
  - `DatabaseLock = TRUE` in `multi-process-local.cfg` makes `run.sh` exit 3.
- `repro.sh` on the real binary (in-memory store, port 18180): 40 attempts, each with exactly one `201` and seven `400` among eight overlapping sibling creates; the same top-level CIDR twice gave `201` then `409`.
- `repro-postgres.sh` on two `-tags postgres` processes sharing one scratch database on PostgreSQL 16.15: 40 attempts, each with exactly one `201` and seven `400`; the same top-level CIDR on both processes at once gave one `201` and one `409`; 81 pools stored, none overlapping a sibling. The same script built from 0e327fa (before #267, with only the guard-line check removed) failed on attempt 1 with two `201`s and stored `10.1.0.0/17` beside `10.1.0.0/22` under one parent.

Note: `.gitignore` ignores `.claude/*` but re-includes `.claude/skills/`, so this folder is tracked; its `out/` directory is ignored by the local `.gitignore`.
