//go:build sqlite

package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"cloudpam/internal/domain"
	"cloudpam/internal/storage"
)

// CreateRecommendation persists a new recommendation.
func (s *Store) CreateRecommendation(ctx context.Context, rec domain.Recommendation) error {
	metadataJSON := "{}"
	if rec.Metadata != nil {
		if b, err := json.Marshal(rec.Metadata); err == nil {
			metadataJSON = string(b)
		}
	}

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO recommendations (id, pool_id, type, status, priority, title, description, suggested_cidr, rule_id, score, metadata, dismiss_reason, applied_pool_id, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		rec.ID, rec.PoolID, string(rec.Type), string(rec.Status), string(rec.Priority),
		rec.Title, rec.Description, nilIfEmpty(rec.SuggestedCIDR), nilIfEmpty(rec.RuleID),
		rec.Score, metadataJSON, nilIfEmpty(rec.DismissReason), rec.AppliedPoolID,
		rec.CreatedAt.Format(time.RFC3339), rec.UpdatedAt.Format(time.RFC3339),
	)
	return err
}

// GetRecommendation returns a single recommendation by ID.
func (s *Store) GetRecommendation(ctx context.Context, id string) (*domain.Recommendation, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, pool_id, type, status, priority, title, description, suggested_cidr, rule_id, score, metadata, dismiss_reason, applied_pool_id, created_at, updated_at
		 FROM recommendations WHERE id = ?`, id,
	)
	return scanRecommendation(row)
}

// ListRecommendations returns paginated recommendations matching the filters.
func (s *Store) ListRecommendations(ctx context.Context, filters domain.RecommendationFilters) ([]domain.Recommendation, int, error) {
	var where []string
	var args []any

	if filters.PoolID != 0 {
		where = append(where, "pool_id = ?")
		args = append(args, filters.PoolID)
	}
	if filters.Type != "" {
		where = append(where, "type = ?")
		args = append(args, filters.Type)
	}
	if filters.Status != "" {
		where = append(where, "status = ?")
		args = append(args, filters.Status)
	}
	if filters.Priority != "" {
		where = append(where, "priority = ?")
		args = append(args, filters.Priority)
	}

	whereClause := ""
	if len(where) > 0 {
		whereClause = " WHERE " + strings.Join(where, " AND ")
	}

	// Count
	var total int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM recommendations"+whereClause, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	// Paginate
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
		`SELECT id, pool_id, type, status, priority, title, description, suggested_cidr, rule_id, score, metadata, dismiss_reason, applied_pool_id, created_at, updated_at
		 FROM recommendations%s ORDER BY created_at DESC LIMIT ? OFFSET ?`, whereClause,
	)
	args = append(args, pageSize, offset)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var out []domain.Recommendation
	for rows.Next() {
		r, err := scanRecommendationRow(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, r)
	}
	if out == nil {
		out = []domain.Recommendation{}
	}
	return out, total, rows.Err()
}

// UpdateRecommendationStatus updates a recommendation's status and optional fields.
func (s *Store) UpdateRecommendationStatus(ctx context.Context, id string, status domain.RecommendationStatus, dismissReason string, appliedPoolID *int64) error {
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := s.db.ExecContext(ctx,
		`UPDATE recommendations SET status = ?, dismiss_reason = ?, applied_pool_id = ?, updated_at = ? WHERE id = ?`,
		string(status), nilIfEmpty(dismissReason), appliedPoolID, now, id,
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

// DeletePendingForPool removes all pending recommendations for a pool.
func (s *Store) DeletePendingForPool(ctx context.Context, poolID int64) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM recommendations WHERE pool_id = ? AND status = ?`,
		poolID, string(domain.RecommendationStatusPending),
	)
	return err
}

func scanRecommendation(row *sql.Row) (*domain.Recommendation, error) {
	var r domain.Recommendation
	var metadataJSON, createdAt, updatedAt string
	var suggestedCIDR, ruleID, dismissReason sql.NullString
	var appliedPoolID sql.NullInt64

	if err := row.Scan(&r.ID, &r.PoolID, &r.Type, &r.Status, &r.Priority,
		&r.Title, &r.Description, &suggestedCIDR, &ruleID, &r.Score,
		&metadataJSON, &dismissReason, &appliedPoolID, &createdAt, &updatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, storage.ErrNotFound
		}
		return nil, err
	}

	if suggestedCIDR.Valid {
		r.SuggestedCIDR = suggestedCIDR.String
	}
	if ruleID.Valid {
		r.RuleID = ruleID.String
	}
	if dismissReason.Valid {
		r.DismissReason = dismissReason.String
	}
	if appliedPoolID.Valid {
		r.AppliedPoolID = &appliedPoolID.Int64
	}
	_ = json.Unmarshal([]byte(metadataJSON), &r.Metadata)
	r.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	r.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	return &r, nil
}

func scanRecommendationRow(rows *sql.Rows) (domain.Recommendation, error) {
	var r domain.Recommendation
	var metadataJSON, createdAt, updatedAt string
	var suggestedCIDR, ruleID, dismissReason sql.NullString
	var appliedPoolID sql.NullInt64

	if err := rows.Scan(&r.ID, &r.PoolID, &r.Type, &r.Status, &r.Priority,
		&r.Title, &r.Description, &suggestedCIDR, &ruleID, &r.Score,
		&metadataJSON, &dismissReason, &appliedPoolID, &createdAt, &updatedAt); err != nil {
		return domain.Recommendation{}, err
	}

	if suggestedCIDR.Valid {
		r.SuggestedCIDR = suggestedCIDR.String
	}
	if ruleID.Valid {
		r.RuleID = ruleID.String
	}
	if dismissReason.Valid {
		r.DismissReason = dismissReason.String
	}
	if appliedPoolID.Valid {
		r.AppliedPoolID = &appliedPoolID.Int64
	}
	_ = json.Unmarshal([]byte(metadataJSON), &r.Metadata)
	r.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	r.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	return r, nil
}

func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
