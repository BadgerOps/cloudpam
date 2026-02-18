//go:build sqlite

package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"

	"cloudpam/internal/domain"
)

// GetSecuritySettings retrieves security settings from the database.
func (s *Store) GetSecuritySettings(ctx context.Context) (*domain.SecuritySettings, error) {
	var raw string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = 'security'`).Scan(&raw)
	if err == sql.ErrNoRows {
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

// UpdateSecuritySettings saves security settings to the database.
func (s *Store) UpdateSecuritySettings(ctx context.Context, settings *domain.SecuritySettings) error {
	raw, err := json.Marshal(settings)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO settings (key, value, updated_at) VALUES ('security', ?, datetime('now'))
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		string(raw))
	return err
}
