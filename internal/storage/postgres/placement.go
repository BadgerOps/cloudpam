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

// placementLockWait and placementIdleLimit are vars so tests can shorten them.
// PostgreSQL counts the time between ListPools and the first INSERT as idle, so the
// idle limit bounds the in-process overlap check. The two run one after the other,
// and together with the 5s commit margin they fit inside the 15s HTTP write timeout
// in cmd/cloudpam/main.go: a placement that would outrun the response is rolled back
// and reported busy instead of committing after the client has given up.
var (
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
	// READ COMMITTED is pinned, not inherited from default_transaction_isolation. Under REPEATABLE
	// READ or SERIALIZABLE the snapshot is taken before the lock wait ends, so ListPools would miss
	// the pools the previous holder just committed.
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return err
	}
	defer func() { _ = endTx(ctx, tx.Rollback) }()

	if _, err := tx.Exec(ctx, `SELECT set_config('lock_timeout', $1, true), set_config('idle_in_transaction_session_timeout', $2, true)`,
		placementLockWait, placementIdleLimit); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('cloudpam/pool-placement/' || $1::text, 0))`, s.orgID); err != nil {
		if sqlState(err) == lockNotAvailable {
			return fmt.Errorf("pool placement lock not acquired within %s: %w", placementLockWait, storage.ErrBusy)
		}
		return err
	}
	if err := fn(placementTx{tx: tx, orgID: s.orgID}); err != nil {
		return busyIfIdleKilled(err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return busyIfIdleKilled(endTx(ctx, tx.Commit))
}

func sqlState(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code
	}
	return ""
}

// busyIfIdleKilled maps the server ending the session for idling in the transaction to
// ErrBusy: nothing was stored and the caller may retry.
func busyIfIdleKilled(err error) error {
	if err != nil && sqlState(err) == idleInTxTimeout {
		return fmt.Errorf("pool placement session ended after idling %s: %w", placementIdleLimit, storage.ErrBusy)
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

type placementTx struct {
	tx    pgx.Tx
	orgID string
}

func (t placementTx) ListPools(ctx context.Context) ([]domain.Pool, error) {
	return listPools(ctx, t.tx, t.orgID)
}

// CreatePool runs each insert in its own savepoint. A failed INSERT aborts the whole PostgreSQL
// transaction, and the rest of a schema/apply batch must still be stored.
func (t placementTx) CreatePool(ctx context.Context, in domain.CreatePool) (domain.Pool, error) {
	sp, err := t.tx.Begin(ctx)
	if err != nil {
		return domain.Pool{}, busyIfIdleKilled(err)
	}
	p, err := createPool(ctx, sp, t.orgID, in)
	if err != nil {
		_ = sp.Rollback(ctx)
		return domain.Pool{}, busyIfIdleKilled(err)
	}
	if err := sp.Commit(ctx); err != nil {
		return domain.Pool{}, busyIfIdleKilled(err)
	}
	return p, nil
}
