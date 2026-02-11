//go:build sqlite

package auth

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	_ "modernc.org/sqlite" // CGO-less SQLite driver
)

// SQLiteKeyStore is a SQLite-backed implementation of KeyStore.
type SQLiteKeyStore struct {
	db *sql.DB
}

// NewSQLiteKeyStore creates a new SQLite-backed key store.
func NewSQLiteKeyStore(dsn string) (*SQLiteKeyStore, error) {
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	if _, err := db.Exec(`PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000;`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("set pragmas: %w", err)
	}
	return &SQLiteKeyStore{db: db}, nil
}

// NewSQLiteKeyStoreFromDB creates a new SQLite-backed key store using an existing DB connection.
func NewSQLiteKeyStoreFromDB(db *sql.DB) *SQLiteKeyStore {
	return &SQLiteKeyStore{db: db}
}

// Close closes the database connection.
func (s *SQLiteKeyStore) Close() error {
	return s.db.Close()
}

// Create stores a new API key.
func (s *SQLiteKeyStore) Create(ctx context.Context, key *APIKey) error {
	if key == nil {
		return ErrKeyNotFound
	}

	scopesJSON, err := json.Marshal(key.Scopes)
	if err != nil {
		return fmt.Errorf("marshal scopes: %w", err)
	}

	var expiresAt sql.NullString
	if key.ExpiresAt != nil {
		expiresAt = sql.NullString{String: key.ExpiresAt.Format(time.RFC3339Nano), Valid: true}
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO api_keys (id, prefix, name, hash, salt, scopes, created_at, expires_at, revoked)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		key.ID,
		key.Prefix,
		key.Name,
		key.Hash,
		key.Salt,
		string(scopesJSON),
		key.CreatedAt.Format(time.RFC3339Nano),
		expiresAt,
		boolToInt(key.Revoked),
	)
	if err != nil {
		// Check for unique constraint violation on prefix
		if isUniqueViolation(err) {
			return ErrInvalidKeyFormat
		}
		return fmt.Errorf("insert api key: %w", err)
	}

	return nil
}

// GetByPrefix retrieves an API key by its prefix.
func (s *SQLiteKeyStore) GetByPrefix(ctx context.Context, prefix string) (*APIKey, error) {
	return s.scanKey(s.db.QueryRowContext(ctx, `
		SELECT id, prefix, name, hash, salt, scopes, created_at, expires_at, last_used_at, revoked
		FROM api_keys WHERE prefix = ?
	`, prefix))
}

// GetByID retrieves an API key by its ID.
func (s *SQLiteKeyStore) GetByID(ctx context.Context, id string) (*APIKey, error) {
	return s.scanKey(s.db.QueryRowContext(ctx, `
		SELECT id, prefix, name, hash, salt, scopes, created_at, expires_at, last_used_at, revoked
		FROM api_keys WHERE id = ?
	`, id))
}

// List returns all API keys (without hash/salt for security).
func (s *SQLiteKeyStore) List(ctx context.Context) ([]*APIKey, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, prefix, name, scopes, created_at, expires_at, last_used_at, revoked
		FROM api_keys ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("query api keys: %w", err)
	}
	defer rows.Close()

	var keys []*APIKey
	for rows.Next() {
		var (
			key                          APIKey
			scopesJSON                   string
			createdAt                    string
			expiresAt, lastUsedAt        sql.NullString
			revoked                      int
		)

		if err := rows.Scan(&key.ID, &key.Prefix, &key.Name, &scopesJSON, &createdAt, &expiresAt, &lastUsedAt, &revoked); err != nil {
			return nil, fmt.Errorf("scan api key: %w", err)
		}

		key.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
		key.Revoked = revoked != 0

		if err := json.Unmarshal([]byte(scopesJSON), &key.Scopes); err != nil {
			key.Scopes = nil
		}

		if expiresAt.Valid {
			t, _ := time.Parse(time.RFC3339Nano, expiresAt.String)
			key.ExpiresAt = &t
		}

		if lastUsedAt.Valid {
			t, _ := time.Parse(time.RFC3339Nano, lastUsedAt.String)
			key.LastUsedAt = &t
		}

		keys = append(keys, &key)
	}

	return keys, rows.Err()
}

// Revoke marks an API key as revoked.
func (s *SQLiteKeyStore) Revoke(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE api_keys SET revoked = 1 WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("revoke api key: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrKeyNotFound
	}
	return nil
}

// UpdateLastUsed updates the last used timestamp for an API key.
func (s *SQLiteKeyStore) UpdateLastUsed(ctx context.Context, id string, t time.Time) error {
	res, err := s.db.ExecContext(ctx, `UPDATE api_keys SET last_used_at = ? WHERE id = ?`,
		t.Format(time.RFC3339Nano), id)
	if err != nil {
		return fmt.Errorf("update last used: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrKeyNotFound
	}
	return nil
}

// Delete permanently removes an API key.
func (s *SQLiteKeyStore) Delete(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM api_keys WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete api key: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrKeyNotFound
	}
	return nil
}

// scanKey scans a single row into an APIKey.
// Returns nil, nil if no row found.
func (s *SQLiteKeyStore) scanKey(row *sql.Row) (*APIKey, error) {
	var (
		key                          APIKey
		scopesJSON                   string
		createdAt                    string
		expiresAt, lastUsedAt        sql.NullString
		revoked                      int
	)

	err := row.Scan(&key.ID, &key.Prefix, &key.Name, &key.Hash, &key.Salt,
		&scopesJSON, &createdAt, &expiresAt, &lastUsedAt, &revoked)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scan api key: %w", err)
	}

	key.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
	key.Revoked = revoked != 0

	if err := json.Unmarshal([]byte(scopesJSON), &key.Scopes); err != nil {
		key.Scopes = nil
	}

	if expiresAt.Valid {
		t, _ := time.Parse(time.RFC3339Nano, expiresAt.String)
		key.ExpiresAt = &t
	}

	if lastUsedAt.Valid {
		t, _ := time.Parse(time.RFC3339Nano, lastUsedAt.String)
		key.LastUsedAt = &t
	}

	return &key, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// isUniqueViolation checks if the error is a SQLite UNIQUE constraint violation.
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return contains(msg, "UNIQUE constraint failed") || contains(msg, "duplicate")
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchSubstr(s, substr)
}

func searchSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
