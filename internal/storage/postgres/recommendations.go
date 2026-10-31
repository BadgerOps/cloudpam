//go:build postgres

package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"cloudpam/internal/domain"
	"cloudpam/internal/storage"
)

var _ storage.RecommendationStore = (*Store)(nil)

// CreateRecommendation persists a new recommendation. A duplicate id returns storage.ErrConflict.
func (s *Store) CreateRecommendation(ctx context.Context, rec domain.Recommendation) error {
	metadataJSON := "{}"
	if rec.Metadata != nil {
		if b, err := json.Marshal(rec.Metadata); err == nil {
			metadataJSON = string(b)
		}
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO recommendations (
			id, organization_id, pool_id, type, status, priority, title, description,
			suggested_cidr, rule_id, score, metadata, dismiss_reason, applied_pool_id,
			created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12::jsonb, $13, $14, $15, $16)`,
		rec.ID, s.orgID, rec.PoolID, string(rec.Type), string(rec.Status), string(rec.Priority),
		rec.Title, rec.Description, rec.SuggestedCIDR, rec.RuleID, rec.Score, metadataJSON,
		rec.DismissReason, rec.AppliedPoolID, rec.CreatedAt, rec.UpdatedAt,
	)
	return storage.WrapIfConflict(err)
}

// GetRecommendation returns one recommendation, or storage.ErrNotFound.
func (s *Store) GetRecommendation(ctx context.Context, id string) (*domain.Recommendation, error) {
	rec, err := scanRecommendation(s.pool.QueryRow(ctx,
		`SELECT `+recommendationColumns+`
		 FROM recommendations
		 WHERE id = $1 AND organization_id = $2`,
		id, s.orgID,
	))
	if err != nil {
		return nil, err
	}
	return &rec, nil
}

// ListRecommendations returns one page of matches, newest first, and the total match count.
func (s *Store) ListRecommendations(ctx context.Context, filters domain.RecommendationFilters) ([]domain.Recommendation, int, error) {
	where := []string{"organization_id = $1"}
	args := []any{s.orgID}
	addArg := func(value any) string {
		args = append(args, value)
		return fmt.Sprintf("$%d", len(args))
	}
	if filters.PoolID != 0 {
		where = append(where, "pool_id = "+addArg(filters.PoolID))
	}
	if filters.Type != "" {
		where = append(where, "type = "+addArg(filters.Type))
	}
	if filters.Status != "" {
		where = append(where, "status = "+addArg(filters.Status))
	}
	if filters.Priority != "" {
		where = append(where, "priority = "+addArg(filters.Priority))
	}
	whereClause := " WHERE " + strings.Join(where, " AND ")

	var total int
	if err := s.pool.QueryRow(ctx, "SELECT COUNT(*) FROM recommendations"+whereClause, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	page, pageSize := storage.NormalizePage(filters.Page, filters.PageSize)
	offset := storage.PageOffset(page, pageSize, total)

	// One Generate batch shares a created_at, so id makes the order total and
	// paging through a batch neither repeats nor skips a row.
	queryArgs := append(append([]any{}, args...), pageSize, offset)
	query := fmt.Sprintf(
		`SELECT %s FROM recommendations%s
		 ORDER BY created_at DESC, id DESC
		 LIMIT $%d OFFSET $%d`,
		recommendationColumns, whereClause, len(queryArgs)-1, len(queryArgs),
	)
	rows, err := s.pool.Query(ctx, query, queryArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	out := []domain.Recommendation{}
	for rows.Next() {
		rec, err := scanRecommendation(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, rec)
	}
	return out, total, rows.Err()
}

// UpdateRecommendationStatus sets status, dismiss reason, and applied pool, and bumps updated_at.
// It returns storage.ErrNotFound when no row in this organization has the id.
func (s *Store) UpdateRecommendationStatus(ctx context.Context, id string, status domain.RecommendationStatus, dismissReason string, appliedPoolID *int64) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE recommendations
		 SET status = $1, dismiss_reason = $2, applied_pool_id = $3, updated_at = $4
		 WHERE id = $5 AND organization_id = $6`,
		string(status), dismissReason, appliedPoolID, time.Now().UTC(), id, s.orgID,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return storage.ErrNotFound
	}
	return nil
}

// DeletePendingForPool removes the pool's pending recommendations; applied and dismissed stay.
func (s *Store) DeletePendingForPool(ctx context.Context, poolID int64) error {
	_, err := s.pool.Exec(ctx,
		`DELETE FROM recommendations WHERE organization_id = $1 AND pool_id = $2 AND status = $3`,
		s.orgID, poolID, string(domain.RecommendationStatusPending),
	)
	return err
}

const recommendationColumns = `id, pool_id, type, status, priority, title, description,
	suggested_cidr, rule_id, score, metadata::text, dismiss_reason, applied_pool_id,
	created_at, updated_at`

func scanRecommendation(row pgx.Row) (domain.Recommendation, error) {
	var rec domain.Recommendation
	var metadataJSON string
	err := row.Scan(
		&rec.ID, &rec.PoolID, &rec.Type, &rec.Status, &rec.Priority, &rec.Title, &rec.Description,
		&rec.SuggestedCIDR, &rec.RuleID, &rec.Score, &metadataJSON, &rec.DismissReason, &rec.AppliedPoolID,
		&rec.CreatedAt, &rec.UpdatedAt,
	)
	if err == pgx.ErrNoRows {
		return domain.Recommendation{}, storage.ErrNotFound
	}
	if err != nil {
		return domain.Recommendation{}, err
	}
	if err := json.Unmarshal([]byte(metadataJSON), &rec.Metadata); err != nil {
		return domain.Recommendation{}, fmt.Errorf("decode metadata for recommendation %s: %w", rec.ID, err)
	}
	return rec, nil
}
