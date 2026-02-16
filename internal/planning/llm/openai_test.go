package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

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
