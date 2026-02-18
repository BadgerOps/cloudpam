//go:build sqlite

package sqlite

import (
    "context"
    "database/sql"
    "encoding/binary"
    "encoding/json"
    "errors"
    "fmt"
    "net"
    "net/netip"
    "os"
    "strconv"
    "strings"
    "time"

    _ "modernc.org/sqlite" // CGO-less SQLite driver

    "cloudpam/internal/cidr"
    "cloudpam/internal/domain"
    "cloudpam/internal/storage"
)

type Store struct {
    db *sql.DB
}

// Connection pool defaults for SQLite.
// Override via environment variables:
//   - SQLITE_MAX_OPEN_CONNS (default: 1 — SQLite allows only one writer)
//   - SQLITE_MAX_IDLE_CONNS (default: 2)
//   - SQLITE_CONN_MAX_LIFETIME_SECS (default: 3600 — 1 hour)
//   - SQLITE_CONN_MAX_IDLE_SECS (default: 600 — 10 minutes)
const (
    defaultMaxOpenConns     = 1
    defaultMaxIdleConns     = 2
    defaultConnMaxLifetime  = 3600 // seconds
    defaultConnMaxIdleTime  = 600  // seconds
)

func New(dsn string) (*Store, error) {
    db, err := sql.Open("sqlite", dsn)
    if err != nil {
        return nil, err
    }

    // Configure connection pool from env vars with sensible defaults.
    db.SetMaxOpenConns(envIntOrDefault("SQLITE_MAX_OPEN_CONNS", defaultMaxOpenConns))
    db.SetMaxIdleConns(envIntOrDefault("SQLITE_MAX_IDLE_CONNS", defaultMaxIdleConns))
    db.SetConnMaxLifetime(time.Duration(envIntOrDefault("SQLITE_CONN_MAX_LIFETIME_SECS", defaultConnMaxLifetime)) * time.Second)
    db.SetConnMaxIdleTime(time.Duration(envIntOrDefault("SQLITE_CONN_MAX_IDLE_SECS", defaultConnMaxIdleTime)) * time.Second)

    if _, err := db.Exec(`PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000; PRAGMA foreign_keys=ON;`); err != nil {
        _ = db.Close()
        return nil, err
    }
    if err := runMigrations(db); err != nil {
        _ = db.Close()
        return nil, err
    }
    // Capture schema status silently; can be surfaced via Status()
    var _schemaVersion, _minSupported int
    var _appVersion, _appliedAt string
    _ = db.QueryRow(`SELECT schema_version, min_supported_schema, app_version, applied_at FROM schema_info WHERE id=1`).Scan(&_schemaVersion, &_minSupported, &_appVersion, &_appliedAt)
    return &Store{db: db}, nil
}

// envIntOrDefault reads an integer from the named environment variable,
// falling back to def if unset or unparseable.
func envIntOrDefault(name string, def int) int {
    v := os.Getenv(name)
    if v == "" {
        return def
    }
    n, err := strconv.Atoi(v)
    if err != nil {
        return def
    }
    return n
}

// Status returns schema_migrations and schema_info summary for the given DSN without creating a Store.
func Status(dsn string) (string, error) {
    db, err := sql.Open("sqlite", dsn)
    if err != nil { return "", err }
    defer db.Close()
    // ensure tables may exist; do not run migrations here
    var latest int
    _ = db.QueryRow(`SELECT COALESCE(MAX(version),0) FROM schema_migrations`).Scan(&latest)
    var schemaVersion, minSupported int
    var appVersion, appliedAt string
    _ = db.QueryRow(`SELECT schema_version, min_supported_schema, app_version, applied_at FROM schema_info WHERE id=1`).Scan(&schemaVersion, &minSupported, &appVersion, &appliedAt)
    // Count applied
    var count int
    _ = db.QueryRow(`SELECT COUNT(1) FROM schema_migrations`).Scan(&count)
    return fmt.Sprintf("schema_version=%d applied=%d latest=%d app_version=%s applied_at=%s min_supported=%d", schemaVersion, count, latest, appVersion, appliedAt, minSupported), nil
}

var _ storage.Store = (*Store)(nil)

// Close closes the underlying database connection and releases resources.
func (s *Store) Close() error {
    if s.db != nil {
        return s.db.Close()
    }
    return nil
}

func (s *Store) ListPools(ctx context.Context) ([]domain.Pool, error) {
    rows, err := s.db.QueryContext(ctx, `SELECT id, name, cidr, parent_id, account_id, type, status, source, description, tags, created_at, updated_at FROM pools WHERE deleted_at IS NULL ORDER BY id ASC`)
    if err != nil {
        return nil, err
    }
    defer rows.Close()
    var out []domain.Pool
    for rows.Next() {
        var p domain.Pool
        var createdAt, updatedAt sql.NullString
        var parent, account sql.NullInt64
        var poolType, poolStatus, poolSource, description, tagsJSON sql.NullString
        if err := rows.Scan(&p.ID, &p.Name, &p.CIDR, &parent, &account, &poolType, &poolStatus, &poolSource, &description, &tagsJSON, &createdAt, &updatedAt); err != nil {
            return nil, err
        }
        if parent.Valid {
            p.ParentID = &parent.Int64
        }
        if account.Valid {
            p.AccountID = &account.Int64
        }
        // Set new fields with defaults
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
        if createdAt.Valid {
            if t, e := time.Parse(time.RFC3339, createdAt.String); e == nil {
                p.CreatedAt = t
            }
        }
        if updatedAt.Valid {
            if t, e := time.Parse(time.RFC3339, updatedAt.String); e == nil {
                p.UpdatedAt = t
            }
        }
        out = append(out, p)
    }
    return out, rows.Err()
}

func (s *Store) CreatePool(ctx context.Context, in domain.CreatePool) (domain.Pool, error) {
    if in.Name == "" || in.CIDR == "" {
        return domain.Pool{}, fmt.Errorf("name and cidr required: %w", storage.ErrValidation)
    }
    now := time.Now().UTC()

    // Apply defaults for new fields
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

    // Serialize tags to JSON
    var tagsJSON string
    if in.Tags != nil {
        if b, e := json.Marshal(in.Tags); e == nil {
            tagsJSON = string(b)
        }
    } else {
        tagsJSON = "{}"
    }

    nowStr := now.Format(time.RFC3339)
    res, err := s.db.ExecContext(ctx, `INSERT INTO pools(name, cidr, parent_id, account_id, type, status, source, description, tags, created_at, updated_at) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
        in.Name, in.CIDR, in.ParentID, in.AccountID, string(poolType), string(poolStatus), string(poolSource), in.Description, tagsJSON, nowStr, nowStr)
    if err != nil {
        return domain.Pool{}, storage.WrapIfConflict(err)
    }
    id, err := res.LastInsertId()
    if err != nil {
        return domain.Pool{}, err
    }

    // Copy tags to avoid shared reference
    var tags map[string]string
    if in.Tags != nil {
        tags = make(map[string]string, len(in.Tags))
        for k, v := range in.Tags {
            tags[k] = v
        }
    }

    return domain.Pool{
        ID:          id,
        Name:        in.Name,
        CIDR:        in.CIDR,
        ParentID:    in.ParentID,
        AccountID:   in.AccountID,
        Type:        poolType,
        Status:      poolStatus,
        Source:      poolSource,
        Description: in.Description,
        Tags:        tags,
        CreatedAt:   now,
        UpdatedAt:   now,
    }, nil
}

func (s *Store) GetPool(ctx context.Context, id int64) (domain.Pool, bool, error) {
    var p domain.Pool
    var createdAt, updatedAt sql.NullString
    var parent, account sql.NullInt64
    var poolType, poolStatus, poolSource, description, tagsJSON sql.NullString
    row := s.db.QueryRowContext(ctx, `SELECT id, name, cidr, parent_id, account_id, type, status, source, description, tags, created_at, updated_at FROM pools WHERE id=? AND deleted_at IS NULL`, id)
    if err := row.Scan(&p.ID, &p.Name, &p.CIDR, &parent, &account, &poolType, &poolStatus, &poolSource, &description, &tagsJSON, &createdAt, &updatedAt); err != nil {
        if errors.Is(err, sql.ErrNoRows) {
            return domain.Pool{}, false, nil
        }
        return domain.Pool{}, false, err
    }
    if parent.Valid {
        p.ParentID = &parent.Int64
    }
    if account.Valid {
        p.AccountID = &account.Int64
    }
    // Set new fields with defaults
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
    if createdAt.Valid {
        if t, e := time.Parse(time.RFC3339, createdAt.String); e == nil {
            p.CreatedAt = t
        }
    }
    if updatedAt.Valid {
        if t, e := time.Parse(time.RFC3339, updatedAt.String); e == nil {
            p.UpdatedAt = t
        }
    }
    return p, true, nil
}

func (s *Store) UpdatePoolAccount(ctx context.Context, id int64, accountID *int64) (domain.Pool, bool, error) {
    // Update and then fetch
    now := time.Now().UTC().Format(time.RFC3339)
    if _, err := s.db.ExecContext(ctx, `UPDATE pools SET account_id=?, updated_at=? WHERE id=?`, accountID, now, id); err != nil {
        return domain.Pool{}, false, err
    }
    return s.GetPool(ctx, id)
}

func (s *Store) UpdatePoolMeta(ctx context.Context, id int64, name *string, accountID *int64) (domain.Pool, bool, error) {
    // Fetch current
    p, ok, err := s.GetPool(ctx, id)
    if err != nil || !ok { return domain.Pool{}, ok, err }
    if name != nil { p.Name = *name }
    // Always set accountID (caller controls whether to clear or set)
    p.AccountID = accountID
    now := time.Now().UTC().Format(time.RFC3339)
    if _, err := s.db.ExecContext(ctx, `UPDATE pools SET name=?, account_id=?, updated_at=? WHERE id=?`, p.Name, p.AccountID, now, id); err != nil {
        return domain.Pool{}, false, err
    }
    return s.GetPool(ctx, id)
}

// UpdatePool updates pool metadata with support for new fields.
func (s *Store) UpdatePool(ctx context.Context, id int64, update domain.UpdatePool) (domain.Pool, bool, error) {
    // Fetch current
    p, ok, err := s.GetPool(ctx, id)
    if err != nil || !ok {
        return domain.Pool{}, ok, err
    }
    if update.Name != nil {
        p.Name = *update.Name
    }
    // AccountID is always set (can be nil to clear)
    p.AccountID = update.AccountID
    if update.Type != nil {
        p.Type = *update.Type
    }
    if update.Status != nil {
        p.Status = *update.Status
    }
    if update.Description != nil {
        p.Description = *update.Description
    }
    if update.Tags != nil {
        if *update.Tags != nil {
            p.Tags = make(map[string]string, len(*update.Tags))
            for k, v := range *update.Tags {
                p.Tags[k] = v
            }
        } else {
            p.Tags = nil
        }
    }

    // Serialize tags to JSON
    var tagsJSON string
    if p.Tags != nil {
        if b, e := json.Marshal(p.Tags); e == nil {
            tagsJSON = string(b)
        }
    } else {
        tagsJSON = "{}"
    }

    now := time.Now().UTC().Format(time.RFC3339)
    if _, err := s.db.ExecContext(ctx, `UPDATE pools SET name=?, account_id=?, type=?, status=?, description=?, tags=?, updated_at=? WHERE id=?`,
        p.Name, p.AccountID, string(p.Type), string(p.Status), p.Description, tagsJSON, now, id); err != nil {
        return domain.Pool{}, false, err
    }
    return s.GetPool(ctx, id)
}

func (s *Store) DeletePool(ctx context.Context, id int64) (bool, error) {
    // check children (only non-deleted)
    var cnt int
    if err := s.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM pools WHERE parent_id=? AND deleted_at IS NULL`, id).Scan(&cnt); err != nil {
        return false, err
    }
    if cnt > 0 { return false, fmt.Errorf("pool has child pools: %w", storage.ErrConflict) }
    now := time.Now().UTC().Format(time.RFC3339)
    res, err := s.db.ExecContext(ctx, `UPDATE pools SET deleted_at=?, updated_at=? WHERE id=? AND deleted_at IS NULL`, now, now, id)
    if err != nil { return false, err }
    n, _ := res.RowsAffected()
    return n > 0, nil
}

func (s *Store) DeletePoolCascade(ctx context.Context, id int64) (bool, error) {
    // Load all non-deleted pools, compute subtree, soft-delete
    ps, err := s.ListPools(ctx)
    if err != nil { return false, err }
    exists := false
    children := map[int64][]int64{}
    for _, p := range ps {
        if p.ID == id { exists = true }
        if p.ParentID != nil {
            children[*p.ParentID] = append(children[*p.ParentID], p.ID)
        }
    }
    if !exists { return false, nil }
    // BFS
    queue := []int64{id}
    order := []int64{}
    for len(queue) > 0 {
        cur := queue[0]; queue = queue[1:]
        order = append(order, cur)
        for _, ch := range children[cur] { queue = append(queue, ch) }
    }
    now := time.Now().UTC().Format(time.RFC3339)
    for _, pid := range order {
        _, err := s.db.ExecContext(ctx, `UPDATE pools SET deleted_at=?, updated_at=? WHERE id=? AND deleted_at IS NULL`, now, now, pid)
        if err != nil { return false, err }
    }
    return true, nil
}

// Accounts
func (s *Store) ListAccounts(ctx context.Context) ([]domain.Account, error) {
    rows, err := s.db.QueryContext(ctx, `SELECT id, key, name, provider, external_id, description, platform, tier, environment, regions, created_at, updated_at FROM accounts WHERE deleted_at IS NULL ORDER BY id ASC`)
    if err != nil {
        return nil, err
    }
    defer rows.Close()
    var out []domain.Account
    for rows.Next() {
        var a domain.Account
        var ts string
        var provider, extid, desc, platform, tier, env sql.NullString
        var regions, updatedAt sql.NullString
        if err := rows.Scan(&a.ID, &a.Key, &a.Name, &provider, &extid, &desc, &platform, &tier, &env, &regions, &ts, &updatedAt); err != nil {
            return nil, err
        }
        if provider.Valid { a.Provider = provider.String }
        if extid.Valid { a.ExternalID = extid.String }
        if desc.Valid { a.Description = desc.String }
        if platform.Valid { a.Platform = platform.String }
        if tier.Valid { a.Tier = tier.String }
        if env.Valid { a.Environment = env.String }
        if regions.Valid && regions.String != "" {
            var arr []string
            if err := json.Unmarshal([]byte(regions.String), &arr); err == nil { a.Regions = arr }
        }
        if t, e := time.Parse(time.RFC3339, ts); e == nil {
            a.CreatedAt = t
        }
        if updatedAt.Valid {
            if t, e := time.Parse(time.RFC3339, updatedAt.String); e == nil { a.UpdatedAt = t }
        }
        out = append(out, a)
    }
    return out, rows.Err()
}

func (s *Store) CreateAccount(ctx context.Context, in domain.CreateAccount) (domain.Account, error) {
    if in.Key == "" || in.Name == "" {
        return domain.Account{}, fmt.Errorf("key and name required: %w", storage.ErrValidation)
    }
    var regions string
    if len(in.Regions) > 0 { if b, e := json.Marshal(in.Regions); e == nil { regions = string(b) } }
    res, err := s.db.ExecContext(ctx, `INSERT INTO accounts(key, name, provider, external_id, description, platform, tier, environment, regions, created_at) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, in.Key, in.Name, in.Provider, in.ExternalID, in.Description, in.Platform, in.Tier, in.Environment, regions, time.Now().UTC().Format(time.RFC3339))
    if err != nil {
        return domain.Account{}, storage.WrapIfConflict(err)
    }
    id, err := res.LastInsertId()
    if err != nil {
        return domain.Account{}, err
    }
    return domain.Account{ID: id, Key: in.Key, Name: in.Name, Provider: in.Provider, ExternalID: in.ExternalID, Description: in.Description, Platform: in.Platform, Tier: in.Tier, Environment: in.Environment, Regions: append([]string(nil), in.Regions...), CreatedAt: time.Now().UTC()}, nil
}

func (s *Store) GetAccount(ctx context.Context, id int64) (domain.Account, bool, error) {
    row := s.db.QueryRowContext(ctx, `SELECT id, key, name, provider, external_id, description, platform, tier, environment, regions, created_at, updated_at FROM accounts WHERE id=? AND deleted_at IS NULL`, id)
    var a domain.Account
    var ts string
    var provider, extid, desc, platform, tier, env sql.NullString
    var regions, updatedAt sql.NullString
    if err := row.Scan(&a.ID, &a.Key, &a.Name, &provider, &extid, &desc, &platform, &tier, &env, &regions, &ts, &updatedAt); err != nil {
        if errors.Is(err, sql.ErrNoRows) { return domain.Account{}, false, nil }
        return domain.Account{}, false, err
    }
    if provider.Valid { a.Provider = provider.String }
    if extid.Valid { a.ExternalID = extid.String }
    if desc.Valid { a.Description = desc.String }
    if platform.Valid { a.Platform = platform.String }
    if tier.Valid { a.Tier = tier.String }
    if env.Valid { a.Environment = env.String }
    if regions.Valid && regions.String != "" {
        var arr []string
        if err := json.Unmarshal([]byte(regions.String), &arr); err == nil { a.Regions = arr }
    }
    if t, e := time.Parse(time.RFC3339, ts); e == nil { a.CreatedAt = t }
    if updatedAt.Valid {
        if t, e := time.Parse(time.RFC3339, updatedAt.String); e == nil { a.UpdatedAt = t }
    }
    return a, true, nil
}

func (s *Store) GetAccountByKey(ctx context.Context, key string) (*domain.Account, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, key, name, provider, external_id, description, platform, tier, environment, regions, created_at, updated_at FROM accounts WHERE key=? AND deleted_at IS NULL`, key)
	var a domain.Account
	var ts string
	var provider, extid, desc, platform, tier, env sql.NullString
	var regions, updatedAt sql.NullString
	if err := row.Scan(&a.ID, &a.Key, &a.Name, &provider, &extid, &desc, &platform, &tier, &env, &regions, &ts, &updatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, storage.ErrNotFound
		}
		return nil, err
	}
	if provider.Valid { a.Provider = provider.String }
	if extid.Valid { a.ExternalID = extid.String }
	if desc.Valid { a.Description = desc.String }
	if platform.Valid { a.Platform = platform.String }
	if tier.Valid { a.Tier = tier.String }
	if env.Valid { a.Environment = env.String }
	if regions.Valid && regions.String != "" {
		var arr []string
		if err := json.Unmarshal([]byte(regions.String), &arr); err == nil { a.Regions = arr }
	}
	if t, e := time.Parse(time.RFC3339, ts); e == nil { a.CreatedAt = t }
	if updatedAt.Valid {
		if t, e := time.Parse(time.RFC3339, updatedAt.String); e == nil { a.UpdatedAt = t }
	}
	return &a, nil
}

func (s *Store) UpdateAccount(ctx context.Context, id int64, update domain.Account) (domain.Account, bool, error) {
    // Fetch current (non-deleted)
    rows, err := s.db.QueryContext(ctx, `SELECT id, key, name, provider, external_id, description, platform, tier, environment, regions, created_at, updated_at FROM accounts WHERE id=? AND deleted_at IS NULL`, id)
    if err != nil { return domain.Account{}, false, err }
    defer rows.Close()
    if !rows.Next() { return domain.Account{}, false, nil }
    var a domain.Account
    var ts string
    var provider, extid, desc, platform, tier, env sql.NullString
    var regions, updatedAt sql.NullString
    if err := rows.Scan(&a.ID, &a.Key, &a.Name, &provider, &extid, &desc, &platform, &tier, &env, &regions, &ts, &updatedAt); err != nil { return domain.Account{}, false, err }
    if provider.Valid { a.Provider = provider.String }
    if extid.Valid { a.ExternalID = extid.String }
    if desc.Valid { a.Description = desc.String }
    if platform.Valid { a.Platform = platform.String }
    if tier.Valid { a.Tier = tier.String }
    if env.Valid { a.Environment = env.String }
    if regions.Valid && regions.String != "" { var arr []string; _ = json.Unmarshal([]byte(regions.String), &arr); a.Regions = arr }
    // Apply update
    if update.Name != "" { a.Name = update.Name }
    a.Provider = update.Provider
    a.ExternalID = update.ExternalID
    a.Description = update.Description
    a.Platform = update.Platform
    a.Tier = update.Tier
    a.Environment = update.Environment
    if update.Regions != nil { a.Regions = append([]string(nil), update.Regions...) }
    // persist
    now := time.Now().UTC()
    a.UpdatedAt = now
    var regionsOut *string
    if a.Regions != nil { if b, e := json.Marshal(a.Regions); e == nil { s := string(b); regionsOut = &s } }
    if _, err := s.db.ExecContext(ctx, `UPDATE accounts SET name=?, provider=?, external_id=?, description=?, platform=?, tier=?, environment=?, regions=?, updated_at=? WHERE id=?`, a.Name, a.Provider, a.ExternalID, a.Description, a.Platform, a.Tier, a.Environment, regionsOut, now.Format(time.RFC3339), id); err != nil {
        return domain.Account{}, false, err
    }
    return a, true, nil
}

func (s *Store) DeleteAccount(ctx context.Context, id int64) (bool, error) {
    var cnt int
    if err := s.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM pools WHERE account_id=? AND deleted_at IS NULL`, id).Scan(&cnt); err != nil { return false, err }
    if cnt > 0 { return false, fmt.Errorf("account in use by pools: %w", storage.ErrConflict) }
    now := time.Now().UTC().Format(time.RFC3339)
    res, err := s.db.ExecContext(ctx, `UPDATE accounts SET deleted_at=?, updated_at=? WHERE id=? AND deleted_at IS NULL`, now, now, id)
    if err != nil { return false, err }
    n, _ := res.RowsAffected()
    return n > 0, nil
}

func (s *Store) DeleteAccountCascade(ctx context.Context, id int64) (bool, error) {
    // Soft-delete all pools referencing this account, including their descendants, then soft-delete account
    ps, err := s.ListPools(ctx)
    if err != nil { return false, err }
    // Check account exists (non-deleted)
    var accCnt int
    if err := s.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM accounts WHERE id=? AND deleted_at IS NULL`, id).Scan(&accCnt); err != nil { return false, err }
    if accCnt == 0 { return false, nil }
    // Build adjacency
    children := map[int64][]int64{}
    for _, p := range ps {
        if p.ParentID != nil { children[*p.ParentID] = append(children[*p.ParentID], p.ID) }
    }
    // Collect roots: pools with account_id = id
    roots := []int64{}
    for _, p := range ps { if p.AccountID != nil && *p.AccountID == id { roots = append(roots, p.ID) } }
    // Collect all to soft-delete
    toDel := map[int64]struct{}{}
    var dfs func(int64)
    dfs = func(n int64){
        if _, ok := toDel[n]; ok { return }
        toDel[n] = struct{}{}
        for _, ch := range children[n] { dfs(ch) }
    }
    for _, r := range roots { dfs(r) }
    // Soft-delete pools
    now := time.Now().UTC().Format(time.RFC3339)
    for pid := range toDel { if _, err := s.db.ExecContext(ctx, `UPDATE pools SET deleted_at=?, updated_at=? WHERE id=? AND deleted_at IS NULL`, now, now, pid); err != nil { return false, err } }
    // Soft-delete account
    if _, err := s.db.ExecContext(ctx, `UPDATE accounts SET deleted_at=?, updated_at=? WHERE id=? AND deleted_at IS NULL`, now, now, id); err != nil { return false, err }
    return true, nil
}

// GetPoolWithStats returns a pool with its computed statistics.
func (s *Store) GetPoolWithStats(ctx context.Context, id int64) (*domain.PoolWithStats, error) {
    p, ok, err := s.GetPool(ctx, id)
    if err != nil {
        return nil, err
    }
    if !ok {
        return nil, fmt.Errorf("pool not found: %w", storage.ErrNotFound)
    }
    stats, err := s.calculatePoolStats(ctx, p)
    if err != nil {
        return nil, err
    }
    return &domain.PoolWithStats{
        Pool:  p,
        Stats: *stats,
    }, nil
}

// GetPoolHierarchy returns the pool hierarchy tree starting from rootID.
// If rootID is nil, returns all top-level pools with their children.
func (s *Store) GetPoolHierarchy(ctx context.Context, rootID *int64) ([]domain.PoolWithStats, error) {
    // Load all pools
    pools, err := s.ListPools(ctx)
    if err != nil {
        return nil, err
    }

    // Build maps for quick lookup
    poolMap := make(map[int64]domain.Pool)
    childrenMap := make(map[int64][]int64)
    for _, p := range pools {
        poolMap[p.ID] = p
        if p.ParentID != nil {
            childrenMap[*p.ParentID] = append(childrenMap[*p.ParentID], p.ID)
        }
    }

    // Recursive function to build tree
    var buildTree func(pid int64) (domain.PoolWithStats, error)
    buildTree = func(pid int64) (domain.PoolWithStats, error) {
        p := poolMap[pid]
        stats, err := s.calculatePoolStats(ctx, p)
        if err != nil {
            return domain.PoolWithStats{}, err
        }
        result := domain.PoolWithStats{
            Pool:  p,
            Stats: *stats,
        }
        for _, childID := range childrenMap[pid] {
            child, err := buildTree(childID)
            if err != nil {
                return domain.PoolWithStats{}, err
            }
            result.Children = append(result.Children, child)
        }
        return result, nil
    }

    var result []domain.PoolWithStats

    if rootID != nil {
        // Return subtree from specific root
        if _, ok := poolMap[*rootID]; !ok {
            return nil, fmt.Errorf("root pool not found: %w", storage.ErrNotFound)
        }
        tree, err := buildTree(*rootID)
        if err != nil {
            return nil, err
        }
        result = append(result, tree)
    } else {
        // Return all top-level pools (no parent)
        for _, p := range pools {
            if p.ParentID == nil {
                tree, err := buildTree(p.ID)
                if err != nil {
                    return nil, err
                }
                result = append(result, tree)
            }
        }
    }

    return result, nil
}

// GetPoolChildren returns the direct children of a pool.
func (s *Store) GetPoolChildren(ctx context.Context, parentID int64) ([]domain.Pool, error) {
    // Check parent exists
    _, ok, err := s.GetPool(ctx, parentID)
    if err != nil {
        return nil, err
    }
    if !ok {
        return nil, fmt.Errorf("parent pool not found: %w", storage.ErrNotFound)
    }

    rows, err := s.db.QueryContext(ctx, `SELECT id, name, cidr, parent_id, account_id, type, status, source, description, tags, created_at, updated_at FROM pools WHERE parent_id=? AND deleted_at IS NULL ORDER BY id ASC`, parentID)
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var children []domain.Pool
    for rows.Next() {
        var p domain.Pool
        var createdAt, updatedAt sql.NullString
        var parent, account sql.NullInt64
        var poolType, poolStatus, poolSource, description, tagsJSON sql.NullString
        if err := rows.Scan(&p.ID, &p.Name, &p.CIDR, &parent, &account, &poolType, &poolStatus, &poolSource, &description, &tagsJSON, &createdAt, &updatedAt); err != nil {
            return nil, err
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
        if createdAt.Valid {
            if t, e := time.Parse(time.RFC3339, createdAt.String); e == nil {
                p.CreatedAt = t
            }
        }
        if updatedAt.Valid {
            if t, e := time.Parse(time.RFC3339, updatedAt.String); e == nil {
                p.UpdatedAt = t
            }
        }
        children = append(children, p)
    }
    return children, rows.Err()
}

// CalculatePoolUtilization calculates statistics for a pool.
func (s *Store) CalculatePoolUtilization(ctx context.Context, id int64) (*domain.PoolStats, error) {
    p, ok, err := s.GetPool(ctx, id)
    if err != nil {
        return nil, err
    }
    if !ok {
        return nil, fmt.Errorf("pool not found: %w", storage.ErrNotFound)
    }
    return s.calculatePoolStats(ctx, p)
}

// calculatePoolStats calculates stats for a pool.
func (s *Store) calculatePoolStats(ctx context.Context, p domain.Pool) (*domain.PoolStats, error) {
    // Parse the pool's CIDR to get total IPs
    prefix, err := netip.ParsePrefix(p.CIDR)
    if err != nil {
        return &domain.PoolStats{}, nil
    }

    var totalIPs int64
    if prefix.Addr().Is4() {
        totalIPs = int64(1) << (32 - prefix.Bits())
    } else {
        // For IPv6, cap at max int64 for practical purposes
        bits := 128 - prefix.Bits()
        if bits >= 63 {
            totalIPs = 1<<63 - 1 // max int64
        } else {
            totalIPs = int64(1) << bits
        }
    }

    // Get direct children (non-deleted)
    rows, err := s.db.QueryContext(ctx, `SELECT cidr FROM pools WHERE parent_id=? AND deleted_at IS NULL`, p.ID)
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var directChildren int
    var usedIPs int64
    for rows.Next() {
        var childCIDR string
        if err := rows.Scan(&childCIDR); err != nil {
            return nil, err
        }
        directChildren++

        // Calculate used IPs from child's CIDR
        childPrefix, err := netip.ParsePrefix(childCIDR)
        if err != nil {
            continue
        }
        if childPrefix.Addr().Is4() {
            usedIPs += int64(1) << (32 - childPrefix.Bits())
        }
    }
    if err := rows.Err(); err != nil {
        return nil, err
    }

    // Count all descendants recursively
    var totalChildCount int
    var countDescendants func(parentID int64) (int, error)
    countDescendants = func(parentID int64) (int, error) {
        var count int
        childRows, err := s.db.QueryContext(ctx, `SELECT id FROM pools WHERE parent_id=? AND deleted_at IS NULL`, parentID)
        if err != nil {
            return 0, err
        }
        defer childRows.Close()
        for childRows.Next() {
            var childID int64
            if err := childRows.Scan(&childID); err != nil {
                return 0, err
            }
            count++
            subCount, err := countDescendants(childID)
            if err != nil {
                return 0, err
            }
            count += subCount
        }
        return count, childRows.Err()
    }

    totalChildCount, err = countDescendants(p.ID)
    if err != nil {
        return nil, err
    }

    // Calculate utilization percentage
    var utilization float64
    if totalIPs > 0 {
        utilization = float64(usedIPs) / float64(totalIPs) * 100
    }

    return &domain.PoolStats{
        TotalIPs:       totalIPs,
        UsedIPs:        usedIPs,
        AvailableIPs:   totalIPs - usedIPs,
        Utilization:    utilization,
        ChildCount:     totalChildCount,
        DirectChildren: directChildren,
    }, nil
}

// Search performs a paginated search across pools and accounts.
// SQLite has no native CIDR type, so containment is done in Go after loading.
func (s *Store) Search(ctx context.Context, req domain.SearchRequest) (domain.SearchResponse, error) {
    // Parse CIDR filters once
    var cidrContains, cidrWithin netip.Prefix
    var hasCIDRContains, hasCIDRWithin bool
    if req.CIDRContains != "" {
        p, err := cidr.ParseCIDROrIP(req.CIDRContains)
        if err != nil {
            return domain.SearchResponse{}, fmt.Errorf("invalid cidr_contains: %w", err)
        }
        cidrContains = p
        hasCIDRContains = true
    }
    if req.CIDRWithin != "" {
        p, err := cidr.ParseCIDROrIP(req.CIDRWithin)
        if err != nil {
            return domain.SearchResponse{}, fmt.Errorf("invalid cidr_within: %w", err)
        }
        cidrWithin = p
        hasCIDRWithin = true
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

    query := strings.ToLower(req.Query)
    var items []domain.SearchResultItem

    // Search pools — use SQL LIKE for text search, then filter CIDR in Go
    if searchPools {
        var poolRows *sql.Rows
        var err error
        if query != "" {
            like := "%" + query + "%"
            poolRows, err = s.db.QueryContext(ctx,
                `SELECT id, name, cidr, parent_id, account_id, type, status, source, description FROM pools WHERE deleted_at IS NULL AND (name LIKE ? OR cidr LIKE ? OR description LIKE ?) ORDER BY id`,
                like, like, like)
        } else {
            poolRows, err = s.db.QueryContext(ctx,
                `SELECT id, name, cidr, parent_id, account_id, type, status, source, description FROM pools WHERE deleted_at IS NULL ORDER BY id`)
        }
        if err != nil {
            return domain.SearchResponse{}, err
        }
        defer poolRows.Close()

        for poolRows.Next() {
            var id int64
            var name, cidrStr string
            var parent, account sql.NullInt64
            var poolType, poolStatus, poolSource, description sql.NullString
            if err := poolRows.Scan(&id, &name, &cidrStr, &parent, &account, &poolType, &poolStatus, &poolSource, &description); err != nil {
                return domain.SearchResponse{}, err
            }

            // Apply CIDR filters in Go
            if hasCIDRContains || hasCIDRWithin {
                poolPrefix, err := netip.ParsePrefix(cidrStr)
                if err != nil {
                    continue
                }
                if hasCIDRContains && !cidr.PrefixContains(poolPrefix, cidrContains) {
                    continue
                }
                if hasCIDRWithin && !cidr.PrefixContains(cidrWithin, poolPrefix) {
                    continue
                }
            }

            item := domain.SearchResultItem{
                Type: "pool",
                ID:   id,
                Name: name,
                CIDR: cidrStr,
            }
            if parent.Valid {
                v := parent.Int64
                item.ParentID = &v
            }
            if account.Valid {
                v := account.Int64
                item.AccountID = &v
            }
            if poolType.Valid {
                item.PoolType = poolType.String
            }
            if poolStatus.Valid {
                item.Status = poolStatus.String
            }
            if description.Valid {
                item.Description = description.String
            }
            items = append(items, item)
        }
        if err := poolRows.Err(); err != nil {
            return domain.SearchResponse{}, err
        }
    }

    // Search accounts — CIDR filters don't apply to accounts
    if searchAccounts && !hasCIDRContains && !hasCIDRWithin {
        var accRows *sql.Rows
        var err error
        if query != "" {
            like := "%" + query + "%"
            accRows, err = s.db.QueryContext(ctx,
                `SELECT id, key, name, provider, description FROM accounts WHERE deleted_at IS NULL AND (name LIKE ? OR key LIKE ? OR description LIKE ?) ORDER BY id`,
                like, like, like)
        } else {
            accRows, err = s.db.QueryContext(ctx,
                `SELECT id, key, name, provider, description FROM accounts WHERE deleted_at IS NULL ORDER BY id`)
        }
        if err != nil {
            return domain.SearchResponse{}, err
        }
        defer accRows.Close()

        for accRows.Next() {
            var id int64
            var key, name string
            var provider, description sql.NullString
            if err := accRows.Scan(&id, &key, &name, &provider, &description); err != nil {
                return domain.SearchResponse{}, err
            }
            item := domain.SearchResultItem{
                Type:       "account",
                ID:         id,
                Name:       name,
                AccountKey: key,
            }
            if provider.Valid {
                item.Provider = provider.String
            }
            if description.Valid {
                item.Description = description.String
            }
            items = append(items, item)
        }
        if err := accRows.Err(); err != nil {
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

// ipv4ToUint32 converts an IPv4 address to uint32.
func ipv4ToUint32(a netip.Addr) uint32 {
    b := a.As4()
    return binary.BigEndian.Uint32(b[:])
}

// uint32ToIPv4 converts a uint32 to an IPv4 address.
func uint32ToIPv4(u uint32) netip.Addr {
    var b [4]byte
    binary.BigEndian.PutUint32(b[:], u)
    ip := net.IPv4(b[0], b[1], b[2], b[3])
    addr, _ := netip.ParseAddr(ip.String())
    return addr
}
