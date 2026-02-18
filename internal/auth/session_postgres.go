//go:build postgres

package auth

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresSessionStore is a PostgreSQL-backed implementation of SessionStore.
type PostgresSessionStore struct {
	pool    *pgxpool.Pool
	ownPool bool
}

// NewPostgresSessionStore creates a new PostgreSQL-backed session store.
func NewPostgresSessionStore(connStr string) (*PostgresSessionStore, error) {
	pool, err := pgxpool.New(context.Background(), connStr)
	if err != nil {
		return nil, err
	}
	return &PostgresSessionStore{pool: pool, ownPool: true}, nil
}

// NewPostgresSessionStoreFromPool creates a session store using an existing pool.
func NewPostgresSessionStoreFromPool(pool *pgxpool.Pool) *PostgresSessionStore {
	return &PostgresSessionStore{pool: pool, ownPool: false}
}

func (s *PostgresSessionStore) Close() error {
	if s.ownPool {
		s.pool.Close()
	}
	return nil
}

func (s *PostgresSessionStore) Create(ctx context.Context, session *Session) error {
	if session == nil || session.ID == "" || session.UserID == "" {
		return ErrInvalidSession
	}

	metadataJSON := []byte("{}")
	if session.Metadata != nil {
		b, err := json.Marshal(session.Metadata)
		if err == nil {
			metadataJSON = b
		}
	}

	_, err := s.pool.Exec(ctx, `
		INSERT INTO sessions (id, user_id, role, created_at, expires_at, metadata)
		VALUES ($1, $2, $3, $4, $5, $6::jsonb)`,
		session.ID, session.UserID, string(session.Role),
		session.CreatedAt, session.ExpiresAt, string(metadataJSON),
	)
	return err
}

func (s *PostgresSessionStore) Get(ctx context.Context, id string) (*Session, error) {
	if id == "" {
		return nil, nil
	}

	var session Session
	var role string
	var metadataJSON []byte

	err := s.pool.QueryRow(ctx, `
		SELECT id, user_id, role, created_at, expires_at, metadata
		FROM sessions WHERE id = $1`, id).
		Scan(&session.ID, &session.UserID, &role, &session.CreatedAt, &session.ExpiresAt, &metadataJSON)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}

	session.Role = Role(role)
	if len(metadataJSON) > 0 {
		_ = json.Unmarshal(metadataJSON, &session.Metadata)
	}

	if session.IsExpired() {
		return nil, ErrSessionExpired
	}

	return &session, nil
}

func (s *PostgresSessionStore) Delete(ctx context.Context, id string) error {
	if id == "" {
		return ErrSessionNotFound
	}
	tag, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrSessionNotFound
	}
	return nil
}

func (s *PostgresSessionStore) DeleteByUserID(ctx context.Context, userID string) error {
	if userID == "" {
		return nil
	}
	_, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE user_id = $1`, userID)
	return err
}

func (s *PostgresSessionStore) ListByUserID(ctx context.Context, userID string) ([]*Session, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, user_id, role, created_at, expires_at FROM sessions WHERE user_id = $1 AND expires_at > NOW() ORDER BY created_at ASC`,
		userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var sessions []*Session
	for rows.Next() {
		var sess Session
		var role string
		if err := rows.Scan(&sess.ID, &sess.UserID, &role, &sess.CreatedAt, &sess.ExpiresAt); err != nil {
			return nil, err
		}
		sess.Role = Role(role)
		sessions = append(sessions, &sess)
	}
	return sessions, rows.Err()
}

func (s *PostgresSessionStore) Cleanup(ctx context.Context) (int, error) {
	tag, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE expires_at < $1`, time.Now().UTC())
	if err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}
