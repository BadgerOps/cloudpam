//go:build postgres

package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"cloudpam/internal/domain"
	"cloudpam/internal/storage"
	"cloudpam/internal/storage/storagetest"
)

func TestPostgresRestore(t *testing.T) {
	resetDB(t)
	storagetest.RunRestoreSuite(t, testDB.store)
}

// restoreWhileDeleting restores pool id under the placement lock, starts del while the
// restore is uncommitted, and gives del time to finish before committing. It returns
// del's result once both have committed.
func restoreWhileDeleting(t *testing.T, s *Store, id int64, del func() (bool, error)) (bool, error) {
	t.Helper()
	type result struct {
		ok  bool
		err error
	}
	done := make(chan result, 1)
	err := s.WithPlacementLock(context.Background(), func(ps storage.PlacementStore) error {
		if _, ok, err := ps.RestorePool(context.Background(), id); err != nil || !ok {
			t.Fatalf("RestorePool(%d) = %v, %v", id, ok, err)
		}
		go func() {
			ok, err := del()
			done <- result{ok, err}
		}()
		select {
		case r := <-done:
			done <- r
		case <-time.After(500 * time.Millisecond):
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WithPlacementLock(): %v", err)
	}
	r := <-done
	return r.ok, r.err
}

func TestPostgresRestoreSerializesWithDelete(t *testing.T) {
	ctx := context.Background()

	t.Run("cascade delete of the parent takes the restored child", func(t *testing.T) {
		resetDB(t)
		s := testDB.store
		parent := mustCreatePool(t, s, "parent", "100.80.0.0/16", nil)
		child := mustCreatePool(t, s, "child", "100.80.1.0/24", &parent.ID)
		if ok, err := s.DeletePool(ctx, child.ID); err != nil || !ok {
			t.Fatalf("DeletePool(child) = %v, %v", ok, err)
		}

		ok, err := restoreWhileDeleting(t, s, child.ID, func() (bool, error) { return s.DeletePoolCascade(ctx, parent.ID) })
		if err != nil || !ok {
			t.Fatalf("DeletePoolCascade(parent) = %v, %v", ok, err)
		}
		if _, live, err := s.GetPool(ctx, child.ID); err != nil || live {
			t.Fatalf("GetPool(child) live = %v, err = %v; want the child deleted with its parent", live, err)
		}
	})

	t.Run("account delete sees the restored pool", func(t *testing.T) {
		resetDB(t)
		s := testDB.store
		acct, err := s.CreateAccount(ctx, domain.CreateAccount{Key: "aws:restore-race", Name: "restore race"})
		if err != nil {
			t.Fatalf("CreateAccount(): %v", err)
		}
		pool, err := s.CreatePool(ctx, domain.CreatePool{Name: "owned", CIDR: "100.81.0.0/24", AccountID: &acct.ID})
		if err != nil {
			t.Fatalf("CreatePool(): %v", err)
		}
		if ok, err := s.DeletePool(ctx, pool.ID); err != nil || !ok {
			t.Fatalf("DeletePool() = %v, %v", ok, err)
		}

		ok, err := restoreWhileDeleting(t, s, pool.ID, func() (bool, error) { return s.DeleteAccount(ctx, acct.ID) })
		if !errors.Is(err, storage.ErrConflict) || ok {
			t.Fatalf("DeleteAccount() = %v, %v; want ErrConflict because the restored pool uses the account", ok, err)
		}
		if _, live, err := s.GetAccount(ctx, acct.ID); err != nil || !live {
			t.Fatalf("GetAccount() live = %v, err = %v; want the account kept", live, err)
		}
	})
}
