//go:build sqlite

package sqlite

import (
	"context"
	"fmt"
	"time"

	"cloudpam/internal/domain"
	"cloudpam/internal/storage"
)

func (s *Store) ListDeletedPools(ctx context.Context) ([]domain.Pool, error) {
	out, err := s.queryPools(ctx, `WHERE deleted_at IS NOT NULL ORDER BY deleted_at DESC, id DESC`)
	if out == nil && err == nil {
		out = []domain.Pool{}
	}
	return out, err
}

// RestorePool checks the parent and account in the UPDATE itself, so a parent
// deleted after the check cannot leave a live pool under it.
func (s *Store) RestorePool(ctx context.Context, id int64) (domain.Pool, bool, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := s.db.ExecContext(ctx, `
		UPDATE pools SET deleted_at = NULL, updated_at = ?
		WHERE id = ? AND deleted_at IS NOT NULL
		  AND (parent_id IS NULL OR EXISTS (SELECT 1 FROM pools pp WHERE pp.id = pools.parent_id AND pp.deleted_at IS NULL))
		  AND (account_id IS NULL OR EXISTS (SELECT 1 FROM accounts a WHERE a.id = pools.account_id AND a.deleted_at IS NULL))`,
		now, id)
	if err != nil {
		return domain.Pool{}, false, storage.WrapIfConflict(err)
	}
	if n, _ := res.RowsAffected(); n == 1 {
		p, ok, err := s.GetPool(ctx, id)
		return p, ok, err
	}
	ps, err := s.queryPools(ctx, `WHERE id = ?`, id)
	if err != nil || len(ps) == 0 || ps[0].DeletedAt == nil {
		return domain.Pool{}, false, err
	}
	return domain.Pool{}, false, s.restoreBlocker(ctx, ps[0])
}

func (s *Store) restoreBlocker(ctx context.Context, p domain.Pool) error {
	if p.ParentID != nil {
		if _, live, err := s.GetPool(ctx, *p.ParentID); err != nil {
			return err
		} else if !live {
			return fmt.Errorf("parent pool %d is deleted; restore it first: %w", *p.ParentID, storage.ErrConflict)
		}
	}
	if p.AccountID != nil {
		if _, live, err := s.GetAccount(ctx, *p.AccountID); err != nil {
			return err
		} else if !live {
			return fmt.Errorf("account %d is deleted; restore it first: %w", *p.AccountID, storage.ErrConflict)
		}
	}
	return fmt.Errorf("pool %d changed during restore: %w", p.ID, storage.ErrConflict)
}

func (s *Store) ListDeletedAccounts(ctx context.Context) ([]domain.Account, error) {
	out, err := s.queryAccounts(ctx, `WHERE deleted_at IS NOT NULL ORDER BY deleted_at DESC, id DESC`)
	if out == nil && err == nil {
		out = []domain.Account{}
	}
	return out, err
}

func (s *Store) RestoreAccount(ctx context.Context, id int64) (domain.Account, bool, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := s.db.ExecContext(ctx, `UPDATE accounts SET deleted_at = NULL, updated_at = ? WHERE id = ? AND deleted_at IS NOT NULL`, now, id)
	if err != nil {
		return domain.Account{}, false, storage.WrapIfConflict(err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return domain.Account{}, false, nil
	}
	return s.GetAccount(ctx, id)
}
