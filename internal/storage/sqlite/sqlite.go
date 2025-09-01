//go:build sqlite

package sqlite

import (
    "context"
    "database/sql"
    "errors"
    "encoding/json"
    "fmt"
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

func (s *Store) ListPools(ctx context.Context) ([]domain.Pool, error) {
    rows, err := s.db.QueryContext(ctx, `SELECT id, name, cidr, parent_id, account_id, created_at FROM pools ORDER BY id ASC`)
    if err != nil {
        return nil, err
    }
    defer rows.Close()
    var out []domain.Pool
    for rows.Next() {
        var p domain.Pool
        var ts string
        var parent sql.NullInt64
        var account sql.NullInt64
        if err := rows.Scan(&p.ID, &p.Name, &p.CIDR, &parent, &account, &ts); err != nil {
            return nil, err
        }
        if parent.Valid {
            p.ParentID = &parent.Int64
        }
        if account.Valid {
            p.AccountID = &account.Int64
        }
        if t, e := time.Parse(time.RFC3339, ts); e == nil {
            p.CreatedAt = t
        }
        out = append(out, p)
    }
    return out, rows.Err()
}

func (s *Store) CreatePool(ctx context.Context, in domain.CreatePool) (domain.Pool, error) {
    if in.Name == "" || in.CIDR == "" {
        return domain.Pool{}, errors.New("name and cidr required")
    }
    res, err := s.db.ExecContext(ctx, `INSERT INTO pools(name, cidr, parent_id, account_id, created_at) VALUES(?, ?, ?, ?, ?)`, in.Name, in.CIDR, in.ParentID, in.AccountID, time.Now().UTC().Format(time.RFC3339))
    if err != nil {
        return domain.Pool{}, err
    }
    id, err := res.LastInsertId()
    if err != nil {
        return domain.Pool{}, err
    }
    return domain.Pool{ID: id, Name: in.Name, CIDR: in.CIDR, ParentID: in.ParentID, AccountID: in.AccountID, CreatedAt: time.Now().UTC()}, nil
}

func (s *Store) GetPool(ctx context.Context, id int64) (domain.Pool, bool, error) {
    var p domain.Pool
    var ts string
    var parent sql.NullInt64
    row := s.db.QueryRowContext(ctx, `SELECT id, name, cidr, parent_id, account_id, created_at FROM pools WHERE id=?`, id)
    var account sql.NullInt64
    if err := row.Scan(&p.ID, &p.Name, &p.CIDR, &parent, &account, &ts); err != nil {
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
    if t, e := time.Parse(time.RFC3339, ts); e == nil {
        p.CreatedAt = t
    }
    return p, true, nil
}

func (s *Store) UpdatePoolAccount(ctx context.Context, id int64, accountID *int64) (domain.Pool, bool, error) {
    // Update and then fetch
    if _, err := s.db.ExecContext(ctx, `UPDATE pools SET account_id=? WHERE id=?`, accountID, id); err != nil {
        return domain.Pool{}, false, err
    }
    return s.GetPool(ctx, id)
}

func (s *Store) UpdatePoolMeta(ctx context.Context, id int64, name *string, accountID *int64) (domain.Pool, bool, error) {
    // Fetch current
    p, ok, err := s.GetPool(ctx, id)
    if err != nil || !ok { return domain.Pool{}, ok, err }
    if name != nil { p.Name = *name }
    // accountID can be nil to clear
    if accountID != nil || true { p.AccountID = accountID }
    if _, err := s.db.ExecContext(ctx, `UPDATE pools SET name=?, account_id=? WHERE id=?`, p.Name, p.AccountID, id); err != nil {
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
    if cnt > 0 { return false, errors.New("pool has child pools") }
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
        return domain.Account{}, errors.New("key and name required")
    }
    var regions string
    if len(in.Regions) > 0 { if b, e := json.Marshal(in.Regions); e == nil { regions = string(b) } }
    res, err := s.db.ExecContext(ctx, `INSERT INTO accounts(key, name, provider, external_id, description, platform, tier, environment, regions, created_at) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, in.Key, in.Name, in.Provider, in.ExternalID, in.Description, in.Platform, in.Tier, in.Environment, regions, time.Now().UTC().Format(time.RFC3339))
    if err != nil {
        return domain.Account{}, err
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
    if cnt > 0 { return false, errors.New("account in use by pools") }
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
