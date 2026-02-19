//go:build sqlite

package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"

	"cloudpam/internal/domain"
	"cloudpam/internal/storage"
)

// CreateProvider stores a new OIDC provider configuration.
func (s *Store) CreateProvider(ctx context.Context, p *domain.OIDCProvider) error {
	roleMapping, err := json.Marshal(p.RoleMapping)
	if err != nil {
		return err
	}

	_, err = s.db.ExecContext(ctx,
		`INSERT INTO oidc_providers (id, name, issuer_url, client_id, client_secret_encrypted, scopes, role_mapping, default_role, auto_provision, enabled, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.ID, p.Name, p.IssuerURL, p.ClientID, p.ClientSecretEncrypted,
		p.Scopes, string(roleMapping), p.DefaultRole,
		boolToInt(p.AutoProvision), boolToInt(p.Enabled),
		p.CreatedAt.Format(time.RFC3339), p.UpdatedAt.Format(time.RFC3339),
	)
	if err != nil {
		msg := err.Error()
		if strings.Contains(msg, "UNIQUE") && strings.Contains(msg, "issuer_url") {
			return storage.ErrDuplicateIssuer
		}
		return storage.WrapIfConflict(err)
	}
	return nil
}

// GetProvider retrieves an OIDC provider by ID.
func (s *Store) GetProvider(ctx context.Context, id string) (*domain.OIDCProvider, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, name, issuer_url, client_id, client_secret_encrypted, scopes, role_mapping, default_role, auto_provision, enabled, created_at, updated_at
		 FROM oidc_providers WHERE id = ?`, id,
	)
	return scanProvider(row)
}

// GetProviderByIssuer retrieves an OIDC provider by issuer URL.
func (s *Store) GetProviderByIssuer(ctx context.Context, issuerURL string) (*domain.OIDCProvider, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, name, issuer_url, client_id, client_secret_encrypted, scopes, role_mapping, default_role, auto_provision, enabled, created_at, updated_at
		 FROM oidc_providers WHERE issuer_url = ?`, issuerURL,
	)
	return scanProvider(row)
}

// ListProviders returns all configured OIDC providers.
func (s *Store) ListProviders(ctx context.Context) ([]*domain.OIDCProvider, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, issuer_url, client_id, client_secret_encrypted, scopes, role_mapping, default_role, auto_provision, enabled, created_at, updated_at
		 FROM oidc_providers ORDER BY name`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanProviderRows(rows)
}

// ListEnabledProviders returns only enabled OIDC providers.
func (s *Store) ListEnabledProviders(ctx context.Context) ([]*domain.OIDCProvider, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, issuer_url, client_id, client_secret_encrypted, scopes, role_mapping, default_role, auto_provision, enabled, created_at, updated_at
		 FROM oidc_providers WHERE enabled = 1 ORDER BY name`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanProviderRows(rows)
}

// UpdateProvider modifies an existing OIDC provider configuration.
func (s *Store) UpdateProvider(ctx context.Context, p *domain.OIDCProvider) error {
	roleMapping, err := json.Marshal(p.RoleMapping)
	if err != nil {
		return err
	}

	res, err := s.db.ExecContext(ctx,
		`UPDATE oidc_providers SET name = ?, issuer_url = ?, client_id = ?, client_secret_encrypted = ?, scopes = ?, role_mapping = ?, default_role = ?, auto_provision = ?, enabled = ?, updated_at = ?
		 WHERE id = ?`,
		p.Name, p.IssuerURL, p.ClientID, p.ClientSecretEncrypted,
		p.Scopes, string(roleMapping), p.DefaultRole,
		boolToInt(p.AutoProvision), boolToInt(p.Enabled),
		p.UpdatedAt.Format(time.RFC3339), p.ID,
	)
	if err != nil {
		msg := err.Error()
		if strings.Contains(msg, "UNIQUE") && strings.Contains(msg, "issuer_url") {
			return storage.ErrDuplicateIssuer
		}
		return storage.WrapIfConflict(err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return storage.ErrNotFound
	}
	return nil
}

// DeleteProvider removes an OIDC provider by ID.
func (s *Store) DeleteProvider(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM oidc_providers WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return storage.ErrNotFound
	}
	return nil
}

// scanProvider scans a single row into an OIDCProvider.
func scanProvider(row *sql.Row) (*domain.OIDCProvider, error) {
	var p domain.OIDCProvider
	var roleMappingJSON, createdAt, updatedAt string
	var autoProvision, enabled int

	if err := row.Scan(&p.ID, &p.Name, &p.IssuerURL, &p.ClientID, &p.ClientSecretEncrypted,
		&p.Scopes, &roleMappingJSON, &p.DefaultRole,
		&autoProvision, &enabled, &createdAt, &updatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, storage.ErrNotFound
		}
		return nil, err
	}

	p.AutoProvision = autoProvision == 1
	p.Enabled = enabled == 1
	p.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	p.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)

	p.RoleMapping = make(map[string]string)
	_ = json.Unmarshal([]byte(roleMappingJSON), &p.RoleMapping)

	return &p, nil
}

// scanProviderRows scans multiple rows into a slice of OIDCProvider pointers.
func scanProviderRows(rows *sql.Rows) ([]*domain.OIDCProvider, error) {
	var out []*domain.OIDCProvider
	for rows.Next() {
		var p domain.OIDCProvider
		var roleMappingJSON, createdAt, updatedAt string
		var autoProvision, enabled int

		if err := rows.Scan(&p.ID, &p.Name, &p.IssuerURL, &p.ClientID, &p.ClientSecretEncrypted,
			&p.Scopes, &roleMappingJSON, &p.DefaultRole,
			&autoProvision, &enabled, &createdAt, &updatedAt); err != nil {
			return nil, err
		}

		p.AutoProvision = autoProvision == 1
		p.Enabled = enabled == 1
		p.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		p.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)

		p.RoleMapping = make(map[string]string)
		_ = json.Unmarshal([]byte(roleMappingJSON), &p.RoleMapping)

		out = append(out, &p)
	}
	if out == nil {
		out = []*domain.OIDCProvider{}
	}
	return out, rows.Err()
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
