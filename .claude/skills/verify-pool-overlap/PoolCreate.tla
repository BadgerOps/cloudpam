---------------------------- MODULE PoolCreate ----------------------------
(***************************************************************************)
(* Concurrent POST /api/v1/pools requests against CloudPAM.               *)
(*                                                                         *)
(* Code modelled, at the sync commit recorded in SKILL.md:                 *)
(*                                                                         *)
(*   createPool (internal/api/pool_handlers.go) validates the body, then   *)
(*   calls Server.placePools(ctx, siblingsOnly, ...) in                    *)
(*   internal/api/pool_placement.go. placePools takes placeGate, then:     *)
(*                                                                         *)
(*   Store without storage.PlacementLocker (MemoryStore, SQLite),          *)
(*   DatabaseLock = FALSE: place(ctx, s.store, ...) runs on the store.     *)
(*                                                                         *)
(*   ListAndCheck(r)  = placeGate.acquire(), ListPools, then               *)
(*                      poolIndex.admit on that snapshot. It checks the    *)
(*                      parent, compares siblings only (same parent_id,    *)
(*                      or both top-level) and skips an exact-equal CIDR.  *)
(*                      A refused entry is not inserted, and the deferred  *)
(*                      release frees placeGate.                           *)
(*   Insert(r)        = Store.CreatePool, which commits on its own, then   *)
(*                      the deferred placeGate.release().                  *)
(*                                                                         *)
(*   PostgreSQL (Store implements PlacementLocker), DatabaseLock = TRUE:   *)
(*   Store.WithPlacementLock (internal/storage/postgres/placement.go)      *)
(*   opens one READ COMMITTED transaction, takes pg_advisory_xact_lock on  *)
(*   a per-organization key, and runs place() on placementTx.              *)
(*                                                                         *)
(*   ListAndCheck(r)  = placeGate.acquire(), BEGIN, pg_advisory_xact_lock, *)
(*                      ListPools in the transaction, poolIndex.admit. A   *)
(*                      refused entry inserts nothing; the empty           *)
(*                      transaction ends, which frees the advisory lock,   *)
(*                      then placeGate is released.                        *)
(*   TxInsert(r)      = placementTx.CreatePool: the INSERT runs in a       *)
(*                      savepoint of that transaction. No other session    *)
(*                      sees it before COMMIT.                             *)
(*   Commit(r)        = endTx(ctx, tx.Commit). The rows become visible,    *)
(*                      then the advisory lock and placeGate are released. *)
(*   SessionLost(r)   = the transaction ends without COMMIT: ctx cancelled *)
(*                      (the ctx.Err() check), the process dies,           *)
(*                      idle_in_transaction_session_timeout, or            *)
(*                      pg_terminate_backend. The lock and the uncommitted *)
(*                      inserts go together.                               *)
(*                                                                         *)
(* Every store refuses an exact (parent, cidr) duplicate with ErrConflict: *)
(* SQLite through uniq_pools_parent_cidr (migrations/0001_init.sql),       *)
(* MemoryStore.CreatePool by a scan of live pools, Postgres through        *)
(* idx_pools_org_cidr on (organization_id, cidr), which also refuses an    *)
(* equal CIDR under another parent.                                        *)
(*                                                                         *)
(* placeGate is a field of Server, so it serializes requests inside one    *)
(* process only. ReqProcess says which process serves each request; all    *)
(* processes share the one store. The advisory lock key depends only on    *)
(* the organization, so one dbHolder stands for it across every process.   *)
(***************************************************************************)
EXTENDS Naturals, FiniteSets

CONSTANTS
  Requests,              \* concurrent POST /api/v1/pools requests
  ReqCIDR,               \* [Requests -> CIDR label] the body's "cidr"
  ReqParent,             \* [Requests -> scope]      the body's "parent_id" (or "root")
  ReqProcess,            \* [Requests -> process]    the CloudPAM process that serves it
  Overlaps(_, _),        \* prefixesOverlapIPv4 on two distinct CIDR labels
  StoreRejectsDuplicate, \* TRUE = every store at the sync commit; FALSE = no (parent, cidr) check
  DatabaseLock           \* TRUE = the store implements storage.PlacementLocker (PostgreSQL)

NoHolder == "none"
Processes == {ReqProcess[r] : r \in Requests}

VARIABLES
  pools,       \* committed pools, what every session's ListPools sees: [req, cidr, parent]
  uncommitted, \* pools inserted inside a placement transaction that has not committed
  pc,          \* per request: "start" | "checked" | "inserted" | "done"
  outcome,     \* per request: "pending" | "created" | "rejected" | "failed"
  holder,      \* per process: the request holding that Server's placeGate, or NoHolder
  dbHolder     \* the request holding the organization's placement advisory lock, or NoHolder

vars == <<pools, uncommitted, pc, outcome, holder, dbHolder>>

Pool(r) == [req |-> r, cidr |-> ReqCIDR[r], parent |-> ReqParent[r]]

\* Pools poolIndex.admit compares against under siblingsOnly: same parent_id, or both top-level.
Siblings(r, ps) == {p \in ps : p.parent = ReqParent[r]}

\* poolIndex.admit rejects on overlap but skips an exact-equal CIDR (s.pool.CIDR != in.CIDR).
HandlerConflict(r, ps) ==
  \E p \in Siblings(r, ps) : p.cidr # ReqCIDR[r] /\ Overlaps(p.cidr, ReqCIDR[r])

\* What the store's (parent, cidr) uniqueness rejects.
DuplicateExists(r, ps) == \E p \in Siblings(r, ps) : p.cidr = ReqCIDR[r]

Refused(r) == StoreRejectsDuplicate /\ DuplicateExists(r, pools)

Release(r) == [holder EXCEPT ![ReqProcess[r]] = NoHolder]

Init ==
  /\ pools = {}
  /\ uncommitted = {}
  /\ pc = [r \in Requests |-> "start"]
  /\ outcome = [r \in Requests |-> "pending"]
  /\ holder = [s \in Processes |-> NoHolder]
  /\ dbHolder = NoHolder

\* placePools: placeGate.acquire(), then (PostgreSQL) BEGIN and the advisory lock, then ListPools and
\* poolIndex.admit. One step: the list runs only once both locks are held, and every placement takes
\* placeGate first, so a request waiting on the advisory lock has read nothing.
ListAndCheck(r) ==
  /\ pc[r] = "start"
  /\ holder[ReqProcess[r]] = NoHolder
  /\ DatabaseLock => dbHolder = NoHolder
  /\ IF HandlerConflict(r, pools)
       THEN /\ outcome' = [outcome EXCEPT ![r] = "rejected"]   \* 400 "cidr overlaps with existing block"
            /\ pc' = [pc EXCEPT ![r] = "done"]
            /\ UNCHANGED <<holder, dbHolder>>                   \* rollback and Unlock in the same step
       ELSE /\ pc' = [pc EXCEPT ![r] = "checked"]
            /\ holder' = [holder EXCEPT ![ReqProcess[r]] = r]
            /\ dbHolder' = IF DatabaseLock THEN r ELSE NoHolder
            /\ UNCHANGED outcome
  /\ UNCHANGED <<pools, uncommitted>>

\* No PlacementLocker: Store.CreatePool commits on its own, then the deferred placeGate.release().
Insert(r) ==
  /\ ~DatabaseLock
  /\ pc[r] = "checked"
  /\ IF Refused(r)
       THEN /\ outcome' = [outcome EXCEPT ![r] = "rejected"]   \* 409 from storage.ErrConflict
            /\ UNCHANGED pools
       ELSE /\ pools' = pools \union {Pool(r)}
            /\ outcome' = [outcome EXCEPT ![r] = "created"]    \* 201
  /\ pc' = [pc EXCEPT ![r] = "done"]
  /\ holder' = Release(r)
  /\ UNCHANGED <<uncommitted, dbHolder>>

\* placementTx.CreatePool: INSERT in a savepoint. A duplicate rolls back only the savepoint.
TxInsert(r) ==
  /\ DatabaseLock
  /\ pc[r] = "checked"
  /\ uncommitted' = IF Refused(r) THEN uncommitted ELSE uncommitted \union {Pool(r)}
  /\ pc' = [pc EXCEPT ![r] = "inserted"]
  /\ UNCHANGED <<pools, outcome, holder, dbHolder>>

\* fn returned nil and ctx is live: COMMIT. PostgreSQL makes the rows visible before it frees the
\* transaction's advisory lock, so the next holder's ListPools sees them.
Commit(r) ==
  /\ pc[r] = "inserted"
  /\ pools' = pools \union (uncommitted \intersect {Pool(r)})
  /\ uncommitted' = uncommitted \ {Pool(r)}
  /\ outcome' = [outcome EXCEPT ![r] = IF Pool(r) \in uncommitted THEN "created" ELSE "rejected"]
  /\ pc' = [pc EXCEPT ![r] = "done"]
  /\ dbHolder' = NoHolder
  /\ holder' = Release(r)

\* The placement transaction ends without COMMIT. The deferred Rollback, or the server on session
\* death, drops the advisory lock and the request's inserts together.
SessionLost(r) ==
  /\ DatabaseLock
  /\ pc[r] \in {"checked", "inserted"}
  /\ uncommitted' = uncommitted \ {Pool(r)}
  /\ outcome' = [outcome EXCEPT ![r] = "failed"]               \* 500, or the client is gone
  /\ pc' = [pc EXCEPT ![r] = "done"]
  /\ dbHolder' = NoHolder
  /\ holder' = Release(r)
  /\ UNCHANGED pools

\* All requests finished: a correct end state, not a deadlock.
Done == /\ \A r \in Requests : pc[r] = "done"
        /\ UNCHANGED vars

Next ==
  \/ \E r \in Requests : ListAndCheck(r)
  \/ \E r \in Requests : Insert(r)
  \/ \E r \in Requests : TxInsert(r)
  \/ \E r \in Requests : Commit(r)
  \/ \E r \in Requests : SessionLost(r)
  \/ Done

Spec == Init /\ [][Next]_vars

-----------------------------------------------------------------------------

TypeOK ==
  /\ pools \subseteq {Pool(r) : r \in Requests}
  /\ uncommitted \subseteq {Pool(r) : r \in Requests}
  /\ pc \in [Requests -> {"start", "checked", "inserted", "done"}]
  /\ outcome \in [Requests -> {"pending", "created", "rejected", "failed"}]
  /\ holder \in [Processes -> Requests \union {NoHolder}]
  /\ dbHolder \in Requests \union {NoHolder}

NoOverlappingSiblings ==
  \A p, q \in pools :
    (p # q /\ p.parent = q.parent) => (p.cidr # q.cidr /\ ~Overlaps(p.cidr, q.cidr))

\* Pools are never deleted in this model, so a conflict at rejection time is still visible now.
RejectionsAreJustified ==
  \A r \in Requests :
    outcome[r] = "rejected" =>
      \E p \in Siblings(r, pools) : p.cidr = ReqCIDR[r] \/ Overlaps(p.cidr, ReqCIDR[r])

=============================================================================
