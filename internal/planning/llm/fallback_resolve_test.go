package llm

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"cloudpam/internal/domain"
)

func fallbackDoc() *domain.AIProviderSettings {
	return &domain.AIProviderSettings{
		APIKeyEncrypted: "enc(primary)",
		Fallbacks: []domain.AIFallbackProvider{
			{ID: "a", Provider: ProviderAnthropic, Model: "claude-x", APIKeyEncrypted: "enc(k1)"},
			{ID: "b", Provider: ProviderOpenAI, Endpoint: "http://ollama:11434/v1"},
			{ID: "c", Provider: ProviderOpenAI, Endpoint: "http://gateway:8080/v1", Model: "m3", APIKeyEncrypted: "garbage"},
		},
	}
}

func TestResolverResolvesFallbacksWithInheritedLimits(t *testing.T) {
	env := Config{Model: DefaultModel, MaxTokens: 100, Temperature: 0.3}
	got, err := NewResolver(env, &fakeSettingsSource{doc: fallbackDoc()}, fakeDecrypt).Resolve(context.Background())
	if err != nil {
		t.Fatalf("Resolve: an unreadable fallback key must not fail the primary: %v", err)
	}
	if got.Config.APIKey != "primary" || !got.APIKeyConfigured {
		t.Fatalf("primary = %+v", got.Config)
	}
	want := []ResolvedFallback{
		{ID: "a", Config: Config{Provider: ProviderAnthropic, Model: "claude-x", APIKey: "k1", MaxTokens: 100, Temperature: 0.3}, APIKeyConfigured: true},
		{ID: "b", Config: Config{Provider: ProviderOpenAI, Endpoint: "http://ollama:11434/v1", Model: DefaultModel, MaxTokens: 100, Temperature: 0.3}},
		{ID: "c", Config: Config{Provider: ProviderOpenAI, Endpoint: "http://gateway:8080/v1", Model: "m3", MaxTokens: 100, Temperature: 0.3}, KeyUnreadable: true},
	}
	if len(got.Fallbacks) != len(want) {
		t.Fatalf("fallbacks = %+v, want %d entries", got.Fallbacks, len(want))
	}
	for i := range want {
		if got.Fallbacks[i] != want[i] {
			t.Errorf("fallback %d = %+v, want %+v", i, got.Fallbacks[i], want[i])
		}
	}
	if got.Fallbacks[0].Config.Label() != "anthropic claude-x" {
		t.Fatalf("label = %q", got.Fallbacks[0].Config.Label())
	}
}

func TestResolverFallbackLimitsFollowTheStoredPrimary(t *testing.T) {
	doc := fallbackDoc()
	doc.MaxTokens = 777
	doc.Temperature = ptrF(0)
	got, err := NewResolver(defaultEnv(), &fakeSettingsSource{doc: doc}, fakeDecrypt).Resolve(context.Background())
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	for _, fb := range got.Fallbacks {
		if fb.Config.MaxTokens != 777 || fb.Config.Temperature != 0 {
			t.Fatalf("fallback %s limits = %d/%v, want the stored primary's 777/0", fb.ID, fb.Config.MaxTokens, fb.Config.Temperature)
		}
	}
}

func TestResolverWarnsOncePerUnreadableFallbackSet(t *testing.T) {
	h := &countingHandler{}
	prev := slog.Default()
	slog.SetDefault(slog.New(h))
	t.Cleanup(func() { slog.SetDefault(prev) })

	src := &fakeSettingsSource{doc: fallbackDoc()}
	r := NewResolver(defaultEnv(), src, fakeDecrypt)
	for i := 0; i < 3; i++ {
		if _, err := r.Resolve(context.Background()); err != nil {
			t.Fatalf("Resolve: %v", err)
		}
	}
	if got := h.count(); got != 1 {
		t.Fatalf("warnings for one unreadable fallback key = %d, want 1", got)
	}
	// The primary key turning unreadable is a different set and warns again.
	src.doc.APIKeyEncrypted = "garbage-primary"
	if _, err := r.Resolve(context.Background()); !errors.Is(err, ErrStoredKeyUnreadable) {
		t.Fatalf("Resolve: %v", err)
	}
	if got := h.count(); got != 2 {
		t.Fatalf("warnings after the primary key broke too = %d, want 2", got)
	}
}

func TestManagerBuildsChainFromFallbacks(t *testing.T) {
	src := &mutableSource{doc: *fallbackDoc()}
	mgr := NewManager(NewResolver(defaultEnv(), src, fakeDecrypt))

	p1, resolved, err := mgr.Provider(context.Background())
	if err != nil {
		t.Fatalf("Provider: %v", err)
	}
	chain, ok := p1.(*Chain)
	if !ok {
		t.Fatalf("provider = %T, want a *Chain when fallbacks are stored", p1)
	}
	links := Links(chain, "")
	if len(links) != 4 {
		t.Fatalf("links = %d, want the primary plus three fallbacks", len(links))
	}
	if links[0].Label != resolved.Config.Label() || links[1].Label != "anthropic claude-x" || links[1].Provider.Name() != ProviderAnthropic {
		t.Fatalf("links = %+v", links)
	}
	if !links[1].Provider.Available() || links[2].Provider.Available() == false || links[3].Provider.Available() {
		t.Fatalf("availability: keyed anthropic=%v authless openai=%v unreadable=%v", links[1].Provider.Available(), links[2].Provider.Available(), links[3].Provider.Available())
	}

	p2, _, err := mgr.Provider(context.Background())
	if err != nil {
		t.Fatalf("Provider: %v", err)
	}
	if p2 != p1 {
		t.Fatal("unchanged configuration must reuse the chain")
	}

	doc := *fallbackDoc()
	doc.Fallbacks[0].Model = "claude-y"
	src.set(doc)
	p3, _, err := mgr.Provider(context.Background())
	if err != nil {
		t.Fatalf("Provider: %v", err)
	}
	if p3 == p1 {
		t.Fatal("a changed fallback must rebuild the chain")
	}

	doc.Fallbacks = nil
	src.set(doc)
	p4, _, err := mgr.Provider(context.Background())
	if err != nil {
		t.Fatalf("Provider: %v", err)
	}
	if _, isChain := p4.(*Chain); isChain {
		t.Fatal("without fallbacks the primary is returned on its own")
	}
}

func TestResolverDefaultsModelPerProviderKind(t *testing.T) {
	t.Setenv("CLOUDPAM_LLM_PROVIDER", "")
	t.Setenv("CLOUDPAM_LLM_MODEL", "")
	doc := &domain.AIProviderSettings{
		Provider:  ProviderAnthropic,
		Fallbacks: []domain.AIFallbackProvider{{ID: "a", Provider: ProviderAnthropic}, {ID: "b", Provider: ProviderOpenAI}},
	}
	got, err := NewResolver(ConfigFromEnv(), &fakeSettingsSource{doc: doc}, fakeDecrypt).Resolve(context.Background())
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got.Config.Model != DefaultAnthropicModel || got.Sources.Model != domain.AISettingSourceDefault {
		t.Errorf("primary model = %q (%s), want %q from the default", got.Config.Model, got.Sources.Model, DefaultAnthropicModel)
	}
	if got.Fallbacks[0].Config.Model != DefaultAnthropicModel || got.Fallbacks[1].Config.Model != DefaultModel {
		t.Errorf("fallback models = %q, %q", got.Fallbacks[0].Config.Model, got.Fallbacks[1].Config.Model)
	}
}
