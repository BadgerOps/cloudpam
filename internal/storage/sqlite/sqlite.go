//go:build sqlite

package sqlite

import (
    "context"
    "database/sql"
    "errors"
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
    if _, err := db.Exec(`PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000;`); err != nil {
        _ = db.Close()
        return nil, err
    }
    if err := migrate(db); err != nil {
        _ = db.Close()
        return nil, err
    }
    return &Store{db: db}, nil
}

func migrate(db *sql.DB) error {
    // Minimal bootstrap migration with additive column for parent_id.
    if _, err := db.Exec(`
CREATE TABLE IF NOT EXISTS pools (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    cidr TEXT NOT NULL,
    parent_id INTEGER NULL,
    account_id INTEGER NULL,
    created_at TIMESTAMP NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now'))
);
`); err != nil {
        return err
    }
    // Ensure parent_id exists for older schemas.
    var cnt int
    if err := db.QueryRow(`SELECT COUNT(1) FROM pragma_table_info('pools') WHERE name='parent_id'`).Scan(&cnt); err == nil && cnt == 0 {
        if _, err := db.Exec(`ALTER TABLE pools ADD COLUMN parent_id INTEGER NULL`); err != nil {
            return err
        }
    }
    // Ensure account_id exists.
    cnt = 0
    if err := db.QueryRow(`SELECT COUNT(1) FROM pragma_table_info('pools') WHERE name='account_id'`).Scan(&cnt); err == nil && cnt == 0 {
        if _, err := db.Exec(`ALTER TABLE pools ADD COLUMN account_id INTEGER NULL`); err != nil {
            return err
        }
    }
    // Accounts table
    if _, err := db.Exec(`
CREATE TABLE IF NOT EXISTS accounts (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    key TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL,
    provider TEXT NULL,
    external_id TEXT NULL,
    description TEXT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ','now'))
);
`); err != nil {
        return err
    }
    return nil
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

// Accounts
func (s *Store) ListAccounts(ctx context.Context) ([]domain.Account, error) {
    rows, err := s.db.QueryContext(ctx, `SELECT id, key, name, provider, external_id, description, created_at FROM accounts ORDER BY id ASC`)
    if err != nil {
        return nil, err
    }
    defer rows.Close()
    var out []domain.Account
    for rows.Next() {
        var a domain.Account
        var ts string
        if err := rows.Scan(&a.ID, &a.Key, &a.Name, &a.Provider, &a.ExternalID, &a.Description, &ts); err != nil {
            return nil, err
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
    res, err := s.db.ExecContext(ctx, `INSERT INTO accounts(key, name, provider, external_id, description, created_at) VALUES(?, ?, ?, ?, ?, ?)`, in.Key, in.Name, in.Provider, in.ExternalID, in.Description, time.Now().UTC().Format(time.RFC3339))
    if err != nil {
        return domain.Account{}, err
    }
    id, err := res.LastInsertId()
    if err != nil {
        return domain.Account{}, err
    }
    return domain.Account{ID: id, Key: in.Key, Name: in.Name, Provider: in.Provider, ExternalID: in.ExternalID, Description: in.Description, CreatedAt: time.Now().UTC()}, nil
}
