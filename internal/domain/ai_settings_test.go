package domain

import (
	"math"
	"testing"
)

func f64(v float64) *float64 { return &v }

func TestDefaultAIProviderSettingsIsUnset(t *testing.T) {
	d := DefaultAIProviderSettings()
	if d.Endpoint != "" || d.Model != "" || d.MaxTokens != 0 || d.Temperature != nil || d.APIKeyEncrypted != "" || d.State != "" {
		t.Fatalf("defaults should store nothing, got %+v", d)
	}
}

func TestNormalizeAIProviderSettingsNilReturnsDefaults(t *testing.T) {
	got := NormalizeAIProviderSettings(nil)
	if got == nil || got.State != "" || got.Model != "" {
		t.Fatalf("NormalizeAIProviderSettings(nil) = %+v, want defaults", got)
	}
}

func TestNormalizeAIProviderSettings(t *testing.T) {
	tests := []struct {
		name  string
		in    AIProviderSettings
		check func(t *testing.T, got *AIProviderSettings)
	}{
		{
			name: "trims strings",
			in:   AIProviderSettings{Endpoint: "  http://llm:11434/v1  ", Model: " llama3 ", APIKeyEncrypted: " abcd "},
			check: func(t *testing.T, got *AIProviderSettings) {
				if got.Endpoint != "http://llm:11434/v1" || got.Model != "llama3" || got.APIKeyEncrypted != "abcd" {
					t.Fatalf("not trimmed: %+v", got)
				}
			},
		},
		{
			name: "clamps max tokens",
			in:   AIProviderSettings{MaxTokens: 5_000_000},
			check: func(t *testing.T, got *AIProviderSettings) {
				if got.MaxTokens != MaxAIProviderMaxTokens {
					t.Fatalf("MaxTokens = %d, want %d", got.MaxTokens, MaxAIProviderMaxTokens)
				}
			},
		},
		{
			name: "negative max tokens means unset",
			in:   AIProviderSettings{MaxTokens: -3},
			check: func(t *testing.T, got *AIProviderSettings) {
				if got.MaxTokens != 0 {
					t.Fatalf("MaxTokens = %d, want 0", got.MaxTokens)
				}
			},
		},
		{
			name: "clamps temperature high and low",
			in:   AIProviderSettings{Temperature: f64(7)},
			check: func(t *testing.T, got *AIProviderSettings) {
				if got.Temperature == nil || *got.Temperature != MaxAIProviderTemperature {
					t.Fatalf("Temperature = %v, want %v", got.Temperature, MaxAIProviderTemperature)
				}
			},
		},
		{
			name: "negative temperature clamps to zero",
			in:   AIProviderSettings{Temperature: f64(-1)},
			check: func(t *testing.T, got *AIProviderSettings) {
				if got.Temperature == nil || *got.Temperature != 0 {
					t.Fatalf("Temperature = %v, want 0", got.Temperature)
				}
			},
		},
		{
			name: "nan temperature is dropped",
			in:   AIProviderSettings{Temperature: f64(math.NaN())},
			check: func(t *testing.T, got *AIProviderSettings) {
				if got.Temperature != nil {
					t.Fatalf("Temperature = %v, want nil", *got.Temperature)
				}
			},
		},
		{
			name: "unknown state maps to configured when something is stored",
			in:   AIProviderSettings{Model: "gpt-4o-mini", State: "bogus"},
			check: func(t *testing.T, got *AIProviderSettings) {
				if got.State != AIProviderStateConfigured {
					t.Fatalf("State = %q, want configured", got.State)
				}
			},
		},
		{
			name: "empty state maps to configured when something is stored",
			in:   AIProviderSettings{APIKeyEncrypted: "abcd"},
			check: func(t *testing.T, got *AIProviderSettings) {
				if got.State != AIProviderStateConfigured {
					t.Fatalf("State = %q, want configured", got.State)
				}
			},
		},
		{
			name: "known state is kept",
			in:   AIProviderSettings{Model: "m", State: AIProviderStateFailing},
			check: func(t *testing.T, got *AIProviderSettings) {
				if got.State != AIProviderStateFailing {
					t.Fatalf("State = %q, want failing", got.State)
				}
			},
		},
		{
			name: "test result for an env-only config is kept",
			in:   AIProviderSettings{State: AIProviderStateVerified},
			check: func(t *testing.T, got *AIProviderSettings) {
				if got.State != AIProviderStateVerified {
					t.Fatalf("State = %q, want verified", got.State)
				}
			},
		},
		{
			name: "unknown state with nothing stored is empty",
			in:   AIProviderSettings{State: "not_configured"},
			check: func(t *testing.T, got *AIProviderSettings) {
				if got.State != "" {
					t.Fatalf("State = %q, want empty", got.State)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := tt.in
			tt.check(t, NormalizeAIProviderSettings(&in))
		})
	}
}
