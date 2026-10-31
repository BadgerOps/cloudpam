package storage

import (
	"context"
	"errors"
	"strconv"
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
		t.Run("object referenced as relationship "+side+" conflicts until unlinked", func(t *testing.T) {
			s := newNetworkStoreForExtraTests()
			obj := mustCreateNetworkObjectExtra(t, s, domain.CreateNetworkObject{AccountID: 1, Name: "vpc"})
			rel := domain.CreateNetworkRelationship{ID: "rel-" + side, Type: domain.NetworkRelationshipContains, SourceKind: "pool", SourceID: "7", TargetKind: domain.NetworkEntityKindObject, TargetID: strconv.FormatInt(obj.ID, 10)}
			if side == "source" {
				rel.SourceKind, rel.SourceID, rel.TargetKind, rel.TargetID = rel.TargetKind, rel.TargetID, rel.SourceKind, rel.SourceID
			}
			if _, err := s.UpsertNetworkRelationship(ctx, rel); err != nil {
				t.Fatalf("UpsertNetworkRelationship: %v", err)
			}
			if err := s.DeleteNetworkObject(ctx, obj.ID); !errors.Is(err, ErrConflict) {
				t.Fatalf("error = %v, want ErrConflict", err)
			}
			// The store API has no relationship delete; drop the row directly to model unlinking.
			s.store.mu.Lock()
			delete(s.relationships, rel.ID)
			s.store.mu.Unlock()
			if err := s.DeleteNetworkObject(ctx, obj.ID); err != nil {
				t.Fatalf("DeleteNetworkObject after unlink: %v", err)
			}
		})
	}

	t.Run("relationship naming the same id under another kind does not block", func(t *testing.T) {
		s := newNetworkStoreForExtraTests()
		obj := mustCreateNetworkObjectExtra(t, s, domain.CreateNetworkObject{AccountID: 1, Name: "vpc"})
		if _, err := s.UpsertNetworkRelationship(ctx, domain.CreateNetworkRelationship{Type: domain.NetworkRelationshipContains, SourceKind: "pool", SourceID: strconv.FormatInt(obj.ID, 10), TargetKind: "discovered", TargetID: "x"}); err != nil {
			t.Fatalf("UpsertNetworkRelationship: %v", err)
		}
		if err := s.DeleteNetworkObject(ctx, obj.ID); err != nil {
			t.Fatalf("DeleteNetworkObject: %v", err)
		}
	})
}
