package storage

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"

	"cloudpam/internal/domain"
)

func TestMemoryNetworkStore_DeleteNetworkObject(t *testing.T) {
	ctx := context.Background()

	t.Run("unreferenced object is deleted", func(t *testing.T) {
		s := newNetworkStoreForExtraTests()
		obj := mustCreateNetworkObjectExtra(t, s, domain.CreateNetworkObject{AccountID: 1, Name: "vpc"})
		if err := s.DeleteNetworkObject(ctx, obj.ID); err != nil {
			t.Fatalf("DeleteNetworkObject: %v", err)
		}
		if _, found, err := s.GetNetworkObject(ctx, obj.ID); err != nil || found {
			t.Fatalf("GetNetworkObject after delete: found=%v err=%v, want not found", found, err)
		}
	})

	t.Run("unknown id is not found", func(t *testing.T) {
		s := newNetworkStoreForExtraTests()
		if err := s.DeleteNetworkObject(ctx, 999); !errors.Is(err, ErrNotFound) {
			t.Fatalf("error = %v, want ErrNotFound", err)
		}
	})

	t.Run("object with a child conflicts until the child is gone", func(t *testing.T) {
		s := newNetworkStoreForExtraTests()
		parent := mustCreateNetworkObjectExtra(t, s, domain.CreateNetworkObject{AccountID: 1, Name: "vpc"})
		child := mustCreateNetworkObjectExtra(t, s, domain.CreateNetworkObject{AccountID: 1, Name: "subnet", ParentObjectID: &parent.ID})
		if err := s.DeleteNetworkObject(ctx, parent.ID); !errors.Is(err, ErrConflict) {
			t.Fatalf("error = %v, want ErrConflict", err)
		}
		if _, found, _ := s.GetNetworkObject(ctx, parent.ID); !found {
			t.Fatal("parent was deleted despite the conflict")
		}
		if err := s.DeleteNetworkObject(ctx, child.ID); err != nil {
			t.Fatalf("DeleteNetworkObject(child): %v", err)
		}
		if err := s.DeleteNetworkObject(ctx, parent.ID); err != nil {
			t.Fatalf("DeleteNetworkObject(parent) after child removed: %v", err)
		}
	})

	for _, side := range []string{"source", "target"} {
		t.Run("relationship referencing the object as "+side+" is removed", func(t *testing.T) {
			s := newNetworkStoreForExtraTests()
			obj := mustCreateNetworkObjectExtra(t, s, domain.CreateNetworkObject{AccountID: 1, Name: "vpc"})
			rel := domain.CreateNetworkRelationship{ID: "rel-" + side, Type: domain.NetworkRelationshipContains, SourceKind: "pool", SourceID: "7", TargetKind: domain.NetworkEntityKindObject, TargetID: strconv.FormatInt(obj.ID, 10)}
			if side == "source" {
				rel.SourceKind, rel.SourceID, rel.TargetKind, rel.TargetID = rel.TargetKind, rel.TargetID, rel.SourceKind, rel.SourceID
			}
			if _, err := s.UpsertNetworkRelationship(ctx, rel); err != nil {
				t.Fatalf("UpsertNetworkRelationship: %v", err)
			}
			if err := s.DeleteNetworkObject(ctx, obj.ID); err != nil {
				t.Fatalf("DeleteNetworkObject: %v", err)
			}
			rels, err := s.ListNetworkRelationships(ctx, domain.NetworkRelationshipFilters{IDs: []string{rel.ID}})
			if err != nil || len(rels) != 0 {
				t.Fatalf("relationships after delete = %+v err=%v, want none", rels, err)
			}
		})
	}

	t.Run("relationships for other objects or another kind with the same id are untouched", func(t *testing.T) {
		s := newNetworkStoreForExtraTests()
		obj := mustCreateNetworkObjectExtra(t, s, domain.CreateNetworkObject{AccountID: 1, Name: "vpc"})
		other := mustCreateNetworkObjectExtra(t, s, domain.CreateNetworkObject{AccountID: 1, Name: "other-vpc"})
		idText := strconv.FormatInt(obj.ID, 10)
		keep := []domain.CreateNetworkRelationship{
			{ID: "rel-other-object", Type: domain.NetworkRelationshipContains, SourceKind: domain.NetworkEntityKindObject, SourceID: strconv.FormatInt(other.ID, 10), TargetKind: "pool", TargetID: idText},
			{ID: "rel-pool-same-id", Type: domain.NetworkRelationshipContains, SourceKind: "pool", SourceID: idText, TargetKind: "discovered", TargetID: "x"},
		}
		for _, rel := range keep {
			if _, err := s.UpsertNetworkRelationship(ctx, rel); err != nil {
				t.Fatalf("UpsertNetworkRelationship(%s): %v", rel.ID, err)
			}
		}
		if err := s.DeleteNetworkObject(ctx, obj.ID); err != nil {
			t.Fatalf("DeleteNetworkObject: %v", err)
		}
		rels, err := s.ListNetworkRelationships(ctx, domain.NetworkRelationshipFilters{IDs: []string{"rel-other-object", "rel-pool-same-id"}})
		if err != nil || len(rels) != 2 {
			t.Fatalf("unrelated relationships after delete = %+v err=%v, want both kept", rels, err)
		}
	})

	t.Run("child still blocks and the relationship survives the blocked delete", func(t *testing.T) {
		s := newNetworkStoreForExtraTests()
		parent := mustCreateNetworkObjectExtra(t, s, domain.CreateNetworkObject{AccountID: 1, Name: "vpc"})
		mustCreateNetworkObjectExtra(t, s, domain.CreateNetworkObject{AccountID: 1, Name: "subnet", ParentObjectID: &parent.ID})
		rel := domain.CreateNetworkRelationship{ID: "rel-parent", Type: domain.NetworkRelationshipContains, SourceKind: domain.NetworkEntityKindObject, SourceID: strconv.FormatInt(parent.ID, 10), TargetKind: "pool", TargetID: "7"}
		if _, err := s.UpsertNetworkRelationship(ctx, rel); err != nil {
			t.Fatalf("UpsertNetworkRelationship: %v", err)
		}
		err := s.DeleteNetworkObject(ctx, parent.ID)
		if !errors.Is(err, ErrConflict) {
			t.Fatalf("error = %v, want ErrConflict", err)
		}
		if strings.Contains(err.Error(), "relationship") {
			t.Fatalf("conflict message should mention only children: %v", err)
		}
		rels, lerr := s.ListNetworkRelationships(ctx, domain.NetworkRelationshipFilters{IDs: []string{rel.ID}})
		if lerr != nil || len(rels) != 1 {
			t.Fatalf("relationships after blocked delete = %+v err=%v, want it kept", rels, lerr)
		}
	})
}
