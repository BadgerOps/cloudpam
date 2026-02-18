//go:build sqlite

package auth

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

// SQLiteSessionStore is a SQLite-backed implementation of SessionStore.
type SQLiteSessionStore struct {
	db *sql.DB
}

// NewSQLiteSessionStore creates a new SQLite-backed session store.
func NewSQLiteSessionStore(dsn string) (*SQLiteSessionStore, error) {
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	if _, err := db.Exec(`PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000;`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("set pragmas: %w", err)
	}
	return &SQLiteSessionStore{db: db}, nil
}

// NewSQLiteSessionStoreFromDB creates a store using an existing DB connection.
func NewSQLiteSessionStoreFromDB(db *sql.DB) *SQLiteSessionStore {
	return &SQLiteSessionStore{db: db}
}

func (s *SQLiteSessionStore) Close() error { return s.db.Close() }

func (s *SQLiteSessionStore) Create(ctx context.Context, session *Session) error {
	if session == nil || session.ID == "" || session.UserID == "" {
		return ErrInvalidSession
	}

	metadataJSON := "{}"
	if session.Metadata != nil {
		b, err := json.Marshal(session.Metadata)
		if err != nil {
			return fmt.Errorf("marshal metadata: %w", err)
		}
		metadataJSON = string(b)
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO sessions (id, user_id, role, created_at, expires_at, metadata)
		VALUES (?, ?, ?, ?, ?, ?)
	`,
		session.ID, session.UserID, string(session.Role),
		session.CreatedAt.Format(time.RFC3339Nano),
		session.ExpiresAt.Format(time.RFC3339Nano),
		metadataJSON,
	)
	if err != nil {
		return fmt.Errorf("insert session: %w", err)
	}
	return nil
}

func (s *SQLiteSessionStore) Get(ctx context.Context, id string) (*Session, error) {
	if id == "" {
		return nil, nil
	}

	var (
		session                    Session
		role                       string
		createdAt, expiresAt       string
		metadataJSON               string
	)

	err := s.db.QueryRowContext(ctx, `
		SELECT id, user_id, role, created_at, expires_at, metadata
		FROM sessions WHERE id = ?
	`, id).Scan(&session.ID, &session.UserID, &role, &createdAt, &expiresAt, &metadataJSON)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scan session: %w", err)
	}

	session.Role = Role(role)
	session.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
	session.ExpiresAt, _ = time.Parse(time.RFC3339Nano, expiresAt)

	if metadataJSON != "" && metadataJSON != "{}" {
		_ = json.Unmarshal([]byte(metadataJSON), &session.Metadata)
	}

	if session.IsExpired() {
		return nil, ErrSessionExpired
	}

	return &session, nil
}

func (s *SQLiteSessionStore) Delete(ctx context.Context, id string) error {
	if id == "" {
		return ErrSessionNotFound
	}
	res, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrSessionNotFound
	}
	return nil
}

func (s *SQLiteSessionStore) DeleteByUserID(ctx context.Context, userID string) error {
	if userID == "" {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ?`, userID)
	if err != nil {
		return fmt.Errorf("delete sessions by user: %w", err)
	}
	return nil
}

func (s *SQLiteSessionStore) ListByUserID(ctx context.Context, userID string) ([]*Session, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, user_id, role, created_at, expires_at FROM sessions WHERE user_id = ? AND expires_at > datetime('now') ORDER BY created_at ASC`,
		userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var sessions []*Session
	for rows.Next() {
		var sess Session
		var role, createdAt, expiresAt string
		if err := rows.Scan(&sess.ID, &sess.UserID, &role, &createdAt, &expiresAt); err != nil {
			return nil, err
		}
		sess.Role = Role(role)
		sess.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		sess.ExpiresAt, _ = time.Parse(time.RFC3339, expiresAt)
		sessions = append(sessions, &sess)
	}
	return sessions, rows.Err()
}

func (s *SQLiteSessionStore) Cleanup(ctx context.Context) (int, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at < ?`,
		time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return 0, fmt.Errorf("cleanup sessions: %w", err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}
