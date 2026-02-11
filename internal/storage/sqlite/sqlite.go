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
    "time"

    _ "modernc.org/sqlite" // CGO-less SQLite driver

    "cloudpam/internal/domain"
    "cloudpam/internal/storage"
)

type Store struct {
    db *sql.DB
}

func New(dsn string) (*Store, error) {
    db, err := sql.Open("sqlite", dsn)
    if err != nil {
        return nil, err
    }
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
    rows, err := s.db.QueryContext(ctx, `SELECT id, name, cidr, parent_id, account_id, type, status, source, description, tags, created_at, updated_at FROM pools ORDER BY id ASC`)
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
    row := s.db.QueryRowContext(ctx, `SELECT id, name, cidr, parent_id, account_id, type, status, source, description, tags, created_at, updated_at FROM pools WHERE id=?`, id)
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
    // check children
    var cnt int
    if err := s.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM pools WHERE parent_id=?`, id).Scan(&cnt); err != nil {
        return false, err
    }
    if cnt > 0 { return false, fmt.Errorf("pool has child pools: %w", storage.ErrConflict) }
    res, err := s.db.ExecContext(ctx, `DELETE FROM pools WHERE id=?`, id)
    if err != nil { return false, err }
    n, _ := res.RowsAffected()
    return n > 0, nil
}

func (s *Store) DeletePoolCascade(ctx context.Context, id int64) (bool, error) {
    // Load all pools, compute subtree, delete
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
    // Delete leaves-first or any order since individual deletes are fine
    for i := len(order)-1; i >=0; i-- {
        _, err := s.db.ExecContext(ctx, `DELETE FROM pools WHERE id=?`, order[i])
        if err != nil { return false, err }
    }
    return true, nil
}

// Accounts
func (s *Store) ListAccounts(ctx context.Context) ([]domain.Account, error) {
    rows, err := s.db.QueryContext(ctx, `SELECT id, key, name, provider, external_id, description, platform, tier, environment, regions, created_at FROM accounts ORDER BY id ASC`)
    if err != nil {
        return nil, err
    }
    defer rows.Close()
    var out []domain.Account
    for rows.Next() {
        var a domain.Account
        var ts string
        var provider, extid, desc, platform, tier, env sql.NullString
        var regions sql.NullString
        if err := rows.Scan(&a.ID, &a.Key, &a.Name, &provider, &extid, &desc, &platform, &tier, &env, &regions, &ts); err != nil {
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
    row := s.db.QueryRowContext(ctx, `SELECT id, key, name, provider, external_id, description, created_at FROM accounts WHERE id=?`, id)
    var a domain.Account
    var ts string
    if err := row.Scan(&a.ID, &a.Key, &a.Name, &a.Provider, &a.ExternalID, &a.Description, &ts); err != nil {
        if errors.Is(err, sql.ErrNoRows) { return domain.Account{}, false, nil }
        return domain.Account{}, false, err
    }
    if t, e := time.Parse(time.RFC3339, ts); e == nil { a.CreatedAt = t }
    return a, true, nil
}

func (s *Store) UpdateAccount(ctx context.Context, id int64, update domain.Account) (domain.Account, bool, error) {
    // Fetch current
    rows, err := s.db.QueryContext(ctx, `SELECT id, key, name, provider, external_id, description, platform, tier, environment, regions, created_at FROM accounts WHERE id=?`, id)
    if err != nil { return domain.Account{}, false, err }
    defer rows.Close()
    if !rows.Next() { return domain.Account{}, false, nil }
    var a domain.Account
    var ts string
    var provider, extid, desc, platform, tier, env sql.NullString
    var regions sql.NullString
    if err := rows.Scan(&a.ID, &a.Key, &a.Name, &provider, &extid, &desc, &platform, &tier, &env, &regions, &ts); err != nil { return domain.Account{}, false, err }
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
    var regionsOut *string
    if a.Regions != nil { if b, e := json.Marshal(a.Regions); e == nil { s := string(b); regionsOut = &s } }
    if _, err := s.db.ExecContext(ctx, `UPDATE accounts SET name=?, provider=?, external_id=?, description=?, platform=?, tier=?, environment=?, regions=? WHERE id=?`, a.Name, a.Provider, a.ExternalID, a.Description, a.Platform, a.Tier, a.Environment, regionsOut, id); err != nil {
        return domain.Account{}, false, err
    }
    return a, true, nil
}

func (s *Store) DeleteAccount(ctx context.Context, id int64) (bool, error) {
    var cnt int
    if err := s.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM pools WHERE account_id=?`, id).Scan(&cnt); err != nil { return false, err }
    if cnt > 0 { return false, fmt.Errorf("account in use by pools: %w", storage.ErrConflict) }
    res, err := s.db.ExecContext(ctx, `DELETE FROM accounts WHERE id=?`, id)
    if err != nil { return false, err }
    n, _ := res.RowsAffected()
    return n > 0, nil
}

func (s *Store) DeleteAccountCascade(ctx context.Context, id int64) (bool, error) {
    // Delete all pools referencing this account, including their descendants, then delete account
    ps, err := s.ListPools(ctx)
    if err != nil { return false, err }
    // Check account exists
    var accCnt int
    if err := s.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM accounts WHERE id=?`, id).Scan(&accCnt); err != nil { return false, err }
    if accCnt == 0 { return false, nil }
    // Build adjacency
    children := map[int64][]int64{}
    for _, p := range ps {
        if p.ParentID != nil { children[*p.ParentID] = append(children[*p.ParentID], p.ID) }
    }
    // Collect roots: pools with account_id = id
    roots := []int64{}
    for _, p := range ps { if p.AccountID != nil && *p.AccountID == id { roots = append(roots, p.ID) } }
    // Collect all to delete
    toDel := map[int64]struct{}{}
    var dfs func(int64)
    dfs = func(n int64){
        if _, ok := toDel[n]; ok { return }
        toDel[n] = struct{}{}
        for _, ch := range children[n] { dfs(ch) }
    }
    for _, r := range roots { dfs(r) }
    // Delete pools
    for pid := range toDel { if _, err := s.db.ExecContext(ctx, `DELETE FROM pools WHERE id=?`, pid); err != nil { return false, err } }
    // Delete account
    if _, err := s.db.ExecContext(ctx, `DELETE FROM accounts WHERE id=?`, id); err != nil { return false, err }
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

    rows, err := s.db.QueryContext(ctx, `SELECT id, name, cidr, parent_id, account_id, type, status, source, description, tags, created_at, updated_at FROM pools WHERE parent_id=? ORDER BY id ASC`, parentID)
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

    // Get direct children
    rows, err := s.db.QueryContext(ctx, `SELECT cidr FROM pools WHERE parent_id=?`, p.ID)
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
        childRows, err := s.db.QueryContext(ctx, `SELECT id FROM pools WHERE parent_id=?`, parentID)
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
