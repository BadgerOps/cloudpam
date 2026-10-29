---------------------------- MODULE PoolCreate ----------------------------
(***************************************************************************)
(* Concurrent POST /api/v1/pools requests against CloudPAM.               *)
(*                                                                         *)
(* Code modelled, at the sync commit recorded in SKILL.md:                 *)
(*                                                                         *)
(*   createPool (internal/api/pool_handlers.go) validates the body and the *)
(*   parent, then calls Server.placePools(ctx, siblingsOnly, ...) in       *)
(*   internal/api/pool_placement.go, which does:                           *)
(*                                                                         *)
(*   ListAndCheck(r)  = placeMu.Lock(), s.store.ListPools(ctx), then       *)
(*                      checkPlacement on that snapshot. The check         *)
(*                      compares siblings only (same parent_id, or both    *)
(*                      top-level) and skips an exact-equal CIDR           *)
(*                      (overlapPolicy.compares). A conflict returns at    *)
(*                      once and the deferred Unlock releases placeMu.     *)
(*   Insert(r)        = s.store.CreatePool(ctx, in), still under placeMu,  *)
(*                      then the deferred placeMu.Unlock(). Every store    *)
(*                      rejects an exact (parent, cidr) duplicate with     *)
(*                      ErrConflict: SQLite through uniq_pools_parent_cidr *)
(*                      (migrations/0001_init.sql), MemoryStore.CreatePool *)
(*                      by a scan of live pools, Postgres through          *)
(*                      idx_pools_org_cidr on (organization_id, cidr),     *)
(*                      which also refuses an equal CIDR under another     *)
(*                      parent.                                            *)
(*                                                                         *)
(* placeMu is a field of Server, so it serializes requests inside one      *)
(* process only. ReqProcess says which process serves each request; two    *)
(* processes share the store (several replicas on one Postgres).          *)
(***************************************************************************)
EXTENDS Naturals, FiniteSets

CONSTANTS
  Requests,              \* concurrent POST /api/v1/pools requests
  ReqCIDR,               \* [Requests -> CIDR label] the body's "cidr"
  ReqParent,             \* [Requests -> scope]      the body's "parent_id" (or "root")
  ReqProcess,            \* [Requests -> process]    the CloudPAM process that serves it
  Overlaps(_, _),        \* prefixesOverlapIPv4 on two distinct CIDR labels
  StoreRejectsDuplicate  \* TRUE = every store at the sync commit; FALSE = no (parent, cidr) check

NoHolder == "none"
Processes == {ReqProcess[r] : r \in Requests}

VARIABLES
  pools,     \* set of stored pools: [req, cidr, parent]
  pc,        \* per request: "start" | "checked" | "done"
  outcome,   \* per request: "pending" | "created" | "rejected"
  holder     \* per process: the request holding that Server's placeMu, or NoHolder

vars == <<pools, pc, outcome, holder>>

Pool(r) == [req |-> r, cidr |-> ReqCIDR[r], parent |-> ReqParent[r]]

\* Pools checkPlacement compares against under siblingsOnly: same parent_id, or both top-level.
Siblings(r, ps) == {p \in ps : p.parent = ReqParent[r]}

\* checkPlacement rejects on overlap but skips an exact-equal CIDR (p.CIDR != e.In.CIDR).
HandlerConflict(r, ps) ==
  \E p \in Siblings(r, ps) : p.cidr # ReqCIDR[r] /\ Overlaps(p.cidr, ReqCIDR[r])

\* What the store's (parent, cidr) uniqueness rejects.
DuplicateExists(r, ps) == \E p \in Siblings(r, ps) : p.cidr = ReqCIDR[r]

Init ==
  /\ pools = {}
  /\ pc = [r \in Requests |-> "start"]
  /\ outcome = [r \in Requests |-> "pending"]
  /\ holder = [s \in Processes |-> NoHolder]

\* placePools: Lock, ListPools, checkPlacement. One step: the check reads only the ListPools copy.
ListAndCheck(r) ==
  /\ pc[r] = "start"
  /\ holder[ReqProcess[r]] = NoHolder
  /\ IF HandlerConflict(r, pools)
       THEN /\ outcome' = [outcome EXCEPT ![r] = "rejected"]   \* 400 "cidr overlaps with existing block"
            /\ pc' = [pc EXCEPT ![r] = "done"]
            /\ UNCHANGED holder                                 \* Lock and deferred Unlock in one step
       ELSE /\ pc' = [pc EXCEPT ![r] = "checked"]
            /\ holder' = [holder EXCEPT ![ReqProcess[r]] = r]
            /\ UNCHANGED outcome
  /\ UNCHANGED pools

\* placePools: store.CreatePool, then the deferred Unlock. The store sees the live pools.
Insert(r) ==
  /\ pc[r] = "checked"
  /\ IF StoreRejectsDuplicate /\ DuplicateExists(r, pools)
       THEN /\ outcome' = [outcome EXCEPT ![r] = "rejected"]   \* 409 from storage.ErrConflict
            /\ UNCHANGED pools
       ELSE /\ pools' = pools \union {Pool(r)}
            /\ outcome' = [outcome EXCEPT ![r] = "created"]    \* 201
  /\ pc' = [pc EXCEPT ![r] = "done"]
  /\ holder' = [holder EXCEPT ![ReqProcess[r]] = NoHolder]

\* All requests finished: a correct end state, not a deadlock.
Done == /\ \A r \in Requests : pc[r] = "done"
        /\ UNCHANGED vars

Next ==
  \/ \E r \in Requests : ListAndCheck(r)
  \/ \E r \in Requests : Insert(r)
  \/ Done

Spec == Init /\ [][Next]_vars

-----------------------------------------------------------------------------

TypeOK ==
  /\ pools \subseteq {Pool(r) : r \in Requests}
  /\ pc \in [Requests -> {"start", "checked", "done"}]
  /\ outcome \in [Requests -> {"pending", "created", "rejected"}]
  /\ holder \in [Processes -> Requests \union {NoHolder}]

NoOverlappingSiblings ==
  \A p, q \in pools :
    (p # q /\ p.parent = q.parent) => (p.cidr # q.cidr /\ ~Overlaps(p.cidr, q.cidr))

\* Pools are never deleted in this model, so a conflict at rejection time is still visible now.
RejectionsAreJustified ==
  \A r \in Requests :
    outcome[r] = "rejected" =>
      \E p \in Siblings(r, pools) : p.cidr = ReqCIDR[r] \/ Overlaps(p.cidr, ReqCIDR[r])

=============================================================================
