//go:build sqlite

package sqlite

import (
	"errors"
	"strconv"
	"strings"
	"testing"

	"cloudpam/internal/domain"
	"cloudpam/internal/storage"
)

func TestSQLiteUpsertNetworkRelationshipObjectRefs(t *testing.T) {
	s := newTestStore(t)
	ctx := timeoutCtx(t)

	account, err := s.CreateAccount(ctx, domain.CreateAccount{Key: "aws:111111111111", Name: "prod", Provider: "aws"})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	rel := func(id, sourceKind, sourceID, targetKind, targetID string) domain.CreateNetworkRelationship {
		return domain.CreateNetworkRelationship{ID: id, Type: domain.NetworkRelationshipContains, SourceKind: sourceKind, SourceID: sourceID, TargetKind: targetKind, TargetID: targetID}
	}
	listed := func(t *testing.T, id string) []domain.NetworkRelationship {
		t.Helper()
		rels, err := s.ListNetworkRelationships(ctx, domain.NetworkRelationshipFilters{IDs: []string{id}})
		if err != nil {
			t.Fatalf("ListNetworkRelationships: %v", err)
		}
		return rels
	}

	t.Run("unknown object id as source is a validation error", func(t *testing.T) {
		_, err := s.UpsertNetworkRelationship(ctx, rel("rel-missing-source", domain.NetworkEntityKindObject, "424242", "pool", "7"))
		if !errors.Is(err, storage.ErrValidation) {
			t.Fatalf("error = %v, want ErrValidation", err)
		}
		if !strings.Contains(err.Error(), "network object 424242 not found") {
			t.Fatalf("error message = %q, want it to name the missing object", err)
		}
		if rels := listed(t, "rel-missing-source"); len(rels) != 0 {
			t.Fatalf("relationships after rejected upsert = %+v, want none", rels)
		}
	})

	t.Run("unknown object id as target is a validation error", func(t *testing.T) {
		_, err := s.UpsertNetworkRelationship(ctx, rel("rel-missing-target", "pool", "7", domain.NetworkEntityKindObject, "424242"))
		if !errors.Is(err, storage.ErrValidation) {
			t.Fatalf("error = %v, want ErrValidation", err)
		}
		if rels := listed(t, "rel-missing-target"); len(rels) != 0 {
			t.Fatalf("relationships after rejected upsert = %+v, want none", rels)
		}
	})

	t.Run("non-numeric object id is a validation error", func(t *testing.T) {
		_, err := s.UpsertNetworkRelationship(ctx, rel("rel-text-id", domain.NetworkEntityKindObject, "vpc-abc", "pool", "7"))
		if !errors.Is(err, storage.ErrValidation) {
			t.Fatalf("error = %v, want ErrValidation", err)
		}
		if !strings.Contains(err.Error(), "network object vpc-abc not found") {
			t.Fatalf("error message = %q, want it to name the missing object", err)
		}
	})

	t.Run("non-canonical object id is a validation error", func(t *testing.T) {
		obj, err := s.CreateNetworkObject(ctx, domain.CreateNetworkObject{AccountID: account.ID, Name: "padded-vpc"})
		if err != nil {
			t.Fatalf("CreateNetworkObject(padded-vpc): %v", err)
		}
		padded := "0" + strconv.FormatInt(obj.ID, 10)
		_, err = s.UpsertNetworkRelationship(ctx, rel("rel-padded-id", domain.NetworkEntityKindObject, padded, "pool", "7"))
		if !errors.Is(err, storage.ErrValidation) {
			t.Fatalf("error = %v, want ErrValidation", err)
		}
		if !strings.Contains(err.Error(), "network object "+padded+" not found") {
			t.Fatalf("error message = %q, want it to name the missing object", err)
		}
		if rels := listed(t, "rel-padded-id"); len(rels) != 0 {
			t.Fatalf("relationships after rejected upsert = %+v, want none", rels)
		}
	})

	t.Run("existing object succeeds on both sides", func(t *testing.T) {
		src, err := s.CreateNetworkObject(ctx, domain.CreateNetworkObject{AccountID: account.ID, Name: "vpc"})
		if err != nil {
			t.Fatalf("CreateNetworkObject(vpc): %v", err)
		}
		dst, err := s.CreateNetworkObject(ctx, domain.CreateNetworkObject{AccountID: account.ID, Name: "subnet"})
		if err != nil {
			t.Fatalf("CreateNetworkObject(subnet): %v", err)
		}
		got, err := s.UpsertNetworkRelationship(ctx, rel("rel-both-objects", domain.NetworkEntityKindObject, strconv.FormatInt(src.ID, 10), domain.NetworkEntityKindObject, strconv.FormatInt(dst.ID, 10)))
		if err != nil {
			t.Fatalf("UpsertNetworkRelationship: %v", err)
		}
		if got.SourceID != strconv.FormatInt(src.ID, 10) || got.TargetID != strconv.FormatInt(dst.ID, 10) {
			t.Fatalf("relationship = %+v, want it to reference both objects", got)
		}
	})

	t.Run("pool and discovered kinds are not checked", func(t *testing.T) {
		if _, err := s.UpsertNetworkRelationship(ctx, rel("rel-unchecked", "pool", "424242", "discovered", "not-a-uuid")); err != nil {
			t.Fatalf("UpsertNetworkRelationship(pool, discovered): %v", err)
		}
	})
}
