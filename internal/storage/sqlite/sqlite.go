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
    return nil
}

var _ storage.Store = (*Store)(nil)

func (s *Store) ListPools(ctx context.Context) ([]domain.Pool, error) {
    rows, err := s.db.QueryContext(ctx, `SELECT id, name, cidr, parent_id, created_at FROM pools ORDER BY id ASC`)
    if err != nil {
        return nil, err
    }
    defer rows.Close()
    var out []domain.Pool
    for rows.Next() {
        var p domain.Pool
        var ts string
        var parent sql.NullInt64
        if err := rows.Scan(&p.ID, &p.Name, &p.CIDR, &parent, &ts); err != nil {
            return nil, err
        }
        if parent.Valid {
            p.ParentID = &parent.Int64
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
    res, err := s.db.ExecContext(ctx, `INSERT INTO pools(name, cidr, parent_id, created_at) VALUES(?, ?, ?, ?)`, in.Name, in.CIDR, in.ParentID, time.Now().UTC().Format(time.RFC3339))
    if err != nil {
        return domain.Pool{}, err
    }
    id, err := res.LastInsertId()
    if err != nil {
        return domain.Pool{}, err
    }
    return domain.Pool{ID: id, Name: in.Name, CIDR: in.CIDR, ParentID: in.ParentID, CreatedAt: time.Now().UTC()}, nil
}

func (s *Store) GetPool(ctx context.Context, id int64) (domain.Pool, bool, error) {
    var p domain.Pool
    var ts string
    var parent sql.NullInt64
    row := s.db.QueryRowContext(ctx, `SELECT id, name, cidr, parent_id, created_at FROM pools WHERE id=?`, id)
    if err := row.Scan(&p.ID, &p.Name, &p.CIDR, &parent, &ts); err != nil {
        if errors.Is(err, sql.ErrNoRows) {
            return domain.Pool{}, false, nil
        }
        return domain.Pool{}, false, err
    }
    if parent.Valid {
        p.ParentID = &parent.Int64
    }
    if t, e := time.Parse(time.RFC3339, ts); e == nil {
        p.CreatedAt = t
    }
    return p, true, nil
}
