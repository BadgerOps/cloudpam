//go:build postgres

package postgres

import (
	"context"
	"errors"
	"testing"

	"cloudpam/internal/domain"
	"cloudpam/internal/storage"
)

func TestUpdatePoolCIDR(t *testing.T) {
	resetDB(t)
	ctx := context.Background()
	s := testDB.store
	created, err := s.CreatePool(ctx, domain.CreatePool{Name: "wide", CIDR: "10.0.0.0/16"})
	if err != nil {
		t.Fatal(err)
	}

	updated, found, err := s.UpdatePoolCIDR(ctx, created.ID, "10.0.0.0/23")
	if err != nil || !found {
		t.Fatalf("UpdatePoolCIDR: found=%v err=%v", found, err)
	}
	if updated.CIDR != "10.0.0.0/23" {
		t.Errorf("returned cidr %q, want 10.0.0.0/23", updated.CIDR)
	}
	got, _, _ := s.GetPool(ctx, created.ID)
	if got.CIDR != "10.0.0.0/23" {
		t.Errorf("stored cidr %q, want 10.0.0.0/23", got.CIDR)
	}
	stats, err := s.CalculatePoolUtilization(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stats.TotalIPs != 512 {
		t.Errorf("total ips after shrink %d, want 512", stats.TotalIPs)
	}

	if _, found, err := s.UpdatePoolCIDR(ctx, 999, "10.1.0.0/24"); err != nil || found {
		t.Errorf("unknown pool: found=%v err=%v, want found=false", found, err)
	}
	if _, err := s.DeletePool(ctx, created.ID); err != nil {
		t.Fatal(err)
	}
	if _, found, err := s.UpdatePoolCIDR(ctx, created.ID, "10.1.0.0/24"); err != nil || found {
		t.Errorf("deleted pool: found=%v err=%v, want found=false", found, err)
	}
}

func TestUpdatePoolCIDRRejectsUsedCIDR(t *testing.T) {
	resetDB(t)
	ctx := context.Background()
	s := testDB.store
	a, err := s.CreatePool(ctx, domain.CreatePool{Name: "a", CIDR: "10.0.0.0/16"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreatePool(ctx, domain.CreatePool{Name: "b", CIDR: "10.0.0.0/23"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.UpdatePoolCIDR(ctx, a.ID, "10.0.0.0/23"); !errors.Is(err, storage.ErrConflict) {
		t.Errorf("UpdatePoolCIDR onto a used cidr: err=%v, want ErrConflict", err)
	}
}

func TestPlacementLockRollsBackRangeChanges(t *testing.T) {
	resetDB(t)
	ctx := context.Background()
	s := testDB.store
	parent, err := s.CreatePool(ctx, domain.CreatePool{Name: "corp", CIDR: "10.0.0.0/16"})
	if err != nil {
		t.Fatal(err)
	}
	keep, err := s.CreatePool(ctx, domain.CreatePool{Name: "keep", CIDR: "10.0.1.0/25", ParentID: &parent.ID})
	if err != nil {
		t.Fatal(err)
	}
	absorb, err := s.CreatePool(ctx, domain.CreatePool{Name: "absorb", CIDR: "10.0.1.128/25", ParentID: &parent.ID})
	if err != nil {
		t.Fatal(err)
	}

	abort := errors.New("abort after writing")
	err = s.WithPlacementLock(ctx, func(ps storage.PlacementStore) error {
		if _, found, err := ps.UpdatePoolCIDR(ctx, keep.ID, "10.0.1.0/24"); err != nil || !found {
			t.Fatalf("UpdatePoolCIDR in lock: found=%v err=%v", found, err)
		}
		if deleted, err := ps.DeletePool(ctx, absorb.ID); err != nil || !deleted {
			t.Fatalf("DeletePool in lock: deleted=%v err=%v", deleted, err)
		}
		return abort
	})
	if !errors.Is(err, abort) {
		t.Fatalf("WithPlacementLock: err=%v, want the fn error", err)
	}
	if got, _, _ := s.GetPool(ctx, keep.ID); got.CIDR != "10.0.1.0/25" {
		t.Errorf("kept pool cidr %q after rollback, want 10.0.1.0/25", got.CIDR)
	}
	if _, found, _ := s.GetPool(ctx, absorb.ID); !found {
		t.Error("absorbed pool is gone after rollback")
	}
}
