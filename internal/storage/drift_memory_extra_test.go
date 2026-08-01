package storage

import (
	"context"
	"errors"
	"testing"
	"time"

	"cloudpam/internal/domain"
)

var extraDriftBase = time.Date(2025, 5, 1, 8, 0, 0, 0, time.UTC)

func newDriftStoreForExtraTests() *MemoryDriftStore {
	return NewMemoryDriftStore(NewMemoryStore())
}

func mustCreateDriftExtra(t *testing.T, s *MemoryDriftStore, item domain.DriftItem) {
	t.Helper()
	if err := s.CreateDriftItem(context.Background(), item); err != nil {
		t.Fatalf("CreateDriftItem(%s): %v", item.ID, err)
	}
}

func TestMemoryDriftStoreExtra_CreateGetRoundTrip(t *testing.T) {
	ctx := context.Background()
	s := newDriftStoreForExtraTests()

	item := domain.DriftItem{
		ID:           "drift-1",
		AccountID:    1,
		PoolID:       tPtr(int64(10)),
		Type:         domain.DriftTypeUnmanaged,
		Severity:     domain.DriftSeverityWarning,
		Status:       domain.DriftStatusOpen,
		Title:        "Unmanaged VPC",
		Description:  "vpc-1 has no pool",
		ResourceCIDR: "10.0.0.0/16",
		DetectedAt:   extraDriftBase,
		UpdatedAt:    extraDriftBase,
	}
	mustCreateDriftExtra(t, s, item)

	got, err := s.GetDriftItem(ctx, "drift-1")
	if err != nil {
		t.Fatalf("GetDriftItem: %v", err)
	}
	if got.Title != "Unmanaged VPC" || got.ResourceCIDR != "10.0.0.0/16" {
		t.Errorf("GetDriftItem = %+v, want the stored item", got)
	}
	if got.PoolID == nil || *got.PoolID != 10 {
		t.Errorf("PoolID = %v, want 10", got.PoolID)
	}

	// Creating with the same ID replaces the row (upsert semantics).
	item.Title = "Replaced"
	mustCreateDriftExtra(t, s, item)
	got, err = s.GetDriftItem(ctx, "drift-1")
	if err != nil {
		t.Fatalf("GetDriftItem: %v", err)
	}
	if got.Title != "Replaced" {
		t.Errorf("Title = %q, want Replaced", got.Title)
	}
	_, total, err := s.ListDriftItems(ctx, domain.DriftFilters{})
	if err != nil {
		t.Fatalf("ListDriftItems: %v", err)
	}
	if total != 1 {
		t.Errorf("total = %d, want 1 (same ID must not duplicate)", total)
	}

	if _, err := s.GetDriftItem(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetDriftItem(missing) error = %v, want ErrNotFound", err)
	}
}

func TestMemoryDriftStoreExtra_ListFilters(t *testing.T) {
	ctx := context.Background()
	s := newDriftStoreForExtraTests()

	seed := []domain.DriftItem{
		{ID: "a", AccountID: 1, Type: domain.DriftTypeUnmanaged, Severity: domain.DriftSeverityCritical, Status: domain.DriftStatusOpen},
		{ID: "b", AccountID: 1, Type: domain.DriftTypeCIDRMismatch, Severity: domain.DriftSeverityWarning, Status: domain.DriftStatusIgnored},
		{ID: "c", AccountID: 2, Type: domain.DriftTypeOrphanedPool, Severity: domain.DriftSeverityInfo, Status: domain.DriftStatusResolved},
	}
	for i, item := range seed {
		item.DetectedAt = extraDriftBase.Add(time.Duration(i) * time.Minute)
		mustCreateDriftExtra(t, s, item)
	}

	tests := []struct {
		name    string
		filters domain.DriftFilters
		want    []string
	}{
		{"no filters", domain.DriftFilters{}, []string{"a", "b", "c"}},
		{"account", domain.DriftFilters{AccountID: 1}, []string{"a", "b"}},
		{"type", domain.DriftFilters{Type: "cidr_mismatch"}, []string{"b"}},
		{"severity", domain.DriftFilters{Severity: "info"}, []string{"c"}},
		{"status", domain.DriftFilters{Status: "open"}, []string{"a"}},
		{"account and severity combined", domain.DriftFilters{AccountID: 1, Severity: "critical"}, []string{"a"}},
		{"no match", domain.DriftFilters{AccountID: 99}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			items, total, err := s.ListDriftItems(ctx, tt.filters)
			if err != nil {
				t.Fatalf("ListDriftItems: %v", err)
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

func TestMemoryDriftStoreExtra_ListSortsNewestFirstAndPaginates(t *testing.T) {
	ctx := context.Background()
	s := newDriftStoreForExtraTests()

	for _, offset := range []int{2, 0, 3, 1} {
		mustCreateDriftExtra(t, s, domain.DriftItem{
			ID:         string(rune('a' + offset)),
			AccountID:  1,
			DetectedAt: extraDriftBase.Add(time.Duration(offset) * time.Hour),
		})
	}

	tests := []struct {
		name      string
		page      int
		pageSize  int
		wantLen   int
		wantFirst string
	}{
		{"defaults return everything newest first", 0, 0, 4, "d"},
		{"first page", 1, 3, 3, "d"},
		{"partial second page", 2, 3, 1, "a"},
		{"page past the end", 10, 3, 0, ""},
		{"negative page falls back to first", -2, 2, 2, "d"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			items, total, err := s.ListDriftItems(ctx, domain.DriftFilters{Page: tt.page, PageSize: tt.pageSize})
			if err != nil {
				t.Fatalf("ListDriftItems: %v", err)
			}
			if total != 4 {
				t.Errorf("total = %d, want 4", total)
			}
			if len(items) != tt.wantLen {
				t.Fatalf("len = %d, want %d", len(items), tt.wantLen)
			}
			if tt.wantLen > 0 && items[0].ID != tt.wantFirst {
				t.Errorf("first = %q, want %q", items[0].ID, tt.wantFirst)
			}
			for i := 1; i < len(items); i++ {
				if items[i-1].DetectedAt.Before(items[i].DetectedAt) {
					t.Fatalf("not sorted newest-first: %v then %v", items[i-1].DetectedAt, items[i].DetectedAt)
				}
			}
		})
	}
}

func TestMemoryDriftStoreExtra_UpdateDriftStatus(t *testing.T) {
	ctx := context.Background()
	s := newDriftStoreForExtraTests()

	if err := s.UpdateDriftStatus(ctx, "missing", domain.DriftStatusResolved, ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("UpdateDriftStatus(missing) error = %v, want ErrNotFound", err)
	}

	mustCreateDriftExtra(t, s, domain.DriftItem{ID: "d1", AccountID: 1, Status: domain.DriftStatusOpen, DetectedAt: extraDriftBase, UpdatedAt: extraDriftBase})

	if err := s.UpdateDriftStatus(ctx, "d1", domain.DriftStatusIgnored, "false positive"); err != nil {
		t.Fatalf("UpdateDriftStatus: %v", err)
	}
	got, err := s.GetDriftItem(ctx, "d1")
	if err != nil {
		t.Fatalf("GetDriftItem: %v", err)
	}
	if got.Status != domain.DriftStatusIgnored {
		t.Errorf("Status = %q, want ignored", got.Status)
	}
	if got.IgnoreReason != "false positive" {
		t.Errorf("IgnoreReason = %q, want false positive", got.IgnoreReason)
	}
	if !got.UpdatedAt.After(extraDriftBase) {
		t.Errorf("UpdatedAt = %v, want bumped past %v", got.UpdatedAt, extraDriftBase)
	}
	if got.ResolvedAt != nil {
		t.Errorf("ResolvedAt = %v, want nil for a non-resolved status", got.ResolvedAt)
	}

	if err := s.UpdateDriftStatus(ctx, "d1", domain.DriftStatusResolved, ""); err != nil {
		t.Fatalf("UpdateDriftStatus(resolved): %v", err)
	}
	got, err = s.GetDriftItem(ctx, "d1")
	if err != nil {
		t.Fatalf("GetDriftItem: %v", err)
	}
	if got.Status != domain.DriftStatusResolved {
		t.Errorf("Status = %q, want resolved", got.Status)
	}
	if got.ResolvedAt == nil {
		t.Fatal("ResolvedAt = nil, want set when the status becomes resolved")
	}
	if got.IgnoreReason != "" {
		t.Errorf("IgnoreReason = %q, want cleared", got.IgnoreReason)
	}
}

func TestMemoryDriftStoreExtra_UpdateDriftDetailsMerges(t *testing.T) {
	ctx := context.Background()
	s := newDriftStoreForExtraTests()

	if err := s.UpdateDriftDetails(ctx, "missing", map[string]string{"a": "1"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("UpdateDriftDetails(missing) error = %v, want ErrNotFound", err)
	}

	// Starting from a nil Details map the store must allocate one.
	mustCreateDriftExtra(t, s, domain.DriftItem{ID: "d1", AccountID: 1, UpdatedAt: extraDriftBase})
	if err := s.UpdateDriftDetails(ctx, "d1", map[string]string{"a": "1", "b": "2"}); err != nil {
		t.Fatalf("UpdateDriftDetails: %v", err)
	}
	got, err := s.GetDriftItem(ctx, "d1")
	if err != nil {
		t.Fatalf("GetDriftItem: %v", err)
	}
	if got.Details["a"] != "1" || got.Details["b"] != "2" {
		t.Fatalf("Details = %v, want {a:1 b:2}", got.Details)
	}
	if !got.UpdatedAt.After(extraDriftBase) {
		t.Errorf("UpdatedAt = %v, want bumped", got.UpdatedAt)
	}

	// A second call merges rather than replaces.
	if err := s.UpdateDriftDetails(ctx, "d1", map[string]string{"b": "changed", "c": "3"}); err != nil {
		t.Fatalf("UpdateDriftDetails: %v", err)
	}
	got, err = s.GetDriftItem(ctx, "d1")
	if err != nil {
		t.Fatalf("GetDriftItem: %v", err)
	}
	want := map[string]string{"a": "1", "b": "changed", "c": "3"}
	if len(got.Details) != len(want) {
		t.Fatalf("Details = %v, want %v", got.Details, want)
	}
	for k, v := range want {
		if got.Details[k] != v {
			t.Errorf("Details[%s] = %q, want %q", k, got.Details[k], v)
		}
	}

	// An empty merge is a no-op for the payload.
	if err := s.UpdateDriftDetails(ctx, "d1", nil); err != nil {
		t.Fatalf("UpdateDriftDetails(nil): %v", err)
	}
	got, err = s.GetDriftItem(ctx, "d1")
	if err != nil {
		t.Fatalf("GetDriftItem: %v", err)
	}
	if len(got.Details) != len(want) {
		t.Errorf("Details = %v, want unchanged %v", got.Details, want)
	}
}

func TestMemoryDriftStoreExtra_DeleteOpenForAccount(t *testing.T) {
	ctx := context.Background()
	s := newDriftStoreForExtraTests()

	seed := []domain.DriftItem{
		{ID: "open-1", AccountID: 1, Status: domain.DriftStatusOpen},
		{ID: "open-2", AccountID: 1, Status: domain.DriftStatusOpen},
		{ID: "ignored", AccountID: 1, Status: domain.DriftStatusIgnored},
		{ID: "resolved", AccountID: 1, Status: domain.DriftStatusResolved},
		{ID: "other-account-open", AccountID: 2, Status: domain.DriftStatusOpen},
	}
	for _, item := range seed {
		mustCreateDriftExtra(t, s, item)
	}

	if err := s.DeleteOpenForAccount(ctx, 1); err != nil {
		t.Fatalf("DeleteOpenForAccount: %v", err)
	}

	survivors := map[string]bool{"ignored": true, "resolved": true, "other-account-open": true}
	items, total, err := s.ListDriftItems(ctx, domain.DriftFilters{})
	if err != nil {
		t.Fatalf("ListDriftItems: %v", err)
	}
	if total != len(survivors) {
		t.Fatalf("total = %d, want %d", total, len(survivors))
	}
	for _, item := range items {
		if !survivors[item.ID] {
			t.Errorf("unexpected survivor %q", item.ID)
		}
	}
	if _, err := s.GetDriftItem(ctx, "open-1"); !errors.Is(err, ErrNotFound) {
		t.Errorf("open-1 still present: %v", err)
	}

	// Deleting for an account with no open items is a no-op.
	if err := s.DeleteOpenForAccount(ctx, 1); err != nil {
		t.Fatalf("second DeleteOpenForAccount: %v", err)
	}
	if _, total, err = s.ListDriftItems(ctx, domain.DriftFilters{}); err != nil || total != len(survivors) {
		t.Errorf("total = %d (err %v), want %d", total, err, len(survivors))
	}
}
