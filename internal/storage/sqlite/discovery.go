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

// ListDiscoveredResources returns paginated discovered resources for an account.
func (s *Store) ListDiscoveredResources(ctx context.Context, accountID int64, filters domain.DiscoveryFilters) ([]domain.DiscoveredResource, int, error) {
	where := []string{"account_id = ?"}
	args := []any{accountID}

	if filters.Provider != "" {
		where = append(where, "provider = ?")
		args = append(args, filters.Provider)
	}
	if filters.Region != "" {
		where = append(where, "region = ?")
		args = append(args, filters.Region)
	}
	if filters.ResourceType != "" {
		where = append(where, "resource_type = ?")
		args = append(args, filters.ResourceType)
	}
	if filters.Status != "" {
		where = append(where, "status = ?")
		args = append(args, filters.Status)
	}
	if filters.HasPool != nil {
		if *filters.HasPool {
			where = append(where, "pool_id IS NOT NULL")
		} else {
			where = append(where, "pool_id IS NULL")
		}
	}

	whereClause := strings.Join(where, " AND ")

	// Count total
	var total int
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM discovered_resources WHERE %s", whereClause)
	if err := s.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
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
		"SELECT id, account_id, provider, region, resource_type, resource_id, name, cidr, parent_resource_id, pool_id, status, metadata, discovered_at, last_seen_at FROM discovered_resources WHERE %s ORDER BY discovered_at DESC LIMIT ? OFFSET ?",
		whereClause,
	)
	args = append(args, pageSize, offset)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var out []domain.DiscoveredResource
	for rows.Next() {
		r, err := scanDiscoveredResource(rows)
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

// GetDiscoveredResource returns a single discovered resource by UUID.
func (s *Store) GetDiscoveredResource(ctx context.Context, id uuid.UUID) (*domain.DiscoveredResource, error) {
	row := s.db.QueryRowContext(ctx,
		"SELECT id, account_id, provider, region, resource_type, resource_id, name, cidr, parent_resource_id, pool_id, status, metadata, discovered_at, last_seen_at FROM discovered_resources WHERE id = ?",
		id.String(),
	)
	var r domain.DiscoveredResource
	var idStr, metadataJSON, discoveredAt, lastSeenAt string
	var parentResID sql.NullString
	var poolID sql.NullInt64
	if err := row.Scan(&idStr, &r.AccountID, &r.Provider, &r.Region, &r.ResourceType, &r.ResourceID, &r.Name, &r.CIDR, &parentResID, &poolID, &r.Status, &metadataJSON, &discoveredAt, &lastSeenAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, storage.ErrNotFound
		}
		return nil, err
	}
	r.ID = uuid.MustParse(idStr)
	if parentResID.Valid {
		r.ParentResourceID = &parentResID.String
	}
	if poolID.Valid {
		r.PoolID = &poolID.Int64
	}
	_ = json.Unmarshal([]byte(metadataJSON), &r.Metadata)
	r.DiscoveredAt, _ = time.Parse(time.RFC3339, discoveredAt)
	r.LastSeenAt, _ = time.Parse(time.RFC3339, lastSeenAt)
	return &r, nil
}

// UpsertDiscoveredResource inserts or updates a resource keyed by (account_id, resource_id).
func (s *Store) UpsertDiscoveredResource(ctx context.Context, res domain.DiscoveredResource) error {
	if res.ID == uuid.Nil {
		res.ID = uuid.New()
	}
	metadataJSON := "{}"
	if res.Metadata != nil {
		if b, err := json.Marshal(res.Metadata); err == nil {
			metadataJSON = string(b)
		}
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if res.DiscoveredAt.IsZero() {
		res.DiscoveredAt = time.Now().UTC()
	}
	if res.LastSeenAt.IsZero() {
		res.LastSeenAt = time.Now().UTC()
	}

	var parentResID *string
	if res.ParentResourceID != nil {
		parentResID = res.ParentResourceID
	}

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO discovered_resources (id, account_id, provider, region, resource_type, resource_id, name, cidr, parent_resource_id, pool_id, status, metadata, discovered_at, last_seen_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(account_id, resource_id) DO UPDATE SET
		   name = excluded.name,
		   cidr = excluded.cidr,
		   region = excluded.region,
		   parent_resource_id = excluded.parent_resource_id,
		   status = excluded.status,
		   metadata = excluded.metadata,
		   last_seen_at = excluded.last_seen_at`,
		res.ID.String(), res.AccountID, res.Provider, res.Region, string(res.ResourceType), res.ResourceID,
		res.Name, res.CIDR, parentResID, res.PoolID, string(res.Status), metadataJSON,
		res.DiscoveredAt.Format(time.RFC3339), now,
	)
	return err
}

// MarkStaleResources marks active resources not seen since the given time as stale.
func (s *Store) MarkStaleResources(ctx context.Context, accountID int64, before time.Time) (int, error) {
	res, err := s.db.ExecContext(ctx,
		"UPDATE discovered_resources SET status = ? WHERE account_id = ? AND status = ? AND last_seen_at < ?",
		string(domain.DiscoveryStatusStale), accountID, string(domain.DiscoveryStatusActive), before.Format(time.RFC3339),
	)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// LinkResourceToPool links a discovered resource to a managed pool.
func (s *Store) LinkResourceToPool(ctx context.Context, resourceID uuid.UUID, poolID int64) error {
	res, err := s.db.ExecContext(ctx,
		"UPDATE discovered_resources SET pool_id = ? WHERE id = ?",
		poolID, resourceID.String(),
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

// UnlinkResource removes the pool link from a discovered resource.
func (s *Store) UnlinkResource(ctx context.Context, resourceID uuid.UUID) error {
	res, err := s.db.ExecContext(ctx,
		"UPDATE discovered_resources SET pool_id = NULL WHERE id = ?",
		resourceID.String(),
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

// DeleteDiscoveredResource deletes a discovered resource by ID.
func (s *Store) DeleteDiscoveredResource(ctx context.Context, id uuid.UUID) error {
	res, err := s.db.ExecContext(ctx,
		"DELETE FROM discovered_resources WHERE id = ?",
		id.String(),
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

// CreateSyncJob creates a new sync job record.
func (s *Store) CreateSyncJob(ctx context.Context, job domain.SyncJob) (domain.SyncJob, error) {
	if job.ID == uuid.Nil {
		job.ID = uuid.New()
	}
	if job.CreatedAt.IsZero() {
		job.CreatedAt = time.Now().UTC()
	}

	var startedAt, completedAt *string
	if job.StartedAt != nil {
		s := job.StartedAt.Format(time.RFC3339)
		startedAt = &s
	}
	if job.CompletedAt != nil {
		s := job.CompletedAt.Format(time.RFC3339)
		completedAt = &s
	}

	var agentID *string
	if job.AgentID != nil {
		s := job.AgentID.String()
		agentID = &s
	}

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sync_jobs (id, account_id, status, source, agent_id, started_at, completed_at, resources_found, resources_created, resources_updated, resources_deleted, error_message, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		job.ID.String(), job.AccountID, string(job.Status), job.Source, agentID, startedAt, completedAt,
		job.ResourcesFound, job.ResourcesCreated, job.ResourcesUpdated, job.ResourcesDeleted,
		job.ErrorMessage, job.CreatedAt.Format(time.RFC3339),
	)
	if err != nil {
		return domain.SyncJob{}, err
	}
	return job, nil
}

// UpdateSyncJob updates an existing sync job.
func (s *Store) UpdateSyncJob(ctx context.Context, job domain.SyncJob) error {
	var startedAt, completedAt *string
	if job.StartedAt != nil {
		s := job.StartedAt.Format(time.RFC3339)
		startedAt = &s
	}
	if job.CompletedAt != nil {
		s := job.CompletedAt.Format(time.RFC3339)
		completedAt = &s
	}

	var agentID *string
	if job.AgentID != nil {
		s := job.AgentID.String()
		agentID = &s
	}

	res, err := s.db.ExecContext(ctx,
		`UPDATE sync_jobs SET status = ?, source = ?, agent_id = ?, started_at = ?, completed_at = ?, resources_found = ?, resources_created = ?, resources_updated = ?, resources_deleted = ?, error_message = ? WHERE id = ?`,
		string(job.Status), job.Source, agentID, startedAt, completedAt,
		job.ResourcesFound, job.ResourcesCreated, job.ResourcesUpdated, job.ResourcesDeleted,
		job.ErrorMessage, job.ID.String(),
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

// GetSyncJob returns a sync job by UUID.
func (s *Store) GetSyncJob(ctx context.Context, id uuid.UUID) (*domain.SyncJob, error) {
	row := s.db.QueryRowContext(ctx,
		"SELECT id, account_id, status, source, agent_id, started_at, completed_at, resources_found, resources_created, resources_updated, resources_deleted, error_message, created_at FROM sync_jobs WHERE id = ?",
		id.String(),
	)
	return scanSyncJob(row)
}

// ListSyncJobs returns recent sync jobs for an account.
func (s *Store) ListSyncJobs(ctx context.Context, accountID int64, limit int) ([]domain.SyncJob, error) {
	if limit < 1 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx,
		"SELECT id, account_id, status, source, agent_id, started_at, completed_at, resources_found, resources_created, resources_updated, resources_deleted, error_message, created_at FROM sync_jobs WHERE account_id = ? ORDER BY created_at DESC LIMIT ?",
		accountID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.SyncJob
	for rows.Next() {
		var j domain.SyncJob
		var idStr, createdAt string
		var startedAt, completedAt, agentID sql.NullString
		if err := rows.Scan(&idStr, &j.AccountID, &j.Status, &j.Source, &agentID, &startedAt, &completedAt, &j.ResourcesFound, &j.ResourcesCreated, &j.ResourcesUpdated, &j.ResourcesDeleted, &j.ErrorMessage, &createdAt); err != nil {
			return nil, err
		}
		j.ID = uuid.MustParse(idStr)
		j.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		if agentID.Valid {
			aid := uuid.MustParse(agentID.String)
			j.AgentID = &aid
		}
		if startedAt.Valid {
			t, _ := time.Parse(time.RFC3339, startedAt.String)
			j.StartedAt = &t
		}
		if completedAt.Valid {
			t, _ := time.Parse(time.RFC3339, completedAt.String)
			j.CompletedAt = &t
		}
		out = append(out, j)
	}
	if out == nil {
		out = []domain.SyncJob{}
	}
	return out, rows.Err()
}

// scanDiscoveredResource scans a row into a DiscoveredResource.
func scanDiscoveredResource(rows *sql.Rows) (domain.DiscoveredResource, error) {
	var r domain.DiscoveredResource
	var idStr, metadataJSON, discoveredAt, lastSeenAt string
	var parentResID sql.NullString
	var poolID sql.NullInt64
	if err := rows.Scan(&idStr, &r.AccountID, &r.Provider, &r.Region, &r.ResourceType, &r.ResourceID, &r.Name, &r.CIDR, &parentResID, &poolID, &r.Status, &metadataJSON, &discoveredAt, &lastSeenAt); err != nil {
		return domain.DiscoveredResource{}, err
	}
	r.ID = uuid.MustParse(idStr)
	if parentResID.Valid {
		r.ParentResourceID = &parentResID.String
	}
	if poolID.Valid {
		r.PoolID = &poolID.Int64
	}
	_ = json.Unmarshal([]byte(metadataJSON), &r.Metadata)
	r.DiscoveredAt, _ = time.Parse(time.RFC3339, discoveredAt)
	r.LastSeenAt, _ = time.Parse(time.RFC3339, lastSeenAt)
	return r, nil
}

// scanSyncJob scans a single row into a SyncJob.
func scanSyncJob(row *sql.Row) (*domain.SyncJob, error) {
	var j domain.SyncJob
	var idStr, createdAt string
	var startedAt, completedAt, agentID sql.NullString
	if err := row.Scan(&idStr, &j.AccountID, &j.Status, &j.Source, &agentID, &startedAt, &completedAt, &j.ResourcesFound, &j.ResourcesCreated, &j.ResourcesUpdated, &j.ResourcesDeleted, &j.ErrorMessage, &createdAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, storage.ErrNotFound
		}
		return nil, err
	}
	j.ID = uuid.MustParse(idStr)
	j.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	if agentID.Valid {
		aid := uuid.MustParse(agentID.String)
		j.AgentID = &aid
	}
	if startedAt.Valid {
		t, _ := time.Parse(time.RFC3339, startedAt.String)
		j.StartedAt = &t
	}
	if completedAt.Valid {
		t, _ := time.Parse(time.RFC3339, completedAt.String)
		j.CompletedAt = &t
	}
	return &j, nil
}

// UpsertAgent inserts or updates a discovery agent.
func (s *Store) UpsertAgent(ctx context.Context, agent domain.DiscoveryAgent) error {
	if agent.ID == uuid.Nil {
		agent.ID = uuid.New()
	}
	if agent.CreatedAt.IsZero() {
		agent.CreatedAt = time.Now().UTC()
	}

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO discovery_agents (id, name, account_id, api_key_id, version, hostname, last_seen_at, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
		   name = excluded.name,
		   account_id = excluded.account_id,
		   api_key_id = excluded.api_key_id,
		   version = excluded.version,
		   hostname = excluded.hostname,
		   last_seen_at = excluded.last_seen_at`,
		agent.ID.String(), agent.Name, agent.AccountID, agent.APIKeyID,
		agent.Version, agent.Hostname, agent.LastSeenAt.Format(time.RFC3339),
		agent.CreatedAt.Format(time.RFC3339),
	)
	return err
}

// GetAgent returns a discovery agent by ID.
func (s *Store) GetAgent(ctx context.Context, id uuid.UUID) (*domain.DiscoveryAgent, error) {
	row := s.db.QueryRowContext(ctx,
		"SELECT id, name, account_id, api_key_id, version, hostname, last_seen_at, created_at FROM discovery_agents WHERE id = ?",
		id.String(),
	)

	var a domain.DiscoveryAgent
	var idStr, lastSeenAt, createdAt string
	if err := row.Scan(&idStr, &a.Name, &a.AccountID, &a.APIKeyID, &a.Version, &a.Hostname, &lastSeenAt, &createdAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, storage.ErrNotFound
		}
		return nil, err
	}

	a.ID = uuid.MustParse(idStr)
	a.LastSeenAt, _ = time.Parse(time.RFC3339, lastSeenAt)
	a.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	return &a, nil
}

// ListAgents returns all discovery agents, optionally filtered by account ID.
func (s *Store) ListAgents(ctx context.Context, accountID int64) ([]domain.DiscoveryAgent, error) {
	query := "SELECT id, name, account_id, api_key_id, version, hostname, last_seen_at, created_at FROM discovery_agents"
	args := []any{}

	if accountID > 0 {
		query += " WHERE account_id = ?"
		args = append(args, accountID)
	}

	query += " ORDER BY created_at DESC"

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var agents []domain.DiscoveryAgent
	for rows.Next() {
		var a domain.DiscoveryAgent
		var idStr, lastSeenAt, createdAt string
		if err := rows.Scan(&idStr, &a.Name, &a.AccountID, &a.APIKeyID, &a.Version, &a.Hostname, &lastSeenAt, &createdAt); err != nil {
			return nil, err
		}
		a.ID = uuid.MustParse(idStr)
		a.LastSeenAt, _ = time.Parse(time.RFC3339, lastSeenAt)
		a.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		agents = append(agents, a)
	}

	if agents == nil {
		agents = []domain.DiscoveryAgent{}
	}
	return agents, rows.Err()
}

// CreateBootstrapToken creates a new bootstrap token.
func (s *Store) CreateBootstrapToken(ctx context.Context, token domain.BootstrapToken) (domain.BootstrapToken, error) {
	if token.ID == "" {
		token.ID = uuid.New().String()
	}
	if token.CreatedAt.IsZero() {
		token.CreatedAt = time.Now().UTC()
	}

	var accountID *int64
	if token.AccountID != nil {
		accountID = token.AccountID
	}

	var expiresAt *string
	if token.ExpiresAt != nil {
		exp := token.ExpiresAt.Format(time.RFC3339)
		expiresAt = &exp
	}

	var maxUses *int
	if token.MaxUses != nil {
		maxUses = token.MaxUses
	}

	_, err := s.db.ExecContext(ctx,
		`INSERT INTO bootstrap_tokens (id, name, token_hash, account_id, created_by, expires_at, revoked, used_count, max_uses, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		token.ID, token.Name, token.TokenHash, accountID, token.CreatedBy, expiresAt,
		0, 0, maxUses, token.CreatedAt.Format(time.RFC3339),
	)
	if err != nil {
		return domain.BootstrapToken{}, err
	}

	// Don't return plaintext token
	token.Token = ""
	return token, nil
}

// GetBootstrapToken returns a bootstrap token by ID.
func (s *Store) GetBootstrapToken(ctx context.Context, id string) (*domain.BootstrapToken, error) {
	row := s.db.QueryRowContext(ctx,
		"SELECT id, name, token_hash, account_id, created_by, expires_at, revoked, used_count, max_uses, created_at FROM bootstrap_tokens WHERE id = ?",
		id,
	)

	var t domain.BootstrapToken
	var accountID sql.NullInt64
	var expiresAt sql.NullString
	var maxUses sql.NullInt64
	var revoked int
	var createdAt string

	if err := row.Scan(&t.ID, &t.Name, &t.TokenHash, &accountID, &t.CreatedBy, &expiresAt, &revoked, &t.UsedCount, &maxUses, &createdAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, storage.ErrNotFound
		}
		return nil, err
	}

	if accountID.Valid {
		t.AccountID = &accountID.Int64
	}
	if expiresAt.Valid {
		exp, _ := time.Parse(time.RFC3339, expiresAt.String)
		t.ExpiresAt = &exp
	}
	if maxUses.Valid {
		mu := int(maxUses.Int64)
		t.MaxUses = &mu
	}
	t.Revoked = revoked != 0
	t.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)

	return &t, nil
}

// GetBootstrapTokenByToken returns a bootstrap token by hash lookup.
func (s *Store) GetBootstrapTokenByToken(ctx context.Context, tokenHash []byte) (*domain.BootstrapToken, error) {
	row := s.db.QueryRowContext(ctx,
		"SELECT id, name, token_hash, account_id, created_by, expires_at, revoked, used_count, max_uses, created_at FROM bootstrap_tokens WHERE token_hash = ?",
		tokenHash,
	)

	var t domain.BootstrapToken
	var accountID sql.NullInt64
	var expiresAt sql.NullString
	var maxUses sql.NullInt64
	var revoked int
	var createdAt string

	if err := row.Scan(&t.ID, &t.Name, &t.TokenHash, &accountID, &t.CreatedBy, &expiresAt, &revoked, &t.UsedCount, &maxUses, &createdAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, storage.ErrNotFound
		}
		return nil, err
	}

	if accountID.Valid {
		t.AccountID = &accountID.Int64
	}
	if expiresAt.Valid {
		exp, _ := time.Parse(time.RFC3339, expiresAt.String)
		t.ExpiresAt = &exp
	}
	if maxUses.Valid {
		mu := int(maxUses.Int64)
		t.MaxUses = &mu
	}
	t.Revoked = revoked != 0
	t.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)

	return &t, nil
}

// ListBootstrapTokens returns all bootstrap tokens.
func (s *Store) ListBootstrapTokens(ctx context.Context) ([]domain.BootstrapToken, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT id, name, token_hash, account_id, created_by, expires_at, revoked, used_count, max_uses, created_at FROM bootstrap_tokens ORDER BY created_at DESC",
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tokens []domain.BootstrapToken
	for rows.Next() {
		var t domain.BootstrapToken
		var accountID sql.NullInt64
		var expiresAt sql.NullString
		var maxUses sql.NullInt64
		var revoked int
		var createdAt string

		if err := rows.Scan(&t.ID, &t.Name, &t.TokenHash, &accountID, &t.CreatedBy, &expiresAt, &revoked, &t.UsedCount, &maxUses, &createdAt); err != nil {
			return nil, err
		}

		if accountID.Valid {
			t.AccountID = &accountID.Int64
		}
		if expiresAt.Valid {
			exp, _ := time.Parse(time.RFC3339, expiresAt.String)
			t.ExpiresAt = &exp
		}
		if maxUses.Valid {
			mu := int(maxUses.Int64)
			t.MaxUses = &mu
		}
		t.Revoked = revoked != 0
		t.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)

		tokens = append(tokens, t)
	}

	if tokens == nil {
		tokens = []domain.BootstrapToken{}
	}
	return tokens, rows.Err()
}

// RevokeBootstrapToken marks a token as revoked.
func (s *Store) RevokeBootstrapToken(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx,
		"UPDATE bootstrap_tokens SET revoked = 1 WHERE id = ?",
		id,
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

// IncrementBootstrapTokenUses increments the used count.
func (s *Store) IncrementBootstrapTokenUses(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx,
		"UPDATE bootstrap_tokens SET used_count = used_count + 1 WHERE id = ?",
		id,
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
