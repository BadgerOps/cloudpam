package llm

import (
	"context"
	"log/slog"
	"os"
	"slices"
	"strconv"
	"strings"
)

// Message represents a chat message sent to or received from the LLM.
type Message struct {
	Role    string `json:"role"` // "system", "user", "assistant"
	Content string `json:"content"`
}

// Options configures a single LLM completion request.
// Unset fields fall back to the provider's configured defaults.
type Options struct {
	MaxTokens int64
	// Temperature is a pointer so that an explicit 0 (fully deterministic
	// sampling) is distinguishable from "not specified".
	Temperature *float64
}

// Response is the result of a non-streaming completion.
type Response struct {
	Content      string
	FinishReason string
	PromptTokens int64
	OutputTokens int64
}

// StreamEvent is a single chunk from a streaming completion.
type StreamEvent struct {
	Delta        string // incremental text content
	Done         bool   // true when the stream is finished
	FinishReason string
	// Err is set on the final event when the stream ended on a read error
	// rather than a clean end of stream.
	Err error
	// Fallback is set on the first event of a stream that a later provider
	// in a Chain answered after an earlier one failed. The event carries no
	// text.
	Fallback *FallbackNotice
}

// Provider abstracts an LLM backend (OpenAI, Ollama, vLLM, etc.).
type Provider interface {
	// Complete sends messages and returns a full response.
	Complete(ctx context.Context, messages []Message, opts Options) (*Response, error)

	// StreamComplete sends messages and returns a channel of streaming events.
	StreamComplete(ctx context.Context, messages []Message, opts Options) (<-chan StreamEvent, error)

	// Name returns the provider name (e.g. "openai").
	Name() string

	// Available returns true if the provider is configured and ready.
	Available() bool
}

// Provider kinds. The kind selects the wire protocol and the default host;
// the endpoint, when set, overrides the host only.
const (
	ProviderOpenAI    = "openai"
	ProviderAnthropic = "anthropic"
)

// ProviderKinds lists the supported kinds in display order.
var ProviderKinds = []string{ProviderOpenAI, ProviderAnthropic}

// ValidProviderKind reports whether kind names a supported protocol.
func ValidProviderKind(kind string) bool {
	return slices.Contains(ProviderKinds, kind)
}

// NewProvider builds the provider for cfg.Provider. The kind is validated at
// the boundaries (environment and PATCH), so an unknown value here is treated
// as the OpenAI-compatible default rather than refused.
func NewProvider(cfg Config) Provider {
	if cfg.Provider == ProviderAnthropic {
		return NewAnthropicProvider(cfg)
	}
	return NewOpenAIProvider(cfg)
}

// Config holds LLM provider configuration.
type Config struct {
	Provider    string // ProviderOpenAI (default) or ProviderAnthropic
	APIKey      string
	Model       string
	Endpoint    string // base URL override (for Ollama, vLLM, Azure, gateways)
	MaxTokens   int64
	Temperature float64
}

// Built-in defaults used when neither the settings store nor the environment
// sets a value.
const (
	DefaultProvider    = ProviderOpenAI
	DefaultModel       = "gpt-4o"
	DefaultMaxTokens   = int64(4096)
	DefaultTemperature = 0.7

	// DefaultAnthropicModel is the model an Anthropic provider uses when
	// none is set; DefaultModel is an OpenAI model the Messages API rejects.
	DefaultAnthropicModel = "claude-opus-5-5"
)

// DefaultModelFor returns the built-in model for a provider kind.
func DefaultModelFor(kind string) string {
	if kind == ProviderAnthropic {
		return DefaultAnthropicModel
	}
	return DefaultModel
}

// ConfigFromEnv reads LLM configuration from environment variables. An
// unsupported CLOUDPAM_LLM_PROVIDER is logged and ignored, like a malformed
// numeric value.
func ConfigFromEnv() Config {
	provider := strings.ToLower(strings.TrimSpace(os.Getenv("CLOUDPAM_LLM_PROVIDER")))
	switch {
	case provider == "":
		provider = DefaultProvider
	case !ValidProviderKind(provider):
		slog.Default().Warn("ignoring unsupported CLOUDPAM_LLM_PROVIDER",
			"value", provider, "supported", strings.Join(ProviderKinds, ", "))
		provider = DefaultProvider
	}

	maxTokens := DefaultMaxTokens
	if v := os.Getenv("CLOUDPAM_LLM_MAX_TOKENS"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			maxTokens = n
		}
	}

	temperature := DefaultTemperature
	if v := os.Getenv("CLOUDPAM_LLM_TEMPERATURE"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			temperature = f
		}
	}

	model := os.Getenv("CLOUDPAM_LLM_MODEL")
	if model == "" {
		model = DefaultModelFor(provider)
	}

	return Config{
		Provider:    provider,
		APIKey:      os.Getenv("CLOUDPAM_LLM_API_KEY"),
		Model:       model,
		Endpoint:    os.Getenv("CLOUDPAM_LLM_ENDPOINT"),
		MaxTokens:   maxTokens,
		Temperature: temperature,
	}
}
