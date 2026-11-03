package domain

import (
	"fmt"
	"testing"
)

func TestNormalizeAIProviderSettingsFallbacks(t *testing.T) {
	in := &AIProviderSettings{Fallbacks: []AIFallbackProvider{
		{ID: " a ", Provider: " Anthropic ", Endpoint: " http://gw ", Model: " claude ", APIKeyEncrypted: " ct "},
		{ID: "", Provider: "openai"},
		{ID: "no-kind"},
	}}
	got := NormalizeAIProviderSettings(in)
	if len(got.Fallbacks) != 1 {
		t.Fatalf("fallbacks = %+v, want only the addressable entry", got.Fallbacks)
	}
	want := AIFallbackProvider{ID: "a", Provider: "anthropic", Endpoint: "http://gw", Model: "claude", APIKeyEncrypted: "ct"}
	if got.Fallbacks[0] != want {
		t.Fatalf("fallback = %+v, want %+v", got.Fallbacks[0], want)
	}
	if !got.HasStoredValues() || got.State != AIProviderStateConfigured {
		t.Fatalf("a document with only fallbacks counts as stored: %+v", got)
	}

	var many []AIFallbackProvider
	for i := 0; i < MaxAIFallbackProviders+2; i++ {
		many = append(many, AIFallbackProvider{ID: fmt.Sprint(i), Provider: "openai"})
	}
	if got := NormalizeAIProviderSettings(&AIProviderSettings{Fallbacks: many}); len(got.Fallbacks) != MaxAIFallbackProviders {
		t.Fatalf("fallbacks = %d, want the cap %d", len(got.Fallbacks), MaxAIFallbackProviders)
	}
	if got := NormalizeAIProviderSettings(&AIProviderSettings{}); got.Fallbacks != nil {
		t.Fatalf("no fallbacks must stay nil, got %v", got.Fallbacks)
	}
}

func TestAIProviderSettingsCloneCopiesFallbacks(t *testing.T) {
	in := &AIProviderSettings{Fallbacks: []AIFallbackProvider{{ID: "a", Provider: "openai"}}}
	out := in.Clone()
	in.Fallbacks[0].Model = "changed"
	in.Fallbacks = append(in.Fallbacks, AIFallbackProvider{ID: "b", Provider: "openai"})
	if len(out.Fallbacks) != 1 || out.Fallbacks[0].Model != "" {
		t.Fatalf("clone shares the fallback slice: %+v", out.Fallbacks)
	}
}
