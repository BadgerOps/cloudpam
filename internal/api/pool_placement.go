package api

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"cloudpam/internal/domain"
	"cloudpam/internal/planning"
	"cloudpam/internal/storage"
	"cloudpam/internal/validation"
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

// poolFieldError is a CreatePool field that fails the checks every pool must
// pass. Msg and Detail map onto the API error body; Error() joins them for
// per-entry reports.
type poolFieldError struct {
	Field  string
	Value  string
	Msg    string
	Detail string
}

func (e *poolFieldError) Error() string {
	if e.Detail != "" {
		return e.Msg + ": " + e.Detail
	}
	return e.Msg
}

// validatePoolFields checks name, CIDR (format, IPv4 only, canonical, reserved
// ranges, prefix bounds) and the type, status and source enumerations when set.
// Containment, overlap and parent existence are placement concerns.
func validatePoolFields(in domain.CreatePool) *poolFieldError {
	if err := validation.ValidateName(in.Name); err != nil {
		return &poolFieldError{Field: "name", Value: in.Name, Msg: err.Error()}
	}
	if err := validation.ValidateCIDR(in.CIDR); err != nil {
		return &poolFieldError{Field: "cidr", Value: in.CIDR, Msg: err.Error()}
	}
	if in.CIDR != strings.TrimSpace(in.CIDR) {
		return &poolFieldError{Field: "cidr", Value: in.CIDR, Msg: "cidr has leading or trailing whitespace"}
	}
	if in.Type != "" && !domain.IsValidPoolType(in.Type) {
		return &poolFieldError{Field: "type", Value: string(in.Type), Msg: "invalid pool type", Detail: fmt.Sprintf("valid types: %v", domain.ValidPoolTypes)}
	}
	if in.Status != "" && !domain.IsValidPoolStatus(in.Status) {
		return &poolFieldError{Field: "status", Value: string(in.Status), Msg: "invalid pool status", Detail: fmt.Sprintf("valid statuses: %v", domain.ValidPoolStatuses)}
	}
	if in.Source != "" && !domain.IsValidPoolSource(in.Source) {
		return &poolFieldError{Field: "source", Value: string(in.Source), Msg: "invalid pool source", Detail: fmt.Sprintf("valid sources: %v", domain.ValidPoolSources)}
	}
	return nil
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

// placementBudget bounds a placement or pool delete from the moment it queues on placeGate to
// its last write. It is a var so tests can shorten it. With the store's 5s commit margin it fits
// inside the server's 15s write timeout, so a placement never commits after the client
// has stopped waiting for the response.
var placementBudget = 10 * time.Second

var _ planning.PoolPlacer = (*Server)(nil)

// PlacePool creates one pool the way POST /api/v1/pools does and returns it once stored, on
// PostgreSQL once committed. Its errors carry storage sentinels for callers outside this package:
// an invalid field, a missing parent or a pool outside its parent is ErrValidation, an
// overlapping sibling is ErrConflict, and a placement past its budget is ErrBusy.
func (s *Server) PlacePool(ctx context.Context, in domain.CreatePool) (domain.Pool, error) {
	placed, err := s.placePools(ctx, siblingsOnly, []poolPlacement{{In: in}})
	if err != nil {
		return domain.Pool{}, err
	}
	p, err := placed[0].Pool, placed[0].Err
	var invalid *poolFieldError
	var missing *missingParentError
	var outside *containmentError
	var overlap *placementConflict
	switch {
	case errors.As(err, &invalid), errors.As(err, &missing), errors.As(err, &outside):
		return p, fmt.Errorf("%w: %w", storage.ErrValidation, err)
	case errors.As(err, &overlap):
		return p, fmt.Errorf("%w: %w", storage.ErrConflict, err)
	}
	return p, err
}

func (s *Server) placePools(ctx context.Context, policy overlapPolicy, batch []poolPlacement) ([]placedPool, error) {
	var out []placedPool
	err := s.WithPlacement(ctx, func(work context.Context, ps storage.PlacementStore) error {
		var err error
		out, err = place(work, ps, policy, batch)
		return err
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// deletePool deletes a pool, and with cascade its whole subtree, under the placement lock. A
// placement that has already listed the pool as a parent therefore stores its child first, and the
// delete then sees that child, instead of the child landing under a deleted parent (#269).
func (s *Server) deletePool(ctx context.Context, id int64, cascade bool) (bool, error) {
	var ok bool
	err := s.WithPlacement(ctx, func(work context.Context, ps storage.PlacementStore) error {
		var err error
		if cascade {
			ok, err = ps.DeletePoolCascade(work, id)
		} else {
			ok, err = ps.DeletePool(work, id)
		}
		return err
	})
	return ok, err
}

// deleteAccountCascade deletes an account and every pool tree assigned to it under the placement
// lock, for the same reason as deletePool.
func (s *Server) deleteAccountCascade(ctx context.Context, id int64) (bool, error) {
	var ok bool
	err := s.WithPlacement(ctx, func(work context.Context, ps storage.PlacementStore) error {
		var err error
		ok, err = ps.DeleteAccountCascade(work, id)
		return err
	})
	return ok, err
}

// restorePool brings a soft-deleted pool back under the placement lock, checked against
// its live parent and siblings the way POST /api/v1/pools checks a new pool. A deleted
// parent or an overlapping sibling is ErrConflict.
func (s *Server) restorePool(ctx context.Context, deleted domain.Pool) (domain.Pool, bool, error) {
	var p domain.Pool
	var ok bool
	err := s.WithPlacement(ctx, func(work context.Context, ps storage.PlacementStore) error {
		live, err := ps.ListPools(work)
		if err != nil {
			return err
		}
		in := domain.CreatePool{Name: deleted.Name, CIDR: deleted.CIDR, ParentID: deleted.ParentID}
		var missing *missingParentError
		var overlap *placementConflict
		switch err := indexPools(live).admit(0, "", in); {
		case errors.As(err, &missing):
			return fmt.Errorf("parent pool %d is deleted; restore it first: %w", missing.ParentID, storage.ErrConflict)
		case errors.As(err, &overlap):
			return fmt.Errorf("pool %q (%s) overlaps with live pool #%d (%s): %w", deleted.Name, deleted.CIDR, overlap.With.ID, overlap.With.CIDR, storage.ErrConflict)
		case err != nil:
			return fmt.Errorf("%w: %w", storage.ErrConflict, err)
		}
		p, ok, err = ps.RestorePool(work, deleted.ID)
		return err
	})
	return p, ok, err
}

// WithPlacement runs fn holding placeGate and, on a store several processes share, the store's
// placement lock, all within placementBudget. fn must use work and ps for every read and write.
// Code that resizes or deletes a pool needs it too, not only code that creates pools, or a
// placement running at the same time can check against a stale range.
func (s *Server) WithPlacement(ctx context.Context, fn func(work context.Context, ps storage.PlacementStore) error) error {
	work, cancel := context.WithTimeout(ctx, placementBudget)
	defer cancel()

	if err := s.placeGate.acquire(work); err != nil {
		return busyIfBudgetSpent(ctx, err)
	}
	defer s.placeGate.release()

	locker, ok := s.store.(storage.PlacementLocker)
	if !ok {
		return busyIfBudgetSpent(ctx, fn(work, s.store))
	}
	return busyIfBudgetSpent(ctx, locker.WithPlacementLock(work, func(ps storage.PlacementStore) error {
		return fn(work, ps)
	}))
}

// errLinkPoolGone reports that the pool a resource was being linked to was deleted before the
// link got the placement guard.
var errLinkPoolGone = errors.New("pool not found")

// linkResource links a discovered resource to a live pool under the placement guard, which reclaim,
// resize and consolidation also hold while they read or move a pool's links. The pool is rechecked
// under the guard because one of them may have deleted it while this request waited.
func (s *Server) linkResource(ctx context.Context, ds storage.DiscoveryStore, resourceID uuid.UUID, poolID int64) error {
	return s.WithPlacement(ctx, func(work context.Context, ps storage.PlacementStore) error {
		if _, found, err := ps.GetPool(work, poolID); err != nil {
			return err
		} else if !found {
			return fmt.Errorf("pool %d: %w", poolID, errLinkPoolGone)
		}
		return storage.LinksIn(ps, ds).LinkResourceToPool(work, resourceID, poolID)
	})
}

// placementGate serializes placements, resizes, restores and pool deletes inside one process. Unlike a mutex, a request
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
	out := make([]placedPool, len(batch))
	for i, e := range batch {
		if ferr := validatePoolFields(e.In); ferr != nil {
			out[i].Err = ferr
		}
	}
	if policy != siblingsOnly {
		if err := checkPlan(policy, live, pools, batch, out); err != nil {
			return nil, err
		}
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}
	stored := make(map[string]int64, len(batch))
	for i, e := range batch {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if out[i].Err != nil {
			continue
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

// checkPlan validates the whole batch before anything is stored. Entries that already
// failed in out are skipped, so their error stays per entry.
func checkPlan(policy overlapPolicy, live []domain.Pool, pools *poolIndex, batch []poolPlacement, out []placedPool) error {
	cidrByRef := make(map[string]string, len(batch))
	for i, e := range batch {
		if out[i].Err != nil {
			continue
		}
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
	return checkPlacement(live, batch, out)
}

func checkPlacement(live []domain.Pool, batch []poolPlacement, out []placedPool) error {
	for i, e := range batch {
		if out[i].Err != nil {
			continue
		}
		for _, p := range live {
			if cidrsOverlap(p.CIDR, e.In.CIDR) {
				return &placementConflict{Pool: e.In, With: p}
			}
		}
		for j, prev := range batch[:i] {
			if out[j].Err != nil {
				continue
			}
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
