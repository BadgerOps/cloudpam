//go:build postgres

package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"net/netip"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"cloudpam/internal/cidr"
	"cloudpam/internal/domain"
	"cloudpam/internal/storage"
)

// defaultOrgID is the UUID of the default organization for single-tenant deployments.
const defaultOrgID = "00000000-0000-0000-0000-000000000001"

// Store implements storage.Store backed by PostgreSQL.
type Store struct {
	pool  *pgxpool.Pool
	orgID string // current organization UUID
}

var _ storage.Store = (*Store)(nil)

// New creates a new PostgreSQL-backed store.
// connStr is a PostgreSQL connection string (e.g., postgres://user:pass@host/db).
func New(connStr string) (*Store, error) {
	ctx := context.Background()
	config, err := pgxpool.ParseConfig(connStr)
	if err != nil {
		return nil, fmt.Errorf("parse connection string: %w", err)
	}

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("create connection pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	if err := runMigrations(ctx, pool); err != nil {
		pool.Close()
		return nil, fmt.Errorf("run migrations: %w", err)
	}

	return &Store{pool: pool, orgID: defaultOrgID}, nil
}

// NewFromPool creates a Store from an existing connection pool. Migrations are NOT run.
func NewFromPool(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool, orgID: defaultOrgID}
}

// Pool returns the underlying pgxpool for shared access (audit logger, key store).
func (s *Store) Pool() *pgxpool.Pool {
	return s.pool
}

// Close closes the connection pool.
func (s *Store) Close() error {
	s.pool.Close()
	return nil
}

// Ping checks database connectivity (implements storage.HealthCheck).
func (s *Store) Ping(ctx context.Context) error {
	return s.pool.Ping(ctx)
}

// Stats returns connection pool statistics (implements storage.HealthCheck).
func (s *Store) Stats() *storage.DBStats {
	stat := s.pool.Stat()
	return &storage.DBStats{
		MaxOpenConnections: int(stat.MaxConns()),
		OpenConnections:    int(stat.TotalConns()),
		InUse:              int(stat.AcquiredConns()),
		Idle:               int(stat.IdleConns()),
		WaitCount:          stat.EmptyAcquireCount(),
		WaitDuration:       stat.AcquireDuration().Nanoseconds(),
	}
}

// =============================================================================
// Pool Operations
// =============================================================================

const poolColumns = `seq_id, name, cidr, parent_id, account_id, type, status, source, description, tags, created_at, updated_at`

func (s *Store) scanPool(row pgx.Row) (domain.Pool, bool, error) {
	var p domain.Pool
	var parentSeq, accountSeq *int64
	var tagsJSON []byte
	var createdAt, updatedAt time.Time

	err := row.Scan(
		&p.ID, &p.Name, &p.CIDR,
		&parentSeq, &accountSeq,
		&p.Type, &p.Status, &p.Source,
		&p.Description, &tagsJSON,
		&createdAt, &updatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return domain.Pool{}, false, nil
		}
		return domain.Pool{}, false, err
	}

	p.ParentID = parentSeq
	p.AccountID = accountSeq
	p.CreatedAt = createdAt
	p.UpdatedAt = updatedAt

	if len(tagsJSON) > 0 {
		_ = json.Unmarshal(tagsJSON, &p.Tags)
	}

	return p, true, nil
}

func (s *Store) ListPools(ctx context.Context) ([]domain.Pool, error) {
	query := fmt.Sprintf(`
		SELECT p.%s
		FROM pools p
		WHERE p.organization_id = $1 AND p.deleted_at IS NULL
		ORDER BY p.seq_id ASC`, poolColumnsWithParentAccount())

	rows, err := s.pool.Query(ctx, query, s.orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.Pool
	for rows.Next() {
		var p domain.Pool
		var parentSeq, accountSeq *int64
		var tagsJSON []byte
		var createdAt, updatedAt time.Time

		if err := rows.Scan(
			&p.ID, &p.Name, &p.CIDR,
			&parentSeq, &accountSeq,
			&p.Type, &p.Status, &p.Source,
			&p.Description, &tagsJSON,
			&createdAt, &updatedAt,
		); err != nil {
			return nil, err
		}

		p.ParentID = parentSeq
		p.AccountID = accountSeq
		p.CreatedAt = createdAt
		p.UpdatedAt = updatedAt

		if len(tagsJSON) > 0 {
			_ = json.Unmarshal(tagsJSON, &p.Tags)
		}

		out = append(out, p)
	}
	return out, rows.Err()
}

// poolColumnsWithParentAccount returns the SELECT clause resolving parent/account UUIDs to seq_ids.
func poolColumnsWithParentAccount() string {
	return `seq_id, p.name, p.cidr::text,
		(SELECT pp.seq_id FROM pools pp WHERE pp.id = p.parent_id),
		(SELECT a.seq_id FROM accounts a WHERE a.id = p.account_id),
		p.type, p.status, p.source, p.description, p.tags,
		p.created_at, p.updated_at`
}

func (s *Store) CreatePool(ctx context.Context, in domain.CreatePool) (domain.Pool, error) {
	if in.Name == "" || in.CIDR == "" {
		return domain.Pool{}, fmt.Errorf("name and cidr required: %w", storage.ErrValidation)
	}

	poolType := in.Type
	if poolType == "" {
		poolType = domain.PoolTypeSubnet
	}
	poolStatus := in.Status
	if poolStatus == "" {
		poolStatus = domain.PoolStatusActive
	}
	poolSource := in.Source
	if poolSource == "" {
		poolSource = domain.PoolSourceManual
	}

	tags := in.Tags
	if tags == nil {
		tags = map[string]string{}
	}
	tagsJSON, _ := json.Marshal(tags)

	// Resolve parent seq_id -> UUID
	var parentUUID *string
	if in.ParentID != nil {
		var uuid string
		err := s.pool.QueryRow(ctx, `SELECT id FROM pools WHERE seq_id = $1 AND organization_id = $2 AND deleted_at IS NULL`, *in.ParentID, s.orgID).Scan(&uuid)
		if err != nil {
			return domain.Pool{}, fmt.Errorf("parent pool not found: %w", storage.ErrNotFound)
		}
		parentUUID = &uuid
	}

	// Resolve account seq_id -> UUID
	var accountUUID *string
	if in.AccountID != nil {
		var uuid string
		err := s.pool.QueryRow(ctx, `SELECT id FROM accounts WHERE seq_id = $1 AND organization_id = $2 AND deleted_at IS NULL`, *in.AccountID, s.orgID).Scan(&uuid)
		if err != nil {
			return domain.Pool{}, fmt.Errorf("account not found: %w", storage.ErrNotFound)
		}
		accountUUID = &uuid
	}

	var p domain.Pool
	var parentSeq, accountSeq *int64
	var tagsOut []byte
	var createdAt, updatedAt time.Time

	err := s.pool.QueryRow(ctx, `
		INSERT INTO pools (organization_id, parent_id, account_id, name, description, cidr, type, status, source, tags)
		VALUES ($1, $2, $3, $4, $5, $6::inet, $7, $8, $9, $10::jsonb)
		RETURNING seq_id, name, cidr::text,
			(SELECT pp.seq_id FROM pools pp WHERE pp.id = pools.parent_id),
			(SELECT a.seq_id FROM accounts a WHERE a.id = pools.account_id),
			type, status, source, description, tags, created_at, updated_at`,
		s.orgID, parentUUID, accountUUID, in.Name, in.Description, in.CIDR,
		string(poolType), string(poolStatus), string(poolSource), string(tagsJSON),
	).Scan(
		&p.ID, &p.Name, &p.CIDR,
		&parentSeq, &accountSeq,
		&p.Type, &p.Status, &p.Source,
		&p.Description, &tagsOut,
		&createdAt, &updatedAt,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return domain.Pool{}, fmt.Errorf("pool with this CIDR already exists: %w", storage.ErrConflict)
		}
		return domain.Pool{}, err
	}

	p.ParentID = parentSeq
	p.AccountID = accountSeq
	p.CreatedAt = createdAt
	p.UpdatedAt = updatedAt
	if len(tagsOut) > 0 {
		_ = json.Unmarshal(tagsOut, &p.Tags)
	}

	return p, nil
}

func (s *Store) GetPool(ctx context.Context, id int64) (domain.Pool, bool, error) {
	query := fmt.Sprintf(`
		SELECT p.%s
		FROM pools p
		WHERE p.seq_id = $1 AND p.organization_id = $2 AND p.deleted_at IS NULL`, poolColumnsWithParentAccount())

	row := s.pool.QueryRow(ctx, query, id, s.orgID)
	return s.scanPool(row)
}

func (s *Store) UpdatePoolAccount(ctx context.Context, id int64, accountID *int64) (domain.Pool, bool, error) {
	var accountUUID *string
	if accountID != nil {
		var uuid string
		err := s.pool.QueryRow(ctx, `SELECT id FROM accounts WHERE seq_id = $1 AND organization_id = $2 AND deleted_at IS NULL`, *accountID, s.orgID).Scan(&uuid)
		if err != nil {
			return domain.Pool{}, false, fmt.Errorf("account not found: %w", storage.ErrNotFound)
		}
		accountUUID = &uuid
	}

	tag, err := s.pool.Exec(ctx, `
		UPDATE pools SET account_id = $2
		WHERE seq_id = $1 AND organization_id = $3 AND deleted_at IS NULL`,
		id, accountUUID, s.orgID)
	if err != nil {
		return domain.Pool{}, false, err
	}
	if tag.RowsAffected() == 0 {
		return domain.Pool{}, false, nil
	}

	p, found, err := s.GetPool(ctx, id)
	return p, found, err
}

func (s *Store) UpdatePoolMeta(ctx context.Context, id int64, name *string, accountID *int64) (domain.Pool, bool, error) {
	// Resolve account
	var accountUUID *string
	if accountID != nil {
		var uuid string
		err := s.pool.QueryRow(ctx, `SELECT id FROM accounts WHERE seq_id = $1 AND organization_id = $2 AND deleted_at IS NULL`, *accountID, s.orgID).Scan(&uuid)
		if err != nil {
			return domain.Pool{}, false, fmt.Errorf("account not found: %w", storage.ErrNotFound)
		}
		accountUUID = &uuid
	}

	setClauses := []string{"account_id = $2"}
	args := []any{id, accountUUID, s.orgID}
	argIdx := 4

	if name != nil {
		setClauses = append(setClauses, fmt.Sprintf("name = $%d", argIdx))
		args = append(args, *name)
		argIdx++
	}

	query := fmt.Sprintf(`UPDATE pools SET %s WHERE seq_id = $1 AND organization_id = $3 AND deleted_at IS NULL`,
		strings.Join(setClauses, ", "))

	tag, err := s.pool.Exec(ctx, query, args...)
	if err != nil {
		return domain.Pool{}, false, err
	}
	if tag.RowsAffected() == 0 {
		return domain.Pool{}, false, nil
	}

	return s.GetPool(ctx, id)
}

func (s *Store) UpdatePool(ctx context.Context, id int64, update domain.UpdatePool) (domain.Pool, bool, error) {
	setClauses := []string{}
	args := []any{id, s.orgID}
	argIdx := 3

	// Resolve account
	var accountUUID *string
	if update.AccountID != nil {
		var uuid string
		err := s.pool.QueryRow(ctx, `SELECT id FROM accounts WHERE seq_id = $1 AND organization_id = $2 AND deleted_at IS NULL`, *update.AccountID, s.orgID).Scan(&uuid)
		if err != nil {
			return domain.Pool{}, false, fmt.Errorf("account not found: %w", storage.ErrNotFound)
		}
		accountUUID = &uuid
	}
	setClauses = append(setClauses, fmt.Sprintf("account_id = $%d", argIdx))
	args = append(args, accountUUID)
	argIdx++

	if update.Name != nil {
		setClauses = append(setClauses, fmt.Sprintf("name = $%d", argIdx))
		args = append(args, *update.Name)
		argIdx++
	}
	if update.Type != nil {
		setClauses = append(setClauses, fmt.Sprintf("type = $%d", argIdx))
		args = append(args, string(*update.Type))
		argIdx++
	}
	if update.Status != nil {
		setClauses = append(setClauses, fmt.Sprintf("status = $%d", argIdx))
		args = append(args, string(*update.Status))
		argIdx++
	}
	if update.Description != nil {
		setClauses = append(setClauses, fmt.Sprintf("description = $%d", argIdx))
		args = append(args, *update.Description)
		argIdx++
	}
	if update.Tags != nil {
		tagsJSON, _ := json.Marshal(*update.Tags)
		setClauses = append(setClauses, fmt.Sprintf("tags = $%d::jsonb", argIdx))
		args = append(args, string(tagsJSON))
		argIdx++
	}

	if len(setClauses) == 0 {
		return s.GetPool(ctx, id)
	}

	query := fmt.Sprintf(`UPDATE pools SET %s WHERE seq_id = $1 AND organization_id = $2 AND deleted_at IS NULL`,
		strings.Join(setClauses, ", "))

	tag, err := s.pool.Exec(ctx, query, args...)
	if err != nil {
		return domain.Pool{}, false, err
	}
	if tag.RowsAffected() == 0 {
		return domain.Pool{}, false, nil
	}

	return s.GetPool(ctx, id)
}

func (s *Store) DeletePool(ctx context.Context, id int64) (bool, error) {
	// Check for children
	var childCount int
	err := s.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM pools
		WHERE parent_id = (SELECT id FROM pools WHERE seq_id = $1 AND organization_id = $2)
		  AND deleted_at IS NULL`, id, s.orgID).Scan(&childCount)
	if err != nil {
		return false, err
	}
	if childCount > 0 {
		return false, fmt.Errorf("pool has child pools: %w", storage.ErrConflict)
	}

	tag, err := s.pool.Exec(ctx, `
		UPDATE pools SET deleted_at = NOW()
		WHERE seq_id = $1 AND organization_id = $2 AND deleted_at IS NULL`, id, s.orgID)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

func (s *Store) DeletePoolCascade(ctx context.Context, id int64) (bool, error) {
	// Use recursive CTE to find all descendants, then soft-delete all
	tag, err := s.pool.Exec(ctx, `
		WITH RECURSIVE subtree AS (
			SELECT id FROM pools WHERE seq_id = $1 AND organization_id = $2 AND deleted_at IS NULL
			UNION ALL
			SELECT p.id FROM pools p JOIN subtree s ON p.parent_id = s.id WHERE p.deleted_at IS NULL
		)
		UPDATE pools SET deleted_at = NOW() WHERE id IN (SELECT id FROM subtree)`, id, s.orgID)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// =============================================================================
// Pool Statistics & Hierarchy
// =============================================================================

func (s *Store) GetPoolWithStats(ctx context.Context, id int64) (*domain.PoolWithStats, error) {
	p, found, err := s.GetPool(ctx, id)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, fmt.Errorf("pool not found: %w", storage.ErrNotFound)
	}

	stats, err := s.CalculatePoolUtilization(ctx, id)
	if err != nil {
		return nil, err
	}

	return &domain.PoolWithStats{Pool: p, Stats: *stats}, nil
}

func (s *Store) GetPoolHierarchy(ctx context.Context, rootID *int64) ([]domain.PoolWithStats, error) {
	var pools []domain.Pool
	var err error

	if rootID != nil {
		pools, err = s.getAllPoolsForHierarchy(ctx, rootID)
	} else {
		pools, err = s.getAllPoolsForHierarchy(ctx, nil)
	}
	if err != nil {
		return nil, err
	}

	return s.buildTree(ctx, pools, rootID)
}

func (s *Store) getAllPoolsForHierarchy(ctx context.Context, rootID *int64) ([]domain.Pool, error) {
	if rootID != nil {
		// Use recursive CTE to collect descendant UUIDs, then join to get full pool data
		query := fmt.Sprintf(`
			WITH RECURSIVE subtree AS (
				SELECT id FROM pools WHERE seq_id = $1 AND organization_id = $2 AND deleted_at IS NULL
				UNION ALL
				SELECT p.id FROM pools p JOIN subtree s ON p.parent_id = s.id WHERE p.deleted_at IS NULL
			)
			SELECT p.%s
			FROM pools p
			WHERE p.id IN (SELECT id FROM subtree)
			ORDER BY p.seq_id`, poolColumnsWithParentAccount())

		rows, err := s.pool.Query(ctx, query, *rootID, s.orgID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		return s.scanPools(rows)
	}

	// All pools
	return s.ListPools(ctx)
}

func (s *Store) scanPools(rows pgx.Rows) ([]domain.Pool, error) {
	var out []domain.Pool
	for rows.Next() {
		var p domain.Pool
		var parentSeq, accountSeq *int64
		var tagsJSON []byte
		var createdAt, updatedAt time.Time

		if err := rows.Scan(
			&p.ID, &p.Name, &p.CIDR,
			&parentSeq, &accountSeq,
			&p.Type, &p.Status, &p.Source,
			&p.Description, &tagsJSON,
			&createdAt, &updatedAt,
		); err != nil {
			return nil, err
		}

		p.ParentID = parentSeq
		p.AccountID = accountSeq
		p.CreatedAt = createdAt
		p.UpdatedAt = updatedAt
		if len(tagsJSON) > 0 {
			_ = json.Unmarshal(tagsJSON, &p.Tags)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) buildTree(ctx context.Context, pools []domain.Pool, rootID *int64) ([]domain.PoolWithStats, error) {
	// Build parent -> children map
	children := make(map[int64][]int64)
	poolMap := make(map[int64]domain.Pool)
	for _, p := range pools {
		poolMap[p.ID] = p
		if p.ParentID != nil {
			children[*p.ParentID] = append(children[*p.ParentID], p.ID)
		}
	}

	var buildNode func(id int64) domain.PoolWithStats
	buildNode = func(id int64) domain.PoolWithStats {
		p := poolMap[id]
		stats := calculatePoolStatsFromMap(p, poolMap)
		result := domain.PoolWithStats{Pool: p, Stats: stats}
		for _, childID := range children[id] {
			result.Children = append(result.Children, buildNode(childID))
		}
		return result
	}

	var result []domain.PoolWithStats
	if rootID != nil {
		for _, p := range pools {
			if p.ID == *rootID {
				result = append(result, buildNode(p.ID))
				break
			}
		}
		if len(result) == 0 {
			return nil, fmt.Errorf("root pool not found: %w", storage.ErrNotFound)
		}
	} else {
		for _, p := range pools {
			if p.ParentID == nil {
				result = append(result, buildNode(p.ID))
			}
		}
	}

	return result, nil
}

func (s *Store) GetPoolChildren(ctx context.Context, parentID int64) ([]domain.Pool, error) {
	// Verify parent exists
	var exists bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pools WHERE seq_id = $1 AND organization_id = $2 AND deleted_at IS NULL)`, parentID, s.orgID).Scan(&exists)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, fmt.Errorf("parent pool not found: %w", storage.ErrNotFound)
	}

	query := fmt.Sprintf(`
		SELECT p.%s
		FROM pools p
		WHERE p.parent_id = (SELECT id FROM pools WHERE seq_id = $1 AND organization_id = $2)
		  AND p.deleted_at IS NULL
		ORDER BY p.seq_id`, poolColumnsWithParentAccount())

	rows, err := s.pool.Query(ctx, query, parentID, s.orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return s.scanPools(rows)
}

func (s *Store) CalculatePoolUtilization(ctx context.Context, id int64) (*domain.PoolStats, error) {
	p, found, err := s.GetPool(ctx, id)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, fmt.Errorf("pool not found: %w", storage.ErrNotFound)
	}

	// Get all pools for stats calculation
	allPools, err := s.ListPools(ctx)
	if err != nil {
		return nil, err
	}

	poolMap := make(map[int64]domain.Pool)
	for _, pool := range allPools {
		poolMap[pool.ID] = pool
	}

	stats := calculatePoolStatsFromMap(p, poolMap)
	return &stats, nil
}

// calculatePoolStatsFromMap computes stats for a pool given a map of all pools.
func calculatePoolStatsFromMap(p domain.Pool, poolMap map[int64]domain.Pool) domain.PoolStats {
	prefix, err := netip.ParsePrefix(p.CIDR)
	if err != nil {
		return domain.PoolStats{}
	}

	var totalIPs int64
	if prefix.Addr().Is4() {
		totalIPs = int64(1) << (32 - prefix.Bits())
	} else {
		bits := 128 - prefix.Bits()
		if bits >= 63 {
			totalIPs = int64(1) << 62
		} else {
			totalIPs = int64(1) << bits
		}
	}

	var directChildren int
	var usedIPs int64

	var countDescendants func(parentID int64) int
	countDescendants = func(parentID int64) int {
		count := 0
		for _, child := range poolMap {
			if child.ParentID != nil && *child.ParentID == parentID {
				count++
				count += countDescendants(child.ID)
			}
		}
		return count
	}

	for _, child := range poolMap {
		if child.ParentID != nil && *child.ParentID == p.ID {
			directChildren++
			childPrefix, err := netip.ParsePrefix(child.CIDR)
			if err != nil {
				continue
			}
			if childPrefix.Addr().Is4() {
				usedIPs += int64(1) << (32 - childPrefix.Bits())
			}
		}
	}

	totalChildCount := countDescendants(p.ID)

	var utilization float64
	if totalIPs > 0 {
		utilization = float64(usedIPs) / float64(totalIPs) * 100
	}

	return domain.PoolStats{
		TotalIPs:       totalIPs,
		UsedIPs:        usedIPs,
		AvailableIPs:   totalIPs - usedIPs,
		Utilization:    utilization,
		ChildCount:     totalChildCount,
		DirectChildren: directChildren,
	}
}

// =============================================================================
// Account Operations
// =============================================================================

func (s *Store) ListAccounts(ctx context.Context) ([]domain.Account, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT seq_id, key, name, provider, external_id, description,
			platform, tier, environment, regions, created_at, updated_at
		FROM accounts
		WHERE organization_id = $1 AND deleted_at IS NULL
		ORDER BY seq_id ASC`, s.orgID)
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

func scanAccount(rows pgx.Rows) (domain.Account, error) {
	var a domain.Account
	var provider, externalID, description, platform, tier, environment *string
	var regionsJSON []byte
	var createdAt, updatedAt time.Time

	if err := rows.Scan(
		&a.ID, &a.Key, &a.Name,
		&provider, &externalID, &description,
		&platform, &tier, &environment,
		&regionsJSON, &createdAt, &updatedAt,
	); err != nil {
		return domain.Account{}, err
	}

	if provider != nil {
		a.Provider = *provider
	}
	if externalID != nil {
		a.ExternalID = *externalID
	}
	if description != nil {
		a.Description = *description
	}
	if platform != nil {
		a.Platform = *platform
	}
	if tier != nil {
		a.Tier = *tier
	}
	if environment != nil {
		a.Environment = *environment
	}
	a.CreatedAt = createdAt
	a.UpdatedAt = updatedAt

	if len(regionsJSON) > 0 {
		_ = json.Unmarshal(regionsJSON, &a.Regions)
	}

	return a, nil
}

func (s *Store) CreateAccount(ctx context.Context, in domain.CreateAccount) (domain.Account, error) {
	if in.Key == "" || in.Name == "" {
		return domain.Account{}, fmt.Errorf("key and name required: %w", storage.ErrValidation)
	}

	regionsJSON, _ := json.Marshal(in.Regions)
	if in.Regions == nil {
		regionsJSON = []byte("[]")
	}

	var a domain.Account
	var provider, externalID, description, platform, tier, environment *string
	var regionsOut []byte
	var createdAt time.Time

	var updatedAt time.Time
	err := s.pool.QueryRow(ctx, `
		INSERT INTO accounts (organization_id, key, name, provider, external_id, description, platform, tier, environment, regions)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10::jsonb)
		RETURNING seq_id, key, name, provider, external_id, description, platform, tier, environment, regions, created_at, updated_at`,
		s.orgID, in.Key, in.Name,
		nullStr(in.Provider), nullStr(in.ExternalID), nullStr(in.Description),
		nullStr(in.Platform), nullStr(in.Tier), nullStr(in.Environment),
		string(regionsJSON),
	).Scan(
		&a.ID, &a.Key, &a.Name,
		&provider, &externalID, &description,
		&platform, &tier, &environment,
		&regionsOut, &createdAt, &updatedAt,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return domain.Account{}, fmt.Errorf("account with this key already exists: %w", storage.ErrConflict)
		}
		return domain.Account{}, err
	}

	if provider != nil {
		a.Provider = *provider
	}
	if externalID != nil {
		a.ExternalID = *externalID
	}
	if description != nil {
		a.Description = *description
	}
	if platform != nil {
		a.Platform = *platform
	}
	if tier != nil {
		a.Tier = *tier
	}
	if environment != nil {
		a.Environment = *environment
	}
	a.CreatedAt = createdAt
	a.UpdatedAt = updatedAt
	if len(regionsOut) > 0 {
		_ = json.Unmarshal(regionsOut, &a.Regions)
	}

	return a, nil
}

func (s *Store) UpdateAccount(ctx context.Context, id int64, update domain.Account) (domain.Account, bool, error) {
	regionsJSON, _ := json.Marshal(update.Regions)
	if update.Regions == nil {
		regionsJSON = []byte("[]")
	}

	tag, err := s.pool.Exec(ctx, `
		UPDATE accounts SET
			name = CASE WHEN $3 = '' THEN name ELSE $3 END,
			provider = $4, external_id = $5, description = $6,
			platform = $7, tier = $8, environment = $9, regions = $10::jsonb
		WHERE seq_id = $1 AND organization_id = $2 AND deleted_at IS NULL`,
		id, s.orgID, update.Name,
		nullStr(update.Provider), nullStr(update.ExternalID), nullStr(update.Description),
		nullStr(update.Platform), nullStr(update.Tier), nullStr(update.Environment),
		string(regionsJSON))
	if err != nil {
		return domain.Account{}, false, err
	}
	if tag.RowsAffected() == 0 {
		return domain.Account{}, false, nil
	}

	return s.GetAccount(ctx, id)
}

func (s *Store) DeleteAccount(ctx context.Context, id int64) (bool, error) {
	// Check for pools referencing this account
	var poolCount int
	err := s.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM pools
		WHERE account_id = (SELECT id FROM accounts WHERE seq_id = $1 AND organization_id = $2)
		  AND deleted_at IS NULL`, id, s.orgID).Scan(&poolCount)
	if err != nil {
		return false, err
	}
	if poolCount > 0 {
		return false, fmt.Errorf("account in use by pools: %w", storage.ErrConflict)
	}

	tag, err := s.pool.Exec(ctx, `
		UPDATE accounts SET deleted_at = NOW()
		WHERE seq_id = $1 AND organization_id = $2 AND deleted_at IS NULL`, id, s.orgID)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

func (s *Store) DeleteAccountCascade(ctx context.Context, id int64) (bool, error) {
	// Get account UUID
	var accountUUID string
	err := s.pool.QueryRow(ctx, `SELECT id FROM accounts WHERE seq_id = $1 AND organization_id = $2 AND deleted_at IS NULL`, id, s.orgID).Scan(&accountUUID)
	if err != nil {
		return false, nil // not found
	}

	// Soft-delete pools linked to this account and their descendants
	_, err = s.pool.Exec(ctx, `
		WITH RECURSIVE pool_tree AS (
			SELECT id FROM pools WHERE account_id = $1 AND deleted_at IS NULL
			UNION ALL
			SELECT p.id FROM pools p JOIN pool_tree pt ON p.parent_id = pt.id WHERE p.deleted_at IS NULL
		)
		UPDATE pools SET deleted_at = NOW() WHERE id IN (SELECT id FROM pool_tree)`, accountUUID)
	if err != nil {
		return false, err
	}

	// Soft-delete the account
	tag, err := s.pool.Exec(ctx, `UPDATE accounts SET deleted_at = NOW() WHERE id = $1 AND deleted_at IS NULL`, accountUUID)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

func (s *Store) GetAccount(ctx context.Context, id int64) (domain.Account, bool, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT seq_id, key, name, provider, external_id, description,
			platform, tier, environment, regions, created_at, updated_at
		FROM accounts
		WHERE seq_id = $1 AND organization_id = $2 AND deleted_at IS NULL`, id, s.orgID)
	if err != nil {
		return domain.Account{}, false, err
	}
	defer rows.Close()

	if !rows.Next() {
		return domain.Account{}, false, nil
	}

	a, err := scanAccount(rows)
	if err != nil {
		return domain.Account{}, false, err
	}
	return a, true, nil
}

// =============================================================================
// Search
// =============================================================================

// Search performs a paginated search across pools and accounts.
// PostgreSQL supports native CIDR operators >> (contains) and << (within).
func (s *Store) Search(ctx context.Context, req domain.SearchRequest) (domain.SearchResponse, error) {
	// Validate CIDR filters
	if req.CIDRContains != "" {
		if _, err := cidr.ParseCIDROrIP(req.CIDRContains); err != nil {
			return domain.SearchResponse{}, fmt.Errorf("invalid cidr_contains: %w", err)
		}
	}
	if req.CIDRWithin != "" {
		if _, err := cidr.ParseCIDROrIP(req.CIDRWithin); err != nil {
			return domain.SearchResponse{}, fmt.Errorf("invalid cidr_within: %w", err)
		}
	}

	// Determine which types to include
	searchPools := true
	searchAccounts := true
	if len(req.Types) > 0 {
		searchPools = false
		searchAccounts = false
		for _, t := range req.Types {
			switch t {
			case "pool":
				searchPools = true
			case "account":
				searchAccounts = true
			}
		}
	}

	var items []domain.SearchResultItem

	// Search pools using native PostgreSQL CIDR operators
	if searchPools {
		var conditions []string
		var args []any
		argIdx := 1

		conditions = append(conditions, fmt.Sprintf("p.organization_id = $%d", argIdx))
		args = append(args, s.orgID)
		argIdx++

		conditions = append(conditions, "p.deleted_at IS NULL")

		if req.Query != "" {
			like := "%" + strings.ToLower(req.Query) + "%"
			conditions = append(conditions, fmt.Sprintf("(LOWER(p.name) LIKE $%d OR p.cidr::text LIKE $%d OR LOWER(p.description) LIKE $%d)", argIdx, argIdx, argIdx))
			args = append(args, like)
			argIdx++
		}

		if req.CIDRContains != "" {
			// Find pools whose CIDR contains the given IP/prefix
			conditions = append(conditions, fmt.Sprintf("p.cidr >>= $%d::inet", argIdx))
			args = append(args, req.CIDRContains)
			argIdx++
		}

		if req.CIDRWithin != "" {
			// Find pools that are within the given prefix
			conditions = append(conditions, fmt.Sprintf("p.cidr <<= $%d::inet", argIdx))
			args = append(args, req.CIDRWithin)
			argIdx++
		}

		query := fmt.Sprintf(`
			SELECT p.seq_id, p.name, p.cidr::text,
				(SELECT pp.seq_id FROM pools pp WHERE pp.id = p.parent_id),
				(SELECT a.seq_id FROM accounts a WHERE a.id = p.account_id),
				p.type, p.status, p.description
			FROM pools p
			WHERE %s
			ORDER BY p.seq_id`, strings.Join(conditions, " AND "))

		rows, err := s.pool.Query(ctx, query, args...)
		if err != nil {
			return domain.SearchResponse{}, err
		}
		defer rows.Close()

		for rows.Next() {
			var id int64
			var name, cidrStr string
			var parentSeq, accountSeq *int64
			var poolType, poolStatus string
			var description *string
			if err := rows.Scan(&id, &name, &cidrStr, &parentSeq, &accountSeq, &poolType, &poolStatus, &description); err != nil {
				return domain.SearchResponse{}, err
			}
			item := domain.SearchResultItem{
				Type:      "pool",
				ID:        id,
				Name:      name,
				CIDR:      cidrStr,
				PoolType:  poolType,
				Status:    poolStatus,
				ParentID:  parentSeq,
				AccountID: accountSeq,
			}
			if description != nil {
				item.Description = *description
			}
			items = append(items, item)
		}
		if err := rows.Err(); err != nil {
			return domain.SearchResponse{}, err
		}
	}

	// Search accounts (CIDR filters don't apply to accounts)
	if searchAccounts && req.CIDRContains == "" && req.CIDRWithin == "" {
		var conditions []string
		var args []any
		argIdx := 1

		conditions = append(conditions, fmt.Sprintf("organization_id = $%d", argIdx))
		args = append(args, s.orgID)
		argIdx++

		conditions = append(conditions, "deleted_at IS NULL")

		if req.Query != "" {
			like := "%" + strings.ToLower(req.Query) + "%"
			conditions = append(conditions, fmt.Sprintf("(LOWER(name) LIKE $%d OR LOWER(key) LIKE $%d OR LOWER(description) LIKE $%d)", argIdx, argIdx, argIdx))
			args = append(args, like)
			argIdx++
		}

		query := fmt.Sprintf(`
			SELECT seq_id, key, name, provider, description
			FROM accounts
			WHERE %s
			ORDER BY seq_id`, strings.Join(conditions, " AND "))

		rows, err := s.pool.Query(ctx, query, args...)
		if err != nil {
			return domain.SearchResponse{}, err
		}
		defer rows.Close()

		for rows.Next() {
			var id int64
			var key, name string
			var provider, description *string
			if err := rows.Scan(&id, &key, &name, &provider, &description); err != nil {
				return domain.SearchResponse{}, err
			}
			item := domain.SearchResultItem{
				Type:       "account",
				ID:         id,
				Name:       name,
				AccountKey: key,
			}
			if provider != nil {
				item.Provider = *provider
			}
			if description != nil {
				item.Description = *description
			}
			items = append(items, item)
		}
		if err := rows.Err(); err != nil {
			return domain.SearchResponse{}, err
		}
	}

	// Paginate
	total := len(items)
	page := req.Page
	if page < 1 {
		page = 1
	}
	pageSize := req.PageSize
	if pageSize < 1 {
		pageSize = 50
	}
	if pageSize > 200 {
		pageSize = 200
	}

	start := (page - 1) * pageSize
	if start > total {
		start = total
	}
	end := start + pageSize
	if end > total {
		end = total
	}

	return domain.SearchResponse{
		Items:    items[start:end],
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	}, nil
}

// =============================================================================
// Helpers
// =============================================================================

func nullStr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	// pgx wraps errors; check the error message for PostgreSQL unique violation code 23505
	return strings.Contains(err.Error(), "23505") ||
		strings.Contains(err.Error(), "unique constraint") ||
		strings.Contains(err.Error(), "duplicate key")
}
