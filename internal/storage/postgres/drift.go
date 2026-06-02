//go:build postgres

package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"cloudpam/internal/domain"
	"cloudpam/internal/storage"
)

var _ storage.DriftStore = (*Store)(nil)

func nilStringIfEmpty(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

// CreateDriftItem persists a new drift item.
func (s *Store) CreateDriftItem(ctx context.Context, item domain.DriftItem) error {
	detailsJSON := "{}"
	if item.Details != nil {
		if b, err := json.Marshal(item.Details); err == nil {
			detailsJSON = string(b)
		}
	}

	_, err := s.pool.Exec(ctx,
		`INSERT INTO drift_items (
			id, organization_id, account_id, resource_id, pool_id, type, severity, status,
			title, description, resource_cidr, pool_cidr, details, ignore_reason,
			resolved_at, detected_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13::jsonb, $14, $15, $16, $17)`,
		item.ID, s.orgID, item.AccountID, item.ResourceID, item.PoolID,
		string(item.Type), string(item.Severity), string(item.Status),
		item.Title, item.Description, item.ResourceCIDR, item.PoolCIDR,
		detailsJSON, nilStringIfEmpty(item.IgnoreReason), item.ResolvedAt,
		item.DetectedAt, item.UpdatedAt,
	)
	return storage.WrapIfConflict(err)
}

// GetDriftItem returns a single drift item by ID.
func (s *Store) GetDriftItem(ctx context.Context, id string) (*domain.DriftItem, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT id, account_id, resource_id, pool_id, type, severity, status, title,
			description, resource_cidr, pool_cidr, details::text, ignore_reason,
			resolved_at, detected_at, updated_at
		 FROM drift_items
		 WHERE id = $1 AND organization_id = $2`,
		id, s.orgID,
	)
	return scanPostgresDriftItem(row)
}

// ListDriftItems returns paginated drift items matching the filters.
func (s *Store) ListDriftItems(ctx context.Context, filters domain.DriftFilters) ([]domain.DriftItem, int, error) {
	where := []string{"organization_id = $1"}
	args := []any{s.orgID}
	addArg := func(value any) string {
		args = append(args, value)
		return fmt.Sprintf("$%d", len(args))
	}

	if filters.AccountID != 0 {
		where = append(where, "account_id = "+addArg(filters.AccountID))
	}
	if filters.Type != "" {
		where = append(where, "type = "+addArg(filters.Type))
	}
	if filters.Severity != "" {
		where = append(where, "severity = "+addArg(filters.Severity))
	}
	if filters.Status != "" {
		where = append(where, "status = "+addArg(filters.Status))
	}

	whereClause := " WHERE " + strings.Join(where, " AND ")
	var total int
	if err := s.pool.QueryRow(ctx, "SELECT COUNT(*) FROM drift_items"+whereClause, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	page := filters.Page
	if page < 1 {
		page = 1
	}
	pageSize := filters.PageSize
	if pageSize < 1 {
		pageSize = 50
	}
	offset := (page - 1) * pageSize

	queryArgs := append(append([]any{}, args...), pageSize, offset)
	query := fmt.Sprintf(
		`SELECT id, account_id, resource_id, pool_id, type, severity, status, title,
			description, resource_cidr, pool_cidr, details::text, ignore_reason,
			resolved_at, detected_at, updated_at
		 FROM drift_items%s
		 ORDER BY detected_at DESC
		 LIMIT $%d OFFSET $%d`,
		whereClause, len(queryArgs)-1, len(queryArgs),
	)
	rows, err := s.pool.Query(ctx, query, queryArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	out := []domain.DriftItem{}
	for rows.Next() {
		item, err := scanPostgresDriftItemRow(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, item)
	}
	return out, total, rows.Err()
}

// UpdateDriftStatus updates a drift item's status and optional ignore reason.
func (s *Store) UpdateDriftStatus(ctx context.Context, id string, status domain.DriftStatus, ignoreReason string) error {
	now := time.Now().UTC()
	var resolvedAt *time.Time
	if status == domain.DriftStatusResolved {
		resolvedAt = &now
	}
	tag, err := s.pool.Exec(ctx,
		`UPDATE drift_items
		 SET status = $1, ignore_reason = $2, resolved_at = $3, updated_at = $4
		 WHERE id = $5 AND organization_id = $6`,
		string(status), nilStringIfEmpty(ignoreReason), resolvedAt, now, id, s.orgID,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return storage.ErrNotFound
	}
	return nil
}

// UpdateDriftDetails merges structured detail metadata into a drift item.
func (s *Store) UpdateDriftDetails(ctx context.Context, id string, details map[string]string) error {
	item, err := s.GetDriftItem(ctx, id)
	if err != nil {
		return err
	}
	merged := map[string]string{}
	for key, value := range item.Details {
		merged[key] = value
	}
	for key, value := range details {
		merged[key] = value
	}
	detailsJSON := "{}"
	if b, err := json.Marshal(merged); err == nil {
		detailsJSON = string(b)
	}
	tag, err := s.pool.Exec(ctx,
		`UPDATE drift_items SET details = $1::jsonb, updated_at = $2 WHERE id = $3 AND organization_id = $4`,
		detailsJSON, time.Now().UTC(), id, s.orgID,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return storage.ErrNotFound
	}
	return nil
}

// DeleteOpenForAccount removes all open drift items for an account.
func (s *Store) DeleteOpenForAccount(ctx context.Context, accountID int64) error {
	_, err := s.pool.Exec(ctx,
		`DELETE FROM drift_items WHERE organization_id = $1 AND account_id = $2 AND status = $3`,
		s.orgID, accountID, string(domain.DriftStatusOpen),
	)
	return err
}

type postgresDriftRow interface {
	Scan(dest ...any) error
}

func scanPostgresDriftItem(row postgresDriftRow) (*domain.DriftItem, error) {
	item, err := scanPostgresDriftItemValue(row)
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func scanPostgresDriftItemRow(row postgresDriftRow) (domain.DriftItem, error) {
	return scanPostgresDriftItemValue(row)
}

func scanPostgresDriftItemValue(row postgresDriftRow) (domain.DriftItem, error) {
	var item domain.DriftItem
	var resourceID *uuid.UUID
	var poolID *int64
	var detailsJSON string
	var ignoreReason *string
	var resolvedAt *time.Time

	err := row.Scan(
		&item.ID, &item.AccountID, &resourceID, &poolID,
		&item.Type, &item.Severity, &item.Status, &item.Title, &item.Description,
		&item.ResourceCIDR, &item.PoolCIDR, &detailsJSON, &ignoreReason, &resolvedAt,
		&item.DetectedAt, &item.UpdatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return domain.DriftItem{}, storage.ErrNotFound
		}
		return domain.DriftItem{}, err
	}
	item.ResourceID = resourceID
	item.PoolID = poolID
	if ignoreReason != nil {
		item.IgnoreReason = *ignoreReason
	}
	item.ResolvedAt = resolvedAt
	_ = json.Unmarshal([]byte(detailsJSON), &item.Details)
	return item, nil
}
