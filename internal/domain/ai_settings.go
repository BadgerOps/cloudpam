package domain

import (
	"math"
	"strings"
	"time"
)

// AI provider lifecycle states reported by GET /api/v1/settings/ai.
const (
	AIProviderStateNotConfigured = "not_configured"
	AIProviderStateConfigured    = "configured"
	AIProviderStateVerified      = "verified"
	AIProviderStateFailing       = "failing"
)

// Where an effective AI provider setting came from.
const (
	AISettingSourceDatabase    = "database"
	AISettingSourceEnvironment = "environment"
	AISettingSourceDefault     = "default"
	AISettingSourceNone        = "none"
)

// Bounds for stored AI provider values.
const (
	MaxAIProviderMaxTokens   int64   = 1_000_000
	MaxAIProviderTemperature float64 = 2
	MaxAIProviderModelLength         = 200
)

// AIProviderSettings is the stored document under settings key "ai_provider".
// Zero values mean "not set", so the environment or built-in default applies.
// It holds the API key ciphertext and is never returned by the API.
type AIProviderSettings struct {
	Endpoint        string     `json:"endpoint"`
	Model           string     `json:"model"`
	MaxTokens       int64      `json:"max_tokens"`            // 0 = not set
	Temperature     *float64   `json:"temperature,omitempty"` // nil = not set
	APIKeyEncrypted string     `json:"api_key_encrypted,omitempty"`
	State           string     `json:"state"` // configured | verified | failing; empty when nothing stored
	LastTestedAt    *time.Time `json:"last_tested_at,omitempty"`
	LastTestError   string     `json:"last_test_error,omitempty"` // sanitized
	LastTestModel   string     `json:"last_test_model,omitempty"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

// DefaultAIProviderSettings returns an empty document: nothing stored.
func DefaultAIProviderSettings() AIProviderSettings {
	return AIProviderSettings{}
}

// HasStoredValues reports whether any provider field is set in the document.
func (s *AIProviderSettings) HasStoredValues() bool {
	return s.Endpoint != "" || s.Model != "" || s.MaxTokens != 0 || s.Temperature != nil || s.APIKeyEncrypted != ""
}

// NormalizeAIProviderSettings trims strings, clamps numeric fields into their
// valid ranges and maps unknown states, so old or partial documents are safe.
func NormalizeAIProviderSettings(s *AIProviderSettings) *AIProviderSettings {
	if s == nil {
		d := DefaultAIProviderSettings()
		return &d
	}
	s.Endpoint = strings.TrimSpace(s.Endpoint)
	s.Model = strings.TrimSpace(s.Model)
	s.APIKeyEncrypted = strings.TrimSpace(s.APIKeyEncrypted)
	s.LastTestError = strings.TrimSpace(s.LastTestError)
	s.LastTestModel = strings.TrimSpace(s.LastTestModel)
	if s.MaxTokens < 0 {
		s.MaxTokens = 0
	}
	if s.MaxTokens > MaxAIProviderMaxTokens {
		s.MaxTokens = MaxAIProviderMaxTokens
	}
	if s.Temperature != nil {
		t := *s.Temperature
		switch {
		case math.IsNaN(t):
			s.Temperature = nil
		case t < 0:
			t = 0
			s.Temperature = &t
		case t > MaxAIProviderTemperature:
			t = MaxAIProviderTemperature
			s.Temperature = &t
		}
	}
	switch s.State {
	case AIProviderStateConfigured, AIProviderStateVerified, AIProviderStateFailing:
	default:
		if s.HasStoredValues() {
			s.State = AIProviderStateConfigured
		} else {
			s.State = ""
		}
	}
	return s
}

// AIProviderSettingSources reports where each effective value came from.
type AIProviderSettingSources struct {
	Endpoint    string `json:"endpoint"`
	Model       string `json:"model"`
	APIKey      string `json:"api_key"`
	MaxTokens   string `json:"max_tokens"`
	Temperature string `json:"temperature"`
}

// AIProviderSettingsResponse is the API representation of the effective AI
// provider configuration. It never carries the API key.
type AIProviderSettingsResponse struct {
	Endpoint               string                   `json:"endpoint"`
	Model                  string                   `json:"model"`
	MaxTokens              int64                    `json:"max_tokens"`
	Temperature            float64                  `json:"temperature"`
	APIKeyConfigured       bool                     `json:"api_key_configured"`
	APIKeyStorageAvailable bool                     `json:"api_key_storage_available"`
	Sources                AIProviderSettingSources `json:"sources"`
	State                  string                   `json:"state"`
	LastTestedAt           *time.Time               `json:"last_tested_at,omitempty"`
	LastTestError          string                   `json:"last_test_error,omitempty"`
	LastTestModel          string                   `json:"last_test_model,omitempty"`
}

// UpdateAIProviderSettingsRequest is the PATCH body. Omitted fields are kept.
type UpdateAIProviderSettingsRequest struct {
	Endpoint         *string  `json:"endpoint,omitempty"`    // "" clears the stored value
	Model            *string  `json:"model,omitempty"`       // "" clears
	MaxTokens        *int64   `json:"max_tokens,omitempty"`  // 0 clears
	Temperature      *float64 `json:"temperature,omitempty"` // use ClearTemperature to clear
	ClearTemperature bool     `json:"clear_temperature,omitempty"`
	APIKey           *string  `json:"api_key,omitempty"` // omitted = keep; non-empty = replace; "" = invalid
	ClearAPIKey      bool     `json:"clear_api_key,omitempty"`
}

// AIProviderTestResponse is the result of POST /api/v1/settings/ai/test.
type AIProviderTestResponse struct {
	OK                 bool      `json:"ok"`
	State              string    `json:"state"`
	Endpoint           string    `json:"endpoint"`
	Model              string    `json:"model"`
	LatencyMS          int64     `json:"latency_ms"`
	StreamingSupported bool      `json:"streaming_supported"`
	Error              string    `json:"error,omitempty"` // sanitized
	TestedAt           time.Time `json:"tested_at"`
}

// Clone deep-copies the document so the pointer fields are not shared.
func (s *AIProviderSettings) Clone() *AIProviderSettings {
	if s == nil {
		return nil
	}
	out := *s
	if s.Temperature != nil {
		t := *s.Temperature
		out.Temperature = &t
	}
	if s.LastTestedAt != nil {
		at := *s.LastTestedAt
		out.LastTestedAt = &at
	}
	return &out
}
