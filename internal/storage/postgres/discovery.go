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

var _ storage.DiscoveryStore = (*Store)(nil)

func (s *Store) ListDiscoveredResources(ctx context.Context, accountID int64, filters domain.DiscoveryFilters) ([]domain.DiscoveredResource, int, error) {
	where := []string{"organization_id = $1", "account_id = $2"}
	args := []any{s.orgID, accountID}

	addArg := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}

	if filters.Provider != "" {
		where = append(where, "provider = "+addArg(filters.Provider))
	}
	if filters.Region != "" {
		where = append(where, "region = "+addArg(filters.Region))
	}
	if filters.ResourceType != "" {
		where = append(where, "resource_type = "+addArg(filters.ResourceType))
	}
	if filters.Status != "" {
		where = append(where, "status = "+addArg(filters.Status))
	}
	if filters.HasPool != nil {
		if *filters.HasPool {
			where = append(where, "pool_id IS NOT NULL")
		} else {
			where = append(where, "pool_id IS NULL")
		}
	}

	whereClause := strings.Join(where, " AND ")
	var total int
	if err := s.pool.QueryRow(ctx, "SELECT COUNT(*) FROM discovered_resources WHERE "+whereClause, args...).Scan(&total); err != nil {
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
	query := fmt.Sprintf(`SELECT id, account_id, provider, region, resource_type, resource_id, name, cidr, parent_resource_id, pool_id, status, metadata, discovered_at, last_seen_at
		FROM discovered_resources
		WHERE %s
		ORDER BY discovered_at DESC
		LIMIT $%d OFFSET $%d`, whereClause, len(queryArgs)-1, len(queryArgs))

	rows, err := s.pool.Query(ctx, query, queryArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var out []domain.DiscoveredResource
	for rows.Next() {
		r, err := scanPostgresDiscoveredResource(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, r)
	}
	if out == nil {
		out = []domain.DiscoveredResource{}
	}
	return out, total, rows.Err()
}

func (s *Store) GetDiscoveredResource(ctx context.Context, id uuid.UUID) (*domain.DiscoveredResource, error) {
	row := s.pool.QueryRow(ctx, `SELECT id, account_id, provider, region, resource_type, resource_id, name, cidr, parent_resource_id, pool_id, status, metadata, discovered_at, last_seen_at
		FROM discovered_resources
		WHERE id = $1 AND organization_id = $2`, id, s.orgID)
	r, err := scanPostgresDiscoveredResource(row)
	if err != nil {
		return nil, err
	}
	return &r, nil
}

func (s *Store) UpsertDiscoveredResource(ctx context.Context, res domain.DiscoveredResource) error {
	if res.ID == uuid.Nil {
		res.ID = uuid.New()
	}
	if res.DiscoveredAt.IsZero() {
		res.DiscoveredAt = time.Now().UTC()
	}
	if res.LastSeenAt.IsZero() {
		res.LastSeenAt = time.Now().UTC()
	}
	metadataJSON, _ := json.Marshal(res.Metadata)
	if res.Metadata == nil {
		metadataJSON = []byte("{}")
	}

	_, err := s.pool.Exec(ctx, `INSERT INTO discovered_resources
		(id, organization_id, account_id, provider, region, resource_type, resource_id, name, cidr, parent_resource_id, pool_id, status, metadata, discovered_at, last_seen_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13::jsonb, $14, $15)
		ON CONFLICT (organization_id, account_id, resource_id) DO UPDATE SET
			name = EXCLUDED.name,
			cidr = EXCLUDED.cidr,
			region = EXCLUDED.region,
			parent_resource_id = EXCLUDED.parent_resource_id,
			status = EXCLUDED.status,
			metadata = EXCLUDED.metadata,
			last_seen_at = EXCLUDED.last_seen_at`,
		res.ID, s.orgID, res.AccountID, res.Provider, res.Region, string(res.ResourceType), res.ResourceID,
		res.Name, res.CIDR, res.ParentResourceID, res.PoolID, string(res.Status), string(metadataJSON), res.DiscoveredAt, res.LastSeenAt)
	return err
}

func (s *Store) MarkStaleResources(ctx context.Context, accountID int64, before time.Time) (int, error) {
	tag, err := s.pool.Exec(ctx, `UPDATE discovered_resources
		SET status = $1
		WHERE organization_id = $2 AND account_id = $3 AND status = $4 AND last_seen_at < $5`,
		string(domain.DiscoveryStatusStale), s.orgID, accountID, string(domain.DiscoveryStatusActive), before)
	if err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}

func (s *Store) LinkResourceToPool(ctx context.Context, resourceID uuid.UUID, poolID int64) error {
	tag, err := s.pool.Exec(ctx, `UPDATE discovered_resources
		SET pool_id = $1
		WHERE id = $2 AND organization_id = $3`, poolID, resourceID, s.orgID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return storage.ErrNotFound
	}
	return nil
}

func (s *Store) UnlinkResource(ctx context.Context, resourceID uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `UPDATE discovered_resources
		SET pool_id = NULL
		WHERE id = $1 AND organization_id = $2`, resourceID, s.orgID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return storage.ErrNotFound
	}
	return nil
}

func (s *Store) DeleteDiscoveredResource(ctx context.Context, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM discovered_resources WHERE id = $1 AND organization_id = $2`, id, s.orgID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return storage.ErrNotFound
	}
	return nil
}

func (s *Store) CreateSyncJob(ctx context.Context, job domain.SyncJob) (domain.SyncJob, error) {
	if job.ID == uuid.Nil {
		job.ID = uuid.New()
	}
	if job.CreatedAt.IsZero() {
		job.CreatedAt = time.Now().UTC()
	}
	_, err := s.pool.Exec(ctx, `INSERT INTO sync_jobs
		(id, organization_id, account_id, status, source, agent_id, started_at, completed_at, resources_found, resources_created, resources_updated, resources_deleted, error_message, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)`,
		job.ID, s.orgID, job.AccountID, string(job.Status), job.Source, job.AgentID, job.StartedAt, job.CompletedAt,
		job.ResourcesFound, job.ResourcesCreated, job.ResourcesUpdated, job.ResourcesDeleted, job.ErrorMessage, job.CreatedAt)
	if err != nil {
		return domain.SyncJob{}, err
	}
	return job, nil
}

func (s *Store) UpdateSyncJob(ctx context.Context, job domain.SyncJob) error {
	tag, err := s.pool.Exec(ctx, `UPDATE sync_jobs SET
			status = $1,
			source = $2,
			agent_id = $3,
			started_at = $4,
			completed_at = $5,
			resources_found = $6,
			resources_created = $7,
			resources_updated = $8,
			resources_deleted = $9,
			error_message = $10
		WHERE id = $11 AND organization_id = $12`,
		string(job.Status), job.Source, job.AgentID, job.StartedAt, job.CompletedAt,
		job.ResourcesFound, job.ResourcesCreated, job.ResourcesUpdated, job.ResourcesDeleted, job.ErrorMessage,
		job.ID, s.orgID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return storage.ErrNotFound
	}
	return nil
}

func (s *Store) GetSyncJob(ctx context.Context, id uuid.UUID) (*domain.SyncJob, error) {
	row := s.pool.QueryRow(ctx, `SELECT id, account_id, status, source, agent_id, started_at, completed_at, resources_found, resources_created, resources_updated, resources_deleted, error_message, created_at
		FROM sync_jobs
		WHERE id = $1 AND organization_id = $2`, id, s.orgID)
	return scanPostgresSyncJob(row)
}

func (s *Store) ListSyncJobs(ctx context.Context, accountID int64, limit int) ([]domain.SyncJob, error) {
	if limit < 1 {
		limit = 20
	}
	rows, err := s.pool.Query(ctx, `SELECT id, account_id, status, source, agent_id, started_at, completed_at, resources_found, resources_created, resources_updated, resources_deleted, error_message, created_at
		FROM sync_jobs
		WHERE organization_id = $1 AND account_id = $2
		ORDER BY created_at DESC
		LIMIT $3`, s.orgID, accountID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.SyncJob
	for rows.Next() {
		job, err := scanPostgresSyncJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *job)
	}
	if out == nil {
		out = []domain.SyncJob{}
	}
	return out, rows.Err()
}

func (s *Store) ClaimPendingAgentSync(ctx context.Context, agentID uuid.UUID) (*domain.SyncJob, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	row := tx.QueryRow(ctx, `SELECT id, account_id, status, source, agent_id, started_at, completed_at, resources_found, resources_created, resources_updated, resources_deleted, error_message, created_at
		FROM sync_jobs
		WHERE organization_id = $1 AND status = $2 AND source = 'agent' AND agent_id = $3
		ORDER BY created_at ASC
		LIMIT 1
		FOR UPDATE SKIP LOCKED`, s.orgID, string(domain.SyncJobStatusPending), agentID)
	job, err := scanPostgresSyncJob(row)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	tag, err := tx.Exec(ctx, `UPDATE sync_jobs
		SET status = $1, started_at = $2
		WHERE id = $3 AND organization_id = $4 AND status = $5`,
		string(domain.SyncJobStatusRunning), now, job.ID, s.orgID, string(domain.SyncJobStatusPending))
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, storage.ErrNotFound
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	job.Status = domain.SyncJobStatusRunning
	job.StartedAt = &now
	return job, nil
}

func (s *Store) UpsertAgent(ctx context.Context, agent domain.DiscoveryAgent) error {
	if agent.ID == uuid.Nil {
		agent.ID = uuid.New()
	}
	if agent.CreatedAt.IsZero() {
		agent.CreatedAt = time.Now().UTC()
	}
	if agent.LastSeenAt.IsZero() {
		agent.LastSeenAt = time.Now().UTC()
	}
	status := string(agent.ApprovalStatus)
	if status == "" {
		status = string(domain.AgentApprovalApproved)
	}

	_, err := s.pool.Exec(ctx, `INSERT INTO discovery_agents
		(id, organization_id, name, account_id, api_key_id, version, hostname, last_seen_at, created_at, status, registered_at, approved_at, approved_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		ON CONFLICT (id) DO UPDATE SET
			name = EXCLUDED.name,
			account_id = EXCLUDED.account_id,
			api_key_id = EXCLUDED.api_key_id,
			version = EXCLUDED.version,
			hostname = EXCLUDED.hostname,
			last_seen_at = EXCLUDED.last_seen_at,
			status = EXCLUDED.status,
			registered_at = EXCLUDED.registered_at,
			approved_at = EXCLUDED.approved_at,
			approved_by = EXCLUDED.approved_by`,
		agent.ID, s.orgID, agent.Name, agent.AccountID, agent.APIKeyID, agent.Version, agent.Hostname,
		agent.LastSeenAt, agent.CreatedAt, status, agent.RegisteredAt, agent.ApprovedAt, agent.ApprovedBy)
	return err
}

func (s *Store) GetAgent(ctx context.Context, id uuid.UUID) (*domain.DiscoveryAgent, error) {
	row := s.pool.QueryRow(ctx, `SELECT id, name, account_id, api_key_id, version, hostname, last_seen_at, created_at, status, registered_at, approved_at, approved_by
		FROM discovery_agents
		WHERE id = $1 AND organization_id = $2`, id, s.orgID)
	return scanPostgresAgent(row)
}

func (s *Store) DeleteAgent(ctx context.Context, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM discovery_agents WHERE id = $1 AND organization_id = $2`, id, s.orgID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return storage.ErrNotFound
	}
	return nil
}

func (s *Store) ListAgents(ctx context.Context, accountID int64) ([]domain.DiscoveryAgent, error) {
	args := []any{s.orgID}
	query := `SELECT id, name, account_id, api_key_id, version, hostname, last_seen_at, created_at, status, registered_at, approved_at, approved_by
		FROM discovery_agents
		WHERE organization_id = $1`
	if accountID > 0 {
		args = append(args, accountID)
		query += " AND account_id = $2"
	}
	query += " ORDER BY created_at DESC"

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var agents []domain.DiscoveryAgent
	for rows.Next() {
		agent, err := scanPostgresAgent(rows)
		if err != nil {
			return nil, err
		}
		agents = append(agents, *agent)
	}
	if agents == nil {
		agents = []domain.DiscoveryAgent{}
	}
	return agents, rows.Err()
}

func scanPostgresDiscoveredResource(row pgx.Row) (domain.DiscoveredResource, error) {
	var r domain.DiscoveredResource
	var metadataJSON []byte
	var parentResourceID *string
	var poolID *int64
	if err := row.Scan(&r.ID, &r.AccountID, &r.Provider, &r.Region, &r.ResourceType, &r.ResourceID, &r.Name, &r.CIDR, &parentResourceID, &poolID, &r.Status, &metadataJSON, &r.DiscoveredAt, &r.LastSeenAt); err != nil {
		if err == pgx.ErrNoRows {
			return domain.DiscoveredResource{}, storage.ErrNotFound
		}
		return domain.DiscoveredResource{}, err
	}
	r.ParentResourceID = parentResourceID
	r.PoolID = poolID
	_ = json.Unmarshal(metadataJSON, &r.Metadata)
	return r, nil
}

func scanPostgresSyncJob(row pgx.Row) (*domain.SyncJob, error) {
	var j domain.SyncJob
	if err := row.Scan(&j.ID, &j.AccountID, &j.Status, &j.Source, &j.AgentID, &j.StartedAt, &j.CompletedAt, &j.ResourcesFound, &j.ResourcesCreated, &j.ResourcesUpdated, &j.ResourcesDeleted, &j.ErrorMessage, &j.CreatedAt); err != nil {
		if err == pgx.ErrNoRows {
			return nil, storage.ErrNotFound
		}
		return nil, err
	}
	return &j, nil
}

func scanPostgresAgent(row pgx.Row) (*domain.DiscoveryAgent, error) {
	var a domain.DiscoveryAgent
	var approvalStatus string
	if err := row.Scan(&a.ID, &a.Name, &a.AccountID, &a.APIKeyID, &a.Version, &a.Hostname, &a.LastSeenAt, &a.CreatedAt, &approvalStatus, &a.RegisteredAt, &a.ApprovedAt, &a.ApprovedBy); err != nil {
		if err == pgx.ErrNoRows {
			return nil, storage.ErrNotFound
		}
		return nil, err
	}
	a.ApprovalStatus = domain.AgentApprovalStatus(approvalStatus)
	return &a, nil
}
