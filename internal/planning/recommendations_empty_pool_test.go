package planning

import (
	"context"
	"net/netip"
	"sort"
	"testing"

	"cloudpam/internal/domain"
)

func allocationCIDRs(items []domain.Recommendation) []string {
	var out []string
	for _, r := range items {
		if r.Type == domain.RecommendationTypeAllocation {
			out = append(out, r.SuggestedCIDR)
		}
	}
	sort.Strings(out)
	return out
}

// An empty pool has its whole range free. Recommending that range as an
// allocation produces a child with the parent's own CIDR, which apply refuses
// (child prefix must be longer than the parent). The generator must offer the
// two halves instead.
func TestGenerate_EmptyPoolSuggestsHalvesNotOwnCIDR(t *testing.T) {
	ctx := context.Background()
	svc, st := setupRecService(t)

	pool, err := st.CreatePool(ctx, domain.CreatePool{
		Name: "Empty", CIDR: "10.0.0.0/24", Type: domain.PoolTypeSupernet, Description: "empty",
	})
	if err != nil {
		t.Fatalf("CreatePool() error = %v", err)
	}

	resp, err := svc.Generate(ctx, domain.GenerateRecommendationsRequest{PoolIDs: []int64{pool.ID}})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	got := allocationCIDRs(resp.Items)
	want := []string{"10.0.0.0/25", "10.0.0.128/25"}
	if len(got) != len(want) {
		t.Fatalf("allocation CIDRs = %v, want %v", got, want)
	}
	parent := netip.MustParsePrefix(pool.CIDR)
	for i, c := range got {
		if c != want[i] {
			t.Errorf("allocation CIDRs = %v, want %v", got, want)
		}
		if c == pool.CIDR {
			t.Errorf("recommendation suggests the pool's own CIDR %s", c)
		}
		p := netip.MustParsePrefix(c)
		if !parent.Contains(p.Addr()) || p.Bits() <= parent.Bits() {
			t.Errorf("recommendation %s is not strictly inside %s", c, pool.CIDR)
		}
	}

	// Each half must be applicable: apply creates the child under the pool.
	for _, r := range resp.Items {
		if r.Type != domain.RecommendationTypeAllocation {
			continue
		}
		applied, err := svc.Apply(ctx, r.ID, domain.ApplyRecommendationRequest{})
		if err != nil {
			t.Fatalf("Apply(%s) error = %v", r.SuggestedCIDR, err)
		}
		child, found, err := st.GetPool(ctx, *applied.AppliedPoolID)
		if err != nil || !found {
			t.Fatalf("applied pool not found: found=%v err=%v", found, err)
		}
		if child.ParentID == nil || *child.ParentID != pool.ID {
			t.Errorf("applied pool parent = %v, want %d", child.ParentID, pool.ID)
		}
	}
}

// A pool that cannot be split within the validation prefix bounds gets no
// allocation recommendation at all rather than one that apply would refuse.
func TestGenerate_UnsplittablePoolHasNoAllocation(t *testing.T) {
	ctx := context.Background()
	svc, st := setupRecService(t)

	pool, err := st.CreatePool(ctx, domain.CreatePool{
		Name: "Tiny", CIDR: "10.0.0.0/30", Type: domain.PoolTypeSubnet, Description: "tiny",
	})
	if err != nil {
		t.Fatalf("CreatePool() error = %v", err)
	}

	resp, err := svc.Generate(ctx, domain.GenerateRecommendationsRequest{PoolIDs: []int64{pool.ID}})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if got := allocationCIDRs(resp.Items); len(got) != 0 {
		t.Errorf("allocation CIDRs = %v, want none for an unsplittable /30", got)
	}
}

// Partial gaps are already strictly inside the pool and must be recommended
// as before, not split further.
func TestGenerate_PartialGapIsRecommendedWhole(t *testing.T) {
	ctx := context.Background()
	svc, st := setupRecService(t)

	parent, err := st.CreatePool(ctx, domain.CreatePool{
		Name: "Corp", CIDR: "10.0.0.0/24", Type: domain.PoolTypeSupernet, Description: "corp",
	})
	if err != nil {
		t.Fatalf("CreatePool() error = %v", err)
	}
	parentID := parent.ID
	if _, err := st.CreatePool(ctx, domain.CreatePool{
		Name: "Used", CIDR: "10.0.0.0/25", ParentID: &parentID, Type: domain.PoolTypeSubnet, Description: "used",
	}); err != nil {
		t.Fatalf("CreatePool() error = %v", err)
	}

	resp, err := svc.Generate(ctx, domain.GenerateRecommendationsRequest{PoolIDs: []int64{parent.ID}})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	got := allocationCIDRs(resp.Items)
	if len(got) != 1 || got[0] != "10.0.0.128/25" {
		t.Errorf("allocation CIDRs = %v, want [10.0.0.128/25]", got)
	}
}
