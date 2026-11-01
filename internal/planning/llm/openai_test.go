package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
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
	defer ts.Close()
	provider := NewOpenAIProvider(Config{Model: "m", Endpoint: ts.URL + "/v1"})
	msgs := []Message{{Role: "user", Content: "hi"}}

	if _, err := provider.Complete(context.Background(), msgs, Options{}); !errors.Is(err, ErrInvalidResponse) {
		t.Fatalf("Complete err = %v, want ErrInvalidResponse", err)
	}

	events, err := provider.StreamComplete(context.Background(), msgs, Options{})
	if err != nil {
		t.Fatalf("StreamComplete: %v", err)
	}
	var streamErr error
	for evt := range events {
		if evt.Err != nil {
			streamErr = evt.Err
		}
		if evt.Done {
			break
		}
	}
	if !errors.Is(streamErr, ErrInvalidResponse) {
		t.Fatalf("stream err = %v, want ErrInvalidResponse", streamErr)
	}
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
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")

		chunks := []string{"Hello", " from", " stream", "!"}
		for _, chunk := range chunks {
			data := fmt.Sprintf(`{"choices":[{"delta":{"content":"%s"},"finish_reason":null}]}`, chunk)
			_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
			w.(http.Flusher).Flush()
		}
		_, _ = fmt.Fprintf(w, "data: [DONE]\n\n")
		w.(http.Flusher).Flush()
	}))
	defer ts.Close()

	provider := NewOpenAIProvider(Config{
		APIKey:      "test-key",
		Model:       "gpt-4o",
		Endpoint:    ts.URL + "/v1",
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

	stop := make(chan struct{})
	disconnected := make(chan struct{})
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, `data: {"choices":[{"delta":{"content":"done"},"finish_reason":"stop"}]}`+"\n\n")
		for i := 0; i < trailing; i++ {
			_, _ = fmt.Fprint(w, `data: {"choices":[{"delta":{"content":"late"},"finish_reason":null}]}`+"\n\n")
		}
		w.(http.Flusher).Flush()
		// Never send [DONE]; hold the connection until the client closes the
		// body (or the test gives up), so a closed body is observable.
		select {
		case <-r.Context().Done():
			close(disconnected)
		case <-stop:
		}
	}))
	defer ts.Close()
	defer close(stop)

	provider := NewOpenAIProvider(Config{APIKey: "test-key", Model: "gpt-4o", Endpoint: ts.URL + "/v1"})
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
	case <-disconnected:
	case <-time.After(3 * time.Second):
		t.Fatal("response body was not closed after the first done event")
	}
}

// TestOpenAIProviderStreamIgnoresEmptyFinishReason: some OpenAI-compatible
// proxies send "finish_reason":"" on intermediate chunks instead of null. An
// empty finish reason is not terminal and must not end the stream.
func TestOpenAIProviderStreamIgnoresEmptyFinishReason(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, data := range []string{
			`{"choices":[{"delta":{"content":"Hel"},"finish_reason":""}]}`,
			`{"choices":[{"delta":{"content":"lo"},"finish_reason":""}]}`,
			`{"choices":[{"delta":{"content":"!"},"finish_reason":"stop"}]}`,
			`[DONE]`,
		} {
			_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
		}
	}))
	defer ts.Close()

	provider := NewOpenAIProvider(Config{APIKey: "test-key", Model: "gpt-4o", Endpoint: ts.URL + "/v1"})
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
