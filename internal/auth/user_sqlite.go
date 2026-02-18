//go:build sqlite

package auth

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

// SQLiteUserStore is a SQLite-backed implementation of UserStore.
type SQLiteUserStore struct {
	db *sql.DB
}

// NewSQLiteUserStore creates a new SQLite-backed user store.
func NewSQLiteUserStore(dsn string) (*SQLiteUserStore, error) {
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	if _, err := db.Exec(`PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000;`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("set pragmas: %w", err)
	}
	return &SQLiteUserStore{db: db}, nil
}

// NewSQLiteUserStoreFromDB creates a store using an existing DB connection.
func NewSQLiteUserStoreFromDB(db *sql.DB) *SQLiteUserStore {
	return &SQLiteUserStore{db: db}
}

func (s *SQLiteUserStore) Close() error { return s.db.Close() }

func (s *SQLiteUserStore) Create(ctx context.Context, user *User) error {
	if user == nil {
		return ErrUserNotFound
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO users (id, username, email, display_name, role, password_hash, is_active, created_at, updated_at, auth_provider, oidc_subject, oidc_issuer)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		user.ID, user.Username, user.Email, user.DisplayName, string(user.Role),
		user.PasswordHash, boolToInt(user.IsActive),
		user.CreatedAt.Format(time.RFC3339Nano), user.UpdatedAt.Format(time.RFC3339Nano),
		user.AuthProvider, user.OIDCSubject, user.OIDCIssuer,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return ErrUserExists
		}
		return fmt.Errorf("insert user: %w", err)
	}
	return nil
}

func (s *SQLiteUserStore) GetByID(ctx context.Context, id string) (*User, error) {
	return s.scanUser(s.db.QueryRowContext(ctx, `
		SELECT id, username, email, display_name, role, password_hash, is_active, created_at, updated_at, last_login_at, auth_provider, oidc_subject, oidc_issuer
		FROM users WHERE id = ?
	`, id))
}

func (s *SQLiteUserStore) GetByUsername(ctx context.Context, username string) (*User, error) {
	return s.scanUser(s.db.QueryRowContext(ctx, `
		SELECT id, username, email, display_name, role, password_hash, is_active, created_at, updated_at, last_login_at, auth_provider, oidc_subject, oidc_issuer
		FROM users WHERE username = ?
	`, username))
}

func (s *SQLiteUserStore) List(ctx context.Context) ([]*User, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, username, email, display_name, role, is_active, created_at, updated_at, last_login_at, auth_provider, oidc_subject, oidc_issuer
		FROM users ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("query users: %w", err)
	}
	defer rows.Close()

	var users []*User
	for rows.Next() {
		var (
			u                   User
			role                string
			isActive            int
			createdAt, updatedAt string
			lastLoginAt         sql.NullString
		)
		var authProvider sql.NullString
		var oidcSubject sql.NullString
		var oidcIssuer sql.NullString
		if err := rows.Scan(&u.ID, &u.Username, &u.Email, &u.DisplayName, &role, &isActive, &createdAt, &updatedAt, &lastLoginAt, &authProvider, &oidcSubject, &oidcIssuer); err != nil {
			return nil, fmt.Errorf("scan user: %w", err)
		}
		u.Role = Role(role)
		u.IsActive = isActive != 0
		u.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
		u.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt)
		if lastLoginAt.Valid {
			t, _ := time.Parse(time.RFC3339Nano, lastLoginAt.String)
			u.LastLoginAt = &t
		}
		if authProvider.Valid {
			u.AuthProvider = authProvider.String
		}
		if oidcSubject.Valid {
			u.OIDCSubject = oidcSubject.String
		}
		if oidcIssuer.Valid {
			u.OIDCIssuer = oidcIssuer.String
		}
		// Never include password hash in list results
		users = append(users, &u)
	}
	return users, rows.Err()
}

func (s *SQLiteUserStore) Update(ctx context.Context, user *User) error {
	if user == nil || user.ID == "" {
		return ErrUserNotFound
	}
	res, err := s.db.ExecContext(ctx, `
		UPDATE users SET username = ?, email = ?, display_name = ?, role = ?,
			password_hash = ?, is_active = ?, updated_at = ?
		WHERE id = ?
	`,
		user.Username, user.Email, user.DisplayName, string(user.Role),
		user.PasswordHash, boolToInt(user.IsActive),
		user.UpdatedAt.Format(time.RFC3339Nano), user.ID,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return ErrUserExists
		}
		return fmt.Errorf("update user: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrUserNotFound
	}
	return nil
}

func (s *SQLiteUserStore) Delete(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete user: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrUserNotFound
	}
	return nil
}

func (s *SQLiteUserStore) UpdateLastLogin(ctx context.Context, id string, t time.Time) error {
	res, err := s.db.ExecContext(ctx, `UPDATE users SET last_login_at = ? WHERE id = ?`,
		t.Format(time.RFC3339Nano), id)
	if err != nil {
		return fmt.Errorf("update last login: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrUserNotFound
	}
	return nil
}

func (s *SQLiteUserStore) GetByOIDCIdentity(ctx context.Context, issuer, subject string) (*User, error) {
	return s.scanUser(s.db.QueryRowContext(ctx, `
		SELECT id, username, email, display_name, role, password_hash, is_active, created_at, updated_at, last_login_at, auth_provider, oidc_subject, oidc_issuer
		FROM users WHERE oidc_issuer = ? AND oidc_subject = ?
	`, issuer, subject))
}

func (s *SQLiteUserStore) scanUser(row *sql.Row) (*User, error) {
	var (
		u                    User
		role                 string
		isActive             int
		createdAt, updatedAt string
		lastLoginAt          sql.NullString
		authProvider         sql.NullString
		oidcSubject          sql.NullString
		oidcIssuer           sql.NullString
	)

	err := row.Scan(&u.ID, &u.Username, &u.Email, &u.DisplayName, &role,
		&u.PasswordHash, &isActive, &createdAt, &updatedAt, &lastLoginAt,
		&authProvider, &oidcSubject, &oidcIssuer)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scan user: %w", err)
	}

	u.Role = Role(role)
	u.IsActive = isActive != 0
	u.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
	u.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt)
	if lastLoginAt.Valid {
		t, _ := time.Parse(time.RFC3339Nano, lastLoginAt.String)
		u.LastLoginAt = &t
	}
	if authProvider.Valid {
		u.AuthProvider = authProvider.String
	}
	if oidcSubject.Valid {
		u.OIDCSubject = oidcSubject.String
	}
	if oidcIssuer.Valid {
		u.OIDCIssuer = oidcIssuer.String
	}
	return &u, nil
}
