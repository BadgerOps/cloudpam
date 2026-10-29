package storage

import (
	"context"
	"errors"
	"testing"

	"cloudpam/internal/domain"
)

func TestMemoryStore_CreatePool_DuplicateParentCIDR(t *testing.T) {
	ctx := context.Background()
	m := NewMemoryStore()
	root, err := m.CreatePool(ctx, domain.CreatePool{Name: "root", CIDR: "10.0.0.0/8"})
	if err != nil {
		t.Fatalf("create root: %v", err)
	}
	if _, err := m.CreatePool(ctx, domain.CreatePool{Name: "again", CIDR: "10.0.0.0/8"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("top-level duplicate: want ErrConflict, got %v", err)
	}
	child, err := m.CreatePool(ctx, domain.CreatePool{Name: "child", CIDR: "10.1.0.0/16", ParentID: &root.ID})
	if err != nil {
		t.Fatalf("create child: %v", err)
	}
	if _, err := m.CreatePool(ctx, domain.CreatePool{Name: "child again", CIDR: "10.1.0.0/16", ParentID: &root.ID}); !errors.Is(err, ErrConflict) {
		t.Fatalf("child duplicate: want ErrConflict, got %v", err)
	}
	if _, err := m.CreatePool(ctx, domain.CreatePool{Name: "overlap", CIDR: "10.0.0.0/9"}); err != nil {
		t.Fatalf("overlap must stay allowed in the store: %v", err)
	}
	if _, err := m.CreatePool(ctx, domain.CreatePool{Name: "same cidr, other parent", CIDR: "10.1.0.0/16", ParentID: &child.ID}); err != nil {
		t.Fatalf("same cidr under another parent: %v", err)
	}
	if ok, err := m.DeletePoolCascade(ctx, root.ID); err != nil || !ok {
		t.Fatalf("delete root: %v %v", ok, err)
	}
	if _, err := m.CreatePool(ctx, domain.CreatePool{Name: "after delete", CIDR: "10.0.0.0/8"}); err != nil {
		t.Fatalf("a deleted pool must not block its cidr: %v", err)
	}
}
