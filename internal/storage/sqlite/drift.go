//go:build sqlite

package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"cloudpam/internal/domain"
	"cloudpam/internal/storage"
)

// CreateDriftItem persists a new drift item.
func (s *Store) CreateDriftItem(ctx context.Context, item domain.DriftItem) error {
	detailsJSON := "{}"
	if item.Details != nil {
		if b, err := json.Marshal(item.Details); err == nil {
			detailsJSON = string(b)
		}
	}

	var resourceID *string
	if item.ResourceID != nil {
		s := item.ResourceID.String()
		resourceID = &s
	}

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO drift_items (id, account_id, resource_id, pool_id, type, severity, status, title, description, resource_cidr, pool_cidr, details, ignore_reason, resolved_at, detected_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		item.ID, item.AccountID, resourceID, item.PoolID,
		string(item.Type), string(item.Severity), string(item.Status),
		item.Title, item.Description, item.ResourceCIDR, item.PoolCIDR,
		detailsJSON, nilIfEmpty(item.IgnoreReason), nil,
		item.DetectedAt.Format(time.RFC3339), item.UpdatedAt.Format(time.RFC3339),
	)
	return err
}

// GetDriftItem returns a single drift item by ID.
func (s *Store) GetDriftItem(ctx context.Context, id string) (*domain.DriftItem, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, account_id, resource_id, pool_id, type, severity, status, title, description, resource_cidr, pool_cidr, details, ignore_reason, resolved_at, detected_at, updated_at
		 FROM drift_items WHERE id = ?`, id,
	)
	return scanDriftItem(row)
}

// ListDriftItems returns paginated drift items matching the filters.
func (s *Store) ListDriftItems(ctx context.Context, filters domain.DriftFilters) ([]domain.DriftItem, int, error) {
	var where []string
	var args []any

	if filters.AccountID != 0 {
		where = append(where, "account_id = ?")
		args = append(args, filters.AccountID)
	}
	if filters.Type != "" {
		where = append(where, "type = ?")
		args = append(args, filters.Type)
	}
	if filters.Severity != "" {
		where = append(where, "severity = ?")
		args = append(args, filters.Severity)
	}
	if filters.Status != "" {
		where = append(where, "status = ?")
		args = append(args, filters.Status)
	}

	whereClause := ""
	if len(where) > 0 {
		whereClause = " WHERE " + strings.Join(where, " AND ")
	}

	var total int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM drift_items"+whereClause, args...).Scan(&total); err != nil {
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

	query := fmt.Sprintf(
		`SELECT id, account_id, resource_id, pool_id, type, severity, status, title, description, resource_cidr, pool_cidr, details, ignore_reason, resolved_at, detected_at, updated_at
		 FROM drift_items%s ORDER BY detected_at DESC LIMIT ? OFFSET ?`, whereClause,
	)
	args = append(args, pageSize, offset)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var out []domain.DriftItem
	for rows.Next() {
		d, err := scanDriftItemRow(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, d)
	}
	if out == nil {
		out = []domain.DriftItem{}
	}
	return out, total, rows.Err()
}

// UpdateDriftStatus updates a drift item's status and optional ignore reason.
func (s *Store) UpdateDriftStatus(ctx context.Context, id string, status domain.DriftStatus, ignoreReason string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	var resolvedAt *string
	if status == domain.DriftStatusResolved {
		resolvedAt = &now
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE drift_items SET status = ?, ignore_reason = ?, resolved_at = ?, updated_at = ? WHERE id = ?`,
		string(status), nilIfEmpty(ignoreReason), resolvedAt, now, id,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return storage.ErrNotFound
	}
	return nil
}

// DeleteOpenForAccount removes all open drift items for an account.
func (s *Store) DeleteOpenForAccount(ctx context.Context, accountID int64) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM drift_items WHERE account_id = ? AND status = ?`,
		accountID, string(domain.DriftStatusOpen),
	)
	return err
}

func scanDriftItem(row *sql.Row) (*domain.DriftItem, error) {
	var d domain.DriftItem
	var detailsJSON, detectedAt, updatedAt string
	var resourceID, ignoreReason, resolvedAt, resourceCIDR, poolCIDR sql.NullString
	var poolID sql.NullInt64

	if err := row.Scan(&d.ID, &d.AccountID, &resourceID, &poolID,
		&d.Type, &d.Severity, &d.Status, &d.Title, &d.Description,
		&resourceCIDR, &poolCIDR, &detailsJSON, &ignoreReason, &resolvedAt,
		&detectedAt, &updatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, storage.ErrNotFound
		}
		return nil, err
	}

	populateDriftItem(&d, resourceID, poolID, resourceCIDR, poolCIDR, detailsJSON, ignoreReason, resolvedAt, detectedAt, updatedAt)
	return &d, nil
}

func scanDriftItemRow(rows *sql.Rows) (domain.DriftItem, error) {
	var d domain.DriftItem
	var detailsJSON, detectedAt, updatedAt string
	var resourceID, ignoreReason, resolvedAt, resourceCIDR, poolCIDR sql.NullString
	var poolID sql.NullInt64

	if err := rows.Scan(&d.ID, &d.AccountID, &resourceID, &poolID,
		&d.Type, &d.Severity, &d.Status, &d.Title, &d.Description,
		&resourceCIDR, &poolCIDR, &detailsJSON, &ignoreReason, &resolvedAt,
		&detectedAt, &updatedAt); err != nil {
		return domain.DriftItem{}, err
	}

	populateDriftItem(&d, resourceID, poolID, resourceCIDR, poolCIDR, detailsJSON, ignoreReason, resolvedAt, detectedAt, updatedAt)
	return d, nil
}

func populateDriftItem(d *domain.DriftItem, resourceID sql.NullString, poolID sql.NullInt64, resourceCIDR, poolCIDR sql.NullString, detailsJSON string, ignoreReason, resolvedAt sql.NullString, detectedAt, updatedAt string) {
	if resourceID.Valid {
		uid := uuid.MustParse(resourceID.String)
		d.ResourceID = &uid
	}
	if poolID.Valid {
		d.PoolID = &poolID.Int64
	}
	if resourceCIDR.Valid {
		d.ResourceCIDR = resourceCIDR.String
	}
	if poolCIDR.Valid {
		d.PoolCIDR = poolCIDR.String
	}
	if ignoreReason.Valid {
		d.IgnoreReason = ignoreReason.String
	}
	if resolvedAt.Valid {
		t, _ := time.Parse(time.RFC3339, resolvedAt.String)
		d.ResolvedAt = &t
	}
	_ = json.Unmarshal([]byte(detailsJSON), &d.Details)
	d.DetectedAt, _ = time.Parse(time.RFC3339, detectedAt)
	d.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
}
