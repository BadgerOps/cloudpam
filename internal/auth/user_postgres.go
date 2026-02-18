//go:build postgres

package auth

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresUserStore is a PostgreSQL-backed implementation of UserStore.
type PostgresUserStore struct {
	pool    *pgxpool.Pool
	ownPool bool
	orgID   string
}

// NewPostgresUserStore creates a new PostgreSQL-backed user store with its own connection pool.
func NewPostgresUserStore(connStr string) (*PostgresUserStore, error) {
	pool, err := pgxpool.New(context.Background(), connStr)
	if err != nil {
		return nil, err
	}
	return &PostgresUserStore{pool: pool, ownPool: true, orgID: defaultOrgID}, nil
}

// NewPostgresUserStoreFromPool creates a user store using an existing pool.
func NewPostgresUserStoreFromPool(pool *pgxpool.Pool) *PostgresUserStore {
	return &PostgresUserStore{pool: pool, ownPool: false, orgID: defaultOrgID}
}

func (s *PostgresUserStore) Close() error {
	if s.ownPool {
		s.pool.Close()
	}
	return nil
}

func (s *PostgresUserStore) Create(ctx context.Context, user *User) error {
	if user == nil {
		return ErrUserNotFound
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO users (id, username, email, display_name, role, password_hash, is_active, created_at, updated_at, auth_provider, oidc_subject, oidc_issuer)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`,
		user.ID, user.Username, user.Email, user.DisplayName, string(user.Role),
		user.PasswordHash, user.IsActive, user.CreatedAt, user.UpdatedAt,
		user.AuthProvider, user.OIDCSubject, user.OIDCIssuer,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return ErrUserExists
		}
		return err
	}
	return nil
}

func (s *PostgresUserStore) GetByID(ctx context.Context, id string) (*User, error) {
	return s.scanUser(s.pool.QueryRow(ctx, `
		SELECT id, username, email, display_name, role, password_hash, is_active, created_at, updated_at, last_login_at, auth_provider, oidc_subject, oidc_issuer
		FROM users WHERE id = $1`, id))
}

func (s *PostgresUserStore) GetByUsername(ctx context.Context, username string) (*User, error) {
	return s.scanUser(s.pool.QueryRow(ctx, `
		SELECT id, username, email, display_name, role, password_hash, is_active, created_at, updated_at, last_login_at, auth_provider, oidc_subject, oidc_issuer
		FROM users WHERE username = $1`, username))
}

func (s *PostgresUserStore) List(ctx context.Context) ([]*User, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, username, email, display_name, role, is_active, created_at, updated_at, last_login_at, auth_provider, oidc_subject, oidc_issuer
		FROM users ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []*User
	for rows.Next() {
		var u User
		var role string
		var lastLoginAt *time.Time
		var authProvider, oidcSubject, oidcIssuer *string
		if err := rows.Scan(&u.ID, &u.Username, &u.Email, &u.DisplayName, &role, &u.IsActive, &u.CreatedAt, &u.UpdatedAt, &lastLoginAt, &authProvider, &oidcSubject, &oidcIssuer); err != nil {
			return nil, err
		}
		u.Role = Role(role)
		u.LastLoginAt = lastLoginAt
		if authProvider != nil {
			u.AuthProvider = *authProvider
		}
		if oidcSubject != nil {
			u.OIDCSubject = *oidcSubject
		}
		if oidcIssuer != nil {
			u.OIDCIssuer = *oidcIssuer
		}
		users = append(users, &u)
	}
	return users, rows.Err()
}

func (s *PostgresUserStore) Update(ctx context.Context, user *User) error {
	if user == nil || user.ID == "" {
		return ErrUserNotFound
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE users SET username = $2, email = $3, display_name = $4, role = $5,
			password_hash = $6, is_active = $7, updated_at = $8
		WHERE id = $1`,
		user.ID, user.Username, user.Email, user.DisplayName, string(user.Role),
		user.PasswordHash, user.IsActive, user.UpdatedAt,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return ErrUserExists
		}
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrUserNotFound
	}
	return nil
}

func (s *PostgresUserStore) Delete(ctx context.Context, id string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrUserNotFound
	}
	return nil
}

func (s *PostgresUserStore) UpdateLastLogin(ctx context.Context, id string, t time.Time) error {
	tag, err := s.pool.Exec(ctx, `UPDATE users SET last_login_at = $2 WHERE id = $1`, id, t)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrUserNotFound
	}
	return nil
}

func (s *PostgresUserStore) scanUser(row pgx.Row) (*User, error) {
	var u User
	var role string
	var lastLoginAt *time.Time
	var authProvider, oidcSubject, oidcIssuer *string

	err := row.Scan(&u.ID, &u.Username, &u.Email, &u.DisplayName, &role,
		&u.PasswordHash, &u.IsActive, &u.CreatedAt, &u.UpdatedAt, &lastLoginAt,
		&authProvider, &oidcSubject, &oidcIssuer)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}

	u.Role = Role(role)
	u.LastLoginAt = lastLoginAt
	if authProvider != nil {
		u.AuthProvider = *authProvider
	}
	if oidcSubject != nil {
		u.OIDCSubject = *oidcSubject
	}
	if oidcIssuer != nil {
		u.OIDCIssuer = *oidcIssuer
	}
	return &u, nil
}
