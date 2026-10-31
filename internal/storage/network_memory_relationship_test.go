package storage

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"

	"cloudpam/internal/domain"
)

func TestMemoryNetworkStore_UpsertNetworkRelationshipObjectRefs(t *testing.T) {
	ctx := context.Background()
	rel := func(sourceKind, sourceID, targetKind, targetID string) domain.CreateNetworkRelationship {
		return domain.CreateNetworkRelationship{Type: domain.NetworkRelationshipContains, SourceKind: sourceKind, SourceID: sourceID, TargetKind: targetKind, TargetID: targetID}
	}

	t.Run("unknown object id as source is a validation error", func(t *testing.T) {
		s := newNetworkStoreForExtraTests()
		_, err := s.UpsertNetworkRelationship(ctx, rel(domain.NetworkEntityKindObject, "424242", "pool", "7"))
		if !errors.Is(err, ErrValidation) {
			t.Fatalf("error = %v, want ErrValidation", err)
		}
		if !strings.Contains(err.Error(), "network object 424242 not found") {
			t.Fatalf("error message = %q, want it to name the missing object", err)
		}
	})

	t.Run("unknown object id as target is a validation error", func(t *testing.T) {
		s := newNetworkStoreForExtraTests()
		_, err := s.UpsertNetworkRelationship(ctx, rel("pool", "7", domain.NetworkEntityKindObject, "424242"))
		if !errors.Is(err, ErrValidation) {
			t.Fatalf("error = %v, want ErrValidation", err)
		}
		if rels, _ := s.ListNetworkRelationships(ctx, domain.NetworkRelationshipFilters{}); len(rels) != 0 {
			t.Fatalf("relationships after rejected upsert = %+v, want none", rels)
		}
	})

	t.Run("non-numeric object id is a validation error", func(t *testing.T) {
		s := newNetworkStoreForExtraTests()
		_, err := s.UpsertNetworkRelationship(ctx, rel(domain.NetworkEntityKindObject, "vpc-abc", "pool", "7"))
		if !errors.Is(err, ErrValidation) {
			t.Fatalf("error = %v, want ErrValidation", err)
		}
		if !strings.Contains(err.Error(), "network object vpc-abc not found") {
			t.Fatalf("error message = %q, want it to name the missing object", err)
		}
	})

	t.Run("existing object succeeds on both sides", func(t *testing.T) {
		s := newNetworkStoreForExtraTests()
		src := mustCreateNetworkObjectExtra(t, s, domain.CreateNetworkObject{AccountID: 1, Name: "vpc"})
		dst := mustCreateNetworkObjectExtra(t, s, domain.CreateNetworkObject{AccountID: 1, Name: "subnet"})
		got, err := s.UpsertNetworkRelationship(ctx, rel(domain.NetworkEntityKindObject, strconv.FormatInt(src.ID, 10), domain.NetworkEntityKindObject, strconv.FormatInt(dst.ID, 10)))
		if err != nil {
			t.Fatalf("UpsertNetworkRelationship: %v", err)
		}
		if got.SourceID != strconv.FormatInt(src.ID, 10) || got.TargetID != strconv.FormatInt(dst.ID, 10) {
			t.Fatalf("relationship = %+v, want it to reference both objects", got)
		}
	})

	t.Run("pool and discovered kinds are not checked", func(t *testing.T) {
		s := newNetworkStoreForExtraTests()
		if _, err := s.UpsertNetworkRelationship(ctx, rel("pool", "424242", "discovered", "not-a-uuid")); err != nil {
			t.Fatalf("UpsertNetworkRelationship(pool, discovered): %v", err)
		}
	})
}
