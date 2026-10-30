//go:build postgres

package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"cloudpam/internal/domain"
	"cloudpam/internal/storage"
)

// These are vars so tests can shorten them. placementDeadline bounds the whole
// placement: the lock wait, the list, the overlap check and every insert. With the 5s
// commit margin it fits inside the 15s HTTP write timeout in cmd/cloudpam/main.go, so a
// placement that would outrun the response is rolled back and reported busy instead of
// committing after the client has given up. lock_timeout and the idle limit are
// server-side backstops for a client that stops making progress.
var (
	placementDeadline  = 10 * time.Second
	placementLockWait  = "5s"
	placementIdleLimit = "5s"
)

const (
	placementEndLimit = 5 * time.Second
	lockNotAvailable  = "55P03"
	idleInTxTimeout   = "25P03"
)

var _ storage.PlacementLocker = (*Store)(nil)

// WithPlacementLock implements storage.PlacementLocker with a transaction-scoped advisory lock, so
// the lock, the reads, and the inserts share one session. If that session ends, PostgreSQL drops
// the lock and the inserts together.
func (s *Store) WithPlacementLock(ctx context.Context, fn func(storage.PlacementStore) error) error {
	work, cancel := context.WithTimeout(ctx, placementDeadline)
	defer cancel()

	// READ COMMITTED is pinned, not inherited from default_transaction_isolation. Under REPEATABLE
	// READ or SERIALIZABLE the snapshot is taken before the lock wait ends, so ListPools would miss
	// the pools the previous holder just committed.
	tx, err := s.pool.BeginTx(work, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return placementBusy(ctx, err)
	}
	defer func() { _ = endTx(ctx, tx.Rollback) }()

	if _, err := tx.Exec(work, `SELECT set_config('lock_timeout', $1, true), set_config('idle_in_transaction_session_timeout', $2, true)`,
		placementLockWait, placementIdleLimit); err != nil {
		return placementBusy(ctx, err)
	}
	if _, err := tx.Exec(work, `SELECT pg_advisory_xact_lock(hashtextextended('cloudpam/pool-placement/' || $1::text, 0))`, s.orgID); err != nil {
		return placementBusy(ctx, err)
	}
	if err := fn(placementTx{tx: tx, orgID: s.orgID, work: work, caller: ctx}); err != nil {
		return placementBusy(ctx, err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := work.Err(); err != nil {
		return placementBusy(ctx, err)
	}
	return placementBusy(ctx, endTx(ctx, tx.Commit))
}

func sqlState(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code
	}
	return ""
}

// placementBusy maps every way a placement runs out of time to ErrBusy: a lock wait past
// lock_timeout on any statement, the server ending the session for idling, or the
// placement deadline. In each case nothing was stored and the caller may retry. A
// cancellation that came from the caller is returned as is.
func placementBusy(caller context.Context, err error) error {
	if err == nil {
		return nil
	}
	switch sqlState(err) {
	case lockNotAvailable:
		return fmt.Errorf("pool placement waited longer than %s for a lock: %w", placementLockWait, storage.ErrBusy)
	case idleInTxTimeout:
		return fmt.Errorf("pool placement session ended after idling %s: %w", placementIdleLimit, storage.ErrBusy)
	}
	if errors.Is(err, context.DeadlineExceeded) && caller.Err() == nil {
		return fmt.Errorf("pool placement did not finish within %s: %w", placementDeadline, storage.ErrBusy)
	}
	return err
}

// endTx ends the transaction on a context detached from the request. A cancel that lands during
// COMMIT would otherwise close the connection and leave the caller unsure whether the pools exist.
func endTx(ctx context.Context, end func(context.Context) error) error {
	c, cancel := context.WithTimeout(context.WithoutCancel(ctx), placementEndLimit)
	defer cancel()
	if err := end(c); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
		return err
	}
	return nil
}

// placementTx runs its statements on work, the deadline-bounded context derived from the
// caller's, so every statement of the placement shares one budget.
type placementTx struct {
	tx     pgx.Tx
	orgID  string
	work   context.Context
	caller context.Context
}

func (t placementTx) ListPools(context.Context) ([]domain.Pool, error) {
	return listPools(t.work, t.tx, t.orgID)
}

// CreatePool runs each insert in its own savepoint. A failed INSERT aborts the whole PostgreSQL
// transaction, and the rest of a schema/apply batch must still be stored.
func (t placementTx) CreatePool(_ context.Context, in domain.CreatePool) (domain.Pool, error) {
	sp, err := t.tx.Begin(t.work)
	if err != nil {
		return domain.Pool{}, placementBusy(t.caller, err)
	}
	p, err := createPool(t.work, sp, t.orgID, in)
	if err != nil {
		_ = sp.Rollback(t.work)
		return domain.Pool{}, placementBusy(t.caller, err)
	}
	if err := sp.Commit(t.work); err != nil {
		return domain.Pool{}, placementBusy(t.caller, err)
	}
	return p, nil
}
