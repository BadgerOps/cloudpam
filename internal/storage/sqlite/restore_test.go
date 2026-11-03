//go:build sqlite

package sqlite

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"cloudpam/internal/domain"

	"cloudpam/internal/storage/storagetest"
)

func TestSQLiteRestore(t *testing.T) {
	storagetest.RunRestoreSuite(t, newTestStore(t))
}

func TestSQLiteRestoreSerializesWithDelete(t *testing.T) {
	ctx := context.Background()
	race := func(restore, del func() error) {
		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); _ = restore() }()
		go func() { defer wg.Done(); _ = del() }()
		wg.Wait()
	}

	t.Run("parent delete", func(t *testing.T) {
		s := newTestStore(t)
		for i := 0; i < 200; i++ {
			parent, err := s.CreatePool(ctx, domain.CreatePool{Name: "parent", CIDR: fmt.Sprintf("10.%d.0.0/16", i)})
			if err != nil {
				t.Fatalf("CreatePool(parent): %v", err)
			}
			child, err := s.CreatePool(ctx, domain.CreatePool{Name: "child", CIDR: fmt.Sprintf("10.%d.1.0/24", i), ParentID: &parent.ID})
			if err != nil {
				t.Fatalf("CreatePool(child): %v", err)
			}
			if ok, err := s.DeletePool(ctx, child.ID); err != nil || !ok {
				t.Fatalf("DeletePool(child) = %v, %v", ok, err)
			}
			race(func() error { _, _, err := s.RestorePool(ctx, child.ID); return err },
				func() error { _, err := s.DeletePool(ctx, parent.ID); return err })
			_, childLive, _ := s.GetPool(ctx, child.ID)
			_, parentLive, _ := s.GetPool(ctx, parent.ID)
			if childLive && !parentLive {
				t.Fatalf("iteration %d: restored child %d is live under deleted parent %d", i, child.ID, parent.ID)
			}
		}
	})

	t.Run("account delete", func(t *testing.T) {
		s := newTestStore(t)
		for i := 0; i < 200; i++ {
			acct, err := s.CreateAccount(ctx, domain.CreateAccount{Key: fmt.Sprintf("aws:race-%d", i), Name: "race"})
			if err != nil {
				t.Fatalf("CreateAccount(): %v", err)
			}
			pool, err := s.CreatePool(ctx, domain.CreatePool{Name: "owned", CIDR: fmt.Sprintf("10.%d.0.0/24", i), AccountID: &acct.ID})
			if err != nil {
				t.Fatalf("CreatePool(): %v", err)
			}
			if ok, err := s.DeletePool(ctx, pool.ID); err != nil || !ok {
				t.Fatalf("DeletePool() = %v, %v", ok, err)
			}
			race(func() error { _, _, err := s.RestorePool(ctx, pool.ID); return err },
				func() error { _, err := s.DeleteAccount(ctx, acct.ID); return err })
			_, poolLive, _ := s.GetPool(ctx, pool.ID)
			_, acctLive, _ := s.GetAccount(ctx, acct.ID)
			if poolLive && !acctLive {
				t.Fatalf("iteration %d: restored pool %d is live under deleted account %d", i, pool.ID, acct.ID)
			}
		}
	})
}
