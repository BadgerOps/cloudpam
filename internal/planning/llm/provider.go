package llm

import (
	"context"
	"os"
	"strconv"
)

// Message represents a chat message sent to or received from the LLM.
type Message struct {
	Role    string `json:"role"` // "system", "user", "assistant"
	Content string `json:"content"`
}

// Options configures a single LLM completion request.
type Options struct {
	MaxTokens   int64
	Temperature float64
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

// Config holds LLM provider configuration.
type Config struct {
	APIKey      string
	Model       string
	Endpoint    string // base URL override (for Ollama, vLLM, Azure)
	MaxTokens   int64
	Temperature float64
}

// ConfigFromEnv reads LLM configuration from environment variables.
func ConfigFromEnv() Config {
	maxTokens := int64(4096)
	if v := os.Getenv("CLOUDPAM_LLM_MAX_TOKENS"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			maxTokens = n
		}
	}

	temperature := 0.7
	if v := os.Getenv("CLOUDPAM_LLM_TEMPERATURE"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			temperature = f
		}
	}

	model := os.Getenv("CLOUDPAM_LLM_MODEL")
	if model == "" {
		model = "gpt-4o"
	}

	return Config{
		APIKey:      os.Getenv("CLOUDPAM_LLM_API_KEY"),
		Model:       model,
		Endpoint:    os.Getenv("CLOUDPAM_LLM_ENDPOINT"),
		MaxTokens:   maxTokens,
		Temperature: temperature,
	}
}
