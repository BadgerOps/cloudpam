//go:build sqlite

package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"cloudpam/internal/domain"
)

const poolColumns = `id, name, cidr, parent_id, account_id, type, status, source, description, tags, created_at, updated_at, deleted_at`

const accountColumns = `id, key, name, provider, external_id, description, platform, tier, environment, regions, created_at, updated_at, deleted_at`

type rowScanner interface {
	Scan(dest ...any) error
}

func (s *Store) queryPools(ctx context.Context, clause string, args ...any) ([]domain.Pool, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+poolColumns+` FROM pools `+clause, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Pool
	for rows.Next() {
		p, err := scanPool(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func scanPool(row rowScanner) (domain.Pool, error) {
	var p domain.Pool
	var createdAt, updatedAt, deletedAt sql.NullString
	var parent, account sql.NullInt64
	var poolType, poolStatus, poolSource, description, tagsJSON sql.NullString
	if err := row.Scan(&p.ID, &p.Name, &p.CIDR, &parent, &account, &poolType, &poolStatus, &poolSource, &description, &tagsJSON, &createdAt, &updatedAt, &deletedAt); err != nil {
		return domain.Pool{}, err
	}
	if parent.Valid {
		p.ParentID = &parent.Int64
	}
	if account.Valid {
		p.AccountID = &account.Int64
	}
	p.Type = domain.PoolTypeSubnet
	if poolType.Valid && poolType.String != "" {
		p.Type = domain.PoolType(poolType.String)
	}
	p.Status = domain.PoolStatusActive
	if poolStatus.Valid && poolStatus.String != "" {
		p.Status = domain.PoolStatus(poolStatus.String)
	}
	p.Source = domain.PoolSourceManual
	if poolSource.Valid && poolSource.String != "" {
		p.Source = domain.PoolSource(poolSource.String)
	}
	if description.Valid {
		p.Description = description.String
	}
	if tagsJSON.Valid && tagsJSON.String != "" && tagsJSON.String != "{}" {
		var tags map[string]string
		if err := json.Unmarshal([]byte(tagsJSON.String), &tags); err == nil {
			p.Tags = tags
		}
	}
	p.CreatedAt = parseTime(createdAt)
	p.UpdatedAt = parseTime(updatedAt)
	p.DeletedAt = parseOptionalTime(deletedAt)
	return p, nil
}

func (s *Store) queryAccounts(ctx context.Context, clause string, args ...any) ([]domain.Account, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+accountColumns+` FROM accounts `+clause, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Account
	for rows.Next() {
		a, err := scanAccount(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func scanAccount(row rowScanner) (domain.Account, error) {
	var a domain.Account
	var createdAt sql.NullString
	var provider, extid, desc, platform, tier, env sql.NullString
	var regions, updatedAt, deletedAt sql.NullString
	if err := row.Scan(&a.ID, &a.Key, &a.Name, &provider, &extid, &desc, &platform, &tier, &env, &regions, &createdAt, &updatedAt, &deletedAt); err != nil {
		return domain.Account{}, err
	}
	a.Provider = provider.String
	a.ExternalID = extid.String
	a.Description = desc.String
	a.Platform = platform.String
	a.Tier = tier.String
	a.Environment = env.String
	if regions.Valid && regions.String != "" {
		var arr []string
		if err := json.Unmarshal([]byte(regions.String), &arr); err == nil {
			a.Regions = arr
		}
	}
	a.CreatedAt = parseTime(createdAt)
	a.UpdatedAt = parseTime(updatedAt)
	a.DeletedAt = parseOptionalTime(deletedAt)
	return a, nil
}

func parseTime(v sql.NullString) time.Time {
	if !v.Valid {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, v.String)
	if err != nil {
		return time.Time{}
	}
	return t
}

func parseOptionalTime(v sql.NullString) *time.Time {
	if !v.Valid {
		return nil
	}
	t := parseTime(v)
	return &t
}
