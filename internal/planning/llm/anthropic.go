package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"go.opentelemetry.io/otel/attribute"
)

// anthropicVersion is the Messages API version every request pins. This
// version rejects sampling parameters, so the configured temperature is not
// sent; the model samples with its default.
const anthropicVersion = "2026-10-21"

// AnthropicProvider implements the Provider interface over the Anthropic
// Messages API, which is not OpenAI-compatible: the system prompt is a
// top-level field, the stream is typed events and the key travels in x-api-key.
type AnthropicProvider struct {
	cfg    Config
	client *http.Client
}

// NewAnthropicProvider creates a provider for the Messages API. The endpoint,
// when set, is the base URL of a compatible gateway (the default is
// https://api.anthropic.com); "/v1/messages" is appended.
func NewAnthropicProvider(cfg Config) *AnthropicProvider {
	return &AnthropicProvider{cfg: cfg, client: newHTTPClient()}
}

func (p *AnthropicProvider) Name() string { return ProviderAnthropic }

// Available reports whether a key is configured. The Messages API has no
// authless mode.
func (p *AnthropicProvider) Available() bool { return strings.TrimSpace(p.cfg.APIKey) != "" }

func (p *AnthropicProvider) baseURL() string {
	if endpoint := strings.TrimSpace(p.cfg.Endpoint); endpoint != "" {
		return strings.TrimRight(endpoint, "/")
	}
	return "https://api.anthropic.com"
}

func (p *AnthropicProvider) headers(stream bool) map[string]string {
	h := map[string]string{
		"x-api-key":         strings.TrimSpace(p.cfg.APIKey),
		"anthropic-version": anthropicVersion,
	}
	if stream {
		h["Accept"] = "text/event-stream"
	}
	return h
}

// anthropicRequest is the Messages API request body.
type anthropicRequest struct {
	Model     string             `json:"model"`
	System    string             `json:"system,omitempty"`
	Messages  []anthropicMessage `json:"messages"`
	MaxTokens int64              `json:"max_tokens"`
	Stream    bool               `json:"stream"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// anthropicResponse is the non-streaming reply. Only text blocks are read.
type anthropicResponse struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	StopReason string `json:"stop_reason"`
	Usage      struct {
		InputTokens  int64 `json:"input_tokens"`
		OutputTokens int64 `json:"output_tokens"`
	} `json:"usage"`
}

// anthropicStreamEvent is the union of the stream event payloads that matter:
// content_block_delta carries text, message_delta the stop reason,
// message_stop the end, and error a mid-stream failure.
type anthropicStreamEvent struct {
	Type  string `json:"type"`
	Delta struct {
		Type       string `json:"type"`
		Text       string `json:"text"`
		StopReason string `json:"stop_reason"`
	} `json:"delta"`
}

// request maps the neutral messages onto the Messages API. System messages
// become the top-level system prompt, and empty messages are dropped because
// the API rejects empty content.
func (p *AnthropicProvider) request(messages []Message, opts Options, stream bool) anthropicRequest {
	maxTokens := opts.MaxTokens
	if maxTokens == 0 {
		maxTokens = p.cfg.MaxTokens
	}
	if maxTokens == 0 {
		maxTokens = DefaultMaxTokens
	}
	var system []string
	msgs := make([]anthropicMessage, 0, len(messages))
	for _, m := range messages {
		if strings.TrimSpace(m.Content) == "" {
			continue
		}
		if m.Role == "system" {
			system = append(system, m.Content)
			continue
		}
		msgs = append(msgs, anthropicMessage(m))
	}
	return anthropicRequest{
		Model:     p.cfg.Model,
		System:    strings.Join(system, "\n\n"),
		Messages:  msgs,
		MaxTokens: maxTokens,
		Stream:    stream,
	}
}

func (p *AnthropicProvider) Complete(ctx context.Context, messages []Message, opts Options) (out *Response, err error) {
	ctx, span := startCompletionSpan(ctx, p.Name(), p.cfg.Model, p.baseURL(), false)
	defer func() { _ = endSpanWithError(span, err) }()

	resp, err := postJSON(ctx, p.client, p.baseURL()+"/v1/messages", p.headers(false), p.request(messages, opts, false))
	if err != nil {
		return nil, recordStatus(span, err)
	}
	defer func() { _ = resp.Body.Close() }()
	span.SetAttributes(attribute.Int("http.response.status_code", resp.StatusCode))

	respBody, err := io.ReadAll(newLimitedBody(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	var reply anthropicResponse
	if err := json.Unmarshal(respBody, &reply); err != nil {
		return nil, fmt.Errorf("%w: unmarshal response: %w", ErrInvalidResponse, err)
	}
	var text strings.Builder
	found := false
	for _, block := range reply.Content {
		if block.Type == "text" {
			text.WriteString(block.Text)
			found = true
		}
	}
	if !found {
		return nil, fmt.Errorf("%w: no text content in response", ErrInvalidResponse)
	}
	span.SetAttributes(
		attribute.Int64("llm.usage.prompt_tokens", reply.Usage.InputTokens),
		attribute.Int64("llm.usage.completion_tokens", reply.Usage.OutputTokens),
	)
	return &Response{
		Content:      text.String(),
		FinishReason: reply.StopReason,
		PromptTokens: reply.Usage.InputTokens,
		OutputTokens: reply.Usage.OutputTokens,
	}, nil
}

func (p *AnthropicProvider) StreamComplete(ctx context.Context, messages []Message, opts Options) (events <-chan StreamEvent, err error) {
	ctx, span := startCompletionSpan(ctx, p.Name(), p.cfg.Model, p.baseURL(), true)
	streaming := false
	defer func() {
		if !streaming {
			_ = endSpanWithError(span, err)
		}
	}()

	resp, err := postJSON(ctx, p.client, p.baseURL()+"/v1/messages", p.headers(true), p.request(messages, opts, true))
	if err != nil {
		return nil, recordStatus(span, err)
	}
	span.SetAttributes(attribute.Int("http.response.status_code", resp.StatusCode))

	ch := make(chan StreamEvent, 64)
	streaming = true
	go func() {
		defer close(ch)
		defer span.End()
		defer func() { _ = resp.Body.Close() }()
		readAnthropicStream(newLimitedBody(resp.Body, maxStreamBytes), ch)
	}()
	return ch, nil
}

// readAnthropicStream forwards one StreamEvent per text delta and exactly one
// terminal event. The stop reason arrives in message_delta and the stream
// ends with message_stop; an error event ends it with Err set, and the
// provider's message is not kept because it can echo request details.
func readAnthropicStream(r io.Reader, ch chan<- StreamEvent) {
	done := false
	stopReason := ""
	err := readSSE(r, func(data string) bool {
		var evt anthropicStreamEvent
		if err := json.Unmarshal([]byte(data), &evt); err != nil {
			return true
		}
		switch evt.Type {
		case "content_block_delta":
			if evt.Delta.Type == "text_delta" && evt.Delta.Text != "" {
				ch <- StreamEvent{Delta: evt.Delta.Text}
			}
		case "message_delta":
			if evt.Delta.StopReason != "" {
				stopReason = evt.Delta.StopReason
			}
		case "message_stop":
			ch <- StreamEvent{Done: true, FinishReason: stopReason}
			done = true
		case "error":
			ch <- StreamEvent{Done: true, Err: fmt.Errorf("%w: provider reported an error during the stream", ErrInvalidResponse)}
			done = true
		}
		return !done
	})
	if done {
		return
	}
	ch <- StreamEvent{Done: true, FinishReason: stopReason, Err: err}
}
