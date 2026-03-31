//go:build postgres

package postgres

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5"

	"cloudpam/internal/domain"
	"cloudpam/internal/storage"
)

// CreateProvider stores a new OIDC provider configuration.
func (s *Store) CreateProvider(ctx context.Context, p *domain.OIDCProvider) error {
	roleMapping, err := json.Marshal(p.RoleMapping)
	if err != nil {
		return err
	}

	_, err = s.pool.Exec(ctx,
		`INSERT INTO oidc_providers (
			id, name, issuer_url, client_id, client_secret_encrypted, scopes,
			role_mapping, default_role, auto_provision, enabled, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`,
		p.ID, p.Name, p.IssuerURL, p.ClientID, p.ClientSecretEncrypted,
		p.Scopes, string(roleMapping), p.DefaultRole, p.AutoProvision,
		p.Enabled, p.CreatedAt, p.UpdatedAt,
	)
	if isUniqueViolation(err) {
		return storage.ErrDuplicateIssuer
	}
	return storage.WrapIfConflict(err)
}

// GetProvider retrieves an OIDC provider by ID.
func (s *Store) GetProvider(ctx context.Context, id string) (*domain.OIDCProvider, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT id, name, issuer_url, client_id, client_secret_encrypted, scopes,
		        role_mapping, default_role, auto_provision, enabled, created_at, updated_at
		   FROM oidc_providers
		  WHERE id = $1`,
		id,
	)
	return scanProvider(row)
}

// GetProviderByIssuer retrieves an OIDC provider by issuer URL.
func (s *Store) GetProviderByIssuer(ctx context.Context, issuerURL string) (*domain.OIDCProvider, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT id, name, issuer_url, client_id, client_secret_encrypted, scopes,
		        role_mapping, default_role, auto_provision, enabled, created_at, updated_at
		   FROM oidc_providers
		  WHERE issuer_url = $1`,
		issuerURL,
	)
	return scanProvider(row)
}

// ListProviders returns all configured OIDC providers.
func (s *Store) ListProviders(ctx context.Context) ([]*domain.OIDCProvider, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, name, issuer_url, client_id, client_secret_encrypted, scopes,
		        role_mapping, default_role, auto_provision, enabled, created_at, updated_at
		   FROM oidc_providers
		  ORDER BY name`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanProviderRows(rows)
}

// ListEnabledProviders returns only enabled OIDC providers.
func (s *Store) ListEnabledProviders(ctx context.Context) ([]*domain.OIDCProvider, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, name, issuer_url, client_id, client_secret_encrypted, scopes,
		        role_mapping, default_role, auto_provision, enabled, created_at, updated_at
		   FROM oidc_providers
		  WHERE enabled = TRUE
		  ORDER BY name`,
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

	cmd, err := s.pool.Exec(ctx,
		`UPDATE oidc_providers
		    SET name = $1,
		        issuer_url = $2,
		        client_id = $3,
		        client_secret_encrypted = $4,
		        scopes = $5,
		        role_mapping = $6,
		        default_role = $7,
		        auto_provision = $8,
		        enabled = $9,
		        updated_at = $10
		  WHERE id = $11`,
		p.Name, p.IssuerURL, p.ClientID, p.ClientSecretEncrypted,
		p.Scopes, string(roleMapping), p.DefaultRole, p.AutoProvision,
		p.Enabled, p.UpdatedAt, p.ID,
	)
	if isUniqueViolation(err) {
		return storage.ErrDuplicateIssuer
	}
	if err != nil {
		return storage.WrapIfConflict(err)
	}
	if cmd.RowsAffected() == 0 {
		return storage.ErrNotFound
	}
	return nil
}

// DeleteProvider removes an OIDC provider by ID.
func (s *Store) DeleteProvider(ctx context.Context, id string) error {
	cmd, err := s.pool.Exec(ctx, `DELETE FROM oidc_providers WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if cmd.RowsAffected() == 0 {
		return storage.ErrNotFound
	}
	return nil
}

func scanProvider(row interface {
	Scan(dest ...any) error
}) (*domain.OIDCProvider, error) {
	var p domain.OIDCProvider
	var roleMappingJSON string

	if err := row.Scan(
		&p.ID, &p.Name, &p.IssuerURL, &p.ClientID, &p.ClientSecretEncrypted,
		&p.Scopes, &roleMappingJSON, &p.DefaultRole, &p.AutoProvision,
		&p.Enabled, &p.CreatedAt, &p.UpdatedAt,
	); err != nil {
		if err == pgx.ErrNoRows {
			return nil, storage.ErrNotFound
		}
		return nil, err
	}

	p.RoleMapping = make(map[string]string)
	_ = json.Unmarshal([]byte(roleMappingJSON), &p.RoleMapping)
	return &p, nil
}

func scanProviderRows(rows interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}) ([]*domain.OIDCProvider, error) {
	var out []*domain.OIDCProvider
	for rows.Next() {
		var p domain.OIDCProvider
		var roleMappingJSON string

		if err := rows.Scan(
			&p.ID, &p.Name, &p.IssuerURL, &p.ClientID, &p.ClientSecretEncrypted,
			&p.Scopes, &roleMappingJSON, &p.DefaultRole, &p.AutoProvision,
			&p.Enabled, &p.CreatedAt, &p.UpdatedAt,
		); err != nil {
			return nil, err
		}

		p.RoleMapping = make(map[string]string)
		_ = json.Unmarshal([]byte(roleMappingJSON), &p.RoleMapping)
		out = append(out, &p)
	}
	if out == nil {
		out = []*domain.OIDCProvider{}
	}
	return out, rows.Err()
}
