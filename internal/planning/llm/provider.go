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
}

// Send forwards evt on ch and reports whether it was delivered. It tries the
// channel before looking at ctx, so a terminal event still reaches a consumer
// that is reading after the context ended; only when the channel is full does
// it give up on ctx.Done, which frees a producer from a consumer that stopped
// reading before the terminal event.
func Send(ctx context.Context, ch chan<- StreamEvent, evt StreamEvent) bool {
	select {
	case ch <- evt:
		return true
	default:
	}
	select {
	case ch <- evt:
		return true
	case <-ctx.Done():
		return false
	}
}

// Provider abstracts an LLM backend (OpenAI, Ollama, vLLM, etc.).
type Provider interface {
	// Complete sends messages and returns a full response.
	Complete(ctx context.Context, messages []Message, opts Options) (*Response, error)

	// StreamComplete sends messages and returns a channel of streaming events:
	// one per content chunk, then exactly one terminal event (Done, with Err
	// set when the stream failed), after which the channel is closed. The one
	// exception is a consumer that stops reading before the terminal event:
	// once ctx ends, the producer gives up on the full channel and closes it
	// with no terminal event. A consumer that finds the channel closed without
	// a Done event must treat the stream as ErrStreamInterrupted, not as
	// complete.
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

// Built-in defaults used when neither the settings store nor the environment
// sets a value.
const (
	DefaultModel       = "gpt-4o"
	DefaultMaxTokens   = int64(4096)
	DefaultTemperature = 0.7
)

// ConfigFromEnv reads LLM configuration from environment variables.
func ConfigFromEnv() Config {
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
		model = DefaultModel
	}

	return Config{
		APIKey:      os.Getenv("CLOUDPAM_LLM_API_KEY"),
		Model:       model,
		Endpoint:    os.Getenv("CLOUDPAM_LLM_ENDPOINT"),
		MaxTokens:   maxTokens,
		Temperature: temperature,
	}
}
