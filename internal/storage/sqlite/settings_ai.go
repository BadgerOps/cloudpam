//go:build sqlite

package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"cloudpam/internal/domain"
	"cloudpam/internal/storage"
)

// UpdateAIProviderSettingsIfUnchanged saves settings only when the stored
// document's updated_at equals expectedUpdatedAt. The write is conditional on
// the exact row value that was compared, so a writer that lands in between
// makes it affect no row, which is reported as storage.ErrConflict.
func (s *Store) UpdateAIProviderSettingsIfUnchanged(ctx context.Context, expectedUpdatedAt time.Time, settings *domain.AIProviderSettings) error {
	settings = domain.NormalizeAIProviderSettings(settings.Clone())
	next, err := json.Marshal(settings)
	if err != nil {
		return err
	}

	var raw string
	var res sql.Result
	err = s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = 'ai_provider'`).Scan(&raw)
	switch {
	case err == sql.ErrNoRows:
		if !expectedUpdatedAt.IsZero() {
			return aiSettingsConflict()
		}
		res, err = s.db.ExecContext(ctx,
			`INSERT INTO settings (key, value, updated_at) VALUES ('ai_provider', ?, datetime('now'))
			 ON CONFLICT(key) DO NOTHING`, string(next))
	case err != nil:
		return err
	default:
		var current domain.AIProviderSettings
		if err := json.Unmarshal([]byte(raw), &current); err != nil {
			return err
		}
		if !current.UpdatedAt.Equal(expectedUpdatedAt) {
			return aiSettingsConflict()
		}
		res, err = s.db.ExecContext(ctx,
			`UPDATE settings SET value = ?, updated_at = datetime('now') WHERE key = 'ai_provider' AND value = ?`,
			string(next), raw)
	}
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return aiSettingsConflict()
	}
	return nil
}

func aiSettingsConflict() error {
	return fmt.Errorf("ai provider settings changed concurrently: %w", storage.ErrConflict)
}

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
