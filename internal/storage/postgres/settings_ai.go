//go:build postgres

package postgres

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5"

	"cloudpam/internal/domain"
)

// GetAIProviderSettings retrieves the stored AI provider document. A missing
// row yields an empty document so the environment and defaults apply.
func (s *Store) GetAIProviderSettings(ctx context.Context) (*domain.AIProviderSettings, error) {
	var raw string
	err := s.pool.QueryRow(ctx, `SELECT value FROM settings WHERE key = 'ai_provider'`).Scan(&raw)
	if err == pgx.ErrNoRows {
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
	_, err = s.pool.Exec(ctx,
		`INSERT INTO settings (key, value, updated_at)
		 VALUES ('ai_provider', $1, NOW())
		 ON CONFLICT (key) DO UPDATE
		 SET value = EXCLUDED.value, updated_at = EXCLUDED.updated_at`,
		string(raw),
	)
	return err
}
