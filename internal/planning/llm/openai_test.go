package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestLimitedBodyOverflowErrorIsSticky(t *testing.T) {
	l := &limitedBody{r: strings.NewReader(strings.Repeat("x", 20)), limit: 10}
	buf := make([]byte, 6)
	var firstErr error
	total := 0
	for i := 0; i < 5 && firstErr == nil; i++ {
		n, err := l.Read(buf)
		if n < 0 {
			t.Fatalf("read %d returned negative n=%d", i, n)
		}
		total += n
		firstErr = err
	}
	if !errors.Is(firstErr, ErrInvalidResponse) {
		t.Fatalf("first overflow error = %v, want ErrInvalidResponse", firstErr)
	}
	if total > 10 {
		t.Fatalf("returned %d bytes, want at most the 10-byte limit", total)
	}
	for i := 0; i < 2; i++ {
		n, err := l.Read(buf)
		if n != 0 {
			t.Fatalf("read after overflow returned n=%d, want 0", n)
		}
		if err != firstErr {
			t.Fatalf("read after overflow returned %v, want the same error %v", err, firstErr)
		}
	}
}

// oversizedOpenAI answers every request with about 4 KiB, as JSON or as SSE.
func oversizedOpenAI(t *testing.T) *httptest.Server {
	t.Helper()
	pad := strings.Repeat("x", 4096)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req openaiRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req.Stream {
			w.Header().Set("Content-Type", "text/event-stream")
			for i := 0; i < 8; i++ {
				_, _ = fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":%q}}]}\n\n", pad[:512])
			}
			_, _ = io.WriteString(w, "data: [DONE]\n\n")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"choices":[{"message":{"role":"assistant","content":%q},"finish_reason":"stop"}]}`, pad)
	}))
	t.Cleanup(ts.Close)
	return ts
}

// streamErr drains events and returns the error the stream ended with.
func streamErr(t *testing.T, provider *OpenAIProvider) error {
	t.Helper()
	events, err := provider.StreamComplete(context.Background(), []Message{{Role: "user", Content: "hi"}}, Options{})
	if err != nil {
		t.Fatalf("StreamComplete: %v", err)
	}
	var out error
	for evt := range events {
		if evt.Err != nil {
			out = evt.Err
		}
		if evt.Done {
			break
		}
	}
	return out
}

func TestOpenAIProviderResponseAndStreamCapsAreSeparate(t *testing.T) {
	ts := oversizedOpenAI(t)
	provider := NewOpenAIProvider(Config{Model: "m", Endpoint: ts.URL + "/v1"})
	msgs := []Message{{Role: "user", Content: "hi"}}

	prevResp, prevStream := maxResponseBytes, maxStreamBytes
	t.Cleanup(func() { maxResponseBytes, maxStreamBytes = prevResp, prevStream })

	// Only the stream cap shrunk: the JSON reply still fits, the stream overflows.
	maxResponseBytes, maxStreamBytes = prevResp, 1024
	if _, err := provider.Complete(context.Background(), msgs, Options{}); err != nil {
		t.Fatalf("Complete under the response cap = %v, want nil", err)
	}
	err := streamErr(t, provider)
	if !errors.Is(err, ErrResponseTooLarge) || !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("stream err = %v, want ErrResponseTooLarge (and ErrInvalidResponse)", err)
	}
	if got := DiagnoseError(err, time.Minute); got != "provider response exceeded the size limit" {
		t.Fatalf("DiagnoseError = %q", got)
	}

	// Only the response cap shrunk: the JSON reply overflows, the stream completes.
	maxResponseBytes, maxStreamBytes = 1024, prevStream
	if _, err := provider.Complete(context.Background(), msgs, Options{}); !errors.Is(err, ErrResponseTooLarge) {
		t.Fatalf("Complete err = %v, want ErrResponseTooLarge", err)
	}
	if err := streamErr(t, provider); err != nil {
		t.Fatalf("stream under the stream cap ended with %v, want nil", err)
	}
}

func TestOpenAIProviderRejectsOversizedResponses(t *testing.T) {
	prev, prevStream := maxResponseBytes, maxStreamBytes
	maxResponseBytes, maxStreamBytes = 1024, 1024
	t.Cleanup(func() { maxResponseBytes, maxStreamBytes = prev, prevStream })

	ts := oversizedOpenAI(t)
	provider := NewOpenAIProvider(Config{Model: "m", Endpoint: ts.URL + "/v1"})
	msgs := []Message{{Role: "user", Content: "hi"}}

	if _, err := provider.Complete(context.Background(), msgs, Options{}); !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("Complete err = %v, want ErrInvalidResponse", err)
	}
	if err := streamErr(t, provider); !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("stream err = %v, want ErrInvalidResponse", err)
	}
}

// sseStub is an OpenAI-compatible streaming endpoint. It writes each entry of
// data as one SSE event. With hold set it then keeps the connection open until
// the client drops it, which closes disconnected, or the test ends; otherwise
// it ends the response there. conns counts the connections the client opened.
type sseStub struct {
	URL          string
	disconnected chan struct{}
	dropOnce     sync.Once
	conns        atomic.Int32
}

func newSSEStub(t *testing.T, hold bool, data ...string) *sseStub {
	t.Helper()
	s := &sseStub{disconnected: make(chan struct{})}
	stop := make(chan struct{})
	ts := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, d := range data {
			_, _ = fmt.Fprintf(w, "data: %s\n\n", d)
		}
		w.(http.Flusher).Flush()
		if !hold {
			return
		}
		select {
		case <-r.Context().Done():
			s.dropOnce.Do(func() { close(s.disconnected) })
		case <-stop:
		}
	}))
	ts.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			s.conns.Add(1)
		}
	}
	ts.Start()
	t.Cleanup(ts.Close)
	t.Cleanup(func() { close(stop) })
	s.URL = ts.URL + "/v1"
	return s
}

// chunk is one content delta as the chat completions API streams it.
func chunk(content, finishReason string) string {
	fr := "null"
	if finishReason != "" {
		fr = strconv.Quote(finishReason)
	}
	return fmt.Sprintf(`{"choices":[{"delta":{"content":%q},"finish_reason":%s}]}`, content, fr)
}

func TestOpenAIProviderComplete(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if auth := r.Header.Get("Authorization"); auth != "Bearer test-key" {
			t.Errorf("unexpected auth header: %s", auth)
		}

		var req openaiRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.Model != "gpt-4o" {
			t.Errorf("expected model gpt-4o, got %s", req.Model)
		}
		if len(req.Messages) != 2 {
			t.Fatalf("expected 2 messages, got %d", len(req.Messages))
		}
		if req.Messages[0].Role != "system" {
			t.Errorf("expected system role, got %s", req.Messages[0].Role)
		}

		w.Header().Set("Content-Type", "application/json")
		resp := openaiResponse{
			Choices: []struct {
				Message      openaiMessage `json:"message"`
				FinishReason string        `json:"finish_reason"`
			}{
				{
					Message:      openaiMessage{Role: "assistant", Content: "Hello from test!"},
					FinishReason: "stop",
				},
			},
			Usage: struct {
				PromptTokens     int64 `json:"prompt_tokens"`
				CompletionTokens int64 `json:"completion_tokens"`
			}{
				PromptTokens:     10,
				CompletionTokens: 5,
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	provider := NewOpenAIProvider(Config{
		APIKey:      "test-key",
		Model:       "gpt-4o",
		Endpoint:    ts.URL + "/v1",
		MaxTokens:   1024,
		Temperature: 0.5,
	})

	if !provider.Available() {
		t.Fatal("expected provider to be available")
	}
	if provider.Name() != "openai" {
		t.Errorf("expected name openai, got %s", provider.Name())
	}

	resp, err := provider.Complete(context.Background(), []Message{
		{Role: "system", Content: "You are a helpful assistant."},
		{Role: "user", Content: "Hello"},
	}, Options{})
	if err != nil {
		t.Fatalf("complete: %v", err)
	}

	if resp.Content != "Hello from test!" {
		t.Errorf("expected 'Hello from test!', got %q", resp.Content)
	}
	if resp.FinishReason != "stop" {
		t.Errorf("expected finish_reason 'stop', got %q", resp.FinishReason)
	}
	if resp.PromptTokens != 10 {
		t.Errorf("expected 10 prompt tokens, got %d", resp.PromptTokens)
	}
}

func TestOpenAIProviderCompleteWithoutAPIKeyOmitsAuthorization(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if auth := r.Header.Get("Authorization"); auth != "" {
			t.Errorf("expected no auth header, got %q", auth)
		}

		w.Header().Set("Content-Type", "application/json")
		resp := openaiResponse{
			Choices: []struct {
				Message      openaiMessage `json:"message"`
				FinishReason string        `json:"finish_reason"`
			}{
				{
					Message:      openaiMessage{Role: "assistant", Content: "Hello authless!"},
					FinishReason: "stop",
				},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	provider := NewOpenAIProvider(Config{
		APIKey:   " \t",
		Model:    "local-model",
		Endpoint: " " + ts.URL + "/v1/ ",
	})

	if !provider.Available() {
		t.Fatal("expected provider to be available with custom endpoint and no API key")
	}

	resp, err := provider.Complete(context.Background(), []Message{
		{Role: "user", Content: "Hello"},
	}, Options{})
	if err != nil {
		t.Fatalf("complete: %v", err)
	}

	if resp.Content != "Hello authless!" {
		t.Errorf("expected authless response, got %q", resp.Content)
	}
}

func TestOpenAIProviderStreamComplete(t *testing.T) {
	stub := newSSEStub(t, false, chunk("Hello", ""), chunk(" from", ""), chunk(" stream", ""), chunk("!", ""), "[DONE]")

	provider := NewOpenAIProvider(Config{
		APIKey:      "test-key",
		Model:       "gpt-4o",
		Endpoint:    stub.URL,
		MaxTokens:   1024,
		Temperature: 0.5,
	})

	ch, err := provider.StreamComplete(context.Background(), []Message{
		{Role: "user", Content: "Hello"},
	}, Options{})
	if err != nil {
		t.Fatalf("stream complete: %v", err)
	}

	var fullContent string
	for evt := range ch {
		fullContent += evt.Delta
		if evt.Done {
			break
		}
	}

	if fullContent != "Hello from stream!" {
		t.Errorf("expected 'Hello from stream!', got %q", fullContent)
	}
}

func TestOpenAIProviderNotAvailable(t *testing.T) {
	provider := NewOpenAIProvider(Config{})
	if provider.Available() {
		t.Error("expected provider to not be available with empty API key")
	}

	provider = NewOpenAIProvider(Config{APIKey: " \t", Endpoint: " \n"})
	if provider.Available() {
		t.Error("expected provider to not be available with whitespace-only config")
	}
}

func TestOpenAIProviderAvailableWithAuthlessEndpoint(t *testing.T) {
	provider := NewOpenAIProvider(Config{Endpoint: "http://127.0.0.1:11434/v1"})
	if !provider.Available() {
		t.Error("expected provider to be available with custom endpoint and no API key")
	}
}

func TestOpenAIProviderBaseURLIgnoresWhitespaceEndpoint(t *testing.T) {
	provider := NewOpenAIProvider(Config{
		APIKey:   "test-key",
		Endpoint: " \t",
	})

	if got := provider.baseURL(); got != "https://api.openai.com/v1" {
		t.Errorf("expected default OpenAI base URL, got %q", got)
	}
}

func TestOpenAIProviderAPIError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"message":"rate limited"}}`))
	}))
	defer ts.Close()

	provider := NewOpenAIProvider(Config{
		APIKey:   "test-key",
		Model:    "gpt-4o",
		Endpoint: ts.URL + "/v1",
	})

	_, err := provider.Complete(context.Background(), []Message{
		{Role: "user", Content: "Hello"},
	}, Options{})
	if err == nil {
		t.Fatal("expected error for 429 response")
	}
}

func TestConfigFromEnv(t *testing.T) {
	// Clear the LLM environment so ambient values in the caller's shell cannot
	// influence the defaults under test. t.Setenv restores the originals.
	t.Setenv("CLOUDPAM_LLM_API_KEY", "")
	t.Setenv("CLOUDPAM_LLM_MODEL", "")
	t.Setenv("CLOUDPAM_LLM_ENDPOINT", "")
	t.Setenv("CLOUDPAM_LLM_MAX_TOKENS", "")
	t.Setenv("CLOUDPAM_LLM_TEMPERATURE", "")

	// Test defaults
	cfg := ConfigFromEnv()
	if cfg.Model != "gpt-4o" {
		t.Errorf("expected default model gpt-4o, got %s", cfg.Model)
	}
	if cfg.MaxTokens != 4096 {
		t.Errorf("expected default max_tokens 4096, got %d", cfg.MaxTokens)
	}
	if cfg.Temperature != 0.7 {
		t.Errorf("expected default temperature 0.7, got %f", cfg.Temperature)
	}
}

// captureTemperature runs a single completion against a stub server and returns
// the temperature that was actually sent on the wire.
func captureTemperature(t *testing.T, cfg Config, opts Options) float64 {
	t.Helper()

	var got float64
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req openaiRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request: %v", err)
			return
		}
		got = req.Temperature

		w.Header().Set("Content-Type", "application/json")
		resp := openaiResponse{
			Choices: []struct {
				Message      openaiMessage `json:"message"`
				FinishReason string        `json:"finish_reason"`
			}{
				{Message: openaiMessage{Role: "assistant", Content: "ok"}, FinishReason: "stop"},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	cfg.Endpoint = ts.URL + "/v1"
	provider := NewOpenAIProvider(cfg)
	if _, err := provider.Complete(context.Background(), []Message{{Role: "user", Content: "Hi"}}, opts); err != nil {
		t.Fatalf("complete: %v", err)
	}
	return got
}

func TestOpenAIProviderTemperatureOverride(t *testing.T) {
	cfg := Config{APIKey: "test-key", Model: "gpt-4o", MaxTokens: 1024, Temperature: 0.7}

	zero := 0.0
	if got := captureTemperature(t, cfg, Options{Temperature: &zero}); got != 0 {
		t.Errorf("expected explicit temperature 0 to be sent, got %v", got)
	}

	custom := 0.25
	if got := captureTemperature(t, cfg, Options{Temperature: &custom}); got != 0.25 {
		t.Errorf("expected temperature 0.25, got %v", got)
	}

	if got := captureTemperature(t, cfg, Options{}); got != 0.7 {
		t.Errorf("expected configured default temperature 0.7, got %v", got)
	}
}

// TestOpenAIProviderStreamStopsAfterFirstDone covers #359: once the chunk
// carrying finish_reason is forwarded as Done, the reader must stop, close the
// channel and release the response body even when the server keeps streaming
// trailing chunks (usage, [DONE] or anything else) that nobody consumes.
func TestOpenAIProviderStreamStopsAfterFirstDone(t *testing.T) {
	const trailing = 200 // well beyond the 64-slot channel buffer

	data := []string{chunk("done", "stop")}
	for i := 0; i < trailing; i++ {
		data = append(data, chunk("late", ""))
	}
	// No [DONE]: the stub holds the connection until the client closes the
	// body (or the test gives up), so a closed body is observable.
	stub := newSSEStub(t, true, data...)

	provider := NewOpenAIProvider(Config{APIKey: "test-key", Model: "gpt-4o", Endpoint: stub.URL})
	ch, err := provider.StreamComplete(context.Background(), []Message{{Role: "user", Content: "hi"}}, Options{})
	if err != nil {
		t.Fatalf("StreamComplete: %v", err)
	}

	var content string
	doneSeen := 0
	for evt := range ch {
		content += evt.Delta
		if evt.Err != nil {
			t.Fatalf("unexpected stream error: %v", evt.Err)
		}
		if evt.Done {
			doneSeen++
			break
		}
	}
	if doneSeen != 1 || content != "done" {
		t.Fatalf("expected one done event with content %q, got done=%d content=%q", "done", doneSeen, content)
	}

	// Nothing may follow the first done event, and the channel must close on
	// its own without the request context being cancelled.
	extra := 0
	timeout := time.After(3 * time.Second)
drain:
	for {
		select {
		case evt, ok := <-ch:
			if !ok {
				break drain
			}
			extra++
			if evt.Done {
				doneSeen++
			}
		case <-timeout:
			t.Fatalf("stream channel not closed 3s after the first done event (%d extra events, %d done events)", extra, doneSeen)
		}
	}
	if extra != 0 || doneSeen != 1 {
		t.Fatalf("expected no events after the first done, got %d extra events and %d done events", extra, doneSeen)
	}

	select {
	case <-stub.disconnected:
	case <-time.After(3 * time.Second):
		t.Fatal("response body was not closed after the first done event")
	}
}

// TestOpenAIProviderStreamIgnoresEmptyFinishReason: some OpenAI-compatible
// proxies send "finish_reason":"" on intermediate chunks instead of null. An
// empty finish reason is not terminal and must not end the stream.
func TestOpenAIProviderStreamIgnoresEmptyFinishReason(t *testing.T) {
	stub := newSSEStub(t, false, chunk("Hel", ""), chunk("lo", ""), chunk("!", "stop"), "[DONE]")

	provider := NewOpenAIProvider(Config{APIKey: "test-key", Model: "gpt-4o", Endpoint: stub.URL})
	ch, err := provider.StreamComplete(context.Background(), []Message{{Role: "user", Content: "hi"}}, Options{})
	if err != nil {
		t.Fatalf("StreamComplete: %v", err)
	}

	var content, finish string
	doneSeen := 0
	timeout := time.After(3 * time.Second)
	for open := true; open; {
		select {
		case evt, ok := <-ch:
			if !ok {
				open = false
				break
			}
			if evt.Err != nil {
				t.Fatalf("unexpected stream error: %v", evt.Err)
			}
			content += evt.Delta
			if evt.Done {
				doneSeen++
				finish = evt.FinishReason
			}
		case <-timeout:
			t.Fatal("stream channel not closed within 3s")
		}
	}
	if content != "Hello!" {
		t.Fatalf("content = %q, want %q (an empty finish_reason must not end the stream)", content, "Hello!")
	}
	if doneSeen != 1 || finish != "stop" {
		t.Fatalf("done events = %d (finish %q), want exactly 1 with finish %q", doneSeen, finish, "stop")
	}
}

// recordedSpans installs one recording provider per test binary, because the
// package tracer delegates to the first global provider and never to a later one.
var recordedSpans = sync.OnceValue(func() *tracetest.InMemoryExporter {
	exporter := tracetest.NewInMemoryExporter()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter)))
	return exporter
})

// streamSpanEnded reports whether the stream span for endpoint has been exported.
func streamSpanEnded(exporter *tracetest.InMemoryExporter, endpoint string) bool {
	for _, span := range exporter.GetSpans() {
		if span.Name != "llm.chat.completions" {
			continue
		}
		for _, kv := range span.Attributes {
			if kv.Key == "server.address" && kv.Value.AsString() == endpoint {
				return true
			}
		}
	}
	return false
}

// TestOpenAIProviderStreamEndsWhenContextIsCancelled covers #375: a consumer
// that stops reading before the terminal event (a cancelled handler, a panic
// swallowed by the recovery middleware) must not pin the producer on a channel
// send. Once the context ends, the producer has to close the body, end the
// stream span and close the channel on its own.
func TestOpenAIProviderStreamEndsWhenContextIsCancelled(t *testing.T) {
	exporter := recordedSpans()
	exporter.Reset()

	const pending = 500 // well beyond the 64-slot channel buffer
	data := make([]string, 0, pending)
	for i := 0; i < pending; i++ {
		data = append(data, chunk("late", ""))
	}
	stub := newSSEStub(t, true, data...)

	provider := NewOpenAIProvider(Config{APIKey: "test-key", Model: "gpt-4o", Endpoint: stub.URL})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := provider.StreamComplete(ctx, []Message{{Role: "user", Content: "hi"}}, Options{})
	if err != nil {
		t.Fatalf("StreamComplete: %v", err)
	}

	// One event proves the producer is streaming; after that nobody reads.
	select {
	case <-ch:
	case <-time.After(3 * time.Second):
		t.Fatal("no event within 3s")
	}
	cancel()

	deadline := time.Now().Add(3 * time.Second)
	for !streamSpanEnded(exporter, stub.URL) {
		if time.Now().After(deadline) {
			t.Fatal("stream span still open 3s after the context was cancelled: the producer is stuck")
		}
		time.Sleep(10 * time.Millisecond)
	}
	select {
	case <-stub.disconnected:
	case <-time.After(3 * time.Second):
		t.Fatal("response body was not closed after the context was cancelled")
	}
	timeout := time.After(3 * time.Second)
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return
			}
		case <-timeout:
			t.Fatal("stream channel not closed after the context was cancelled")
		}
	}
}

// TestOpenAIProviderStreamReusesTheConnection covers #375: a stream that ends
// normally must leave the connection in the transport's pool, which needs the
// body read to EOF. Providers send a usage chunk and [DONE] after the
// finish_reason chunk, and end the response right after [DONE].
func TestOpenAIProviderStreamReusesTheConnection(t *testing.T) {
	for name, data := range map[string][]string{
		"finish_reason then usage and [DONE]": {
			chunk("Hi", "stop"),
			`{"choices":[],"usage":{"prompt_tokens":1,"completion_tokens":1}}`,
			"[DONE]",
		},
		"[DONE] only": {chunk("Hi", ""), "[DONE]"},
	} {
		t.Run(name, func(t *testing.T) {
			stub := newSSEStub(t, false, data...)
			provider := NewOpenAIProvider(Config{APIKey: "test-key", Model: "gpt-4o", Endpoint: stub.URL})
			for i := 0; i < 2; i++ {
				ch, err := provider.StreamComplete(context.Background(), []Message{{Role: "user", Content: "hi"}}, Options{})
				if err != nil {
					t.Fatalf("StreamComplete %d: %v", i, err)
				}
				var content string
				for evt := range ch {
					if evt.Err != nil {
						t.Fatalf("stream %d: unexpected error %v", i, evt.Err)
					}
					content += evt.Delta
				}
				if content != "Hi" {
					t.Fatalf("stream %d content = %q, want %q", i, content, "Hi")
				}
			}
			if got := stub.conns.Load(); got != 1 {
				t.Fatalf("two streams opened %d connections, want 1 (the first was not pooled)", got)
			}
		})
	}
}

// TestOpenAIProviderStreamReportsCancellationToAReadingConsumer: giving up on
// a send when ctx ends must not drop the terminal event for a consumer that is
// still reading. The AI service treats a stream that closes without an error
// event as a complete answer and stores it, so the cancellation has to arrive
// as Done with Err set.
func TestOpenAIProviderStreamReportsCancellationToAReadingConsumer(t *testing.T) {
	stub := newSSEStub(t, true, chunk("Hel", ""), chunk("lo", ""))
	provider := NewOpenAIProvider(Config{APIKey: "test-key", Model: "gpt-4o", Endpoint: stub.URL})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := provider.StreamComplete(ctx, []Message{{Role: "user", Content: "hi"}}, Options{})
	if err != nil {
		t.Fatalf("StreamComplete: %v", err)
	}

	var content string
	doneSeen := 0
	var streamErr error
	timeout := time.After(3 * time.Second)
	for open := true; open; {
		select {
		case evt, ok := <-ch:
			if !ok {
				open = false
				break
			}
			content += evt.Delta
			if evt.Done {
				doneSeen++
				streamErr = evt.Err
			}
			if content == "Hello" {
				cancel()
			}
		case <-timeout:
			t.Fatal("stream channel not closed within 3s of cancelling the context")
		}
	}
	if doneSeen != 1 || !errors.Is(streamErr, context.Canceled) {
		t.Fatalf("done events = %d with err %v, want exactly 1 with context.Canceled", doneSeen, streamErr)
	}
}
