---------------------------------- MODULE MC ----------------------------------
(* Concrete bound for PoolCreate: three concurrent requests at the top level.   *)
(* Overlap facts below are what prefixesOverlapIPv4 (internal/api/cidr.go)       *)
(* returns for these CIDRs; 10.0.0.0/9 and 10.128.0.0/9 are the two halves of   *)
(* 10.0.0.0/8 and do not overlap each other.                                     *)
EXTENDS PoolCreate, TLC

MCRequests == {"r1", "r2", "r3"}

\* Race scenario: r1 is a /8, r2 and r3 are its two disjoint halves.
MCReqCIDR == ("r1" :> "10.0.0.0/8") @@ ("r2" :> "10.0.0.0/9") @@ ("r3" :> "10.128.0.0/9")

\* Duplicate scenario: r2 and r3 ask for exactly the same CIDR.
MCReqCIDRDup == ("r1" :> "10.0.0.0/8") @@ ("r2" :> "10.0.0.0/9") @@ ("r3" :> "10.0.0.0/9")

MCReqParent == [r \in MCRequests |-> "root"]

OverlapPairs == { {"10.0.0.0/8", "10.0.0.0/9"}, {"10.0.0.0/8", "10.128.0.0/9"} }
MCOverlaps(a, b) == {a, b} \in OverlapPairs

===============================================================================
