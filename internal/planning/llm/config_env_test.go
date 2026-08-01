package llm

import "testing"

// TestConfigFromEnvNumericFallbacks pins the behaviour that a malformed or
// out-of-range numeric override is ignored in favour of the documented default,
// rather than propagating a zero value into the provider.
func TestConfigFromEnvNumericFallbacks(t *testing.T) {
	tests := []struct {
		name            string
		maxTokens       string
		temperature     string
		model           string
		wantMaxTokens   int64
		wantTemperature float64
		wantModel       string
	}{
		{
			name:            "defaults when unset",
			wantMaxTokens:   4096,
			wantTemperature: 0.7,
			wantModel:       "gpt-4o",
		},
		{
			name:            "valid overrides applied",
			maxTokens:       "512",
			temperature:     "0.1",
			model:           "llama3",
			wantMaxTokens:   512,
			wantTemperature: 0.1,
			wantModel:       "llama3",
		},
		{
			name:            "non numeric max tokens ignored",
			maxTokens:       "lots",
			wantMaxTokens:   4096,
			wantTemperature: 0.7,
			wantModel:       "gpt-4o",
		},
		{
			name:            "zero max tokens ignored",
			maxTokens:       "0",
			wantMaxTokens:   4096,
			wantTemperature: 0.7,
			wantModel:       "gpt-4o",
		},
		{
			name:            "negative max tokens ignored",
			maxTokens:       "-10",
			wantMaxTokens:   4096,
			wantTemperature: 0.7,
			wantModel:       "gpt-4o",
		},
		{
			name:            "non numeric temperature ignored",
			temperature:     "warm",
			wantMaxTokens:   4096,
			wantTemperature: 0.7,
			wantModel:       "gpt-4o",
		},
		{
			name:            "zero temperature is honoured",
			temperature:     "0",
			wantMaxTokens:   4096,
			wantTemperature: 0,
			wantModel:       "gpt-4o",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("CLOUDPAM_LLM_MAX_TOKENS", tc.maxTokens)
			t.Setenv("CLOUDPAM_LLM_TEMPERATURE", tc.temperature)
			t.Setenv("CLOUDPAM_LLM_MODEL", tc.model)
			t.Setenv("CLOUDPAM_LLM_API_KEY", "sk-test")
			t.Setenv("CLOUDPAM_LLM_ENDPOINT", "http://localhost:11434/v1")

			cfg := ConfigFromEnv()
			if cfg.MaxTokens != tc.wantMaxTokens {
				t.Errorf("MaxTokens = %d, want %d", cfg.MaxTokens, tc.wantMaxTokens)
			}
			if cfg.Temperature != tc.wantTemperature {
				t.Errorf("Temperature = %v, want %v", cfg.Temperature, tc.wantTemperature)
			}
			if cfg.Model != tc.wantModel {
				t.Errorf("Model = %q, want %q", cfg.Model, tc.wantModel)
			}
			if cfg.APIKey != "sk-test" {
				t.Errorf("APIKey = %q, want sk-test", cfg.APIKey)
			}
			if cfg.Endpoint != "http://localhost:11434/v1" {
				t.Errorf("Endpoint = %q, want the configured endpoint", cfg.Endpoint)
			}
		})
	}
}
