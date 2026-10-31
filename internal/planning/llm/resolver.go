package llm

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"cloudpam/internal/domain"
)

// ErrStoredKeyUnreadable reports a stored API key that could not be decrypted
// (wrong or missing CLOUDPAM_OIDC_ENCRYPTION_KEY). The resolution that comes
// with it is still usable: it falls back to the environment key when the
// endpoint is not stored in the database, and runs without a key otherwise.
var ErrStoredKeyUnreadable = errors.New("stored ai provider api key cannot be decrypted")

// SettingsSource reads the stored AI provider document.
type SettingsSource interface {
	GetAIProviderSettings(ctx context.Context) (*domain.AIProviderSettings, error)
}

// Resolved is the effective provider configuration and where each value came from.
type Resolved struct {
	Config           Config
	Sources          domain.AIProviderSettingSources
	Stored           domain.AIProviderSettings
	APIKeyConfigured bool
}

// Resolver merges the stored settings over the environment configuration.
// Precedence per field: non-empty stored value, then environment, then the
// built-in default.
//
// The API key is bound to its endpoint: the environment key is used only when
// the effective endpoint comes from the environment or the built-in default.
// A database endpoint only ever receives a database key, so whoever can set
// the endpoint cannot redirect CLOUDPAM_LLM_API_KEY to a host of their choice.
type Resolver struct {
	env     Config
	source  SettingsSource
	decrypt func(string) (string, error)

	mu             sync.Mutex
	lastUnreadable string // last ciphertext warned about, so each is logged once
}

// NewResolver builds a resolver. env is usually ConfigFromEnv(); source may be
// nil (environment only); decrypt opens the stored API key ciphertext.
func NewResolver(env Config, source SettingsSource, decrypt func(string) (string, error)) *Resolver {
	return &Resolver{env: env, source: source, decrypt: decrypt}
}

// Resolve reads the store and returns the effective configuration. It returns
// ErrStoredKeyUnreadable alongside a usable resolution when the stored key
// cannot be decrypted, and a plain error when the store cannot be read.
func (r *Resolver) Resolve(ctx context.Context) (Resolved, error) {
	stored := domain.DefaultAIProviderSettings()
	if r.source != nil {
		doc, err := r.source.GetAIProviderSettings(ctx)
		if err != nil {
			return Resolved{}, fmt.Errorf("load ai provider settings: %w", err)
		}
		if doc != nil {
			stored = *domain.NormalizeAIProviderSettings(doc.Clone())
		}
	}

	out := Resolved{Stored: stored}
	cfg := &out.Config
	src := &out.Sources

	cfg.Endpoint, src.Endpoint = pickString(stored.Endpoint, r.env.Endpoint, "")
	cfg.Model, src.Model = pickString(stored.Model, r.env.Model, DefaultModel)

	switch {
	case stored.MaxTokens > 0:
		cfg.MaxTokens, src.MaxTokens = stored.MaxTokens, domain.AISettingSourceDatabase
	case r.env.MaxTokens > 0 && r.env.MaxTokens != DefaultMaxTokens:
		cfg.MaxTokens, src.MaxTokens = r.env.MaxTokens, domain.AISettingSourceEnvironment
	default:
		cfg.MaxTokens, src.MaxTokens = DefaultMaxTokens, domain.AISettingSourceDefault
	}

	// ConfigFromEnv always fills the temperature, and an explicit 0 is a
	// valid environment value, so the env value is used as given.
	switch {
	case stored.Temperature != nil:
		cfg.Temperature, src.Temperature = *stored.Temperature, domain.AISettingSourceDatabase
	case r.env.Temperature != DefaultTemperature:
		cfg.Temperature, src.Temperature = r.env.Temperature, domain.AISettingSourceEnvironment
	default:
		cfg.Temperature, src.Temperature = DefaultTemperature, domain.AISettingSourceDefault
	}

	var keyErr error
	if stored.APIKeyEncrypted != "" {
		key, err := r.decryptKey(stored.APIKeyEncrypted)
		if err == nil && key != "" {
			cfg.APIKey, src.APIKey = key, domain.AISettingSourceDatabase
		} else {
			keyErr = ErrStoredKeyUnreadable
			r.warnUnreadable(stored.APIKeyEncrypted)
		}
	}
	if cfg.APIKey == "" {
		if r.env.APIKey != "" && src.Endpoint != domain.AISettingSourceDatabase {
			cfg.APIKey, src.APIKey = r.env.APIKey, domain.AISettingSourceEnvironment
		} else {
			src.APIKey = domain.AISettingSourceNone
		}
	}
	out.APIKeyConfigured = cfg.APIKey != ""
	return out, keyErr
}

// warnUnreadable logs an undecryptable stored key once per distinct ciphertext
// rather than on every resolve. The ciphertext itself is never logged.
func (r *Resolver) warnUnreadable(ciphertext string) {
	r.mu.Lock()
	seen := r.lastUnreadable == ciphertext
	r.lastUnreadable = ciphertext
	r.mu.Unlock()
	if seen {
		return
	}
	slog.Default().Warn("stored ai provider api key cannot be decrypted",
		"hint", "check CLOUDPAM_OIDC_ENCRYPTION_KEY or save the api key again; CLOUDPAM_LLM_API_KEY is used only when the endpoint is not stored in the database")
}

func (r *Resolver) decryptKey(ciphertext string) (string, error) {
	if r.decrypt == nil {
		return "", ErrStoredKeyUnreadable
	}
	return r.decrypt(ciphertext)
}

// pickString applies stored > environment > default for a string field. An
// environment value equal to the default is reported as the default.
func pickString(stored, env, def string) (string, string) {
	switch {
	case stored != "":
		return stored, domain.AISettingSourceDatabase
	case env != "" && env != def:
		return env, domain.AISettingSourceEnvironment
	default:
		return def, domain.AISettingSourceDefault
	}
}
