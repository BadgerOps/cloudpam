package llm

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// anthropicFake records the last request and answers with the configured
// non-streaming reply or stream body.
type anthropicFake struct {
	mu      sync.Mutex
	last    *http.Request
	lastRaw map[string]any
	status  int
	reply   string
	stream  string
}

func newAnthropicFake(t *testing.T, f *anthropicFake) *httptest.Server {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.last = r
		f.lastRaw = map[string]any{}
		_ = json.Unmarshal(body, &f.lastRaw)
		f.mu.Unlock()
		if f.status != 0 && f.status != http.StatusOK {
			w.WriteHeader(f.status)
			_, _ = io.WriteString(w, `{"type":"error","error":{"type":"overloaded_error","message":"secret detail"}}`)
			return
		}
		if stream, _ := f.lastRaw["stream"].(bool); stream {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, f.stream)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, f.reply)
	}))
	t.Cleanup(ts.Close)
	return ts
}

func (f *anthropicFake) request() (*http.Request, map[string]any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.last, f.lastRaw
}

const anthropicReply = `{"id":"msg_1","type":"message","role":"assistant","content":[{"type":"text","text":"Hello"},{"type":"text","text":" there"}],"stop_reason":"end_turn","usage":{"input_tokens":12,"output_tokens":3}}`

const anthropicStream = "event: message_start\n" +
	`data: {"type":"message_start","message":{"id":"msg_1","usage":{"input_tokens":12,"output_tokens":1}}}` + "\n\n" +
	"event: content_block_start\n" +
	`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}` + "\n\n" +
	"event: ping\n" +
	`data: {"type":"ping"}` + "\n\n" +
	"event: content_block_delta\n" +
	`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"Hel"}}` + "\n\n" +
	"event: content_block_delta\n" +
	`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"lo"}}` + "\n\n" +
	"event: content_block_stop\n" +
	`data: {"type":"content_block_stop","index":0}` + "\n\n" +
	"event: message_delta\n" +
	`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":2}}` + "\n\n" +
	"event: message_stop\n" +
	`data: {"type":"message_stop"}` + "\n\n"

var anthropicMessages = []Message{
	{Role: "system", Content: "You plan networks."},
	{Role: "user", Content: "Plan a /16"},
	{Role: "assistant", Content: ""},
	{Role: "assistant", Content: "Sure."},
	{Role: "user", Content: "Go on"},
}

func TestAnthropicCompleteSendsMessagesAPIRequest(t *testing.T) {
	fake := &anthropicFake{reply: anthropicReply}
	ts := newAnthropicFake(t, fake)
	p := NewAnthropicProvider(Config{APIKey: " sk-ant-test ", Model: "claude-opus-5-5", Endpoint: ts.URL + "/", MaxTokens: 512, Temperature: 0.2})

	out, err := p.Complete(context.Background(), anthropicMessages, Options{})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if out.Content != "Hello there" || out.FinishReason != "end_turn" || out.PromptTokens != 12 || out.OutputTokens != 3 {
		t.Fatalf("response = %+v", *out)
	}

	req, body := fake.request()
	if req.URL.Path != "/v1/messages" {
		t.Fatalf("path = %q, want /v1/messages", req.URL.Path)
	}
	if got := req.Header.Get("x-api-key"); got != "sk-ant-test" {
		t.Fatalf("x-api-key = %q", got)
	}
	if req.Header.Get("Authorization") != "" {
		t.Fatal("Authorization header must not be sent to the Messages API")
	}
	if got := req.Header.Get("anthropic-version"); got != anthropicVersion {
		t.Fatalf("anthropic-version = %q", got)
	}
	if body["model"] != "claude-opus-5-5" || body["system"] != "You plan networks." || body["max_tokens"] != float64(512) {
		t.Fatalf("body = %v", body)
	}
	if _, sent := body["temperature"]; sent {
		t.Fatal("temperature must not be sent: the pinned api version rejects it")
	}
	msgs, _ := body["messages"].([]any)
	if len(msgs) != 3 {
		t.Fatalf("messages = %v, want the three non-empty non-system messages", msgs)
	}
	first, _ := msgs[0].(map[string]any)
	if first["role"] != "user" || first["content"] != "Plan a /16" {
		t.Fatalf("first message = %v", first)
	}
}

func TestAnthropicCompleteUsesOptionsAndDefaultMaxTokens(t *testing.T) {
	fake := &anthropicFake{reply: anthropicReply}
	ts := newAnthropicFake(t, fake)
	p := NewAnthropicProvider(Config{APIKey: "k", Model: "m", Endpoint: ts.URL})

	if _, err := p.Complete(context.Background(), anthropicMessages, Options{}); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if _, body := fake.request(); body["max_tokens"] != float64(DefaultMaxTokens) {
		t.Fatalf("max_tokens = %v, want the default %d when nothing is configured", body["max_tokens"], DefaultMaxTokens)
	}
	if _, err := p.Complete(context.Background(), anthropicMessages, Options{MaxTokens: 8}); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if _, body := fake.request(); body["max_tokens"] != float64(8) {
		t.Fatalf("max_tokens = %v, want the per-call option", body["max_tokens"])
	}
}

func TestAnthropicStreamCompleteForwardsTextDeltasAndStopReason(t *testing.T) {
	fake := &anthropicFake{stream: anthropicStream}
	ts := newAnthropicFake(t, fake)
	p := NewAnthropicProvider(Config{APIKey: "k", Model: "m", Endpoint: ts.URL})

	events, err := p.StreamComplete(context.Background(), anthropicMessages, Options{})
	if err != nil {
		t.Fatalf("StreamComplete: %v", err)
	}
	var text strings.Builder
	var last StreamEvent
	for evt := range events {
		text.WriteString(evt.Delta)
		last = evt
		if evt.Done {
			break
		}
	}
	if text.String() != "Hello" {
		t.Fatalf("streamed text = %q", text.String())
	}
	if !last.Done || last.Err != nil || last.FinishReason != "end_turn" {
		t.Fatalf("terminal event = %+v", last)
	}
	req, body := fake.request()
	if req.Header.Get("Accept") != "text/event-stream" || body["stream"] != true {
		t.Fatalf("stream request not marked as streaming: accept=%q body=%v", req.Header.Get("Accept"), body)
	}
}

func TestAnthropicStreamErrorEventEndsWithError(t *testing.T) {
	stream := "event: content_block_delta\n" +
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"partial"}}` + "\n\n" +
		"event: error\n" +
		`data: {"type":"error","error":{"type":"overloaded_error","message":"secret detail"}}` + "\n\n"
	ts := newAnthropicFake(t, &anthropicFake{stream: stream})
	p := NewAnthropicProvider(Config{APIKey: "k", Model: "m", Endpoint: ts.URL})

	events, err := p.StreamComplete(context.Background(), anthropicMessages, Options{})
	if err != nil {
		t.Fatalf("StreamComplete: %v", err)
	}
	var text string
	var streamErr error
	for evt := range events {
		text += evt.Delta
		if evt.Done {
			streamErr = evt.Err
			break
		}
	}
	if text != "partial" {
		t.Fatalf("text before the error = %q", text)
	}
	if !errors.Is(streamErr, ErrInvalidResponse) {
		t.Fatalf("stream err = %v, want ErrInvalidResponse", streamErr)
	}
	if strings.Contains(streamErr.Error(), "secret detail") {
		t.Fatal("the provider's error message must not be kept")
	}
}

func TestAnthropicStreamWithoutMessageStopEndsCleanly(t *testing.T) {
	stream := "event: message_delta\n" +
		`data: {"type":"message_delta","delta":{"stop_reason":"max_tokens"}}` + "\n\n"
	ts := newAnthropicFake(t, &anthropicFake{stream: stream})
	p := NewAnthropicProvider(Config{APIKey: "k", Model: "m", Endpoint: ts.URL})

	events, err := p.StreamComplete(context.Background(), anthropicMessages, Options{})
	if err != nil {
		t.Fatalf("StreamComplete: %v", err)
	}
	last := <-events
	if !last.Done || last.Err != nil || last.FinishReason != "max_tokens" {
		t.Fatalf("terminal event = %+v, want a clean done with the stop reason", last)
	}
}

func TestAnthropicNon200IsStatusErrorWithoutBody(t *testing.T) {
	ts := newAnthropicFake(t, &anthropicFake{status: 529})
	p := NewAnthropicProvider(Config{APIKey: "k", Model: "m", Endpoint: ts.URL})

	_, err := p.Complete(context.Background(), anthropicMessages, Options{})
	var statusErr *StatusError
	if !errors.As(err, &statusErr) || statusErr.Status != 529 {
		t.Fatalf("Complete err = %v, want StatusError 529", err)
	}
	if strings.Contains(err.Error(), "secret detail") {
		t.Fatal("the response body must not reach the error")
	}
	_, err = p.StreamComplete(context.Background(), anthropicMessages, Options{})
	if !errors.As(err, &statusErr) || statusErr.Status != 529 {
		t.Fatalf("StreamComplete err = %v, want StatusError 529", err)
	}
}

func TestAnthropicCompleteRejectsReplyWithoutText(t *testing.T) {
	ts := newAnthropicFake(t, &anthropicFake{reply: `{"content":[{"type":"tool_use"}],"stop_reason":"tool_use"}`})
	p := NewAnthropicProvider(Config{APIKey: "k", Model: "m", Endpoint: ts.URL})
	if _, err := p.Complete(context.Background(), anthropicMessages, Options{}); !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("err = %v, want ErrInvalidResponse", err)
	}
}

func TestAnthropicAvailableRequiresKey(t *testing.T) {
	if NewAnthropicProvider(Config{Endpoint: "http://gateway"}).Available() {
		t.Fatal("an endpoint alone must not make the Messages API available: it has no authless mode")
	}
	if !NewAnthropicProvider(Config{APIKey: "k"}).Available() {
		t.Fatal("a key must make the provider available")
	}
	if p := NewAnthropicProvider(Config{APIKey: "k"}); p.baseURL() != "https://api.anthropic.com" {
		t.Fatalf("default base url = %q", p.baseURL())
	}
}
