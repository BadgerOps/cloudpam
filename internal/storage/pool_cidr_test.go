package storage

import (
	"context"
	"testing"

	"cloudpam/internal/domain"
)

func TestMemoryStore_UpdatePoolCIDR(t *testing.T) {
	ctx := context.Background()
	m := NewMemoryStore()
	created, err := m.CreatePool(ctx, domain.CreatePool{Name: "wide", CIDR: "10.0.0.0/16"})
	if err != nil {
		t.Fatal(err)
	}

	updated, found, err := m.UpdatePoolCIDR(ctx, created.ID, "10.0.0.0/23")
	if err != nil || !found {
		t.Fatalf("UpdatePoolCIDR: found=%v err=%v", found, err)
	}
	if updated.CIDR != "10.0.0.0/23" {
		t.Errorf("returned cidr %q, want 10.0.0.0/23", updated.CIDR)
	}
	got, _, _ := m.GetPool(ctx, created.ID)
	if got.CIDR != "10.0.0.0/23" {
		t.Errorf("stored cidr %q, want 10.0.0.0/23", got.CIDR)
	}
	stats, err := m.CalculatePoolUtilization(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stats.TotalIPs != 512 {
		t.Errorf("total ips after shrink %d, want 512", stats.TotalIPs)
	}

	if _, found, err := m.UpdatePoolCIDR(ctx, 999, "10.1.0.0/24"); err != nil || found {
		t.Errorf("unknown pool: found=%v err=%v, want found=false", found, err)
	}
	if _, err := m.DeletePool(ctx, created.ID); err != nil {
		t.Fatal(err)
	}
	if _, found, err := m.UpdatePoolCIDR(ctx, created.ID, "10.1.0.0/24"); err != nil || found {
		t.Errorf("deleted pool: found=%v err=%v, want found=false", found, err)
	}
}
