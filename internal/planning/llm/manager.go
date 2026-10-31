package llm

import (
	"context"
	"errors"
	"sync"
)

// ProviderSource yields the provider for one AI operation. Implementations
// resolve the configuration on every call, so a settings change made on any
// replica is observed by the next operation without a restart.
type ProviderSource interface {
	Provider(ctx context.Context) (Provider, Resolved, error)
}

// Manager resolves the configuration from the settings store on every call
// and caches the last OpenAI provider, rebuilding it when the resolved
// configuration differs. The store stays the source of truth.
type Manager struct {
	resolver *Resolver

	mu       sync.Mutex
	cfg      Config
	provider *OpenAIProvider
}

// NewManager creates a Manager over resolver.
func NewManager(resolver *Resolver) *Manager {
	return &Manager{resolver: resolver}
}

// Provider resolves the current configuration and returns a provider for it.
// When the stored key is unreadable it still returns a provider (built from
// the fallback configuration) together with ErrStoredKeyUnreadable.
func (m *Manager) Provider(ctx context.Context) (Provider, Resolved, error) {
	resolved, err := m.resolver.Resolve(ctx)
	if err != nil && !errors.Is(err, ErrStoredKeyUnreadable) {
		return nil, resolved, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.provider == nil || m.cfg != resolved.Config {
		m.provider = NewOpenAIProvider(resolved.Config)
		m.cfg = resolved.Config
	}
	return m.provider, resolved, err
}

type staticSource struct{ p Provider }

func (s staticSource) Provider(context.Context) (Provider, Resolved, error) {
	return s.p, Resolved{}, nil
}

// StaticProviderSource wraps a fixed provider (tests, or binaries that do not
// read the settings store). A nil provider means AI is unavailable.
func StaticProviderSource(p Provider) ProviderSource {
	return staticSource{p: p}
}
