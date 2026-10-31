package api

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"sync"
	"time"

	"cloudpam/internal/domain"
	"cloudpam/internal/planning"
	"cloudpam/internal/storage"
)

type overlapPolicy int

const (
	siblingsOnly overlapPolicy = iota
	anyLivePool
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

type missingParentError struct {
	ParentID int64
}

func (e *missingParentError) Error() string {
	return fmt.Sprintf("parent_id %d not found", e.ParentID)
}

type containmentError struct {
	Index int
	Ref   string
	Cause error
}

func (e *containmentError) Error() string {
	return "invalid sub-pool cidr: " + e.Cause.Error()
}

// placementBudget bounds a placement from the moment it queues on placeGate to its last
// insert. It is a var so tests can shorten it. With the store's 5s commit margin it fits
// inside the server's 15s write timeout, so a placement never commits after the client
// has stopped waiting for the response.
var placementBudget = 10 * time.Second

var _ planning.PoolPlacer = (*Server)(nil)

// PlacePool creates one pool the way POST /api/v1/pools does and returns it once stored, on
// PostgreSQL once committed. Its errors carry storage sentinels for callers outside this package:
// a missing parent or a pool outside its parent is ErrValidation, an overlapping sibling is
// ErrConflict, and a placement past its budget is ErrBusy.
func (s *Server) PlacePool(ctx context.Context, in domain.CreatePool) (domain.Pool, error) {
	placed, err := s.placePools(ctx, siblingsOnly, []poolPlacement{{In: in}})
	if err != nil {
		return domain.Pool{}, err
	}
	p, err := placed[0].Pool, placed[0].Err
	var missing *missingParentError
	var outside *containmentError
	var overlap *placementConflict
	switch {
	case errors.As(err, &missing), errors.As(err, &outside):
		return p, fmt.Errorf("%w: %w", storage.ErrValidation, err)
	case errors.As(err, &overlap):
		return p, fmt.Errorf("%w: %w", storage.ErrConflict, err)
	}
	return p, err
}

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
	live, err := ps.ListPools(ctx)
	if err != nil {
		return nil, err
	}
	pools := indexPools(live)
	if policy != siblingsOnly {
		if err := checkPlan(policy, live, pools, batch); err != nil {
			return nil, err
		}
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}
	out := make([]placedPool, len(batch))
	stored := make(map[string]int64, len(batch))
	for i, e := range batch {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		in := e.In
		if e.ParentRef != "" {
			if id, ok := stored[e.ParentRef]; ok {
				in.ParentID = &id
			} else if in.ParentID == nil {
				out[i].Err = fmt.Errorf("parent_ref %q not yet created", e.ParentRef)
				continue
			}
		}
		if policy == siblingsOnly {
			if err := pools.admit(i, e.Ref, in); err != nil {
				out[i].Err = err
				continue
			}
		}
		p, err := ps.CreatePool(ctx, in)
		if errors.Is(err, storage.ErrBusy) {
			return nil, err
		}
		if err != nil {
			out[i].Err = err
			continue
		}
		pools.add(p)
		out[i].Pool = p
		stored[e.Ref] = p.ID
	}
	return out, nil
}

type sibling struct {
	pool   domain.Pool
	lo, hi uint32
}

func ipv4Span(cidr string) (lo, hi uint32, ok bool) {
	p, err := netip.ParsePrefix(cidr)
	if err != nil || !p.Addr().Is4() {
		return 0, 0, false
	}
	lo = ipv4ToUint32(p.Masked().Addr())
	return lo, lo + uint32(1)<<uint32(32-p.Bits()) - 1, true
}

type scope struct {
	parentID int64
	topLevel bool
}

func scopeOf(parentID *int64) scope {
	if parentID == nil {
		return scope{topLevel: true}
	}
	return scope{parentID: *parentID}
}

type poolIndex struct {
	byID     map[int64]domain.Pool
	children map[scope][]sibling
}

func indexPools(live []domain.Pool) *poolIndex {
	x := &poolIndex{byID: make(map[int64]domain.Pool, len(live)), children: make(map[scope][]sibling)}
	for _, p := range live {
		x.add(p)
	}
	return x
}

func (x *poolIndex) add(p domain.Pool) {
	x.byID[p.ID] = p
	lo, hi, ok := ipv4Span(p.CIDR)
	if !ok {
		return
	}
	key := scopeOf(p.ParentID)
	x.children[key] = append(x.children[key], sibling{pool: p, lo: lo, hi: hi})
}

func (x *poolIndex) admit(i int, ref string, in domain.CreatePool) error {
	if in.ParentID != nil {
		parent, ok := x.byID[*in.ParentID]
		if !ok {
			return &missingParentError{ParentID: *in.ParentID}
		}
		if err := validateChildCIDR(parent.CIDR, in.CIDR); err != nil {
			return &containmentError{Index: i, Ref: ref, Cause: err}
		}
	}
	lo, hi, ok := ipv4Span(in.CIDR)
	if !ok {
		return nil
	}
	for _, s := range x.children[scopeOf(in.ParentID)] {
		if s.pool.CIDR != in.CIDR && s.lo <= hi && lo <= s.hi {
			return &placementConflict{Pool: in, With: s.pool}
		}
	}
	return nil
}

func checkPlan(policy overlapPolicy, live []domain.Pool, pools *poolIndex, batch []poolPlacement) error {
	cidrByRef := make(map[string]string, len(batch))
	for i, e := range batch {
		var parentCIDR string
		switch {
		case e.ParentRef != "":
			parentCIDR = cidrByRef[e.ParentRef]
		case e.In.ParentID != nil:
			parent, ok := pools.byID[*e.In.ParentID]
			if !ok {
				return &missingParentError{ParentID: *e.In.ParentID}
			}
			parentCIDR = parent.CIDR
		}
		if parentCIDR != "" {
			if err := validateChildCIDR(parentCIDR, e.In.CIDR); err != nil {
				return &containmentError{Index: i, Ref: e.Ref, Cause: err}
			}
		}
		if e.Ref != "" {
			cidrByRef[e.Ref] = e.In.CIDR
		}
	}
	if policy == overlapAllowed {
		return nil
	}
	return checkPlacement(live, batch)
}

func checkPlacement(live []domain.Pool, batch []poolPlacement) error {
	for i, e := range batch {
		for _, p := range live {
			if cidrsOverlap(p.CIDR, e.In.CIDR) {
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

func cidrsOverlap(a, b string) bool {
	pa, errA := netip.ParsePrefix(a)
	pb, errB := netip.ParsePrefix(b)
	return errA == nil && errB == nil && prefixesOverlapIPv4(pa, pb)
}

func sameParent(a, b *int64) bool {
	return a == nil && b == nil || a != nil && b != nil && *a == *b
}
