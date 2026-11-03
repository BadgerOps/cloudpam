package llm

import (
	"context"
	"errors"
	"slices"
	"sync"
)

// ProviderSource yields the provider for one AI operation. Implementations
// resolve the configuration on every call, so a settings change made on any
// replica is observed by the next operation without a restart.
type ProviderSource interface {
	Provider(ctx context.Context) (Provider, Resolved, error)
}

// Manager resolves the configuration from the settings store on every call
// and caches the last provider chain, rebuilding it when any resolved
// configuration differs. The store stays the source of truth.
type Manager struct {
	resolver *Resolver

	mu       sync.Mutex
	cfgs     []Config // primary first, then the fallbacks in order
	provider Provider
}

// NewManager creates a Manager over resolver.
func NewManager(resolver *Resolver) *Manager {
	return &Manager{resolver: resolver}
}

// Provider resolves the current configuration and returns a provider for it:
// the primary alone, or a Chain over the primary and the stored fallbacks.
// When the stored key is unreadable it still returns a provider (built from
// the fallback configuration) together with ErrStoredKeyUnreadable.
func (m *Manager) Provider(ctx context.Context) (Provider, Resolved, error) {
	resolved, err := m.resolver.Resolve(ctx)
	if err != nil && !errors.Is(err, ErrStoredKeyUnreadable) {
		return nil, resolved, err
	}
	cfgs := make([]Config, 0, 1+len(resolved.Fallbacks))
	cfgs = append(cfgs, resolved.Config)
	for _, fb := range resolved.Fallbacks {
		cfgs = append(cfgs, fb.Config)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.provider == nil || !slices.Equal(m.cfgs, cfgs) {
		links := make([]Link, len(cfgs))
		for i, cfg := range cfgs {
			links[i] = Link{Provider: NewProvider(cfg), Label: cfg.Label()}
		}
		m.provider = NewChain(links)
		m.cfgs = cfgs
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
