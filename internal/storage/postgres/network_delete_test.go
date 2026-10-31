//go:build postgres

package postgres

import (
	"context"
	"errors"
	"strconv"
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

	for _, side := range []string{"source", "target"} {
		t.Run("object referenced as relationship "+side+" conflicts until unlinked", func(t *testing.T) {
			obj := newObject(t, domain.CreateNetworkObject{Name: "related-" + side})
			rel := domain.CreateNetworkRelationship{ID: "rel-" + side, Type: domain.NetworkRelationshipContains, SourceKind: "pool", SourceID: "7", TargetKind: domain.NetworkEntityKindObject, TargetID: strconv.FormatInt(obj.ID, 10)}
			if side == "source" {
				rel.SourceKind, rel.SourceID, rel.TargetKind, rel.TargetID = rel.TargetKind, rel.TargetID, rel.SourceKind, rel.SourceID
			}
			if _, err := s.UpsertNetworkRelationship(ctx, rel); err != nil {
				t.Fatalf("UpsertNetworkRelationship: %v", err)
			}
			if err := s.DeleteNetworkObject(ctx, obj.ID); !errors.Is(err, storage.ErrConflict) {
				t.Fatalf("error = %v, want ErrConflict", err)
			}
			// The store API has no relationship delete; drop the row directly to model unlinking.
			if _, err := testDB.pool.Exec(ctx, `DELETE FROM network_relationships WHERE id = $1 AND organization_id = $2`, rel.ID, s.orgID); err != nil {
				t.Fatalf("delete relationship row: %v", err)
			}
			if err := s.DeleteNetworkObject(ctx, obj.ID); err != nil {
				t.Fatalf("DeleteNetworkObject after unlink: %v", err)
			}
		})
	}

	t.Run("relationship in another organization does not block", func(t *testing.T) {
		orgB := "00000000-0000-0000-0000-000000000333"
		if _, err := testDB.pool.Exec(ctx, `INSERT INTO organizations (id, name, slug, plan)
			VALUES ($1, 'Delete Org', 'delete-org', 'free')
			ON CONFLICT (id) DO NOTHING`, orgB); err != nil {
			t.Fatalf("create second org: %v", err)
		}
		storeB := &Store{pool: testDB.pool, orgID: orgB}
		obj := newObject(t, domain.CreateNetworkObject{Name: "org-a-vpc"})
		if _, err := storeB.UpsertNetworkRelationship(ctx, domain.CreateNetworkRelationship{ID: "rel-other-org", Type: domain.NetworkRelationshipContains, SourceKind: domain.NetworkEntityKindObject, SourceID: strconv.FormatInt(obj.ID, 10), TargetKind: "pool", TargetID: "1"}); err != nil {
			t.Fatalf("create org-b relationship: %v", err)
		}
		if err := s.DeleteNetworkObject(ctx, obj.ID); err != nil {
			t.Fatalf("DeleteNetworkObject: %v", err)
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
