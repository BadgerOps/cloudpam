//go:build sqlite

package sqlite

import (
	"math"
	"testing"

	"cloudpam/internal/domain"
)

func TestSearch_PageNearMaxIntIsEmpty(t *testing.T) {
	s := newTestStore(t)
	ctx := timeoutCtx(t)
	if _, err := s.CreatePool(ctx, domain.CreatePool{Name: "prod", CIDR: "10.0.0.0/8"}); err != nil {
		t.Fatalf("create pool: %v", err)
	}

	resp, err := s.Search(ctx, domain.SearchRequest{Query: "prod", Page: math.MaxInt, PageSize: 4})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if resp.Total != 1 {
		t.Errorf("Total = %d, want 1", resp.Total)
	}
	if len(resp.Items) != 0 {
		t.Errorf("len(Items) = %d, want 0", len(resp.Items))
	}
	if resp.Page != math.MaxInt || resp.PageSize != 4 {
		t.Errorf("Page/PageSize = %d/%d, want %d/4", resp.Page, resp.PageSize, math.MaxInt)
	}
}
