//go:build postgres

package auth

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// defaultOrgID is the UUID of the default organization for single-tenant deployments.
const defaultOrgID = "00000000-0000-0000-0000-000000000001"

// PostgresKeyStore is a PostgreSQL-backed implementation of KeyStore.
type PostgresKeyStore struct {
	pool    *pgxpool.Pool
	ownPool bool
	orgID   string
}

// NewPostgresKeyStore creates a new PostgreSQL-backed key store with its own connection pool.
func NewPostgresKeyStore(connStr string) (*PostgresKeyStore, error) {
	pool, err := pgxpool.New(context.Background(), connStr)
	if err != nil {
		return nil, err
	}
	return &PostgresKeyStore{pool: pool, ownPool: true, orgID: defaultOrgID}, nil
}

// NewPostgresKeyStoreFromPool creates a new PostgreSQL-backed key store using an existing pool.
func NewPostgresKeyStoreFromPool(pool *pgxpool.Pool) *PostgresKeyStore {
	return &PostgresKeyStore{pool: pool, ownPool: false, orgID: defaultOrgID}
}

// Close closes the database connection if we own it.
func (s *PostgresKeyStore) Close() error {
	if s.ownPool {
		s.pool.Close()
	}
	return nil
}

// Create stores a new API key.
func (s *PostgresKeyStore) Create(ctx context.Context, key *APIKey) error {
	if key == nil {
		return ErrKeyNotFound
	}

	scopesJSON, _ := json.Marshal(key.Scopes)
	if key.Scopes == nil {
		scopesJSON = []byte("[]")
	}

	_, err := s.pool.Exec(ctx, `
		INSERT INTO api_tokens (id, organization_id, name, prefix, token_hash, token_salt, scopes, created_at, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, $8, $9)`,
		key.ID, s.orgID, key.Name, key.Prefix, key.Hash, key.Salt,
		string(scopesJSON), key.CreatedAt, key.ExpiresAt,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return ErrInvalidKeyFormat
		}
		return err
	}
	return nil
}

// GetByPrefix retrieves an API key by its prefix.
func (s *PostgresKeyStore) GetByPrefix(ctx context.Context, prefix string) (*APIKey, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id, prefix, name, token_hash, token_salt, scopes, created_at, expires_at, last_used_at, revoked_at
		FROM api_tokens
		WHERE prefix = $1 AND organization_id = $2`, prefix, s.orgID)

	return scanAPIKey(row)
}

// GetByID retrieves an API key by its ID.
func (s *PostgresKeyStore) GetByID(ctx context.Context, id string) (*APIKey, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id, prefix, name, token_hash, token_salt, scopes, created_at, expires_at, last_used_at, revoked_at
		FROM api_tokens
		WHERE id = $1 AND organization_id = $2`, id, s.orgID)

	return scanAPIKey(row)
}

// List returns all API keys (without sensitive data).
func (s *PostgresKeyStore) List(ctx context.Context) ([]*APIKey, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, prefix, name, scopes, created_at, expires_at, last_used_at, revoked_at
		FROM api_tokens
		WHERE organization_id = $1
		ORDER BY created_at DESC`, s.orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var keys []*APIKey
	for rows.Next() {
		var k APIKey
		var scopesJSON []byte
		var expiresAt, lastUsedAt, revokedAt *time.Time

		if err := rows.Scan(&k.ID, &k.Prefix, &k.Name, &scopesJSON, &k.CreatedAt, &expiresAt, &lastUsedAt, &revokedAt); err != nil {
			return nil, err
		}

		if len(scopesJSON) > 0 {
			_ = json.Unmarshal(scopesJSON, &k.Scopes)
		}
		k.ExpiresAt = expiresAt
		k.LastUsedAt = lastUsedAt
		k.Revoked = revokedAt != nil

		keys = append(keys, &k)
	}

	return keys, rows.Err()
}

// Revoke marks an API key as revoked.
func (s *PostgresKeyStore) Revoke(ctx context.Context, id string) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE api_tokens SET revoked_at = NOW()
		WHERE id = $1 AND organization_id = $2 AND revoked_at IS NULL`, id, s.orgID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrKeyNotFound
	}
	return nil
}

// UpdateLastUsed updates the last used timestamp for an API key.
func (s *PostgresKeyStore) UpdateLastUsed(ctx context.Context, id string, t time.Time) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE api_tokens SET last_used_at = $2
		WHERE id = $1 AND organization_id = $3`, id, t, s.orgID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrKeyNotFound
	}
	return nil
}

// Delete permanently removes an API key.
func (s *PostgresKeyStore) Delete(ctx context.Context, id string) error {
	tag, err := s.pool.Exec(ctx, `
		DELETE FROM api_tokens WHERE id = $1 AND organization_id = $2`, id, s.orgID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrKeyNotFound
	}
	return nil
}

func scanAPIKey(row pgx.Row) (*APIKey, error) {
	var k APIKey
	var scopesJSON []byte
	var expiresAt, lastUsedAt, revokedAt *time.Time

	err := row.Scan(&k.ID, &k.Prefix, &k.Name, &k.Hash, &k.Salt, &scopesJSON, &k.CreatedAt, &expiresAt, &lastUsedAt, &revokedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}

	if len(scopesJSON) > 0 {
		_ = json.Unmarshal(scopesJSON, &k.Scopes)
	}
	k.ExpiresAt = expiresAt
	k.LastUsedAt = lastUsedAt
	k.Revoked = revokedAt != nil

	return &k, nil
}

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return contains(s, "23505") || contains(s, "unique constraint") || contains(s, "duplicate key")
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
