//go:build postgres

package postgres

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5"

	"cloudpam/internal/domain"
)

// GetSecuritySettings retrieves security settings from PostgreSQL.
func (s *Store) GetSecuritySettings(ctx context.Context) (*domain.SecuritySettings, error) {
	var raw string
	err := s.pool.QueryRow(ctx, `SELECT value FROM settings WHERE key = 'security'`).Scan(&raw)
	if err == pgx.ErrNoRows {
		defaults := domain.DefaultSecuritySettings()
		return &defaults, nil
	}
	if err != nil {
		return nil, err
	}

	var settings domain.SecuritySettings
	if err := json.Unmarshal([]byte(raw), &settings); err != nil {
		return nil, err
	}
	return &settings, nil
}

// UpdateSecuritySettings saves security settings to PostgreSQL.
func (s *Store) UpdateSecuritySettings(ctx context.Context, settings *domain.SecuritySettings) error {
	raw, err := json.Marshal(settings)
	if err != nil {
		return err
	}

	_, err = s.pool.Exec(ctx,
		`INSERT INTO settings (key, value, updated_at)
		 VALUES ('security', $1, NOW())
		 ON CONFLICT (key) DO UPDATE
		 SET value = EXCLUDED.value, updated_at = EXCLUDED.updated_at`,
		string(raw),
	)
	return err
}
