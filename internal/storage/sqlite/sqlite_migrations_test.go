//go:build sqlite

package sqlite

import (
    "context"
    "os"
    "path/filepath"
    "testing"

    "cloudpam/internal/domain"
)

func TestMigrationsAndConstraints(t *testing.T) {
    // Use a temp on-disk DB to exercise PRAGMAs and constraints
    dir := t.TempDir()
    dsn := "file:" + filepath.Join(dir, "test.db") + "?_fk=1&cache=shared"
    s, err := New(dsn)
    if err != nil { t.Fatalf("new sqlite store: %v", err) }
    t.Cleanup(func(){ _ = s.db.Close(); _ = os.RemoveAll(dir) })

    ctx := context.Background()
    // Unique (COALESCE(parent_id,-1), cidr)
    p1, err := s.CreatePool(ctx, domain.CreatePool{Name: "root1", CIDR: "10.0.0.0/16"})
    if err != nil { t.Fatalf("create pool: %v", err) }
    if _, err := s.CreatePool(ctx, domain.CreatePool{Name: "dupRoot", CIDR: p1.CIDR}); err == nil {
        t.Fatalf("expected unique constraint error for duplicate root cidr")
    }

    // FK: account must exist
    accID := int64(999999)
    if _, err := s.CreatePool(ctx, domain.CreatePool{Name: "bad", CIDR: "10.1.0.0/16", AccountID: &accID}); err == nil {
        t.Fatalf("expected FK error for non-existent account")
    }

    // Parent/child and delete restriction
    childCIDR := "10.0.1.0/24"
    child, err := s.CreatePool(ctx, domain.CreatePool{Name: "child", CIDR: childCIDR, ParentID: &p1.ID})
    if err != nil { t.Fatalf("create child: %v", err) }
    if _, err := s.DeletePool(ctx, p1.ID); err == nil {
        t.Fatalf("expected delete restriction when pool has child, got nil error")
    }
    // Cascade helper should delete both
    ok, err := s.DeletePoolCascade(ctx, p1.ID)
    if err != nil || !ok { t.Fatalf("cascade delete: %v ok=%v", err, ok) }
    if _, ok, _ := s.GetPool(ctx, child.ID); ok {
        t.Fatalf("expected child removed after cascade")
    }
}

