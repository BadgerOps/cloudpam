package audit

import (
	"reflect"
	"testing"
	"time"
)

func TestCovQueryOptionsValidateAppliesDefaults(t *testing.T) {
	tests := []struct {
		name          string
		in            QueryOptions
		wantLimit     int
		wantOffset    int
		wantOrderBy   string
		wantOrderDesc bool
	}{
		{
			name:          "zero value gets defaults",
			in:            QueryOptions{},
			wantLimit:     50,
			wantOffset:    0,
			wantOrderBy:   "timestamp",
			wantOrderDesc: true,
		},
		{
			name:          "negative limit gets default",
			in:            QueryOptions{Limit: -10},
			wantLimit:     50,
			wantOffset:    0,
			wantOrderBy:   "timestamp",
			wantOrderDesc: true,
		},
		{
			name:          "excessive limit is capped",
			in:            QueryOptions{Limit: 999999, OrderBy: "action"},
			wantLimit:     10000,
			wantOffset:    0,
			wantOrderBy:   "action",
			wantOrderDesc: false,
		},
		{
			name:          "limit at the cap is preserved",
			in:            QueryOptions{Limit: 10000, OrderBy: "action"},
			wantLimit:     10000,
			wantOrderBy:   "action",
			wantOrderDesc: false,
		},
		{
			name:          "negative offset is clamped",
			in:            QueryOptions{Limit: 25, Offset: -3, OrderBy: "actor_id", OrderDesc: true},
			wantLimit:     25,
			wantOffset:    0,
			wantOrderBy:   "actor_id",
			wantOrderDesc: true,
		},
		{
			name:          "explicit values are preserved",
			in:            QueryOptions{Limit: 100, Offset: 200, OrderBy: "resource_type", OrderDesc: false},
			wantLimit:     100,
			wantOffset:    200,
			wantOrderBy:   "resource_type",
			wantOrderDesc: false,
		},
		{
			name: "empty order by forces newest first even when desc was false",
			in:   QueryOptions{Limit: 5, OrderDesc: false},

			wantLimit:     5,
			wantOrderBy:   "timestamp",
			wantOrderDesc: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			opts := tc.in
			if err := opts.Validate(); err != nil {
				t.Fatalf("Validate() error = %v", err)
			}
			if opts.Limit != tc.wantLimit {
				t.Errorf("Limit = %d, want %d", opts.Limit, tc.wantLimit)
			}
			if opts.Offset != tc.wantOffset {
				t.Errorf("Offset = %d, want %d", opts.Offset, tc.wantOffset)
			}
			if opts.OrderBy != tc.wantOrderBy {
				t.Errorf("OrderBy = %q, want %q", opts.OrderBy, tc.wantOrderBy)
			}
			if opts.OrderDesc != tc.wantOrderDesc {
				t.Errorf("OrderDesc = %v, want %v", opts.OrderDesc, tc.wantOrderDesc)
			}
		})
	}
}

func TestCovDefaultQueryOptions(t *testing.T) {
	opts := DefaultQueryOptions()
	if opts.Limit != 50 {
		t.Errorf("Limit = %d, want 50", opts.Limit)
	}
	if opts.Offset != 0 {
		t.Errorf("Offset = %d, want 0", opts.Offset)
	}
	if opts.OrderBy != "timestamp" {
		t.Errorf("OrderBy = %q, want timestamp", opts.OrderBy)
	}
	if !opts.OrderDesc {
		t.Error("OrderDesc = false, want true (newest first)")
	}

	// Defaults must already satisfy Validate without being altered.
	validated := opts
	if err := validated.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if !reflect.DeepEqual(validated, opts) {
		t.Fatalf("Validate() changed the defaults: %+v -> %+v", opts, validated)
	}
}

func TestCovQueryOptionsBuildersReturnCopies(t *testing.T) {
	base := DefaultQueryOptions()
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC)

	ranged := base.WithTimeRange(start, end)
	if ranged.StartTime == nil || !ranged.StartTime.Equal(start) {
		t.Errorf("StartTime = %v, want %v", ranged.StartTime, start)
	}
	if ranged.EndTime == nil || !ranged.EndTime.Equal(end) {
		t.Errorf("EndTime = %v, want %v", ranged.EndTime, end)
	}
	if base.StartTime != nil || base.EndTime != nil {
		t.Error("WithTimeRange mutated the receiver instead of returning a copy")
	}

	actor := base.WithActor("admin")
	if actor.ActorID != "admin" {
		t.Errorf("ActorID = %q, want admin", actor.ActorID)
	}
	if base.ActorID != "" {
		t.Error("WithActor mutated the receiver")
	}

	action := base.WithAction(ActionDelete)
	if action.Action != ActionDelete {
		t.Errorf("Action = %q, want %q", action.Action, ActionDelete)
	}
	if base.Action != "" {
		t.Error("WithAction mutated the receiver")
	}

	resource := base.WithResource(ResourcePool, "pool-9")
	if resource.ResourceType != ResourcePool || resource.ResourceID != "pool-9" {
		t.Errorf("resource filters = %q/%q", resource.ResourceType, resource.ResourceID)
	}
	if base.ResourceType != "" || base.ResourceID != "" {
		t.Error("WithResource mutated the receiver")
	}

	paged := base.WithPagination(10, 30)
	if paged.Limit != 10 || paged.Offset != 30 {
		t.Errorf("pagination = %d/%d, want 10/30", paged.Limit, paged.Offset)
	}
	if base.Limit != 50 || base.Offset != 0 {
		t.Error("WithPagination mutated the receiver")
	}

	ordered := base.WithOrdering("action", false)
	if ordered.OrderBy != "action" || ordered.OrderDesc {
		t.Errorf("ordering = %q/%v, want action/false", ordered.OrderBy, ordered.OrderDesc)
	}
	if base.OrderBy != "timestamp" || !base.OrderDesc {
		t.Error("WithOrdering mutated the receiver")
	}
}

func TestCovQueryOptionsBuildersChain(t *testing.T) {
	start := time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)

	opts := DefaultQueryOptions().
		WithTimeRange(start, end).
		WithActor("svc-1").
		WithAction(ActionUpdate).
		WithResource(ResourceAccount, "acct-7").
		WithPagination(200, 400).
		WithOrdering("actor_id", false)

	if err := opts.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if opts.StartTime == nil || !opts.StartTime.Equal(start) || opts.EndTime == nil || !opts.EndTime.Equal(end) {
		t.Errorf("time range = %v..%v", opts.StartTime, opts.EndTime)
	}
	if opts.ActorID != "svc-1" || opts.Action != ActionUpdate {
		t.Errorf("actor/action = %q/%q", opts.ActorID, opts.Action)
	}
	if opts.ResourceType != ResourceAccount || opts.ResourceID != "acct-7" {
		t.Errorf("resource = %q/%q", opts.ResourceType, opts.ResourceID)
	}
	if opts.Limit != 200 || opts.Offset != 400 {
		t.Errorf("pagination = %d/%d", opts.Limit, opts.Offset)
	}
	if opts.OrderBy != "actor_id" || opts.OrderDesc {
		t.Errorf("ordering = %q/%v, want actor_id/false to survive Validate", opts.OrderBy, opts.OrderDesc)
	}
}

func TestCovDefaultRetentionPolicy(t *testing.T) {
	policy := DefaultRetentionPolicy()
	const ninetyDays = 90 * 24 * time.Hour

	if policy.MaxAge != ninetyDays {
		t.Errorf("MaxAge = %v, want 90 days", policy.MaxAge)
	}
	if policy.SuccessfulMaxAge != ninetyDays {
		t.Errorf("SuccessfulMaxAge = %v, want 90 days", policy.SuccessfulMaxAge)
	}
	if policy.MaxEvents != 10000000 {
		t.Errorf("MaxEvents = %d, want 10000000", policy.MaxEvents)
	}
	if !policy.RetainSuccessful {
		t.Error("RetainSuccessful = false, want true")
	}
}
