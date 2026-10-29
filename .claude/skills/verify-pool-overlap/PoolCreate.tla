---------------------------- MODULE PoolCreate ----------------------------
(***************************************************************************)
(* Concurrent POST /api/v1/pools requests against one CloudPAM server.     *)
(*                                                                         *)
(* Code modelled (internal/api/pool_handlers.go, createPool, at the sync   *)
(* commit recorded in SKILL.md):                                           *)
(*                                                                         *)
(*   ListAndCheck(r)  = lines 535-566: s.store.ListPools(ctx) takes one    *)
(*                      snapshot under the store's read lock (MemoryStore  *)
(*                      ListPools, store.go) or one SELECT (sqlite.go),    *)
(*                      then the overlap loop runs on that snapshot only.  *)
(*                      Exact-equal CIDRs are skipped (line 550-552,       *)
(*                      "DB uniqueness should also catch").                *)
(*   Insert(r)        = line 569: s.store.CreatePool(ctx, in). One write   *)
(*                      under the store's write lock / one INSERT. The     *)
(*                      only constraint the store enforces is the unique   *)
(*                      index on (parent_id, cidr) from                    *)
(*                      migrations/0001_init.sql; MemoryStore enforces     *)
(*                      nothing.                                           *)
(*   AtomicCreate(r)  = the proposed fix: check and insert as one          *)
(*                      indivisible step (a handler-level mutex, a         *)
(*                      serialized transaction, or a store-side overlap    *)
(*                      constraint). Not in the code today.                *)
(*                                                                         *)
(* Nothing in the handler or the store holds a lock across ListPools and   *)
(* CreatePool, and net/http serves each request on its own goroutine, so   *)
(* the two steps of different requests may interleave freely.             *)
(***************************************************************************)
EXTENDS Naturals, FiniteSets

CONSTANTS
  Requests,              \* concurrent POST /api/v1/pools requests
  ReqCIDR,               \* [Requests -> CIDR label] the body's "cidr"
  ReqParent,             \* [Requests -> scope]      the body's "parent_id" (or "root")
  Overlaps(_, _),        \* prefixesOverlapIPv4 on two distinct CIDR labels
  AtomicCheckAndInsert,  \* FALSE = code as of the sync commit; TRUE = proposed fix
  StoreRejectsDuplicate  \* TRUE = SQLite/Postgres unique index; FALSE = MemoryStore (DEV_MODE=1)

VARIABLES
  pools,     \* set of stored pools: [req, cidr, parent]
  pc,        \* per request: "start" | "checked" | "done"
  snapshot,  \* per request: what ListPools returned to it
  outcome    \* per request: "pending" | "created" | "rejected"

vars == <<pools, pc, snapshot, outcome>>

Pool(r) == [req |-> r, cidr |-> ReqCIDR[r], parent |-> ReqParent[r]]

\* Pools the handler compares against: same parent_id, or both top-level (lines 541-549).
Siblings(r, ps) == {p \in ps : p.parent = ReqParent[r]}

\* The handler's loop rejects on overlap, but skips an exact-equal CIDR (lines 550-559).
HandlerConflict(r, ps) ==
  \E p \in Siblings(r, ps) : p.cidr # ReqCIDR[r] /\ Overlaps(p.cidr, ReqCIDR[r])

\* What the store's unique index on (parent_id, cidr) rejects.
DuplicateExists(r, ps) == \E p \in Siblings(r, ps) : p.cidr = ReqCIDR[r]

Init ==
  /\ pools = {}
  /\ pc = [r \in Requests |-> "start"]
  /\ snapshot = [r \in Requests |-> {}]
  /\ outcome = [r \in Requests |-> "pending"]

\* createPool lines 535-566: ListPools snapshot + overlap loop. One step, because the
\* loop reads only the snapshot and no other request can change that copy.
ListAndCheck(r) ==
  /\ ~AtomicCheckAndInsert
  /\ pc[r] = "start"
  /\ snapshot' = [snapshot EXCEPT ![r] = pools]
  /\ IF HandlerConflict(r, pools)
       THEN /\ outcome' = [outcome EXCEPT ![r] = "rejected"]   \* 400 "cidr overlaps with existing block"
            /\ pc' = [pc EXCEPT ![r] = "done"]
       ELSE /\ pc' = [pc EXCEPT ![r] = "checked"]
            /\ UNCHANGED outcome
  /\ UNCHANGED pools

\* createPool line 569: store.CreatePool. The store sees the live pools, not the snapshot.
Insert(r) ==
  /\ ~AtomicCheckAndInsert
  /\ pc[r] = "checked"
  /\ IF StoreRejectsDuplicate /\ DuplicateExists(r, pools)
       THEN /\ outcome' = [outcome EXCEPT ![r] = "rejected"]   \* 400 from storage.WrapIfConflict
            /\ UNCHANGED pools
       ELSE /\ pools' = pools \union {Pool(r)}
            /\ outcome' = [outcome EXCEPT ![r] = "created"]    \* 201
  /\ pc' = [pc EXCEPT ![r] = "done"]
  /\ UNCHANGED snapshot

\* Proposed fix: the same check and the same insert, but nothing can run between them.
AtomicCreate(r) ==
  /\ AtomicCheckAndInsert
  /\ pc[r] = "start"
  /\ snapshot' = [snapshot EXCEPT ![r] = pools]
  /\ IF HandlerConflict(r, pools) \/ (StoreRejectsDuplicate /\ DuplicateExists(r, pools))
       THEN /\ outcome' = [outcome EXCEPT ![r] = "rejected"]
            /\ UNCHANGED pools
       ELSE /\ pools' = pools \union {Pool(r)}
            /\ outcome' = [outcome EXCEPT ![r] = "created"]
  /\ pc' = [pc EXCEPT ![r] = "done"]

\* All requests finished: a correct end state, not a deadlock.
Done == /\ \A r \in Requests : pc[r] = "done"
        /\ UNCHANGED vars

Next ==
  \/ \E r \in Requests : ListAndCheck(r)
  \/ \E r \in Requests : Insert(r)
  \/ \E r \in Requests : AtomicCreate(r)
  \/ Done

Spec == Init /\ [][Next]_vars

-----------------------------------------------------------------------------
(* Rules *)

TypeOK ==
  /\ pools \subseteq {Pool(r) : r \in Requests}
  /\ pc \in [Requests -> {"start", "checked", "done"}]
  /\ outcome \in [Requests -> {"pending", "created", "rejected"}]

\* THE PROMISE: no two stored pools under the same parent share or overlap address space.
NoOverlappingSiblings ==
  \A p, q \in pools :
    (p # q /\ p.parent = q.parent) => (p.cidr # q.cidr /\ ~Overlaps(p.cidr, q.cidr))

\* The guard never rejects a request that had no conflicting sibling.
\* Pools are never deleted in this model, so a conflict at rejection time is still visible now.
RejectionsAreJustified ==
  \A r \in Requests :
    outcome[r] = "rejected" =>
      \E p \in Siblings(r, pools) : p.cidr = ReqCIDR[r] \/ Overlaps(p.cidr, ReqCIDR[r])

=============================================================================
