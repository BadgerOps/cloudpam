package llm

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const secretBody = `{"error":{"message":"invalid key sk-leaked-1234"}}`

func TestStatusErrorOmitsResponseBody(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(secretBody))
	}))
	defer ts.Close()
	p := NewOpenAIProvider(Config{APIKey: "sk-secret", Model: "m", Endpoint: ts.URL})

	_, err := p.Complete(context.Background(), []Message{{Role: "user", Content: "hi"}}, Options{})
	var se *StatusError
	if !errors.As(err, &se) || se.Status != http.StatusUnauthorized {
		t.Fatalf("Complete err = %v, want *StatusError 401", err)
	}
	if strings.Contains(err.Error(), "sk-leaked") || strings.Contains(err.Error(), "sk-secret") {
		t.Fatal("Complete error leaks the response body or key")
	}

	_, err = p.StreamComplete(context.Background(), []Message{{Role: "user", Content: "hi"}}, Options{})
	if !errors.As(err, &se) || se.Status != http.StatusUnauthorized {
		t.Fatalf("StreamComplete err = %v, want *StatusError 401", err)
	}
	if strings.Contains(err.Error(), "sk-leaked") {
		t.Fatal("StreamComplete error leaks the response body")
	}
	if got := DiagnoseError(err, 15*time.Second); got != "provider returned HTTP 401" {
		t.Fatalf("DiagnoseError = %q", got)
	}
}

func TestRedactEndpoint(t *testing.T) {
	for in, want := range map[string]string{
		"":                                   "",
		"http://ollama:11434/v1":             "http://ollama:11434/v1",
		"https://user:s3cret@llm.example/v1": "https://xxxxx@llm.example/v1",
		"https://token@llm.example/v1":       "https://xxxxx@llm.example/v1",
	} {
		if got := RedactEndpoint(in); got != want {
			t.Errorf("RedactEndpoint(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDiagnoseErrorClasses(t *testing.T) {
	// Unreachable: a closed listener refuses connections.
	ts := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := ts.URL
	ts.Close()
	_, err := NewOpenAIProvider(Config{Endpoint: "http://user:pass@" + strings.TrimPrefix(url, "http://")}).
		Complete(context.Background(), []Message{{Role: "user", Content: "hi"}}, Options{})
	if got := DiagnoseError(err, 15*time.Second); got != "endpoint unreachable" {
		t.Fatalf("unreachable DiagnoseError = %q (err %v)", got, err)
	}

	// Timeout.
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Consume the body so the server notices the client hanging up.
		_, _ = io.Copy(io.Discard, r.Body)
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	}))
	defer slow.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err = NewOpenAIProvider(Config{Endpoint: slow.URL}).Complete(ctx, []Message{{Role: "user", Content: "hi"}}, Options{})
	if got := DiagnoseError(err, 15*time.Second); got != "request timed out after 15s" {
		t.Fatalf("timeout DiagnoseError = %q (err %v)", got, err)
	}

	// Invalid response.
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("<html>not json</html>"))
	}))
	defer bad.Close()
	_, err = NewOpenAIProvider(Config{Endpoint: bad.URL}).Complete(context.Background(), []Message{{Role: "user", Content: "hi"}}, Options{})
	if got := DiagnoseError(err, 15*time.Second); got != "invalid response from provider" {
		t.Fatalf("invalid DiagnoseError = %q (err %v)", got, err)
	}

	if got := DiagnoseError(errors.New("something with secret detail"), 15*time.Second); strings.Contains(got, "secret") {
		t.Fatalf("fallback leaks the error text: %q", got)
	}
	if DiagnoseError(nil, time.Second) != "" {
		t.Fatal("nil error should diagnose to empty")
	}
}
