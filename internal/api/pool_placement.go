package api

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"sync"
	"time"

	"cloudpam/internal/domain"
	"cloudpam/internal/storage"
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

// placementBudget bounds a placement from the moment it queues on placeGate to its last
// insert. It is a var so tests can shorten it. With the store's 5s commit margin it fits
// inside the server's 15s write timeout, so a placement never commits after the client
// has stopped waiting for the response.
var placementBudget = 10 * time.Second

func (s *Server) placePools(ctx context.Context, policy overlapPolicy, batch []poolPlacement) ([]placedPool, error) {
	work, cancel := context.WithTimeout(ctx, placementBudget)
	defer cancel()

	if err := s.placeGate.acquire(work); err != nil {
		return nil, busyIfBudgetSpent(ctx, err)
	}
	defer s.placeGate.release()

	locker, ok := s.store.(storage.PlacementLocker)
	if !ok {
		out, err := place(work, s.store, policy, batch)
		return out, busyIfBudgetSpent(ctx, err)
	}
	var out []placedPool
	err := locker.WithPlacementLock(work, func(ps storage.PlacementStore) error {
		var err error
		out, err = place(work, ps, policy, batch)
		return err
	})
	if err != nil {
		return nil, busyIfBudgetSpent(ctx, err)
	}
	return out, nil
}

// placementGate serializes placements inside one process. Unlike a mutex, a request
// waiting for it gives up when its context ends, so the placement budget also bounds the
// time spent queued behind another placement.
type placementGate struct {
	once sync.Once
	slot chan struct{}
}

func (g *placementGate) acquire(ctx context.Context) error {
	g.once.Do(func() { g.slot = make(chan struct{}, 1) })
	select {
	case g.slot <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (g *placementGate) release() { <-g.slot }

func (g *placementGate) held() bool {
	g.once.Do(func() { g.slot = make(chan struct{}, 1) })
	return len(g.slot) == 1
}

// busyIfBudgetSpent reports the placement budget running out as ErrBusy. A deadline that
// came from the request itself is returned as is.
func busyIfBudgetSpent(request context.Context, err error) error {
	if errors.Is(err, context.DeadlineExceeded) && request.Err() == nil {
		return fmt.Errorf("pool placement did not finish within %s: %w", placementBudget, storage.ErrBusy)
	}
	return err
}

func place(ctx context.Context, ps storage.PlacementStore, policy overlapPolicy, batch []poolPlacement) ([]placedPool, error) {
	if policy != overlapAllowed {
		live, err := ps.ListPools(ctx)
		if err != nil {
			return nil, err
		}
		if c := checkPlacement(policy, live, batch); c != nil {
			return nil, c
		}
	}

	if err := ctx.Err(); err != nil {
		return nil, err
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
		p, err := ps.CreatePool(ctx, in)
		if errors.Is(err, storage.ErrBusy) {
			return nil, err
		}
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
