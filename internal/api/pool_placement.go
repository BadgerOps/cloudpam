package api

import (
	"context"
	"fmt"
	"net/netip"

	"cloudpam/internal/domain"
)

type overlapPolicy int

const (
	anyLivePool overlapPolicy = iota
	siblingsOnly
	overlapAllowed
)

type poolPlacement struct {
	Ref       string
	ParentRef string
	In        domain.CreatePool
}

type placedPool struct {
	Pool domain.Pool
	Err  error
}

type placementConflict struct {
	Pool    domain.CreatePool
	With    domain.Pool
	InBatch bool
}

func (c *placementConflict) Error() string {
	if c.InBatch {
		return fmt.Sprintf("pool %q (%s) overlaps with pool %q (%s) in the same request", c.Pool.Name, c.Pool.CIDR, c.With.Name, c.With.CIDR)
	}
	return fmt.Sprintf("pool %q (%s) overlaps with existing pool %q (%s)", c.Pool.Name, c.Pool.CIDR, c.With.Name, c.With.CIDR)
}

func (s *Server) placePools(ctx context.Context, policy overlapPolicy, batch []poolPlacement) ([]placedPool, error) {
	s.placeMu.Lock()
	defer s.placeMu.Unlock()

	if policy != overlapAllowed {
		live, err := s.store.ListPools(ctx)
		if err != nil {
			return nil, err
		}
		if c := checkPlacement(policy, live, batch); c != nil {
			return nil, c
		}
	}

	out := make([]placedPool, len(batch))
	stored := make(map[string]int64, len(batch))
	for i, e := range batch {
		in := e.In
		if e.ParentRef != "" {
			id, ok := stored[e.ParentRef]
			if !ok {
				out[i].Err = fmt.Errorf("parent_ref %q not yet created", e.ParentRef)
				continue
			}
			in.ParentID = &id
		}
		p, err := s.store.CreatePool(ctx, in)
		if err != nil {
			out[i].Err = err
			continue
		}
		out[i].Pool = p
		stored[e.Ref] = p.ID
	}
	return out, nil
}

func checkPlacement(policy overlapPolicy, live []domain.Pool, batch []poolPlacement) *placementConflict {
	for i, e := range batch {
		for _, p := range live {
			if policy.compares(e, p) && cidrsOverlap(p.CIDR, e.In.CIDR) {
				return &placementConflict{Pool: e.In, With: p}
			}
		}
		for _, prev := range batch[:i] {
			if prev.ParentRef == e.ParentRef && sameParent(prev.In.ParentID, e.In.ParentID) && cidrsOverlap(prev.In.CIDR, e.In.CIDR) {
				return &placementConflict{Pool: e.In, With: domain.Pool{Name: prev.In.Name, CIDR: prev.In.CIDR}, InBatch: true}
			}
		}
	}
	return nil
}

func (policy overlapPolicy) compares(e poolPlacement, p domain.Pool) bool {
	if policy == siblingsOnly {
		return e.ParentRef == "" && sameParent(e.In.ParentID, p.ParentID) && p.CIDR != e.In.CIDR
	}
	return true
}

func cidrsOverlap(a, b string) bool {
	pa, errA := netip.ParsePrefix(a)
	pb, errB := netip.ParsePrefix(b)
	return errA == nil && errB == nil && prefixesOverlapIPv4(pa, pb)
}

func sameParent(a, b *int64) bool {
	return a == nil && b == nil || a != nil && b != nil && *a == *b
}
