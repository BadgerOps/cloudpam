//go:build postgres

package postgres

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"cloudpam/internal/domain"
	"cloudpam/internal/storage"
)

func TestPostgresDeleteNetworkObject(t *testing.T) {
	resetDB(t)
	ctx := context.Background()
	s := testDB.store

	account, err := s.CreateAccount(ctx, domain.CreateAccount{Key: "aws:111111111111", Name: "prod", Provider: "aws"})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	newObject := func(t *testing.T, in domain.CreateNetworkObject) domain.NetworkObject {
		t.Helper()
		in.AccountID = account.ID
		obj, err := s.CreateNetworkObject(ctx, in)
		if err != nil {
			t.Fatalf("CreateNetworkObject(%s): %v", in.Name, err)
		}
		return obj
	}

	t.Run("unreferenced object is deleted", func(t *testing.T) {
		obj := newObject(t, domain.CreateNetworkObject{Name: "lone-vpc"})
		if err := s.DeleteNetworkObject(ctx, obj.ID); err != nil {
			t.Fatalf("DeleteNetworkObject: %v", err)
		}
		if _, found, err := s.GetNetworkObject(ctx, obj.ID); err != nil || found {
			t.Fatalf("GetNetworkObject after delete: found=%v err=%v, want not found", found, err)
		}
	})

	t.Run("unknown id is not found", func(t *testing.T) {
		if err := s.DeleteNetworkObject(ctx, 999999); !errors.Is(err, storage.ErrNotFound) {
			t.Fatalf("error = %v, want ErrNotFound", err)
		}
	})

	t.Run("object with a child conflicts and the child keeps its parent", func(t *testing.T) {
		parent := newObject(t, domain.CreateNetworkObject{Name: "parent-vpc"})
		child := newObject(t, domain.CreateNetworkObject{Name: "child-subnet", ParentObjectID: &parent.ID})
		if err := s.DeleteNetworkObject(ctx, parent.ID); !errors.Is(err, storage.ErrConflict) {
			t.Fatalf("error = %v, want ErrConflict", err)
		}
		got, found, err := s.GetNetworkObject(ctx, child.ID)
		if err != nil || !found || got.ParentObjectID == nil || *got.ParentObjectID != parent.ID {
			t.Fatalf("child after blocked delete = %+v found=%v err=%v, want parent %d kept", got, found, err, parent.ID)
		}
		if err := s.DeleteNetworkObject(ctx, child.ID); err != nil {
			t.Fatalf("DeleteNetworkObject(child): %v", err)
		}
		if err := s.DeleteNetworkObject(ctx, parent.ID); err != nil {
			t.Fatalf("DeleteNetworkObject(parent) after child removed: %v", err)
		}
	})

	relationshipIDs := func(t *testing.T, ids ...string) []domain.NetworkRelationship {
		t.Helper()
		rels, err := s.ListNetworkRelationships(ctx, domain.NetworkRelationshipFilters{IDs: ids})
		if err != nil {
			t.Fatalf("ListNetworkRelationships: %v", err)
		}
		return rels
	}

	for _, side := range []string{"source", "target"} {
		t.Run("relationship referencing the object as "+side+" is removed", func(t *testing.T) {
			obj := newObject(t, domain.CreateNetworkObject{Name: "related-" + side})
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
			if rels := relationshipIDs(t, rel.ID); len(rels) != 0 {
				t.Fatalf("relationships after delete = %+v, want none", rels)
			}
		})
	}

	t.Run("relationships for other objects or another kind with the same id are untouched", func(t *testing.T) {
		obj := newObject(t, domain.CreateNetworkObject{Name: "cascade-vpc"})
		other := newObject(t, domain.CreateNetworkObject{Name: "other-vpc"})
		idText := strconv.FormatInt(obj.ID, 10)
		for _, rel := range []domain.CreateNetworkRelationship{
			{ID: "rel-other-object", Type: domain.NetworkRelationshipContains, SourceKind: domain.NetworkEntityKindObject, SourceID: strconv.FormatInt(other.ID, 10), TargetKind: "pool", TargetID: idText},
			{ID: "rel-pool-same-id", Type: domain.NetworkRelationshipContains, SourceKind: "pool", SourceID: idText, TargetKind: "discovered", TargetID: "x"},
		} {
			if _, err := s.UpsertNetworkRelationship(ctx, rel); err != nil {
				t.Fatalf("UpsertNetworkRelationship(%s): %v", rel.ID, err)
			}
		}
		if err := s.DeleteNetworkObject(ctx, obj.ID); err != nil {
			t.Fatalf("DeleteNetworkObject: %v", err)
		}
		if rels := relationshipIDs(t, "rel-other-object", "rel-pool-same-id"); len(rels) != 2 {
			t.Fatalf("unrelated relationships after delete = %+v, want both kept", rels)
		}
	})

	t.Run("child still blocks and the relationship survives the blocked delete", func(t *testing.T) {
		parent := newObject(t, domain.CreateNetworkObject{Name: "blocked-vpc"})
		newObject(t, domain.CreateNetworkObject{Name: "blocking-subnet", ParentObjectID: &parent.ID})
		rel := domain.CreateNetworkRelationship{ID: "rel-blocked", Type: domain.NetworkRelationshipContains, SourceKind: domain.NetworkEntityKindObject, SourceID: strconv.FormatInt(parent.ID, 10), TargetKind: "pool", TargetID: "7"}
		if _, err := s.UpsertNetworkRelationship(ctx, rel); err != nil {
			t.Fatalf("UpsertNetworkRelationship: %v", err)
		}
		err := s.DeleteNetworkObject(ctx, parent.ID)
		if !errors.Is(err, storage.ErrConflict) {
			t.Fatalf("error = %v, want ErrConflict", err)
		}
		if strings.Contains(err.Error(), "relationship") {
			t.Fatalf("conflict message should mention only children: %v", err)
		}
		if rels := relationshipIDs(t, rel.ID); len(rels) != 1 {
			t.Fatalf("relationships after blocked delete = %+v, want it kept", rels)
		}
	})

	t.Run("relationship in another organization is untouched", func(t *testing.T) {
		orgB := "00000000-0000-0000-0000-000000000333"
		if _, err := testDB.pool.Exec(ctx, `INSERT INTO organizations (id, name, slug, plan)
			VALUES ($1, 'Delete Org', 'delete-org', 'free')
			ON CONFLICT (id) DO NOTHING`, orgB); err != nil {
			t.Fatalf("create second org: %v", err)
		}
		storeB := &Store{pool: testDB.pool, orgID: orgB}
		obj := newObject(t, domain.CreateNetworkObject{Name: "org-a-vpc"})
		// UpsertNetworkRelationship now rejects an object id from another
		// organization, so seed the row directly: the delete must still scope
		// its relationship cleanup to its own organization.
		if _, err := testDB.pool.Exec(ctx, `INSERT INTO network_relationships
			(id, organization_id, type, source_kind, source_id, target_kind, target_id, created_at, updated_at)
			VALUES ('rel-other-org', $1, $2, $3, $4, 'pool', '1', NOW(), NOW())`,
			orgB, string(domain.NetworkRelationshipContains), domain.NetworkEntityKindObject, strconv.FormatInt(obj.ID, 10)); err != nil {
			t.Fatalf("seed org-b relationship: %v", err)
		}
		if err := s.DeleteNetworkObject(ctx, obj.ID); err != nil {
			t.Fatalf("DeleteNetworkObject: %v", err)
		}
		if rels, err := storeB.ListNetworkRelationships(ctx, domain.NetworkRelationshipFilters{IDs: []string{"rel-other-org"}}); err != nil || len(rels) != 1 {
			t.Fatalf("org-b relationship after org-a delete = %+v err=%v, want it kept", rels, err)
		}
		if err := storeB.DeleteNetworkObject(ctx, obj.ID); !errors.Is(err, storage.ErrNotFound) {
			t.Fatalf("cross-org delete error = %v, want ErrNotFound", err)
		}
	})

	t.Run("linked pool and discovered resource survive the delete", func(t *testing.T) {
		pool, err := s.CreatePool(ctx, domain.CreatePool{Name: "linked-pool", CIDR: "10.70.0.0/16", AccountID: &account.ID})
		if err != nil {
			t.Fatalf("create pool: %v", err)
		}
		now := time.Now().UTC()
		resourceID := uuid.New()
		if err := s.UpsertDiscoveredResource(ctx, domain.DiscoveredResource{ID: resourceID, AccountID: account.ID, Provider: "aws", Region: "us-east-1", ResourceType: domain.ResourceTypeVPC, ResourceID: "vpc-linked", CIDR: "10.70.0.0/16", Status: domain.DiscoveryStatusActive, DiscoveredAt: now, LastSeenAt: now}); err != nil {
			t.Fatalf("upsert discovered resource: %v", err)
		}
		obj := newObject(t, domain.CreateNetworkObject{Name: "linked-vpc", PoolID: &pool.ID, SourceDiscoveredID: &resourceID})
		if err := s.DeleteNetworkObject(ctx, obj.ID); err != nil {
			t.Fatalf("DeleteNetworkObject: %v", err)
		}
		if _, found, err := s.GetPool(ctx, pool.ID); err != nil || !found {
			t.Fatalf("linked pool after delete: found=%v err=%v, want it kept", found, err)
		}
		if _, err := s.GetDiscoveredResource(ctx, resourceID); err != nil {
			t.Fatalf("linked discovered resource after delete: %v, want it kept", err)
		}
	})
}
