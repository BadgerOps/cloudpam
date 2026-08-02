package storage

import (
	"testing"
	"time"

	"cloudpam/internal/domain"
	"github.com/google/uuid"
)

// The memory stores used to hand out records whose maps and slices still
// pointed at store-owned state, so a caller mutating a returned value silently
// edited the store — without its lock, and without going through any write API.
// Each test below mutates what the store returned and then re-reads it.

func TestMemoryStorePoolTagsAreNotSharedWithCallers(t *testing.T) {
	ctx := t.Context()
	m := NewMemoryStore()

	created, err := m.CreatePool(ctx, domain.CreatePool{
		Name: "prod", CIDR: "10.0.0.0/16",
		Tags: map[string]string{"env": "prod"},
	})
	if err != nil {
		t.Fatalf("CreatePool: %v", err)
	}

	// Mutating the returned record must not reach the store.
	created.Tags["env"] = "tampered"
	created.Tags["injected"] = "yes"

	got, ok, err := m.GetPool(ctx, created.ID)
	if err != nil || !ok {
		t.Fatalf("GetPool: ok=%v err=%v", ok, err)
	}
	if got.Tags["env"] != "prod" {
		t.Errorf("stored tag env = %q, want %q", got.Tags["env"], "prod")
	}
	if _, injected := got.Tags["injected"]; injected {
		t.Error("caller injected a tag into stored state")
	}

	// The same has to hold for reads and for list results.
	got.Tags["env"] = "tampered-again"
	pools, err := m.ListPools(ctx)
	if err != nil {
		t.Fatalf("ListPools: %v", err)
	}
	if len(pools) != 1 || pools[0].Tags["env"] != "prod" {
		t.Errorf("ListPools tag = %v, want env=prod", pools[0].Tags)
	}

	pools[0].Tags["env"] = "tampered-from-list"
	again, _, err := m.GetPool(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetPool: %v", err)
	}
	if again.Tags["env"] != "prod" {
		t.Errorf("ListPools result aliased stored state: env = %q", again.Tags["env"])
	}
}

func TestMemoryStoreAccountRegionsAreNotSharedWithCallers(t *testing.T) {
	ctx := t.Context()
	m := NewMemoryStore()

	created, err := m.CreateAccount(ctx, domain.CreateAccount{
		Key: "aws-1", Name: "AWS One", Regions: []string{"us-east-1", "us-west-2"},
	})
	if err != nil {
		t.Fatalf("CreateAccount: %v", err)
	}

	created.Regions[0] = "tampered"

	got, ok, err := m.GetAccount(ctx, created.ID)
	if err != nil || !ok {
		t.Fatalf("GetAccount: ok=%v err=%v", ok, err)
	}
	if got.Regions[0] != "us-east-1" {
		t.Errorf("stored region = %q, want %q", got.Regions[0], "us-east-1")
	}

	byKey, err := m.GetAccountByKey(ctx, "aws-1")
	if err != nil {
		t.Fatalf("GetAccountByKey: %v", err)
	}
	byKey.Regions[1] = "tampered"
	final, _, err := m.GetAccount(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetAccount: %v", err)
	}
	if final.Regions[1] != "us-west-2" {
		t.Errorf("GetAccountByKey aliased stored state: region = %q", final.Regions[1])
	}
}

func TestMemoryDiscoveryStoreMetadataIsNotSharedWithCallers(t *testing.T) {
	ctx := t.Context()
	m := NewMemoryStore()
	ds := NewMemoryDiscoveryStore(m)

	// The map the caller keeps after Upsert must not become store state.
	callerMeta := map[string]string{"vpc": "vpc-1"}
	res := domain.DiscoveredResource{
		ID: uuid.New(), AccountID: 1, Provider: "aws", ResourceType: domain.ResourceTypeVPC,
		ResourceID: "vpc-1", CIDR: "10.0.0.0/16", Status: domain.DiscoveryStatusActive,
		Metadata: callerMeta, DiscoveredAt: time.Now().UTC(), LastSeenAt: time.Now().UTC(),
	}
	if err := ds.UpsertDiscoveredResource(ctx, res); err != nil {
		t.Fatalf("UpsertDiscoveredResource: %v", err)
	}
	callerMeta["vpc"] = "tampered"

	got, err := ds.GetDiscoveredResource(ctx, res.ID)
	if err != nil {
		t.Fatalf("GetDiscoveredResource: %v", err)
	}
	if got.Metadata["vpc"] != "vpc-1" {
		t.Errorf("stored metadata = %q, want %q", got.Metadata["vpc"], "vpc-1")
	}

	// And the map handed back by a read must not alias it either.
	got.Metadata["vpc"] = "tampered-on-read"
	again, err := ds.GetDiscoveredResource(ctx, res.ID)
	if err != nil {
		t.Fatalf("GetDiscoveredResource: %v", err)
	}
	if again.Metadata["vpc"] != "vpc-1" {
		t.Errorf("read result aliased stored state: %q", again.Metadata["vpc"])
	}

	list, _, err := ds.ListDiscoveredResources(ctx, 1, domain.DiscoveryFilters{})
	if err != nil {
		t.Fatalf("ListDiscoveredResources: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("len(list) = %d, want 1", len(list))
	}
	list[0].Metadata["vpc"] = "tampered-from-list"
	final, err := ds.GetDiscoveredResource(ctx, res.ID)
	if err != nil {
		t.Fatalf("GetDiscoveredResource: %v", err)
	}
	if final.Metadata["vpc"] != "vpc-1" {
		t.Errorf("list result aliased stored state: %q", final.Metadata["vpc"])
	}
}

func TestMemoryRecommendationStoreMetadataIsNotSharedWithCallers(t *testing.T) {
	ctx := t.Context()
	m := NewMemoryStore()
	rs := NewMemoryRecommendationStore(m)

	callerMeta := map[string]string{"rule": "ALLOC-001"}
	rec := domain.Recommendation{
		ID: "rec-1", PoolID: 1, Type: domain.RecommendationTypeAllocation,
		Status: domain.RecommendationStatusPending, Priority: domain.RecommendationPriorityHigh,
		Title: "Allocate", Metadata: callerMeta, CreatedAt: time.Now().UTC(),
	}
	if err := rs.CreateRecommendation(ctx, rec); err != nil {
		t.Fatalf("CreateRecommendation: %v", err)
	}
	callerMeta["rule"] = "tampered"

	got, err := rs.GetRecommendation(ctx, "rec-1")
	if err != nil {
		t.Fatalf("GetRecommendation: %v", err)
	}
	if got.Metadata["rule"] != "ALLOC-001" {
		t.Errorf("stored metadata = %q, want %q", got.Metadata["rule"], "ALLOC-001")
	}

	got.Metadata["rule"] = "tampered-on-read"
	again, err := rs.GetRecommendation(ctx, "rec-1")
	if err != nil {
		t.Fatalf("GetRecommendation: %v", err)
	}
	if again.Metadata["rule"] != "ALLOC-001" {
		t.Errorf("read result aliased stored state: %q", again.Metadata["rule"])
	}
}

func TestMemoryDriftStoreDetailsAreNotSharedWithCallers(t *testing.T) {
	ctx := t.Context()
	m := NewMemoryStore()
	ds := NewMemoryDriftStore(m)

	callerDetails := map[string]string{"cidr": "10.0.0.0/16"}
	item := domain.DriftItem{
		ID: "drift-1", AccountID: 1, Type: domain.DriftTypeUnmanaged,
		Severity: domain.DriftSeverityWarning, Status: domain.DriftStatusOpen,
		Title: "Unmanaged", Details: callerDetails, DetectedAt: time.Now().UTC(),
	}
	if err := ds.CreateDriftItem(ctx, item); err != nil {
		t.Fatalf("CreateDriftItem: %v", err)
	}
	callerDetails["cidr"] = "tampered"

	got, err := ds.GetDriftItem(ctx, "drift-1")
	if err != nil {
		t.Fatalf("GetDriftItem: %v", err)
	}
	if got.Details["cidr"] != "10.0.0.0/16" {
		t.Errorf("stored detail = %q, want %q", got.Details["cidr"], "10.0.0.0/16")
	}

	got.Details["cidr"] = "tampered-on-read"
	again, err := ds.GetDriftItem(ctx, "drift-1")
	if err != nil {
		t.Fatalf("GetDriftItem: %v", err)
	}
	if again.Details["cidr"] != "10.0.0.0/16" {
		t.Errorf("read result aliased stored state: %q", again.Details["cidr"])
	}
}

func TestMemoryConversationStoreMessagesAreNotSharedWithCallers(t *testing.T) {
	ctx := t.Context()
	m := NewMemoryStore()
	cs := NewMemoryConversationStore(m)

	conv := domain.Conversation{ID: "conv-1", Title: "Planning", CreatedAt: time.Now().UTC()}
	if err := cs.CreateConversation(ctx, conv); err != nil {
		t.Fatalf("CreateConversation: %v", err)
	}
	for _, content := range []string{"first", "second"} {
		if err := cs.AddMessage(ctx, domain.ConversationMessage{
			ID: uuid.NewString(), ConversationID: "conv-1", Role: "user",
			Content: content, CreatedAt: time.Now().UTC(),
		}); err != nil {
			t.Fatalf("AddMessage: %v", err)
		}
	}

	got, err := cs.GetConversation(ctx, "conv-1")
	if err != nil {
		t.Fatalf("GetConversation: %v", err)
	}
	if len(got.Messages) != 2 {
		t.Fatalf("len(messages) = %d, want 2", len(got.Messages))
	}
	got.Messages[0].Content = "tampered"

	again, err := cs.GetConversation(ctx, "conv-1")
	if err != nil {
		t.Fatalf("GetConversation: %v", err)
	}
	if again.Messages[0].Content != "first" {
		t.Errorf("message content = %q, want %q", again.Messages[0].Content, "first")
	}
}

// TestCloneHelpersPreserveEmptyVersusNil guards the subtlety that made an empty
// message history come back as nil: appending zero elements to a nil slice
// yields nil, which is a different value to an API consumer.
func TestCloneHelpersPreserveEmptyVersusNil(t *testing.T) {
	if got := cloneStringSlice(nil); got != nil {
		t.Errorf("cloneStringSlice(nil) = %v, want nil", got)
	}
	if got := cloneStringSlice([]string{}); got == nil {
		t.Error("cloneStringSlice(empty) returned nil, want an empty non-nil slice")
	}
	if got := cloneConversationMessages(nil); got != nil {
		t.Errorf("cloneConversationMessages(nil) = %v, want nil", got)
	}
	if got := cloneConversationMessages([]domain.ConversationMessage{}); got == nil {
		t.Error("cloneConversationMessages(empty) returned nil, want an empty non-nil slice")
	}
	if got := cloneStringStringMap(nil); got != nil {
		t.Errorf("cloneStringStringMap(nil) = %v, want nil", got)
	}
	if got := cloneStringStringMap(map[string]string{}); got == nil {
		t.Error("cloneStringStringMap(empty) returned nil, want an empty non-nil map")
	}
}
