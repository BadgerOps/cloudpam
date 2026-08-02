package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"cloudpam/internal/observability"
)

// startCompletionSpan opens a client span around a chat completion call. When
// tracing is disabled the shared tracer is the OpenTelemetry no-op, so this is
// an interface call against a network round trip.
func (p *OpenAIProvider) startCompletionSpan(ctx context.Context, stream bool) (context.Context, trace.Span) {
	return observability.Tracer().Start(ctx, "llm.chat.completions",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String("llm.provider", p.Name()),
			attribute.String("llm.model", p.cfg.Model),
			attribute.String("server.address", p.baseURL()),
			attribute.Bool("llm.stream", stream),
		),
	)
}

// endSpanWithError records err on the span and closes it.
func endSpanWithError(span trace.Span, err error) error {
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}
	span.End()
	return err
}

// OpenAIProvider implements the Provider interface using the OpenAI-compatible
// chat completions API. Works with OpenAI, Ollama, vLLM, Azure, and any
// endpoint that speaks the same protocol.
type OpenAIProvider struct {
	cfg    Config
	client *http.Client
}

// NewOpenAIProvider creates a provider for an OpenAI-compatible API.
func NewOpenAIProvider(cfg Config) *OpenAIProvider {
	return &OpenAIProvider{
		cfg:    cfg,
		client: &http.Client{},
	}
}

func (p *OpenAIProvider) Name() string { return "openai" }
func (p *OpenAIProvider) Available() bool {
	return strings.TrimSpace(p.cfg.APIKey) != "" || strings.TrimSpace(p.cfg.Endpoint) != ""
}

func (p *OpenAIProvider) baseURL() string {
	if endpoint := strings.TrimSpace(p.cfg.Endpoint); endpoint != "" {
		return strings.TrimRight(endpoint, "/")
	}
	return "https://api.openai.com/v1"
}

// openaiRequest is the request body for the chat completions API.
type openaiRequest struct {
	Model       string          `json:"model"`
	Messages    []openaiMessage `json:"messages"`
	MaxTokens   int64           `json:"max_tokens,omitempty"`
	Temperature float64         `json:"temperature"`
	Stream      bool            `json:"stream"`
}

type openaiMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// openaiResponse is the response body for non-streaming completions.
type openaiResponse struct {
	Choices []struct {
		Message      openaiMessage `json:"message"`
		FinishReason string        `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int64 `json:"prompt_tokens"`
		CompletionTokens int64 `json:"completion_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// openaiStreamChunk is a single SSE chunk from the streaming API.
type openaiStreamChunk struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
}

func (p *OpenAIProvider) Complete(ctx context.Context, messages []Message, opts Options) (out *Response, err error) {
	ctx, span := p.startCompletionSpan(ctx, false)
	defer func() { _ = endSpanWithError(span, err) }()

	maxTokens := opts.MaxTokens
	if maxTokens == 0 {
		maxTokens = p.cfg.MaxTokens
	}
	temp := p.cfg.Temperature
	if opts.Temperature != nil {
		temp = *opts.Temperature
	}

	oaiMsgs := make([]openaiMessage, len(messages))
	for i, m := range messages {
		oaiMsgs[i] = openaiMessage(m)
	}

	body := openaiRequest{
		Model:       p.cfg.Model,
		Messages:    oaiMsgs,
		MaxTokens:   maxTokens,
		Temperature: temp,
		Stream:      false,
	}

	bodyJSON, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL()+"/chat/completions", strings.NewReader(string(bodyJSON)))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if apiKey := strings.TrimSpace(p.cfg.APIKey); apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	span.SetAttributes(attribute.Int("http.response.status_code", resp.StatusCode))

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("api error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var oaiResp openaiResponse
	if err := json.Unmarshal(respBody, &oaiResp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}
	if oaiResp.Error != nil {
		return nil, fmt.Errorf("api error: %s", oaiResp.Error.Message)
	}
	if len(oaiResp.Choices) == 0 {
		return nil, fmt.Errorf("no choices in response")
	}

	span.SetAttributes(
		attribute.Int64("llm.usage.prompt_tokens", oaiResp.Usage.PromptTokens),
		attribute.Int64("llm.usage.completion_tokens", oaiResp.Usage.CompletionTokens),
	)

	return &Response{
		Content:      oaiResp.Choices[0].Message.Content,
		FinishReason: oaiResp.Choices[0].FinishReason,
		PromptTokens: oaiResp.Usage.PromptTokens,
		OutputTokens: oaiResp.Usage.CompletionTokens,
	}, nil
}

func (p *OpenAIProvider) StreamComplete(ctx context.Context, messages []Message, opts Options) (events <-chan StreamEvent, err error) {
	ctx, span := p.startCompletionSpan(ctx, true)
	// The span stays open for the lifetime of the stream, so it is only ended
	// here when the request never got that far.
	streaming := false
	defer func() {
		if !streaming {
			_ = endSpanWithError(span, err)
		}
	}()

	maxTokens := opts.MaxTokens
	if maxTokens == 0 {
		maxTokens = p.cfg.MaxTokens
	}
	temp := p.cfg.Temperature
	if opts.Temperature != nil {
		temp = *opts.Temperature
	}

	oaiMsgs := make([]openaiMessage, len(messages))
	for i, m := range messages {
		oaiMsgs[i] = openaiMessage(m)
	}

	body := openaiRequest{
		Model:       p.cfg.Model,
		Messages:    oaiMsgs,
		MaxTokens:   maxTokens,
		Temperature: temp,
		Stream:      true,
	}

	bodyJSON, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL()+"/chat/completions", strings.NewReader(string(bodyJSON)))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	if apiKey := strings.TrimSpace(p.cfg.APIKey); apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request: %w", err)
	}

	span.SetAttributes(attribute.Int("http.response.status_code", resp.StatusCode))
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		return nil, fmt.Errorf("api error (status %d): %s", resp.StatusCode, string(respBody))
	}

	ch := make(chan StreamEvent, 64)
	streaming = true
	go func() {
		defer close(ch)
		defer span.End()
		defer func() { _ = resp.Body.Close() }()
		p.readSSEStream(resp.Body, ch)
	}()

	return ch, nil
}

func (p *OpenAIProvider) readSSEStream(r io.Reader, ch chan<- StreamEvent) {
	buf := make([]byte, 4096)
	var lineBuf strings.Builder

	for {
		n, err := r.Read(buf)
		if n > 0 {
			lineBuf.Write(buf[:n])
			for {
				text := lineBuf.String()
				idx := strings.Index(text, "\n")
				if idx == -1 {
					break
				}
				line := text[:idx]
				lineBuf.Reset()
				lineBuf.WriteString(text[idx+1:])

				line = strings.TrimSpace(line)
				if line == "" {
					continue
				}
				if !strings.HasPrefix(line, "data: ") {
					continue
				}
				data := strings.TrimPrefix(line, "data: ")
				if data == "[DONE]" {
					ch <- StreamEvent{Done: true}
					return
				}

				var chunk openaiStreamChunk
				if err := json.Unmarshal([]byte(data), &chunk); err != nil {
					continue
				}
				if len(chunk.Choices) == 0 {
					continue
				}

				evt := StreamEvent{
					Delta: chunk.Choices[0].Delta.Content,
				}
				if chunk.Choices[0].FinishReason != nil {
					evt.FinishReason = *chunk.Choices[0].FinishReason
					evt.Done = true
				}
				ch <- evt
			}
		}
		if err != nil {
			ch <- StreamEvent{Done: true}
			return
		}
	}
}
