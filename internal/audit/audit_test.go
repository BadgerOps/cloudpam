package audit

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestMemoryAuditLogger_Log(t *testing.T) {
	logger := NewMemoryAuditLogger()
	ctx := context.Background()

	event := &AuditEvent{
		Actor:        "cpam_abc123",
		ActorType:    ActorTypeAPIKey,
		Action:       ActionCreate,
		ResourceType: ResourcePool,
		ResourceID:   "1",
		ResourceName: "test-pool",
		StatusCode:   201,
	}

	err := logger.Log(ctx, event)
	if err != nil {
		t.Fatalf("Log() error = %v", err)
	}

	// Verify event was stored
	events, total, err := logger.List(ctx, ListOptions{Limit: 10})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if total != 1 {
		t.Fatalf("expected total 1, got %d", total)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	// Verify ID and timestamp were assigned
	if events[0].ID == "" {
		t.Error("expected ID to be assigned")
	}
	if events[0].Timestamp.IsZero() {
		t.Error("expected Timestamp to be assigned")
	}
	if events[0].Actor != "cpam_abc123" {
		t.Errorf("expected Actor 'cpam_abc123', got %q", events[0].Actor)
	}
}

func TestMemoryAuditLogger_Log_NilEvent(t *testing.T) {
	logger := NewMemoryAuditLogger()
	ctx := context.Background()

	err := logger.Log(ctx, nil)
	if err != nil {
		t.Fatalf("Log(nil) should not error, got %v", err)
	}
}

func TestMemoryAuditLogger_Log_WithChanges(t *testing.T) {
	logger := NewMemoryAuditLogger()
	ctx := context.Background()

	event := &AuditEvent{
		Actor:        "cpam_abc123",
		ActorType:    ActorTypeAPIKey,
		Action:       ActionUpdate,
		ResourceType: ResourcePool,
		ResourceID:   "1",
		Changes: &Changes{
			Before: map[string]any{"name": "old-name"},
			After:  map[string]any{"name": "new-name"},
		},
		StatusCode: 200,
	}

	err := logger.Log(ctx, event)
	if err != nil {
		t.Fatalf("Log() error = %v", err)
	}

	events, _, err := logger.List(ctx, ListOptions{Limit: 10})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}

	if events[0].Changes == nil {
		t.Fatal("expected Changes to be set")
	}
	if events[0].Changes.Before["name"] != "old-name" {
		t.Errorf("expected Before name 'old-name', got %v", events[0].Changes.Before["name"])
	}
	if events[0].Changes.After["name"] != "new-name" {
		t.Errorf("expected After name 'new-name', got %v", events[0].Changes.After["name"])
	}
}

func TestMemoryAuditLogger_Log_MaxEvents(t *testing.T) {
	logger := NewMemoryAuditLogger(WithMaxEvents(5))
	ctx := context.Background()

	// Log 10 events
	for i := 0; i < 10; i++ {
		event := &AuditEvent{
			Actor:        "cpam_test",
			ActorType:    ActorTypeAPIKey,
			Action:       ActionCreate,
			ResourceType: ResourcePool,
			ResourceID:   string(rune('0' + i)),
			StatusCode:   201,
		}
		if err := logger.Log(ctx, event); err != nil {
			t.Fatalf("Log() error = %v", err)
		}
	}

	// Should only have 5 events (newest)
	events, total, err := logger.List(ctx, ListOptions{Limit: 100})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if total != 5 {
		t.Fatalf("expected total 5, got %d", total)
	}
	if len(events) != 5 {
		t.Fatalf("expected 5 events, got %d", len(events))
	}
}

func TestMemoryAuditLogger_Log_NewestFirst(t *testing.T) {
	logger := NewMemoryAuditLogger()
	ctx := context.Background()

	// Log events with different resource IDs
	for i := 1; i <= 3; i++ {
		event := &AuditEvent{
			Actor:        "cpam_test",
			ActorType:    ActorTypeAPIKey,
			Action:       ActionCreate,
			ResourceType: ResourcePool,
			ResourceID:   string(rune('0' + i)),
			StatusCode:   201,
		}
		if err := logger.Log(ctx, event); err != nil {
			t.Fatalf("Log() error = %v", err)
		}
		// Small delay to ensure different timestamps
		time.Sleep(time.Millisecond)
	}

	events, _, err := logger.List(ctx, ListOptions{Limit: 10})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}

	// Newest (ResourceID "3") should be first
	if events[0].ResourceID != "3" {
		t.Errorf("expected newest event first (ResourceID '3'), got %q", events[0].ResourceID)
	}
	if events[2].ResourceID != "1" {
		t.Errorf("expected oldest event last (ResourceID '1'), got %q", events[2].ResourceID)
	}
}

func TestMemoryAuditLogger_List_Filtering(t *testing.T) {
	logger := NewMemoryAuditLogger()
	ctx := context.Background()

	// Create diverse events
	events := []*AuditEvent{
		{Actor: "cpam_user1", ActorType: ActorTypeAPIKey, Action: ActionCreate, ResourceType: ResourcePool, ResourceID: "1", StatusCode: 201},
		{Actor: "cpam_user2", ActorType: ActorTypeAPIKey, Action: ActionUpdate, ResourceType: ResourcePool, ResourceID: "1", StatusCode: 200},
		{Actor: "cpam_user1", ActorType: ActorTypeAPIKey, Action: ActionDelete, ResourceType: ResourceAccount, ResourceID: "2", StatusCode: 204},
		{Actor: "anonymous", ActorType: ActorTypeAnonymous, Action: ActionCreate, ResourceType: ResourcePool, ResourceID: "3", StatusCode: 201},
	}
	for _, e := range events {
		if err := logger.Log(ctx, e); err != nil {
			t.Fatalf("Log() error = %v", err)
		}
	}

	tests := []struct {
		name     string
		opts     ListOptions
		expected int
	}{
		{
			name:     "filter by actor",
			opts:     ListOptions{Actor: "cpam_user1", Limit: 100},
			expected: 2,
		},
		{
			name:     "filter by action",
			opts:     ListOptions{Action: ActionCreate, Limit: 100},
			expected: 2,
		},
		{
			name:     "filter by resource type",
			opts:     ListOptions{ResourceType: ResourcePool, Limit: 100},
			expected: 3,
		},
		{
			name:     "filter by multiple criteria",
			opts:     ListOptions{Actor: "cpam_user1", Action: ActionCreate, Limit: 100},
			expected: 1,
		},
		{
			name:     "no matches",
			opts:     ListOptions{Actor: "nonexistent", Limit: 100},
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, total, err := logger.List(ctx, tt.opts)
			if err != nil {
				t.Fatalf("List() error = %v", err)
			}
			if total != tt.expected {
				t.Errorf("expected total %d, got %d", tt.expected, total)
			}
			if len(result) != tt.expected {
				t.Errorf("expected %d events, got %d", tt.expected, len(result))
			}
		})
	}
}

func TestMemoryAuditLogger_List_TimeFiltering(t *testing.T) {
	logger := NewMemoryAuditLogger()
	ctx := context.Background()

	now := time.Now().UTC()
	past := now.Add(-time.Hour)
	future := now.Add(time.Hour)

	// Create event with known timestamp
	event := &AuditEvent{
		ID:           "test-id",
		Timestamp:    now,
		Actor:        "cpam_test",
		ActorType:    ActorTypeAPIKey,
		Action:       ActionCreate,
		ResourceType: ResourcePool,
		ResourceID:   "1",
		StatusCode:   201,
	}
	if err := logger.Log(ctx, event); err != nil {
		t.Fatalf("Log() error = %v", err)
	}

	tests := []struct {
		name     string
		opts     ListOptions
		expected int
	}{
		{
			name:     "since in past",
			opts:     ListOptions{Since: &past, Limit: 100},
			expected: 1,
		},
		{
			name:     "since in future",
			opts:     ListOptions{Since: &future, Limit: 100},
			expected: 0,
		},
		{
			name:     "until in future",
			opts:     ListOptions{Until: &future, Limit: 100},
			expected: 1,
		},
		{
			name:     "until in past",
			opts:     ListOptions{Until: &past, Limit: 100},
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, total, err := logger.List(ctx, tt.opts)
			if err != nil {
				t.Fatalf("List() error = %v", err)
			}
			if total != tt.expected {
				t.Errorf("expected total %d, got %d", tt.expected, total)
			}
			if len(result) != tt.expected {
				t.Errorf("expected %d events, got %d", tt.expected, len(result))
			}
		})
	}
}

func TestMemoryAuditLogger_List_Pagination(t *testing.T) {
	logger := NewMemoryAuditLogger()
	ctx := context.Background()

	// Create 25 events
	for i := 0; i < 25; i++ {
		event := &AuditEvent{
			Actor:        "cpam_test",
			ActorType:    ActorTypeAPIKey,
			Action:       ActionCreate,
			ResourceType: ResourcePool,
			ResourceID:   string(rune('A' + i)),
			StatusCode:   201,
		}
		if err := logger.Log(ctx, event); err != nil {
			t.Fatalf("Log() error = %v", err)
		}
	}

	tests := []struct {
		name          string
		opts          ListOptions
		expectedLen   int
		expectedTotal int
	}{
		{
			name:          "first page",
			opts:          ListOptions{Limit: 10, Offset: 0},
			expectedLen:   10,
			expectedTotal: 25,
		},
		{
			name:          "second page",
			opts:          ListOptions{Limit: 10, Offset: 10},
			expectedLen:   10,
			expectedTotal: 25,
		},
		{
			name:          "third page (partial)",
			opts:          ListOptions{Limit: 10, Offset: 20},
			expectedLen:   5,
			expectedTotal: 25,
		},
		{
			name:          "beyond range",
			opts:          ListOptions{Limit: 10, Offset: 100},
			expectedLen:   0,
			expectedTotal: 25,
		},
		{
			name:          "default limit",
			opts:          ListOptions{Limit: 0},
			expectedLen:   25, // default limit is 50, we only have 25
			expectedTotal: 25,
		},
		{
			name:          "max limit enforcement",
			opts:          ListOptions{Limit: 2000},
			expectedLen:   25, // capped at 1000, but we only have 25
			expectedTotal: 25,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, total, err := logger.List(ctx, tt.opts)
			if err != nil {
				t.Fatalf("List() error = %v", err)
			}
			if total != tt.expectedTotal {
				t.Errorf("expected total %d, got %d", tt.expectedTotal, total)
			}
			if len(result) != tt.expectedLen {
				t.Errorf("expected %d events, got %d", tt.expectedLen, len(result))
			}
		})
	}
}

func TestMemoryAuditLogger_GetByResource(t *testing.T) {
	logger := NewMemoryAuditLogger()
	ctx := context.Background()

	// Create events for different resources
	events := []*AuditEvent{
		{Actor: "cpam_user1", ActorType: ActorTypeAPIKey, Action: ActionCreate, ResourceType: ResourcePool, ResourceID: "1", StatusCode: 201},
		{Actor: "cpam_user2", ActorType: ActorTypeAPIKey, Action: ActionUpdate, ResourceType: ResourcePool, ResourceID: "1", StatusCode: 200},
		{Actor: "cpam_user1", ActorType: ActorTypeAPIKey, Action: ActionDelete, ResourceType: ResourceAccount, ResourceID: "1", StatusCode: 204},
		{Actor: "cpam_user1", ActorType: ActorTypeAPIKey, Action: ActionCreate, ResourceType: ResourcePool, ResourceID: "2", StatusCode: 201},
	}
	for _, e := range events {
		if err := logger.Log(ctx, e); err != nil {
			t.Fatalf("Log() error = %v", err)
		}
	}

	// Get events for pool 1
	result, err := logger.GetByResource(ctx, ResourcePool, "1")
	if err != nil {
		t.Fatalf("GetByResource() error = %v", err)
	}
	if len(result) != 2 {
		t.Errorf("expected 2 events for pool 1, got %d", len(result))
	}

	// Get events for account 1
	result, err = logger.GetByResource(ctx, ResourceAccount, "1")
	if err != nil {
		t.Fatalf("GetByResource() error = %v", err)
	}
	if len(result) != 1 {
		t.Errorf("expected 1 event for account 1, got %d", len(result))
	}

	// Get events for nonexistent resource
	result, err = logger.GetByResource(ctx, ResourcePool, "999")
	if err != nil {
		t.Fatalf("GetByResource() error = %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected 0 events for nonexistent resource, got %d", len(result))
	}
}

func TestMemoryAuditLogger_Concurrency(t *testing.T) {
	logger := NewMemoryAuditLogger()
	ctx := context.Background()

	var wg sync.WaitGroup
	numGoroutines := 100
	eventsPerGoroutine := 10

	// Concurrent writes
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < eventsPerGoroutine; j++ {
				event := &AuditEvent{
					Actor:        "cpam_test",
					ActorType:    ActorTypeAPIKey,
					Action:       ActionCreate,
					ResourceType: ResourcePool,
					ResourceID:   "1",
					StatusCode:   201,
				}
				if err := logger.Log(ctx, event); err != nil {
					t.Errorf("Log() error = %v", err)
				}
			}
		}(i)
	}

	// Concurrent reads
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				_, _, err := logger.List(ctx, ListOptions{Limit: 10})
				if err != nil {
					t.Errorf("List() error = %v", err)
				}
			}
		}()
	}

	wg.Wait()

	// Verify final state
	_, total, err := logger.List(ctx, ListOptions{Limit: 10000})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	expected := numGoroutines * eventsPerGoroutine
	if total != expected {
		t.Errorf("expected %d events, got %d", expected, total)
	}
}

func TestMemoryAuditLogger_ImmutableResults(t *testing.T) {
	logger := NewMemoryAuditLogger()
	ctx := context.Background()

	event := &AuditEvent{
		Actor:        "cpam_test",
		ActorType:    ActorTypeAPIKey,
		Action:       ActionCreate,
		ResourceType: ResourcePool,
		ResourceID:   "1",
		Changes: &Changes{
			Before: map[string]any{"name": "original"},
			After:  map[string]any{"name": "modified"},
		},
		StatusCode: 201,
	}
	if err := logger.Log(ctx, event); err != nil {
		t.Fatalf("Log() error = %v", err)
	}

	// Modify the original event
	event.Actor = "modified_actor"
	event.Changes.Before["name"] = "tampered"

	// Get the stored event
	events, _, err := logger.List(ctx, ListOptions{Limit: 10})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}

	// Verify original values are preserved
	if events[0].Actor != "cpam_test" {
		t.Errorf("expected Actor 'cpam_test', got %q (modification leaked)", events[0].Actor)
	}
	if events[0].Changes.Before["name"] != "original" {
		t.Errorf("expected Changes.Before.name 'original', got %v (modification leaked)", events[0].Changes.Before["name"])
	}

	// Modify the returned event
	events[0].Actor = "another_modification"

	// Get again and verify no modification
	events2, _, err := logger.List(ctx, ListOptions{Limit: 10})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if events2[0].Actor != "cpam_test" {
		t.Errorf("expected Actor 'cpam_test', got %q (returned modification leaked)", events2[0].Actor)
	}
}

func TestWithMaxEvents(t *testing.T) {
	tests := []struct {
		name          string
		maxEvents     int
		expectedMax   int
		logCount      int
		expectedCount int
	}{
		{
			name:          "custom max",
			maxEvents:     100,
			expectedMax:   100,
			logCount:      150,
			expectedCount: 100,
		},
		{
			name:          "zero preserves default",
			maxEvents:     0,
			expectedMax:   DefaultMaxEvents,
			logCount:      10,
			expectedCount: 10,
		},
		{
			name:          "negative preserves default",
			maxEvents:     -1,
			expectedMax:   DefaultMaxEvents,
			logCount:      10,
			expectedCount: 10,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := NewMemoryAuditLogger(WithMaxEvents(tt.maxEvents))
			ctx := context.Background()

			for i := 0; i < tt.logCount; i++ {
				event := &AuditEvent{
					Actor:        "cpam_test",
					ActorType:    ActorTypeAPIKey,
					Action:       ActionCreate,
					ResourceType: ResourcePool,
					ResourceID:   "1",
					StatusCode:   201,
				}
				if err := logger.Log(ctx, event); err != nil {
					t.Fatalf("Log() error = %v", err)
				}
			}

			events, total, err := logger.List(ctx, ListOptions{Limit: 100000})
			if err != nil {
				t.Fatalf("List() error = %v", err)
			}
			if total != tt.expectedCount {
				t.Errorf("expected total %d, got %d", tt.expectedCount, total)
			}
			if len(events) > tt.expectedMax {
				t.Errorf("expected at most %d events, got %d", tt.expectedMax, len(events))
			}
		})
	}
}
