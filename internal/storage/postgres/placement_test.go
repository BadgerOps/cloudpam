//go:build postgres

package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"cloudpam/internal/domain"
	"cloudpam/internal/storage"
)

func otherReplica(t *testing.T, runtimeParams map[string]string) *Store {
	t.Helper()
	cfg, err := pgxpool.ParseConfig(testDB.connStr)
	if err != nil {
		t.Fatalf("parse config: %v", err)
	}
	for k, v := range runtimeParams {
		cfg.ConnConfig.RuntimeParams[k] = v
	}
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatalf("second pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return NewFromPool(pool)
}

// holdPlacementLock takes the placement lock through st and keeps it until release is called.
// fn runs right after the lock is granted; its error becomes WithPlacementLock's.
func holdPlacementLock(t *testing.T, st *Store, fn func(storage.PlacementStore) error) (release func() error) {
	t.Helper()
	held := make(chan struct{})
	stop := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- st.WithPlacementLock(context.Background(), func(ps storage.PlacementStore) error {
			err := fn(ps)
			close(held)
			<-stop
			return err
		})
	}()
	select {
	case <-held:
	case err := <-done:
		t.Fatalf("holder returned before holding the lock: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("holder did not get the lock within 5s")
	}
	return func() error {
		close(stop)
		return <-done
	}
}

func waitForAdvisoryWaiter(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		var waiting int
		if err := testDB.pool.QueryRow(context.Background(), `
			SELECT count(*) FROM pg_locks
			WHERE locktype = 'advisory' AND NOT granted
			  AND database = (SELECT oid FROM pg_database WHERE datname = current_database())`).Scan(&waiting); err != nil {
			t.Fatalf("pg_locks: %v", err)
		}
		if waiting > 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("no session waiting on an advisory lock after 5s")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func cidrs(pools []domain.Pool) []string {
	out := make([]string, 0, len(pools))
	for _, p := range pools {
		out = append(out, p.CIDR)
	}
	return out
}

func TestWithPlacementLock_WaiterSeesHolderPoolsUnderRepeatableReadDefault(t *testing.T) {
	resetDB(t)
	ctx := context.Background()
	waiterStore := otherReplica(t, map[string]string{"default_transaction_isolation": "repeatable read"})

	release := holdPlacementLock(t, testDB.store, func(ps storage.PlacementStore) error {
		_, err := ps.CreatePool(ctx, domain.CreatePool{Name: "first", CIDR: "10.1.0.0/16"})
		return err
	})

	seen := make(chan []domain.Pool, 1)
	waiter := make(chan error, 1)
	go func() {
		waiter <- waiterStore.WithPlacementLock(ctx, func(ps storage.PlacementStore) error {
			pools, err := ps.ListPools(ctx)
			seen <- pools
			return err
		})
	}()
	waitForAdvisoryWaiter(t)
	if err := release(); err != nil {
		t.Fatalf("holder: %v", err)
	}
	if err := <-waiter; err != nil {
		t.Fatalf("waiter: %v", err)
	}
	if got := cidrs(<-seen); len(got) != 1 || got[0] != "10.1.0.0/16" {
		t.Fatalf("waiter listed %v, want the holder's 10.1.0.0/16", got)
	}
}

func TestWithPlacementLock_FnErrorStoresNothing(t *testing.T) {
	resetDB(t)
	ctx := context.Background()
	errStop := errors.New("stop")

	err := testDB.store.WithPlacementLock(ctx, func(ps storage.PlacementStore) error {
		if _, err := ps.CreatePool(ctx, domain.CreatePool{Name: "dropped", CIDR: "10.1.0.0/16"}); err != nil {
			return err
		}
		return errStop
	})
	if !errors.Is(err, errStop) {
		t.Fatalf("want fn's error back, got %v", err)
	}
	pools, err := testDB.store.ListPools(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(pools) != 0 {
		t.Fatalf("fn failed but %v were stored", cidrs(pools))
	}

	quick, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := otherReplica(t, nil).WithPlacementLock(quick, func(storage.PlacementStore) error { return nil }); err != nil {
		t.Fatalf("lock not free after fn failed: %v", err)
	}
}

func TestWithPlacementLock_CancelledInsideFnStoresNothing(t *testing.T) {
	resetDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := testDB.store.WithPlacementLock(ctx, func(ps storage.PlacementStore) error {
		_, err := ps.CreatePool(ctx, domain.CreatePool{Name: "dropped", CIDR: "10.1.0.0/16"})
		cancel()
		return err
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
	pools, err := testDB.store.ListPools(context.Background())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(pools) != 0 {
		t.Fatalf("cancelled placement stored %v", cidrs(pools))
	}
}

func TestWithPlacementLock_LockWaitTimeoutIsErrBusy(t *testing.T) {
	resetDB(t)
	old := placementLockWait
	placementLockWait = "200ms"
	t.Cleanup(func() { placementLockWait = old })

	release := holdPlacementLock(t, testDB.store, func(storage.PlacementStore) error { return nil })
	defer func() { _ = release() }()

	called := false
	err := otherReplica(t, nil).WithPlacementLock(context.Background(), func(storage.PlacementStore) error {
		called = true
		return nil
	})
	if !errors.Is(err, storage.ErrBusy) {
		t.Fatalf("want storage.ErrBusy, got %v", err)
	}
	if called {
		t.Fatal("fn ran without the lock")
	}
}

func TestWithPlacementLock_IdleHolderIsEndedAndReportsBusy(t *testing.T) {
	resetDB(t)
	old := placementIdleLimit
	placementIdleLimit = "200ms"
	t.Cleanup(func() { placementIdleLimit = old })

	err := testDB.store.WithPlacementLock(context.Background(), func(ps storage.PlacementStore) error {
		if _, err := ps.ListPools(context.Background()); err != nil {
			return err
		}
		time.Sleep(600 * time.Millisecond)
		_, err := ps.CreatePool(context.Background(), domain.CreatePool{Name: "late", CIDR: "10.1.0.0/16"})
		return err
	})
	if !errors.Is(err, storage.ErrBusy) {
		t.Fatalf("want storage.ErrBusy after the idle limit, got %v", err)
	}
	pools, err := testDB.store.ListPools(context.Background())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(pools) != 0 {
		t.Fatalf("ended placement stored %v", cidrs(pools))
	}
}

func TestWithPlacementLock_TerminatedHolderStoresNothingAndFreesLock(t *testing.T) {
	resetDB(t)
	ctx := context.Background()

	var holderPID int
	terminated := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- testDB.store.WithPlacementLock(ctx, func(ps storage.PlacementStore) error {
			if _, err := ps.CreatePool(ctx, domain.CreatePool{Name: "doomed", CIDR: "10.9.0.0/16"}); err != nil {
				return err
			}
			<-terminated
			_, _ = ps.CreatePool(ctx, domain.CreatePool{Name: "after", CIDR: "10.10.0.0/16"})
			return nil
		})
	}()

	deadline := time.Now().Add(5 * time.Second)
	for holderPID == 0 {
		_ = testDB.pool.QueryRow(ctx, `
			SELECT pid FROM pg_locks
			WHERE locktype = 'advisory' AND granted
			  AND database = (SELECT oid FROM pg_database WHERE datname = current_database())`).Scan(&holderPID)
		if holderPID == 0 && time.Now().After(deadline) {
			t.Fatal("holder never got the lock")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := testDB.pool.Exec(ctx, `SELECT pg_terminate_backend($1)`, holderPID); err != nil {
		t.Fatalf("terminate holder: %v", err)
	}
	close(terminated)
	if err := <-done; err == nil {
		t.Fatal("holder's session was terminated but WithPlacementLock reported success")
	}

	quick, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var seen []domain.Pool
	if err := otherReplica(t, nil).WithPlacementLock(quick, func(ps storage.PlacementStore) error {
		var err error
		seen, err = ps.ListPools(quick)
		return err
	}); err != nil {
		t.Fatalf("lock not free after the holder's session ended: %v", err)
	}
	if len(seen) != 0 {
		t.Fatalf("terminated holder's pools were stored: %v", cidrs(seen))
	}
}

func TestWithPlacementLock_FailedInsertKeepsTheRestOfTheBatch(t *testing.T) {
	resetDB(t)
	ctx := context.Background()
	if _, err := testDB.store.CreatePool(ctx, domain.CreatePool{Name: "existing", CIDR: "10.0.0.0/16"}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	var dupErr error
	err := testDB.store.WithPlacementLock(ctx, func(ps storage.PlacementStore) error {
		_, dupErr = ps.CreatePool(ctx, domain.CreatePool{Name: "dup", CIDR: "10.0.0.0/16"})
		_, err := ps.CreatePool(ctx, domain.CreatePool{Name: "next", CIDR: "10.1.0.0/16"})
		return err
	})
	if err != nil {
		t.Fatalf("batch after a duplicate: %v", err)
	}
	if !errors.Is(dupErr, storage.ErrConflict) {
		t.Fatalf("duplicate: want storage.ErrConflict, got %v", dupErr)
	}
	pools, err := testDB.store.ListPools(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if got := cidrs(pools); len(got) != 2 || got[1] != "10.1.0.0/16" {
		t.Fatalf("want the seed and 10.1.0.0/16 stored, got %v", got)
	}
}
