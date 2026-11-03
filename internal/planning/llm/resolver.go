package llm

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"
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
	// Fallbacks are the stored fallback providers in order, each with the
	// effective max tokens and temperature. They come from the store only.
	Fallbacks []ResolvedFallback
}

// ResolvedFallback is one fallback provider ready to build. A stored key that
// cannot be decrypted leaves APIKey empty and sets KeyUnreadable, so the link
// is skipped by the chain even when its endpoint would accept an
// unauthenticated request, and is reported as unconfigured by the API.
type ResolvedFallback struct {
	ID               string
	Config           Config
	APIKeyConfigured bool
	KeyUnreadable    bool
}

// Label names a configuration in notices and test results, as "<kind> <model>".
// It never includes the endpoint or key.
func (c Config) Label() string {
	return strings.TrimSpace(c.Provider + " " + c.Model)
}

// Fingerprint identifies the configuration a test result applies to: a hex
// SHA-256 over the effective provider kind, endpoint, model and key source,
// plus the stored key ciphertext when the key comes from the database, so
// re-saving the key changes it. No key material, plaintext or hashed, goes in. A rotation of
// CLOUDPAM_LLM_API_KEY with the same endpoint and model is therefore not
// detected and the earlier test result stays in effect; that is accepted.
func (r Resolved) Fingerprint() string {
	h := sha256.New()
	parts := []string{r.Config.Provider, r.Config.Endpoint, r.Config.Model, r.Sources.APIKey}
	if r.Sources.APIKey == domain.AISettingSourceDatabase {
		parts = append(parts, r.Stored.APIKeyEncrypted)
	}
	for _, part := range parts {
		// A separator keeps "ab"+"c" apart from "a"+"bc".
		h.Write([]byte(part))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// Resolver merges the stored settings over the environment configuration.
// Precedence per field: non-empty stored value, then environment, then the
// built-in default.
//
// The API key is bound to its endpoint and provider kind: the environment key
// is used only when both come from the environment or the built-in default.
// A database endpoint or kind only ever receives a database key, so whoever
// can set either cannot redirect CLOUDPAM_LLM_API_KEY to a host of their
// choice (the kind selects the default host).
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

	cfg.Provider, src.Provider = pickString(stored.Provider, r.env.Provider, DefaultProvider)
	cfg.Endpoint, src.Endpoint = pickString(stored.Endpoint, r.env.Endpoint, "")
	// ConfigFromEnv fills the model with its kind's default when
	// CLOUDPAM_LLM_MODEL is unset; that value must not follow a stored kind
	// change, so it is treated as unset and the effective kind's default
	// applies.
	envModel := r.env.Model
	if envModel == DefaultModelFor(r.env.Provider) {
		envModel = ""
	}
	cfg.Model, src.Model = pickString(stored.Model, envModel, DefaultModelFor(cfg.Provider))

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
	var unreadable []string
	if stored.APIKeyEncrypted != "" {
		key, err := r.decryptKey(stored.APIKeyEncrypted)
		if err == nil && key != "" {
			cfg.APIKey, src.APIKey = key, domain.AISettingSourceDatabase
		} else {
			keyErr = ErrStoredKeyUnreadable
			unreadable = append(unreadable, stored.APIKeyEncrypted)
		}
	}
	if cfg.APIKey == "" {
		if r.env.APIKey != "" && src.Endpoint != domain.AISettingSourceDatabase && src.Provider != domain.AISettingSourceDatabase {
			cfg.APIKey, src.APIKey = r.env.APIKey, domain.AISettingSourceEnvironment
		} else {
			src.APIKey = domain.AISettingSourceNone
		}
	}
	out.APIKeyConfigured = cfg.APIKey != ""

	for _, fb := range stored.Fallbacks {
		link := ResolvedFallback{ID: fb.ID, Config: Config{
			Provider:    fb.Provider,
			Endpoint:    fb.Endpoint,
			Model:       fb.Model,
			MaxTokens:   cfg.MaxTokens,
			Temperature: cfg.Temperature,
		}}
		if link.Config.Model == "" {
			link.Config.Model = DefaultModelFor(fb.Provider)
		}
		if fb.APIKeyEncrypted != "" {
			key, err := r.decryptKey(fb.APIKeyEncrypted)
			if err == nil && key != "" {
				link.Config.APIKey = key
			} else {
				link.KeyUnreadable = true
				unreadable = append(unreadable, fb.APIKeyEncrypted)
			}
		}
		link.APIKeyConfigured = link.Config.APIKey != ""
		out.Fallbacks = append(out.Fallbacks, link)
	}
	if len(unreadable) > 0 {
		r.warnUnreadable(strings.Join(unreadable, "\n"))
	}
	return out, keyErr
}

// warnUnreadable logs undecryptable stored keys once per distinct set of
// ciphertexts rather than on every resolve. The ciphertexts themselves are
// never logged.
func (r *Resolver) warnUnreadable(ciphertexts string) {
	r.mu.Lock()
	seen := r.lastUnreadable == ciphertexts
	r.lastUnreadable = ciphertexts
	r.mu.Unlock()
	if seen {
		return
	}
	slog.Default().Warn("stored ai provider api key cannot be decrypted",
		"keys", strings.Count(ciphertexts, "\n")+1,
		"hint", "check CLOUDPAM_OIDC_ENCRYPTION_KEY or save the api key again; CLOUDPAM_LLM_API_KEY is used only when the endpoint and provider kind are not stored in the database")
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
