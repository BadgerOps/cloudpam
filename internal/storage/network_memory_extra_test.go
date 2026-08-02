package storage

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"cloudpam/internal/domain"
)

func newNetworkStoreForExtraTests() *MemoryNetworkStore {
	return NewMemoryNetworkStore(NewMemoryStore())
}

func mustCreateNetworkObjectExtra(t *testing.T, s *MemoryNetworkStore, in domain.CreateNetworkObject) domain.NetworkObject {
	t.Helper()
	obj, err := s.CreateNetworkObject(context.Background(), in)
	if err != nil {
		t.Fatalf("CreateNetworkObject(%s): %v", in.Name, err)
	}
	return obj
}

func TestMemoryNetworkStoreExtra_NewWithNilBackingStore(t *testing.T) {
	s := NewMemoryNetworkStore(nil)
	if s.store == nil {
		t.Fatal("NewMemoryNetworkStore(nil) left the shared store nil")
	}
	// The store must still be usable (the shared mutex is what matters).
	if _, err := s.CreateNetworkObject(context.Background(), domain.CreateNetworkObject{AccountID: 1, Name: "vpc"}); err != nil {
		t.Fatalf("CreateNetworkObject: %v", err)
	}
}

func TestMemoryNetworkStoreExtra_CreateValidation(t *testing.T) {
	ctx := context.Background()
	s := newNetworkStoreForExtraTests()

	tests := []struct {
		name string
		in   domain.CreateNetworkObject
	}{
		{"missing account", domain.CreateNetworkObject{Name: "vpc"}},
		{"negative account", domain.CreateNetworkObject{AccountID: -1, Name: "vpc"}},
		{"missing name", domain.CreateNetworkObject{AccountID: 1}},
		{"whitespace-only name", domain.CreateNetworkObject{AccountID: 1, Name: "   "}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := s.CreateNetworkObject(ctx, tt.in); !errors.Is(err, ErrValidation) {
				t.Fatalf("error = %v, want ErrValidation", err)
			}
		})
	}

	objs, err := s.ListNetworkObjects(ctx, domain.NetworkObjectFilters{})
	if err != nil {
		t.Fatalf("ListNetworkObjects: %v", err)
	}
	if len(objs) != 0 {
		t.Errorf("rejected objects were persisted: %+v", objs)
	}
}

func TestMemoryNetworkStoreExtra_CreateAppliesDefaultsAndIncrementsIDs(t *testing.T) {
	ctx := context.Background()
	s := newNetworkStoreForExtraTests()

	first := mustCreateNetworkObjectExtra(t, s, domain.CreateNetworkObject{AccountID: 1, Name: "defaults"})
	if first.ID != 1 {
		t.Errorf("first ID = %d, want 1", first.ID)
	}
	if first.ObjectType != domain.NetworkObjectTypeOther {
		t.Errorf("ObjectType = %q, want %q", first.ObjectType, domain.NetworkObjectTypeOther)
	}
	if first.State != domain.NetworkObjectStateManaged {
		t.Errorf("State = %q, want %q", first.State, domain.NetworkObjectStateManaged)
	}
	if first.CreatedAt.IsZero() || first.UpdatedAt.IsZero() {
		t.Errorf("timestamps not set: %+v", first)
	}

	second := mustCreateNetworkObjectExtra(t, s, domain.CreateNetworkObject{
		AccountID:  1,
		Name:       "explicit",
		ObjectType: domain.NetworkObjectTypeVPC,
		State:      domain.NetworkObjectStateImported,
	})
	if second.ID != 2 {
		t.Errorf("second ID = %d, want 2", second.ID)
	}
	if second.ObjectType != domain.NetworkObjectTypeVPC || second.State != domain.NetworkObjectStateImported {
		t.Errorf("explicit type/state overwritten: %+v", second)
	}

	got, ok, err := s.GetNetworkObject(ctx, second.ID)
	if err != nil || !ok {
		t.Fatalf("GetNetworkObject = (%+v, %v, %v)", got, ok, err)
	}
	if got.Name != "explicit" {
		t.Errorf("Name = %q, want explicit", got.Name)
	}

	if _, ok, err := s.GetNetworkObject(ctx, 9999); err != nil || ok {
		t.Errorf("GetNetworkObject(missing) = (ok=%v, err=%v), want (false, nil)", ok, err)
	}
}

func TestMemoryNetworkStoreExtra_ObjectMetadataIsIsolated(t *testing.T) {
	ctx := context.Background()
	s := newNetworkStoreForExtraTests()

	input := map[string]string{"env": "prod"}
	created := mustCreateNetworkObjectExtra(t, s, domain.CreateNetworkObject{
		AccountID: 1, Name: "vpc", Metadata: input,
	})

	// Mutating the caller's map after the write must not reach the store.
	input["env"] = "hacked"
	input["extra"] = "leak"

	// Mutating the returned struct must not reach the store either.
	created.Metadata["env"] = "also-hacked"

	got, ok, err := s.GetNetworkObject(ctx, created.ID)
	if err != nil || !ok {
		t.Fatalf("GetNetworkObject = (%v, %v)", ok, err)
	}
	if got.Metadata["env"] != "prod" {
		t.Errorf("Metadata[env] = %q, want prod", got.Metadata["env"])
	}
	if _, leaked := got.Metadata["extra"]; leaked {
		t.Error("caller mutation added a key to store-owned metadata")
	}

	// Mutating a Get result must not reach the store.
	got.Metadata["env"] = "get-hacked"
	listed, err := s.ListNetworkObjects(ctx, domain.NetworkObjectFilters{})
	if err != nil {
		t.Fatalf("ListNetworkObjects: %v", err)
	}
	if len(listed) != 1 || listed[0].Metadata["env"] != "prod" {
		t.Errorf("store metadata mutated through Get result: %+v", listed)
	}

	// Mutating a List result must not reach the store.
	listed[0].Metadata["env"] = "list-hacked"
	again, _, err := s.GetNetworkObject(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetNetworkObject: %v", err)
	}
	if again.Metadata["env"] != "prod" {
		t.Errorf("store metadata mutated through List result: %q", again.Metadata["env"])
	}
}

func TestMemoryNetworkStoreExtra_ListObjectFilters(t *testing.T) {
	ctx := context.Background()
	s := newNetworkStoreForExtraTests()
	discoveredID := uuid.New()

	mustCreateNetworkObjectExtra(t, s, domain.CreateNetworkObject{
		AccountID: 1, Name: "prod-vpc", Provider: "aws", Region: "us-east-1",
		ObjectType: domain.NetworkObjectTypeVPC, State: domain.NetworkObjectStateManaged,
		CIDR: "10.0.0.0/16", ProviderResourceID: "vpc-111", PoolID: tPtr(int64(5)),
		SourceDiscoveredID: &discoveredID,
	})
	mustCreateNetworkObjectExtra(t, s, domain.CreateNetworkObject{
		AccountID: 1, Name: "staging-subnet", Provider: "gcp", Region: "us-west-2",
		ObjectType: domain.NetworkObjectTypeSubnet, State: domain.NetworkObjectStatePlaceholder,
		CIDR: "10.1.0.0/24", ProviderResourceID: "subnet-222",
	})
	mustCreateNetworkObjectExtra(t, s, domain.CreateNetworkObject{
		AccountID: 2, Name: "other-eip", Provider: "aws", Region: "eu-west-1",
		ObjectType: domain.NetworkObjectTypeEIP, State: domain.NetworkObjectStateIgnored,
		IPAddress: "203.0.113.9", ProviderResourceID: "eipalloc-333",
	})

	tests := []struct {
		name    string
		filters domain.NetworkObjectFilters
		want    []string
	}{
		{"no filters", domain.NetworkObjectFilters{}, []string{"prod-vpc", "staging-subnet", "other-eip"}},
		{"account", domain.NetworkObjectFilters{AccountID: 2}, []string{"other-eip"}},
		{"provider", domain.NetworkObjectFilters{Provider: "gcp"}, []string{"staging-subnet"}},
		{"region", domain.NetworkObjectFilters{Region: "eu-west-1"}, []string{"other-eip"}},
		{"object type", domain.NetworkObjectFilters{ObjectType: "vpc"}, []string{"prod-vpc"}},
		{"state", domain.NetworkObjectFilters{State: "placeholder"}, []string{"staging-subnet"}},
		{"pool id", domain.NetworkObjectFilters{PoolID: 5}, []string{"prod-vpc"}},
		{"pool id with no match", domain.NetworkObjectFilters{PoolID: 6}, nil},
		{"source discovered id", domain.NetworkObjectFilters{SourceDiscoveredID: discoveredID.String()}, []string{"prod-vpc"}},
		{"source discovered id with no match", domain.NetworkObjectFilters{SourceDiscoveredID: uuid.New().String()}, nil},
		{"query matches name", domain.NetworkObjectFilters{Query: "STAGING"}, []string{"staging-subnet"}},
		{"query matches cidr", domain.NetworkObjectFilters{Query: "10.0.0.0"}, []string{"prod-vpc"}},
		{"query matches ip address", domain.NetworkObjectFilters{Query: "203.0.113.9"}, []string{"other-eip"}},
		{"query matches provider resource id", domain.NetworkObjectFilters{Query: "eipalloc"}, []string{"other-eip"}},
		{"query with no match", domain.NetworkObjectFilters{Query: "zzz"}, nil},
		{"account and provider combined", domain.NetworkObjectFilters{AccountID: 1, Provider: "aws"}, []string{"prod-vpc"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := s.ListNetworkObjects(ctx, tt.filters)
			if err != nil {
				t.Fatalf("ListNetworkObjects: %v", err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("len = %d, want %d (%+v)", len(got), len(tt.want), got)
			}
			names := make(map[string]bool, len(got))
			for _, obj := range got {
				names[obj.Name] = true
			}
			for _, want := range tt.want {
				if !names[want] {
					t.Errorf("missing %q in %v", want, names)
				}
			}
		})
	}
}

func TestMemoryNetworkStoreExtra_ListObjectsSortedByAccountRegionName(t *testing.T) {
	ctx := context.Background()
	s := newNetworkStoreForExtraTests()

	mustCreateNetworkObjectExtra(t, s, domain.CreateNetworkObject{AccountID: 2, Region: "us-east-1", Name: "z"})
	mustCreateNetworkObjectExtra(t, s, domain.CreateNetworkObject{AccountID: 1, Region: "us-west-2", Name: "a"})
	mustCreateNetworkObjectExtra(t, s, domain.CreateNetworkObject{AccountID: 1, Region: "us-east-1", Name: "b"})
	mustCreateNetworkObjectExtra(t, s, domain.CreateNetworkObject{AccountID: 1, Region: "us-east-1", Name: "a"})

	got, err := s.ListNetworkObjects(ctx, domain.NetworkObjectFilters{})
	if err != nil {
		t.Fatalf("ListNetworkObjects: %v", err)
	}
	type key struct {
		account int64
		region  string
		name    string
	}
	want := []key{
		{1, "us-east-1", "a"},
		{1, "us-east-1", "b"},
		{1, "us-west-2", "a"},
		{2, "us-east-1", "z"},
	}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d", len(got), len(want))
	}
	for i, w := range want {
		if got[i].AccountID != w.account || got[i].Region != w.region || got[i].Name != w.name {
			t.Errorf("index %d = (%d,%s,%s), want (%d,%s,%s)", i, got[i].AccountID, got[i].Region, got[i].Name, w.account, w.region, w.name)
		}
	}
}

func TestMemoryNetworkStoreExtra_UpdateNetworkObject(t *testing.T) {
	ctx := context.Background()
	s := newNetworkStoreForExtraTests()

	if _, ok, err := s.UpdateNetworkObject(ctx, 404, domain.UpdateNetworkObject{Name: tPtr("x")}); err != nil || ok {
		t.Fatalf("UpdateNetworkObject(missing) = (ok=%v, err=%v), want (false, nil)", ok, err)
	}

	created := mustCreateNetworkObjectExtra(t, s, domain.CreateNetworkObject{
		AccountID: 1, Name: "before", Provider: "aws", Region: "us-east-1",
		CIDR: "10.0.0.0/16", Metadata: map[string]string{"a": "1"},
	})

	// An empty update leaves everything but UpdatedAt alone.
	untouched, ok, err := s.UpdateNetworkObject(ctx, created.ID, domain.UpdateNetworkObject{})
	if err != nil || !ok {
		t.Fatalf("UpdateNetworkObject(empty) = (%v, %v)", ok, err)
	}
	if untouched.Name != "before" || untouched.CIDR != "10.0.0.0/16" || untouched.Metadata["a"] != "1" {
		t.Errorf("empty update changed fields: %+v", untouched)
	}
	if untouched.UpdatedAt.Before(created.UpdatedAt) {
		t.Errorf("UpdatedAt went backwards: %v < %v", untouched.UpdatedAt, created.UpdatedAt)
	}

	discoveredID := uuid.New()
	updated, ok, err := s.UpdateNetworkObject(ctx, created.ID, domain.UpdateNetworkObject{
		ObjectType:         tPtr(domain.NetworkObjectTypeSubnet),
		Provider:           tPtr("gcp"),
		AccountID:          tPtr(int64(3)),
		Region:             tPtr("eu-west-1"),
		Name:               tPtr("after"),
		CIDR:               tPtr("192.168.0.0/24"),
		IPAddress:          tPtr("192.168.0.5"),
		ProviderResourceID: tPtr("subnet-999"),
		ParentObjectID:     tPtr(int64(77)),
		PoolID:             tPtr(int64(88)),
		SourceDiscoveredID: &discoveredID,
		State:              tPtr(domain.NetworkObjectStateIgnored),
		Metadata:           tPtr(map[string]string{"b": "2"}),
	})
	if err != nil || !ok {
		t.Fatalf("UpdateNetworkObject = (%v, %v)", ok, err)
	}

	if updated.ObjectType != domain.NetworkObjectTypeSubnet ||
		updated.Provider != "gcp" ||
		updated.AccountID != 3 ||
		updated.Region != "eu-west-1" ||
		updated.Name != "after" ||
		updated.CIDR != "192.168.0.0/24" ||
		updated.IPAddress != "192.168.0.5" ||
		updated.ProviderResourceID != "subnet-999" ||
		updated.State != domain.NetworkObjectStateIgnored {
		t.Errorf("scalar fields not applied: %+v", updated)
	}
	if updated.ParentObjectID == nil || *updated.ParentObjectID != 77 {
		t.Errorf("ParentObjectID = %v, want 77", updated.ParentObjectID)
	}
	if updated.PoolID == nil || *updated.PoolID != 88 {
		t.Errorf("PoolID = %v, want 88", updated.PoolID)
	}
	if updated.SourceDiscoveredID == nil || *updated.SourceDiscoveredID != discoveredID {
		t.Errorf("SourceDiscoveredID = %v, want %s", updated.SourceDiscoveredID, discoveredID)
	}
	if _, stale := updated.Metadata["a"]; stale {
		t.Error("metadata was merged, want replaced")
	}
	if updated.Metadata["b"] != "2" {
		t.Errorf("Metadata = %v, want {b:2}", updated.Metadata)
	}
	if updated.ID != created.ID {
		t.Errorf("ID = %d, want preserved %d", updated.ID, created.ID)
	}
	if !updated.CreatedAt.Equal(created.CreatedAt) {
		t.Errorf("CreatedAt = %v, want preserved %v", updated.CreatedAt, created.CreatedAt)
	}
}

func TestMemoryNetworkStoreExtra_UpdateObjectMetadataIsIsolated(t *testing.T) {
	ctx := context.Background()
	s := newNetworkStoreForExtraTests()
	created := mustCreateNetworkObjectExtra(t, s, domain.CreateNetworkObject{AccountID: 1, Name: "vpc"})

	metadata := map[string]string{"env": "prod"}
	updated, ok, err := s.UpdateNetworkObject(ctx, created.ID, domain.UpdateNetworkObject{Metadata: &metadata})
	if err != nil || !ok {
		t.Fatalf("UpdateNetworkObject = (%v, %v)", ok, err)
	}

	metadata["env"] = "hacked"
	updated.Metadata["env"] = "also-hacked"

	got, _, err := s.GetNetworkObject(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetNetworkObject: %v", err)
	}
	if got.Metadata["env"] != "prod" {
		t.Errorf("Metadata[env] = %q, want prod", got.Metadata["env"])
	}
}

func TestMemoryNetworkStoreExtra_UpsertRelationshipValidation(t *testing.T) {
	ctx := context.Background()
	s := newNetworkStoreForExtraTests()

	valid := domain.CreateNetworkRelationship{
		Type: domain.NetworkRelationshipContains, SourceKind: "pool", SourceID: "1",
		TargetKind: "discovered", TargetID: "d1",
	}
	tests := []struct {
		name  string
		mutin func(*domain.CreateNetworkRelationship)
	}{
		{"missing type", func(in *domain.CreateNetworkRelationship) { in.Type = "" }},
		{"missing source kind", func(in *domain.CreateNetworkRelationship) { in.SourceKind = "" }},
		{"missing source id", func(in *domain.CreateNetworkRelationship) { in.SourceID = "" }},
		{"missing target kind", func(in *domain.CreateNetworkRelationship) { in.TargetKind = "" }},
		{"missing target id", func(in *domain.CreateNetworkRelationship) { in.TargetID = "" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := valid
			tt.mutin(&in)
			if _, err := s.UpsertNetworkRelationship(ctx, in); !errors.Is(err, ErrValidation) {
				t.Fatalf("error = %v, want ErrValidation", err)
			}
		})
	}

	rels, err := s.ListNetworkRelationships(ctx, domain.NetworkRelationshipFilters{})
	if err != nil {
		t.Fatalf("ListNetworkRelationships: %v", err)
	}
	if len(rels) != 0 {
		t.Errorf("invalid relationships were persisted: %+v", rels)
	}
}

func TestMemoryNetworkStoreExtra_UpsertRelationshipDefaultsAndDeterministicID(t *testing.T) {
	ctx := context.Background()
	s := newNetworkStoreForExtraTests()

	in := domain.CreateNetworkRelationship{
		Type: domain.NetworkRelationshipMatches, SourceKind: "pool", SourceID: "1",
		TargetKind: "discovered", TargetID: "d1", Reason: "first",
		Evidence: []string{"cidr-equal"},
	}
	rel, err := s.UpsertNetworkRelationship(ctx, in)
	if err != nil {
		t.Fatalf("UpsertNetworkRelationship: %v", err)
	}
	if rel.ID == "" {
		t.Fatal("relationship ID was not derived")
	}
	if rel.Confidence != 1 {
		t.Errorf("Confidence = %v, want default 1", rel.Confidence)
	}
	if rel.ResolutionState != "open" {
		t.Errorf("ResolutionState = %q, want default open", rel.ResolutionState)
	}
	if rel.CreatedAt.IsZero() || rel.UpdatedAt.IsZero() {
		t.Errorf("timestamps not set: %+v", rel)
	}

	// The derived ID is a pure function of type/source/target.
	if got := networkRelationshipID(in); got != rel.ID {
		t.Errorf("networkRelationshipID = %q, want %q", got, rel.ID)
	}
	differentReason := in
	differentReason.Reason = "unrelated"
	if got := networkRelationshipID(differentReason); got != rel.ID {
		t.Error("relationship ID depends on fields outside type/source/target")
	}
	differentTarget := in
	differentTarget.TargetID = "d2"
	if networkRelationshipID(differentTarget) == rel.ID {
		t.Error("relationship ID collides across different targets")
	}

	// Re-upsert: CreatedAt is preserved and an empty state keeps the stored one.
	if _, _, err := s.UpdateNetworkRelationshipState(ctx, rel.ID, "resolved", "handled"); err != nil {
		t.Fatalf("UpdateNetworkRelationshipState: %v", err)
	}
	again, err := s.UpsertNetworkRelationship(ctx, domain.CreateNetworkRelationship{
		Type: in.Type, SourceKind: in.SourceKind, SourceID: in.SourceID,
		TargetKind: in.TargetKind, TargetID: in.TargetID, Reason: "second", Confidence: 0.5,
	})
	if err != nil {
		t.Fatalf("UpsertNetworkRelationship(again): %v", err)
	}
	if again.ID != rel.ID {
		t.Errorf("ID = %q, want stable %q", again.ID, rel.ID)
	}
	if !again.CreatedAt.Equal(rel.CreatedAt) {
		t.Errorf("CreatedAt = %v, want preserved %v", again.CreatedAt, rel.CreatedAt)
	}
	if again.ResolutionState != "resolved" {
		t.Errorf("ResolutionState = %q, want preserved resolved", again.ResolutionState)
	}
	if again.Confidence != 0.5 || again.Reason != "second" {
		t.Errorf("mutable fields not applied: %+v", again)
	}

	// An explicit resolution state on the upsert wins.
	explicit, err := s.UpsertNetworkRelationship(ctx, domain.CreateNetworkRelationship{
		ID: rel.ID, Type: in.Type, SourceKind: in.SourceKind, SourceID: in.SourceID,
		TargetKind: in.TargetKind, TargetID: in.TargetID, ResolutionState: "dismissed",
	})
	if err != nil {
		t.Fatalf("UpsertNetworkRelationship(explicit state): %v", err)
	}
	if explicit.ResolutionState != "dismissed" {
		t.Errorf("ResolutionState = %q, want dismissed", explicit.ResolutionState)
	}

	all, err := s.ListNetworkRelationships(ctx, domain.NetworkRelationshipFilters{})
	if err != nil {
		t.Fatalf("ListNetworkRelationships: %v", err)
	}
	if len(all) != 1 {
		t.Errorf("upsert created duplicates: %d rows", len(all))
	}
}

func TestMemoryNetworkStoreExtra_RelationshipEvidenceIsIsolated(t *testing.T) {
	ctx := context.Background()
	s := newNetworkStoreForExtraTests()

	evidence := []string{"cidr-equal"}
	rel, err := s.UpsertNetworkRelationship(ctx, domain.CreateNetworkRelationship{
		Type: domain.NetworkRelationshipMatches, SourceKind: "pool", SourceID: "1",
		TargetKind: "discovered", TargetID: "d1", Evidence: evidence,
	})
	if err != nil {
		t.Fatalf("UpsertNetworkRelationship: %v", err)
	}

	// Mutating the caller's slice after the write must not reach the store.
	evidence[0] = "hacked"
	// Mutating the returned slice must not reach the store either.
	rel.Evidence[0] = "also-hacked"

	listed, err := s.ListNetworkRelationships(ctx, domain.NetworkRelationshipFilters{})
	if err != nil {
		t.Fatalf("ListNetworkRelationships: %v", err)
	}
	if len(listed) != 1 || len(listed[0].Evidence) != 1 || listed[0].Evidence[0] != "cidr-equal" {
		t.Fatalf("stored evidence = %+v, want [cidr-equal]", listed)
	}

	// Mutating a List result must not reach the store.
	listed[0].Evidence[0] = "list-hacked"
	again, err := s.ListNetworkRelationships(ctx, domain.NetworkRelationshipFilters{})
	if err != nil {
		t.Fatalf("ListNetworkRelationships: %v", err)
	}
	if again[0].Evidence[0] != "cidr-equal" {
		t.Errorf("evidence mutated through List result: %q", again[0].Evidence[0])
	}
}

func TestMemoryNetworkStoreExtra_ListRelationshipFilters(t *testing.T) {
	ctx := context.Background()
	s := newNetworkStoreForExtraTests()

	seed := []domain.CreateNetworkRelationship{
		{ID: "rel-a", Type: domain.NetworkRelationshipContains, SourceKind: "pool", SourceID: "1", TargetKind: "discovered", TargetID: "d1", ResolutionState: "open"},
		{ID: "rel-b", Type: domain.NetworkRelationshipConflicts, SourceKind: "discovered", SourceID: "d2", TargetKind: "pool", TargetID: "1", ResolutionState: "resolved"},
		{ID: "rel-c", Type: domain.NetworkRelationshipMatches, SourceKind: "object", SourceID: "o1", TargetKind: "discovered", TargetID: "d3", ResolutionState: "open"},
	}
	for _, in := range seed {
		if _, err := s.UpsertNetworkRelationship(ctx, in); err != nil {
			t.Fatalf("UpsertNetworkRelationship(%s): %v", in.ID, err)
		}
	}

	tests := []struct {
		name    string
		filters domain.NetworkRelationshipFilters
		want    []string
	}{
		{"no filters", domain.NetworkRelationshipFilters{}, []string{"rel-a", "rel-b", "rel-c"}},
		{"ids", domain.NetworkRelationshipFilters{IDs: []string{"rel-a", "rel-c"}}, []string{"rel-a", "rel-c"}},
		{"unknown id", domain.NetworkRelationshipFilters{IDs: []string{"rel-zzz"}}, nil},
		{"type", domain.NetworkRelationshipFilters{Type: "conflicts"}, []string{"rel-b"}},
		{"source kind", domain.NetworkRelationshipFilters{SourceKind: "object"}, []string{"rel-c"}},
		{"source id", domain.NetworkRelationshipFilters{SourceID: "d2"}, []string{"rel-b"}},
		{"target kind", domain.NetworkRelationshipFilters{TargetKind: "pool"}, []string{"rel-b"}},
		{"target id", domain.NetworkRelationshipFilters{TargetID: "d3"}, []string{"rel-c"}},
		{"entity matches source or target", domain.NetworkRelationshipFilters{EntityKind: "pool", EntityID: "1"}, []string{"rel-a", "rel-b"}},
		{"entity with no match", domain.NetworkRelationshipFilters{EntityKind: "pool", EntityID: "999"}, nil},
		{"entity kind without id is ignored", domain.NetworkRelationshipFilters{EntityKind: "pool"}, []string{"rel-a", "rel-b", "rel-c"}},
		{"resolution state", domain.NetworkRelationshipFilters{ResolutionState: "resolved"}, []string{"rel-b"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := s.ListNetworkRelationships(ctx, tt.filters)
			if err != nil {
				t.Fatalf("ListNetworkRelationships: %v", err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("len = %d, want %d (%+v)", len(got), len(tt.want), got)
			}
			for i, want := range tt.want {
				if got[i].ID != want {
					t.Errorf("index %d = %q, want %q (results must be sorted by ID)", i, got[i].ID, want)
				}
			}
		})
	}
}

func TestMemoryNetworkStoreExtra_UpdateRelationshipState(t *testing.T) {
	ctx := context.Background()
	s := newNetworkStoreForExtraTests()

	if _, _, err := s.UpdateNetworkRelationshipState(ctx, "rel-a", "  ", ""); !errors.Is(err, ErrValidation) {
		t.Fatalf("blank state error = %v, want ErrValidation", err)
	}
	if _, ok, err := s.UpdateNetworkRelationshipState(ctx, "rel-missing", "resolved", ""); err != nil || ok {
		t.Fatalf("unknown id = (ok=%v, err=%v), want (false, nil)", ok, err)
	}

	created, err := s.UpsertNetworkRelationship(ctx, domain.CreateNetworkRelationship{
		ID: "rel-a", Type: domain.NetworkRelationshipContains, SourceKind: "pool", SourceID: "1",
		TargetKind: "discovered", TargetID: "d1", Reason: "original",
	})
	if err != nil {
		t.Fatalf("UpsertNetworkRelationship: %v", err)
	}

	// An empty reason preserves the existing one.
	kept, ok, err := s.UpdateNetworkRelationshipState(ctx, "rel-a", "acknowledged", "")
	if err != nil || !ok {
		t.Fatalf("UpdateNetworkRelationshipState = (%v, %v)", ok, err)
	}
	if kept.ResolutionState != "acknowledged" {
		t.Errorf("ResolutionState = %q, want acknowledged", kept.ResolutionState)
	}
	if kept.Reason != "original" {
		t.Errorf("Reason = %q, want preserved original", kept.Reason)
	}
	if kept.UpdatedAt.Before(created.UpdatedAt) {
		t.Errorf("UpdatedAt went backwards: %v < %v", kept.UpdatedAt, created.UpdatedAt)
	}

	replaced, ok, err := s.UpdateNetworkRelationshipState(ctx, "rel-a", "resolved", "handled by ops")
	if err != nil || !ok {
		t.Fatalf("UpdateNetworkRelationshipState = (%v, %v)", ok, err)
	}
	if replaced.Reason != "handled by ops" {
		t.Errorf("Reason = %q, want handled by ops", replaced.Reason)
	}

	persisted, err := s.ListNetworkRelationships(ctx, domain.NetworkRelationshipFilters{IDs: []string{"rel-a"}})
	if err != nil {
		t.Fatalf("ListNetworkRelationships: %v", err)
	}
	if len(persisted) != 1 || persisted[0].ResolutionState != "resolved" || persisted[0].Reason != "handled by ops" {
		t.Errorf("state change not persisted: %+v", persisted)
	}
	if !persisted[0].CreatedAt.Equal(created.CreatedAt) {
		t.Errorf("CreatedAt = %v, want preserved %v", persisted[0].CreatedAt, created.CreatedAt)
	}
}

func TestMemoryNetworkStoreExtra_CloneStringMapPreservesNil(t *testing.T) {
	if got := cloneStringMap(nil); got != nil {
		t.Errorf("cloneStringMap(nil) = %v, want nil", got)
	}
	in := map[string]string{"a": "1", "b": "2"}
	out := cloneStringMap(in)
	if len(out) != 2 || out["a"] != "1" || out["b"] != "2" {
		t.Fatalf("cloneStringMap = %v, want a copy of %v", out, in)
	}
	out["a"] = "changed"
	if in["a"] != "1" {
		t.Error("cloneStringMap returned an aliased map")
	}
}
