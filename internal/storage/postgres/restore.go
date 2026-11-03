//go:build postgres

package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"cloudpam/internal/domain"
	"cloudpam/internal/storage"
)

func (s *Store) ListDeletedPools(ctx context.Context) ([]domain.Pool, error) {
	rows, err := s.pool.Query(ctx, fmt.Sprintf(`
		SELECT p.%s, p.deleted_at
		FROM pools p
		WHERE p.organization_id = $1 AND p.deleted_at IS NOT NULL
		ORDER BY p.deleted_at DESC, p.seq_id DESC`, poolColumnsWithParentAccount()), s.orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Pool{}
	for rows.Next() {
		var deletedAt time.Time
		p, _, err := scanPool(rows, &deletedAt)
		if err != nil {
			return nil, err
		}
		p.DeletedAt = &deletedAt
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) RestorePool(ctx context.Context, id int64) (domain.Pool, bool, error) {
	var p domain.Pool
	var restored bool
	err := s.withPlacementTx(ctx, func(work context.Context, tx pgx.Tx) error {
		var err error
		p, restored, err = restorePool(work, tx, s.orgID, id)
		return err
	})
	return p, restored, err
}

// restorePool checks the parent and account in the UPDATE itself. Callers hold the
// placement lock, which every pool and account delete also takes, so a parent or
// account deleted after the check cannot leave a live pool under it.
func restorePool(ctx context.Context, q querier, orgID string, id int64) (domain.Pool, bool, error) {
	p, restored, err := scanPool(q.QueryRow(ctx, fmt.Sprintf(`
		UPDATE pools p SET deleted_at = NULL
		WHERE p.seq_id = $1 AND p.organization_id = $2 AND p.deleted_at IS NOT NULL
		  AND (p.parent_id IS NULL OR EXISTS (SELECT 1 FROM pools pp WHERE pp.id = p.parent_id AND pp.deleted_at IS NULL))
		  AND (p.account_id IS NULL OR EXISTS (SELECT 1 FROM accounts a WHERE a.id = p.account_id AND a.deleted_at IS NULL))
		RETURNING p.%s`, poolColumnsWithParentAccount()), id, orgID))
	if isUniqueViolation(err) {
		return domain.Pool{}, false, fmt.Errorf("pool with this CIDR already exists: %w", storage.ErrConflict)
	}
	if err != nil || restored {
		return p, restored, err
	}
	return domain.Pool{}, false, restoreBlocker(ctx, q, orgID, id)
}

// restoreBlocker explains why restorePool's UPDATE matched no row: nil when no
// soft-deleted pool has that id, otherwise the conflict that held it back.
func restoreBlocker(ctx context.Context, q querier, orgID string, id int64) error {
	var deleted bool
	var deletedParent, deletedAccount *int64
	rows, err := q.Query(ctx, `
		SELECT p.deleted_at IS NOT NULL,
			(SELECT pp.seq_id FROM pools pp WHERE pp.id = p.parent_id AND pp.deleted_at IS NOT NULL),
			(SELECT a.seq_id FROM accounts a WHERE a.id = p.account_id AND a.deleted_at IS NOT NULL)
		FROM pools p
		WHERE p.seq_id = $1 AND p.organization_id = $2`, id, orgID)
	if err != nil {
		return err
	}
	defer rows.Close()
	if !rows.Next() {
		return rows.Err()
	}
	if err := rows.Scan(&deleted, &deletedParent, &deletedAccount); err != nil {
		return err
	}
	switch {
	case !deleted:
		return nil
	case deletedParent != nil:
		return fmt.Errorf("parent pool %d is deleted; restore it first: %w", *deletedParent, storage.ErrConflict)
	case deletedAccount != nil:
		return fmt.Errorf("account %d is deleted; restore it first: %w", *deletedAccount, storage.ErrConflict)
	}
	return fmt.Errorf("pool %d changed during restore: %w", id, storage.ErrConflict)
}

func (s *Store) ListDeletedAccounts(ctx context.Context) ([]domain.Account, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT seq_id, key, name, provider, external_id, description,
			platform, tier, environment, regions, created_at, updated_at, deleted_at
		FROM accounts
		WHERE organization_id = $1 AND deleted_at IS NOT NULL
		ORDER BY deleted_at DESC, seq_id DESC`, s.orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.Account{}
	for rows.Next() {
		var deletedAt time.Time
		a, err := scanAccount(rows, &deletedAt)
		if err != nil {
			return nil, err
		}
		a.DeletedAt = &deletedAt
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) RestoreAccount(ctx context.Context, id int64) (domain.Account, bool, error) {
	rows, err := s.pool.Query(ctx, `
		UPDATE accounts SET deleted_at = NULL
		WHERE seq_id = $1 AND organization_id = $2 AND deleted_at IS NOT NULL
		RETURNING seq_id, key, name, provider, external_id, description,
			platform, tier, environment, regions, created_at, updated_at`, id, s.orgID)
	if err != nil {
		return domain.Account{}, false, accountRestoreErr(err)
	}
	defer rows.Close()
	if !rows.Next() {
		return domain.Account{}, false, accountRestoreErr(rows.Err())
	}
	a, err := scanAccount(rows)
	if err != nil {
		return domain.Account{}, false, err
	}
	return a, true, nil
}

func accountRestoreErr(err error) error {
	if isUniqueViolation(err) {
		return fmt.Errorf("account key already exists: %w", storage.ErrConflict)
	}
	return err
}
