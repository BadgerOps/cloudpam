package storage

import (
	"context"
	"errors"
	"testing"
	"time"

	"cloudpam/internal/domain"
)

var extraRecBase = time.Date(2025, 6, 1, 9, 0, 0, 0, time.UTC)

func newRecStoreForExtraTests() *MemoryRecommendationStore {
	return NewMemoryRecommendationStore(NewMemoryStore())
}

func mustCreateRecExtra(t *testing.T, s *MemoryRecommendationStore, rec domain.Recommendation) {
	t.Helper()
	if err := s.CreateRecommendation(context.Background(), rec); err != nil {
		t.Fatalf("CreateRecommendation(%s): %v", rec.ID, err)
	}
}

func TestMemoryRecommendationStoreExtra_CreateGetRoundTrip(t *testing.T) {
	ctx := context.Background()
	s := newRecStoreForExtraTests()

	rec := domain.Recommendation{
		ID:            "rec-1",
		PoolID:        3,
		Type:          domain.RecommendationTypeAllocation,
		Status:        domain.RecommendationStatusPending,
		Priority:      domain.RecommendationPriorityHigh,
		Title:         "Allocate /24",
		Description:   "Pool 3 is nearly full",
		SuggestedCIDR: "10.0.5.0/24",
		RuleID:        "alloc-1",
		Score:         88,
		CreatedAt:     extraRecBase,
		UpdatedAt:     extraRecBase,
	}
	mustCreateRecExtra(t, s, rec)

	got, err := s.GetRecommendation(ctx, "rec-1")
	if err != nil {
		t.Fatalf("GetRecommendation: %v", err)
	}
	if got.Title != "Allocate /24" || got.SuggestedCIDR != "10.0.5.0/24" || got.Score != 88 {
		t.Errorf("GetRecommendation = %+v, want the stored recommendation", got)
	}

	// Same ID replaces rather than duplicates.
	rec.Title = "Replaced"
	mustCreateRecExtra(t, s, rec)
	if _, total, err := s.ListRecommendations(ctx, domain.RecommendationFilters{}); err != nil || total != 1 {
		t.Errorf("total = %d (err %v), want 1", total, err)
	}
	got, err = s.GetRecommendation(ctx, "rec-1")
	if err != nil {
		t.Fatalf("GetRecommendation: %v", err)
	}
	if got.Title != "Replaced" {
		t.Errorf("Title = %q, want Replaced", got.Title)
	}

	if _, err := s.GetRecommendation(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetRecommendation(missing) error = %v, want ErrNotFound", err)
	}
}

func TestMemoryRecommendationStoreExtra_ListFilters(t *testing.T) {
	ctx := context.Background()
	s := newRecStoreForExtraTests()

	seed := []domain.Recommendation{
		{ID: "a", PoolID: 1, Type: domain.RecommendationTypeAllocation, Status: domain.RecommendationStatusPending, Priority: domain.RecommendationPriorityHigh},
		{ID: "b", PoolID: 1, Type: domain.RecommendationTypeCompliance, Status: domain.RecommendationStatusApplied, Priority: domain.RecommendationPriorityLow},
		{ID: "c", PoolID: 2, Type: domain.RecommendationTypeAllocation, Status: domain.RecommendationStatusDismissed, Priority: domain.RecommendationPriorityMedium},
	}
	for i, rec := range seed {
		rec.CreatedAt = extraRecBase.Add(time.Duration(i) * time.Minute)
		mustCreateRecExtra(t, s, rec)
	}

	tests := []struct {
		name    string
		filters domain.RecommendationFilters
		want    []string
	}{
		{"no filters", domain.RecommendationFilters{}, []string{"a", "b", "c"}},
		{"pool", domain.RecommendationFilters{PoolID: 1}, []string{"a", "b"}},
		{"type", domain.RecommendationFilters{Type: "compliance"}, []string{"b"}},
		{"status", domain.RecommendationFilters{Status: "dismissed"}, []string{"c"}},
		{"priority", domain.RecommendationFilters{Priority: "medium"}, []string{"c"}},
		{"pool and type combined", domain.RecommendationFilters{PoolID: 1, Type: "allocation"}, []string{"a"}},
		{"no match", domain.RecommendationFilters{PoolID: 99}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			items, total, err := s.ListRecommendations(ctx, tt.filters)
			if err != nil {
				t.Fatalf("ListRecommendations: %v", err)
			}
			if total != len(tt.want) || len(items) != len(tt.want) {
				t.Fatalf("total=%d len=%d, want %d", total, len(items), len(tt.want))
			}
			got := make(map[string]bool, len(items))
			for _, item := range items {
				got[item.ID] = true
			}
			for _, want := range tt.want {
				if !got[want] {
					t.Errorf("missing %q in %v", want, got)
				}
			}
		})
	}
}

func TestMemoryRecommendationStoreExtra_ListSortsNewestFirstAndPaginates(t *testing.T) {
	ctx := context.Background()
	s := newRecStoreForExtraTests()

	for _, offset := range []int{1, 3, 0, 2, 4} {
		mustCreateRecExtra(t, s, domain.Recommendation{
			ID:        string(rune('a' + offset)),
			PoolID:    1,
			CreatedAt: extraRecBase.Add(time.Duration(offset) * time.Hour),
		})
	}

	tests := []struct {
		name      string
		page      int
		pageSize  int
		wantLen   int
		wantFirst string
	}{
		{"defaults", 0, 0, 5, "e"},
		{"first page of two", 1, 2, 2, "e"},
		{"second page of two", 2, 2, 2, "c"},
		{"partial third page", 3, 2, 1, "a"},
		{"page past the end", 20, 2, 0, ""},
		{"page size larger than the result set", 1, 500, 5, "e"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			items, total, err := s.ListRecommendations(ctx, domain.RecommendationFilters{Page: tt.page, PageSize: tt.pageSize})
			if err != nil {
				t.Fatalf("ListRecommendations: %v", err)
			}
			if total != 5 {
				t.Errorf("total = %d, want 5", total)
			}
			if len(items) != tt.wantLen {
				t.Fatalf("len = %d, want %d", len(items), tt.wantLen)
			}
			if tt.wantLen > 0 && items[0].ID != tt.wantFirst {
				t.Errorf("first = %q, want %q", items[0].ID, tt.wantFirst)
			}
			for i := 1; i < len(items); i++ {
				if items[i-1].CreatedAt.Before(items[i].CreatedAt) {
					t.Fatalf("not sorted newest-first: %v then %v", items[i-1].CreatedAt, items[i].CreatedAt)
				}
			}
		})
	}
}

func TestMemoryRecommendationStoreExtra_UpdateStatus(t *testing.T) {
	ctx := context.Background()
	s := newRecStoreForExtraTests()

	if err := s.UpdateRecommendationStatus(ctx, "missing", domain.RecommendationStatusApplied, "", nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("UpdateRecommendationStatus(missing) error = %v, want ErrNotFound", err)
	}

	mustCreateRecExtra(t, s, domain.Recommendation{
		ID: "rec-1", PoolID: 1, Status: domain.RecommendationStatusPending,
		CreatedAt: extraRecBase, UpdatedAt: extraRecBase,
	})

	if err := s.UpdateRecommendationStatus(ctx, "rec-1", domain.RecommendationStatusApplied, "", tPtr(int64(42))); err != nil {
		t.Fatalf("UpdateRecommendationStatus: %v", err)
	}
	got, err := s.GetRecommendation(ctx, "rec-1")
	if err != nil {
		t.Fatalf("GetRecommendation: %v", err)
	}
	if got.Status != domain.RecommendationStatusApplied {
		t.Errorf("Status = %q, want applied", got.Status)
	}
	if got.AppliedPoolID == nil || *got.AppliedPoolID != 42 {
		t.Errorf("AppliedPoolID = %v, want 42", got.AppliedPoolID)
	}
	if !got.UpdatedAt.After(extraRecBase) {
		t.Errorf("UpdatedAt = %v, want bumped past %v", got.UpdatedAt, extraRecBase)
	}
	if !got.CreatedAt.Equal(extraRecBase) {
		t.Errorf("CreatedAt = %v, want preserved %v", got.CreatedAt, extraRecBase)
	}

	// Dismissing clears the applied pool and records the reason.
	if err := s.UpdateRecommendationStatus(ctx, "rec-1", domain.RecommendationStatusDismissed, "not needed", nil); err != nil {
		t.Fatalf("UpdateRecommendationStatus(dismiss): %v", err)
	}
	got, err = s.GetRecommendation(ctx, "rec-1")
	if err != nil {
		t.Fatalf("GetRecommendation: %v", err)
	}
	if got.Status != domain.RecommendationStatusDismissed || got.DismissReason != "not needed" {
		t.Errorf("recommendation = %+v, want dismissed with a reason", got)
	}
	if got.AppliedPoolID != nil {
		t.Errorf("AppliedPoolID = %v, want cleared", *got.AppliedPoolID)
	}
}

func TestMemoryRecommendationStoreExtra_DeletePendingForPool(t *testing.T) {
	ctx := context.Background()
	s := newRecStoreForExtraTests()

	seed := []domain.Recommendation{
		{ID: "p1-pending-a", PoolID: 1, Status: domain.RecommendationStatusPending},
		{ID: "p1-pending-b", PoolID: 1, Status: domain.RecommendationStatusPending},
		{ID: "p1-applied", PoolID: 1, Status: domain.RecommendationStatusApplied},
		{ID: "p1-dismissed", PoolID: 1, Status: domain.RecommendationStatusDismissed},
		{ID: "p2-pending", PoolID: 2, Status: domain.RecommendationStatusPending},
	}
	for _, rec := range seed {
		mustCreateRecExtra(t, s, rec)
	}

	if err := s.DeletePendingForPool(ctx, 1); err != nil {
		t.Fatalf("DeletePendingForPool: %v", err)
	}

	survivors := map[string]bool{"p1-applied": true, "p1-dismissed": true, "p2-pending": true}
	items, total, err := s.ListRecommendations(ctx, domain.RecommendationFilters{})
	if err != nil {
		t.Fatalf("ListRecommendations: %v", err)
	}
	if total != len(survivors) {
		t.Fatalf("total = %d, want %d", total, len(survivors))
	}
	for _, item := range items {
		if !survivors[item.ID] {
			t.Errorf("unexpected survivor %q", item.ID)
		}
	}
	if _, err := s.GetRecommendation(ctx, "p1-pending-a"); !errors.Is(err, ErrNotFound) {
		t.Errorf("p1-pending-a still present: %v", err)
	}

	// A pool with nothing pending is unaffected.
	if err := s.DeletePendingForPool(ctx, 1); err != nil {
		t.Fatalf("second DeletePendingForPool: %v", err)
	}
	if _, total, err = s.ListRecommendations(ctx, domain.RecommendationFilters{}); err != nil || total != len(survivors) {
		t.Errorf("total = %d (err %v), want %d", total, err, len(survivors))
	}
}
