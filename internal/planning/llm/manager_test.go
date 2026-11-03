package llm

import (
	"context"
	"sync"
	"testing"

	"cloudpam/internal/domain"
)

// mutableSource lets a test change the stored document between calls.
type mutableSource struct {
	mu  sync.Mutex
	doc domain.AIProviderSettings
}

func (m *mutableSource) GetAIProviderSettings(context.Context) (*domain.AIProviderSettings, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.doc.Clone(), nil
}

func (m *mutableSource) set(doc domain.AIProviderSettings) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.doc = doc
}

func TestManagerReusesProviderWhenConfigUnchanged(t *testing.T) {
	src := &mutableSource{doc: domain.AIProviderSettings{Endpoint: "http://a/v1", Model: "m1"}}
	mgr := NewManager(NewResolver(defaultEnv(), src, fakeDecrypt))

	p1, r1, err := mgr.Provider(context.Background())
	if err != nil {
		t.Fatalf("Provider: %v", err)
	}
	p2, _, err := mgr.Provider(context.Background())
	if err != nil {
		t.Fatalf("Provider: %v", err)
	}
	if p1 != p2 {
		t.Fatal("expected the cached provider to be reused")
	}
	if r1.Config.Model != "m1" {
		t.Fatalf("model = %q, want m1", r1.Config.Model)
	}
}

func TestManagerSwitchesProtocolWhenStoredKindChanges(t *testing.T) {
	src := &mutableSource{doc: domain.AIProviderSettings{APIKeyEncrypted: "enc(k)"}}
	mgr := NewManager(NewResolver(defaultEnv(), src, fakeDecrypt))

	p1, _, err := mgr.Provider(context.Background())
	if err != nil {
		t.Fatalf("Provider: %v", err)
	}
	if p1.Name() != ProviderOpenAI {
		t.Fatalf("default provider = %q, want %q", p1.Name(), ProviderOpenAI)
	}
	src.set(domain.AIProviderSettings{Provider: ProviderAnthropic, APIKeyEncrypted: "enc(k)"})
	p2, r2, err := mgr.Provider(context.Background())
	if err != nil {
		t.Fatalf("Provider: %v", err)
	}
	if p2.Name() != ProviderAnthropic || !p2.Available() {
		t.Fatalf("provider after kind change = %q available=%v, want an available %q", p2.Name(), p2.Available(), ProviderAnthropic)
	}
	if r2.Sources.Provider != domain.AISettingSourceDatabase {
		t.Fatalf("provider source = %q, want database", r2.Sources.Provider)
	}
}

func TestManagerRebuildsProviderWhenStoredConfigChanges(t *testing.T) {
	src := &mutableSource{doc: domain.AIProviderSettings{Endpoint: "http://a/v1", Model: "m1"}}
	mgr := NewManager(NewResolver(defaultEnv(), src, fakeDecrypt))

	p1, _, err := mgr.Provider(context.Background())
	if err != nil {
		t.Fatalf("Provider: %v", err)
	}
	src.set(domain.AIProviderSettings{Endpoint: "http://a/v1", Model: "m2"})
	p2, r2, err := mgr.Provider(context.Background())
	if err != nil {
		t.Fatalf("Provider: %v", err)
	}
	if p1 == p2 {
		t.Fatal("expected a new provider after the stored model changed")
	}
	if r2.Config.Model != "m2" || p2.(*OpenAIProvider).cfg.Model != "m2" {
		t.Fatalf("new provider model = %q, want m2", p2.(*OpenAIProvider).cfg.Model)
	}
}

func TestManagerConcurrentAccess(t *testing.T) {
	src := &mutableSource{doc: domain.AIProviderSettings{Endpoint: "http://a/v1", Model: "m1"}}
	mgr := NewManager(NewResolver(defaultEnv(), src, fakeDecrypt))
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if i%4 == 0 {
				src.set(domain.AIProviderSettings{Endpoint: "http://a/v1", Model: "m" + string(rune('a'+i))})
			}
			p, _, err := mgr.Provider(context.Background())
			if err != nil || p == nil || !p.Available() {
				t.Errorf("Provider: p=%v err=%v", p, err)
			}
		}(i)
	}
	wg.Wait()
}

func TestStaticProviderSource(t *testing.T) {
	p := NewOpenAIProvider(Config{APIKey: "k"})
	got, _, err := StaticProviderSource(p).Provider(context.Background())
	if err != nil || got != p {
		t.Fatalf("StaticProviderSource returned %v, %v", got, err)
	}
	none, _, err := StaticProviderSource(nil).Provider(context.Background())
	if err != nil || none != nil {
		t.Fatalf("StaticProviderSource(nil) returned %v, %v", none, err)
	}
}
