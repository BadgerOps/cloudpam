package api

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"cloudpam/internal/planning/llm"
)

// TestAIChatReportsMidStreamErrorAsErrorEvent runs the chat route behind the
// logging and audit middleware, as production does, with a provider that
// fails after its first delta. The client must see the delta, then an error
// event with a sanitized message, and never a done event.
func TestAIChatReportsMidStreamErrorAsErrorEvent(t *testing.T) {
	provider := &stubLLMProviderCov{
		available:    true,
		deltas:       []string{"Here is "},
		midStreamErr: errors.Join(llm.ErrResponseTooLarge, errors.New("response body held sk-secret-detail")),
	}
	ai, _, convStore := setupAITestServerCov(t, provider)
	conv := newConversationCov(t, convStore, "chat")

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := LoggingMiddleware(logger)(AuditMiddleware(&mockAuditLogger{}, logger)(ai.srv.mux))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/ai/chat",
		strings.NewReader(`{"session_id":"`+conv+`","message":"Plan a /16 for prod"}`))
	req.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	for _, want := range []string{
		`data: {"delta":"Here is "}`,
		"event: error\ndata: {\"error\":\"provider response exceeded the size limit\"}\n\n",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("SSE body missing %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "event: done") {
		t.Fatalf("a failed stream must not end with a done event:\n%s", body)
	}
	if strings.Contains(body, "sk-secret") {
		t.Fatalf("raw provider error reached the client:\n%s", body)
	}
	if !strings.HasSuffix(body, "\n\n") {
		t.Fatalf("stream did not stop at the error event:\n%s", body)
	}
	if !rr.Flushed {
		t.Error("SSE response was never flushed")
	}
}
