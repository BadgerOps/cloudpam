//go:build sqlite

package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"

	"cloudpam/internal/domain"
)

// GetAIProviderSettings retrieves the stored AI provider document. A missing
// row yields an empty document so the environment and defaults apply.
func (s *Store) GetAIProviderSettings(ctx context.Context) (*domain.AIProviderSettings, error) {
	var raw string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = 'ai_provider'`).Scan(&raw)
	if err == sql.ErrNoRows {
		defaults := domain.DefaultAIProviderSettings()
		return &defaults, nil
	}
	if err != nil {
		return nil, err
	}
	var settings domain.AIProviderSettings
	if err := json.Unmarshal([]byte(raw), &settings); err != nil {
		return nil, err
	}
	return domain.NormalizeAIProviderSettings(&settings), nil
}

// UpdateAIProviderSettings saves the AI provider document.
func (s *Store) UpdateAIProviderSettings(ctx context.Context, settings *domain.AIProviderSettings) error {
	settings = domain.NormalizeAIProviderSettings(settings.Clone())
	raw, err := json.Marshal(settings)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO settings (key, value, updated_at) VALUES ('ai_provider', ?, datetime('now'))
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		string(raw))
	return err
}
