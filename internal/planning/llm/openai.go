package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"cloudpam/internal/observability"
)

// Caps on how much of one provider response body is read: maxResponseBytes
// for a JSON reply, maxStreamBytes for a whole SSE stream, whose framing makes
// it several times larger than the text it carries. Variables so tests can
// shrink them.
var (
	maxResponseBytes int64 = 8 << 20
	maxStreamBytes   int64 = 64 << 20
)

// limitedBody reads at most limit bytes and then fails with
// ErrResponseTooLarge, so an oversized reply is reported rather than truncated.
type limitedBody struct {
	r     io.Reader
	limit int64
	read  int64
	err   error // sticky overflow error
}

func newLimitedBody(r io.Reader, limit int64) *limitedBody {
	// Read one byte past the limit to tell "exactly at the limit" from "over".
	return &limitedBody{r: io.LimitReader(r, limit+1), limit: limit}
}

func (l *limitedBody) Read(p []byte) (int, error) {
	if l.err != nil {
		return 0, l.err
	}
	n, err := l.r.Read(p)
	l.read += int64(n)
	if l.read > l.limit {
		l.err = fmt.Errorf("%w (%d bytes)", ErrResponseTooLarge, l.limit)
		return max(n-int(l.read-l.limit), 0), l.err
	}
	return n, err
}

// startCompletionSpan opens a client span around a chat completion call. When
// tracing is disabled the shared tracer is the OpenTelemetry no-op, so this is
// an interface call against a network round trip.
func startCompletionSpan(ctx context.Context, provider, model, server string, stream bool) (context.Context, trace.Span) {
	return observability.Tracer().Start(ctx, "llm.chat.completions",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String("llm.provider", provider),
			attribute.String("llm.model", model),
			attribute.String("server.address", server),
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

// recordStatus puts a non-200 status on the span and returns err unchanged.
func recordStatus(span trace.Span, err error) error {
	var statusErr *StatusError
	if errors.As(err, &statusErr) {
		span.SetAttributes(attribute.Int("http.response.status_code", statusErr.Status))
	}
	return err
}

// OpenAIProvider implements the Provider interface using the OpenAI-compatible
// chat completions API. Works with OpenAI, Ollama, vLLM, Azure, and any
// endpoint that speaks the same protocol.
type OpenAIProvider struct {
	cfg    Config
	client *http.Client
}

// NewOpenAIProvider creates a provider for an OpenAI-compatible API. Its HTTP
// client refuses connections and redirects to blocked addresses (see
// IsBlockedAddr) and follows at most three redirects.
func NewOpenAIProvider(cfg Config) *OpenAIProvider {
	return &OpenAIProvider{
		cfg:    cfg,
		client: newHTTPClient(),
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

// headers returns the request headers for one call. The key is sent only when
// configured, so authless endpoints (Ollama, vLLM) get no Authorization header.
func (p *OpenAIProvider) headers(stream bool) map[string]string {
	h := map[string]string{}
	if stream {
		h["Accept"] = "text/event-stream"
	}
	if apiKey := strings.TrimSpace(p.cfg.APIKey); apiKey != "" {
		h["Authorization"] = "Bearer " + apiKey
	}
	return h
}

func (p *OpenAIProvider) request(messages []Message, opts Options, stream bool) openaiRequest {
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
	return openaiRequest{
		Model:       p.cfg.Model,
		Messages:    oaiMsgs,
		MaxTokens:   maxTokens,
		Temperature: temp,
		Stream:      stream,
	}
}

func (p *OpenAIProvider) Complete(ctx context.Context, messages []Message, opts Options) (out *Response, err error) {
	ctx, span := startCompletionSpan(ctx, p.Name(), p.cfg.Model, p.baseURL(), false)
	defer func() { _ = endSpanWithError(span, err) }()

	resp, err := postJSON(ctx, p.client, p.baseURL()+"/chat/completions", p.headers(false), p.request(messages, opts, false))
	if err != nil {
		return nil, recordStatus(span, err)
	}
	defer func() { _ = resp.Body.Close() }()
	span.SetAttributes(attribute.Int("http.response.status_code", resp.StatusCode))

	respBody, err := io.ReadAll(newLimitedBody(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	var oaiResp openaiResponse
	if err := json.Unmarshal(respBody, &oaiResp); err != nil {
		return nil, fmt.Errorf("%w: unmarshal response: %w", ErrInvalidResponse, err)
	}
	if oaiResp.Error != nil {
		// The provider's message can echo request details, so it is not kept.
		return nil, fmt.Errorf("%w: provider reported an error", ErrInvalidResponse)
	}
	if len(oaiResp.Choices) == 0 {
		return nil, fmt.Errorf("%w: no choices in response", ErrInvalidResponse)
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
	ctx, span := startCompletionSpan(ctx, p.Name(), p.cfg.Model, p.baseURL(), true)
	// The span stays open for the lifetime of the stream, so it is only ended
	// here when the request never got that far.
	streaming := false
	defer func() {
		if !streaming {
			_ = endSpanWithError(span, err)
		}
	}()

	resp, err := postJSON(ctx, p.client, p.baseURL()+"/chat/completions", p.headers(true), p.request(messages, opts, true))
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
		readOpenAIStream(newLimitedBody(resp.Body, maxStreamBytes), ch)
	}()

	return ch, nil
}

// readOpenAIStream forwards one StreamEvent per content chunk and exactly one
// terminal event (Done, with Err set on a read failure), then returns so the
// caller can close the body and the channel.
func readOpenAIStream(r io.Reader, ch chan<- StreamEvent) {
	done := false
	err := readSSE(r, func(data string) bool {
		if data == "[DONE]" {
			ch <- StreamEvent{Done: true}
			done = true
			return false
		}
		var chunk openaiStreamChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil || len(chunk.Choices) == 0 {
			return true
		}
		evt := StreamEvent{Delta: chunk.Choices[0].Delta.Content}
		// Some OpenAI-compatible proxies send "finish_reason":"" on
		// intermediate chunks instead of null; only a non-empty reason
		// ends the completion.
		if fr := chunk.Choices[0].FinishReason; fr != nil && *fr != "" {
			evt.FinishReason = *fr
			evt.Done = true
		}
		ch <- evt
		// Done is terminal: consumers stop at the first done event and never
		// drain, so anything streamed after finish_reason (usage chunks, the
		// [DONE] sentinel) would block this goroutine on a full channel until
		// the context ended.
		done = evt.Done
		return !done
	})
	if done {
		return
	}
	ch <- StreamEvent{Done: true, Err: err}
}
