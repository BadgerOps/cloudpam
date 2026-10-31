package llm

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"cloudpam/internal/domain"
)

type fakeSettingsSource struct {
	doc *domain.AIProviderSettings
	err error
}

func (f *fakeSettingsSource) GetAIProviderSettings(context.Context) (*domain.AIProviderSettings, error) {
	if f.err != nil {
		return nil, f.err
	}
	if f.doc == nil {
		d := domain.DefaultAIProviderSettings()
		return &d, nil
	}
	return f.doc.Clone(), nil
}

// fakeDecrypt "decrypts" values of the form enc(<plaintext>).
func fakeDecrypt(c string) (string, error) {
	if strings.HasPrefix(c, "enc(") && strings.HasSuffix(c, ")") {
		return strings.TrimSuffix(strings.TrimPrefix(c, "enc("), ")"), nil
	}
	return "", errors.New("decryption failed")
}

func ptrF(v float64) *float64 { return &v }

func defaultEnv() Config {
	return Config{Model: DefaultModel, MaxTokens: DefaultMaxTokens, Temperature: DefaultTemperature}
}

func TestResolverPrecedence(t *testing.T) {
	tests := []struct {
		name        string
		env         Config
		doc         *domain.AIProviderSettings
		wantCfg     Config
		wantSources domain.AIProviderSettingSources
		wantKey     bool
	}{
		{
			name:    "nothing configured uses defaults",
			env:     defaultEnv(),
			wantCfg: Config{Model: DefaultModel, MaxTokens: DefaultMaxTokens, Temperature: DefaultTemperature},
			wantSources: domain.AIProviderSettingSources{
				Endpoint: domain.AISettingSourceDefault, Model: domain.AISettingSourceDefault,
				APIKey: domain.AISettingSourceNone, MaxTokens: domain.AISettingSourceDefault,
				Temperature: domain.AISettingSourceDefault,
			},
		},
		{
			name:    "zero env config falls back to built-in defaults",
			env:     Config{},
			wantCfg: Config{Model: DefaultModel, MaxTokens: DefaultMaxTokens, Temperature: 0},
			wantSources: domain.AIProviderSettingSources{
				Endpoint: domain.AISettingSourceDefault, Model: domain.AISettingSourceDefault,
				APIKey: domain.AISettingSourceNone, MaxTokens: domain.AISettingSourceDefault,
				Temperature: domain.AISettingSourceEnvironment,
			},
		},
		{
			name:    "environment beats defaults",
			env:     Config{APIKey: "env-key", Model: "env-model", Endpoint: "http://env/v1", MaxTokens: 100, Temperature: 0.1},
			wantCfg: Config{APIKey: "env-key", Model: "env-model", Endpoint: "http://env/v1", MaxTokens: 100, Temperature: 0.1},
			wantSources: domain.AIProviderSettingSources{
				Endpoint: domain.AISettingSourceEnvironment, Model: domain.AISettingSourceEnvironment,
				APIKey: domain.AISettingSourceEnvironment, MaxTokens: domain.AISettingSourceEnvironment,
				Temperature: domain.AISettingSourceEnvironment,
			},
			wantKey: true,
		},
		{
			name: "database beats environment per field",
			env:  Config{APIKey: "env-key", Model: "env-model", Endpoint: "http://env/v1", MaxTokens: 100, Temperature: 0.1},
			doc: &domain.AIProviderSettings{
				Endpoint: "http://db/v1", Model: "db-model", MaxTokens: 200,
				Temperature: ptrF(0), APIKeyEncrypted: "enc(db-key)",
			},
			wantCfg: Config{APIKey: "db-key", Model: "db-model", Endpoint: "http://db/v1", MaxTokens: 200, Temperature: 0},
			wantSources: domain.AIProviderSettingSources{
				Endpoint: domain.AISettingSourceDatabase, Model: domain.AISettingSourceDatabase,
				APIKey: domain.AISettingSourceDatabase, MaxTokens: domain.AISettingSourceDatabase,
				Temperature: domain.AISettingSourceDatabase,
			},
			wantKey: true,
		},
		{
			name: "mixed: database model only, rest from environment",
			env:  Config{APIKey: "env-key", Model: "env-model", MaxTokens: DefaultMaxTokens, Temperature: DefaultTemperature},
			doc:  &domain.AIProviderSettings{Model: "db-model"},
			wantCfg: Config{
				APIKey: "env-key", Model: "db-model", MaxTokens: DefaultMaxTokens, Temperature: DefaultTemperature,
			},
			wantSources: domain.AIProviderSettingSources{
				Endpoint: domain.AISettingSourceDefault, Model: domain.AISettingSourceDatabase,
				APIKey: domain.AISettingSourceEnvironment, MaxTokens: domain.AISettingSourceDefault,
				Temperature: domain.AISettingSourceDefault,
			},
			wantKey: true,
		},
		// D1: the environment key is bound to the environment or default endpoint.
		{
			name:    "key binding: env endpoint and env key use the env key",
			env:     Config{APIKey: "env-key", Endpoint: "http://env/v1", Model: DefaultModel, MaxTokens: DefaultMaxTokens, Temperature: DefaultTemperature},
			wantCfg: Config{APIKey: "env-key", Endpoint: "http://env/v1", Model: DefaultModel, MaxTokens: DefaultMaxTokens, Temperature: DefaultTemperature},
			wantSources: domain.AIProviderSettingSources{
				Endpoint: domain.AISettingSourceEnvironment, Model: domain.AISettingSourceDefault,
				APIKey: domain.AISettingSourceEnvironment, MaxTokens: domain.AISettingSourceDefault,
				Temperature: domain.AISettingSourceDefault,
			},
			wantKey: true,
		},
		{
			name:    "key binding: database endpoint never receives the env key",
			env:     Config{APIKey: "env-key", Endpoint: "http://env/v1", Model: DefaultModel, MaxTokens: DefaultMaxTokens, Temperature: DefaultTemperature},
			doc:     &domain.AIProviderSettings{Endpoint: "http://attacker/v1"},
			wantCfg: Config{Endpoint: "http://attacker/v1", Model: DefaultModel, MaxTokens: DefaultMaxTokens, Temperature: DefaultTemperature},
			wantSources: domain.AIProviderSettingSources{
				Endpoint: domain.AISettingSourceDatabase, Model: domain.AISettingSourceDefault,
				APIKey: domain.AISettingSourceNone, MaxTokens: domain.AISettingSourceDefault,
				Temperature: domain.AISettingSourceDefault,
			},
			wantKey: false,
		},
		{
			name:    "key binding: database endpoint uses the database key",
			env:     Config{APIKey: "env-key", Model: DefaultModel, MaxTokens: DefaultMaxTokens, Temperature: DefaultTemperature},
			doc:     &domain.AIProviderSettings{Endpoint: "http://db/v1", APIKeyEncrypted: "enc(db-key)"},
			wantCfg: Config{APIKey: "db-key", Endpoint: "http://db/v1", Model: DefaultModel, MaxTokens: DefaultMaxTokens, Temperature: DefaultTemperature},
			wantSources: domain.AIProviderSettingSources{
				Endpoint: domain.AISettingSourceDatabase, Model: domain.AISettingSourceDefault,
				APIKey: domain.AISettingSourceDatabase, MaxTokens: domain.AISettingSourceDefault,
				Temperature: domain.AISettingSourceDefault,
			},
			wantKey: true,
		},
		{
			name:    "key binding: default endpoint and env key use the env key",
			env:     Config{APIKey: "env-key", Model: DefaultModel, MaxTokens: DefaultMaxTokens, Temperature: DefaultTemperature},
			wantCfg: Config{APIKey: "env-key", Model: DefaultModel, MaxTokens: DefaultMaxTokens, Temperature: DefaultTemperature},
			wantSources: domain.AIProviderSettingSources{
				Endpoint: domain.AISettingSourceDefault, Model: domain.AISettingSourceDefault,
				APIKey: domain.AISettingSourceEnvironment, MaxTokens: domain.AISettingSourceDefault,
				Temperature: domain.AISettingSourceDefault,
			},
			wantKey: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewResolver(tt.env, &fakeSettingsSource{doc: tt.doc}, fakeDecrypt)
			got, err := r.Resolve(context.Background())
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}
			if got.Config != tt.wantCfg {
				t.Errorf("Config = %+v, want %+v", got.Config, tt.wantCfg)
			}
			if got.Sources != tt.wantSources {
				t.Errorf("Sources = %+v, want %+v", got.Sources, tt.wantSources)
			}
			if got.APIKeyConfigured != tt.wantKey {
				t.Errorf("APIKeyConfigured = %v, want %v", got.APIKeyConfigured, tt.wantKey)
			}
		})
	}
}

func TestResolverAuthlessEndpointIsAvailable(t *testing.T) {
	r := NewResolver(defaultEnv(), &fakeSettingsSource{doc: &domain.AIProviderSettings{Endpoint: "http://ollama:11434/v1"}}, fakeDecrypt)
	got, err := r.Resolve(context.Background())
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.APIKeyConfigured || got.Config.APIKey != "" {
		t.Fatal("authless endpoint should not carry a key")
	}
	if !NewOpenAIProvider(got.Config).Available() {
		t.Fatal("authless endpoint must be available")
	}
}

func TestResolverNilSourceUsesEnvironment(t *testing.T) {
	got, err := NewResolver(Config{APIKey: "k", Model: "m", MaxTokens: 1, Temperature: 1}, nil, nil).Resolve(context.Background())
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.Config.APIKey != "k" || got.Sources.APIKey != domain.AISettingSourceEnvironment {
		t.Fatalf("nil source should resolve env, got %+v", got)
	}
}

func TestResolverDecryptFailureFallsBackToEnvironment(t *testing.T) {
	doc := &domain.AIProviderSettings{Model: "db-model", APIKeyEncrypted: "garbage"}
	for _, tt := range []struct {
		name       string
		envKey     string
		wantSource string
	}{
		{"env key present", "env-key", domain.AISettingSourceEnvironment},
		{"no env key", "", domain.AISettingSourceNone},
	} {
		t.Run(tt.name, func(t *testing.T) {
			env := defaultEnv()
			env.APIKey = tt.envKey
			got, err := NewResolver(env, &fakeSettingsSource{doc: doc}, fakeDecrypt).Resolve(context.Background())
			if !errors.Is(err, ErrStoredKeyUnreadable) {
				t.Fatalf("err = %v, want ErrStoredKeyUnreadable", err)
			}
			if got.Config.APIKey != tt.envKey || got.Sources.APIKey != tt.wantSource {
				t.Fatalf("APIKey source = %q key set = %v, want %q", got.Sources.APIKey, got.Config.APIKey != "", tt.wantSource)
			}
			if got.Config.Model != "db-model" {
				t.Fatalf("other stored fields must still apply, model = %q", got.Config.Model)
			}
			if strings.Contains(err.Error(), "garbage") {
				t.Fatal("error leaks the ciphertext")
			}
		})
	}
}

func TestResolverStoreErrorIsReturned(t *testing.T) {
	_, err := NewResolver(defaultEnv(), &fakeSettingsSource{err: errors.New("db down")}, fakeDecrypt).Resolve(context.Background())
	if err == nil || errors.Is(err, ErrStoredKeyUnreadable) {
		t.Fatalf("err = %v, want a store error", err)
	}
}

// countingHandler counts warn-level records.
type countingHandler struct {
	mu    sync.Mutex
	warns int
}

func (h *countingHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *countingHandler) Handle(_ context.Context, r slog.Record) error {
	if r.Level == slog.LevelWarn {
		h.mu.Lock()
		h.warns++
		h.mu.Unlock()
	}
	return nil
}
func (h *countingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *countingHandler) WithGroup(string) slog.Handler      { return h }
func (h *countingHandler) count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.warns
}

func TestResolverWarnsOncePerUnreadableCiphertext(t *testing.T) {
	h := &countingHandler{}
	prev := slog.Default()
	slog.SetDefault(slog.New(h))
	t.Cleanup(func() { slog.SetDefault(prev) })

	src := &fakeSettingsSource{doc: &domain.AIProviderSettings{APIKeyEncrypted: "garbage-one"}}
	r := NewResolver(defaultEnv(), src, fakeDecrypt)
	for i := 0; i < 3; i++ {
		if _, err := r.Resolve(context.Background()); !errors.Is(err, ErrStoredKeyUnreadable) {
			t.Fatalf("Resolve: %v", err)
		}
	}
	if got := h.count(); got != 1 {
		t.Fatalf("warnings for one unreadable ciphertext = %d, want 1", got)
	}
	src.doc = &domain.AIProviderSettings{APIKeyEncrypted: "garbage-two"}
	_, _ = r.Resolve(context.Background())
	_, _ = r.Resolve(context.Background())
	if got := h.count(); got != 2 {
		t.Fatalf("warnings after a new unreadable ciphertext = %d, want 2", got)
	}
}
