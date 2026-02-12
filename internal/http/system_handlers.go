package http

import (
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/getsentry/sentry-go"

	apidocs "cloudpam/docs"
	"cloudpam/internal/audit"
	webui "cloudpam/web"
)

func (s *Server) handleOpenAPISpec(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeErr(r.Context(), w, http.StatusMethodNotAllowed, "method not allowed", "")
		return
	}
	w.Header().Set("Content-Type", "application/yaml")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(apidocs.OpenAPISpec)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ReadinessResponse represents the JSON response for the readiness check endpoint.
type ReadinessResponse struct {
	Status string            `json:"status"`
	Checks map[string]string `json:"checks"`
}

// handleReady checks if the application is ready to accept traffic.
// Unlike /healthz (liveness), this endpoint verifies that dependencies are accessible.
// Returns 200 OK if all checks pass, 503 Service Unavailable otherwise.
func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeErr(r.Context(), w, http.StatusMethodNotAllowed, "method not allowed", "")
		return
	}

	ctx := r.Context()
	checks := make(map[string]string)
	status := "ok"

	// Database check: attempt to list pools to verify database connectivity
	_, err := s.store.ListPools(ctx)
	if err != nil {
		checks["database"] = "error"
		status = "unhealthy"
		s.logger.ErrorContext(ctx, "readiness check failed", appendRequestID(ctx, []any{
			"check", "database",
			"error", err.Error(),
		})...)
	} else {
		checks["database"] = "ok"
	}

	resp := ReadinessResponse{
		Status: status,
		Checks: checks,
	}

	if status == "ok" {
		writeJSON(w, http.StatusOK, resp)
	} else {
		writeJSON(w, http.StatusServiceUnavailable, resp)
	}
}

func (s *Server) handleTestSentry(w http.ResponseWriter, r *http.Request) {
	// Test endpoint to verify Sentry is working
	testType := r.URL.Query().Get("type")

	switch testType {
	case "message":
		// Test message capture
		sentry.CaptureMessage("Sentry test message from CloudPAM")
		sentry.Flush(2 * time.Second)
		writeJSON(w, http.StatusOK, map[string]string{"status": "message sent to Sentry"})
	case "error":
		// Test error capture with 500 status
		s.writeErr(r.Context(), w, http.StatusInternalServerError, "test error for Sentry", "this is a test error to verify Sentry integration")
	case "panic":
		// Test panic recovery
		panic("test panic for Sentry")
	default:
		writeJSON(w, http.StatusOK, map[string]string{
			"message": "Sentry test endpoint",
			"usage":   "?type=message|error|panic",
		})
	}
}

// handleSPA serves the Vite-built React SPA from the embedded dist/ directory.
// Static assets (JS, CSS, images) are served directly; all other paths
// fall back to index.html for client-side routing.
// The Sentry frontend DSN is injected via a meta tag if SENTRY_FRONTEND_DSN is set.
func (s *Server) handleSPA() http.Handler {
	distSub, err := fs.Sub(webui.DistFS, "dist")
	if err != nil {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "UI not built – run: just ui-build", http.StatusNotFound)
		})
	}

	// Cache index.html at init time for SPA fallback.
	indexBytes, err := fs.ReadFile(distSub, "index.html")
	if err != nil {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "UI not built – run: just ui-build", http.StatusNotFound)
		})
	}
	fileServer := http.FileServer(http.FS(distSub))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Serve real files (JS, CSS, assets).
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path != "" {
			if _, err := fs.Stat(distSub, path); err == nil {
				fileServer.ServeHTTP(w, r)
				return
			}
		}
		// SPA fallback: serve index.html with optional Sentry DSN injection.
		html := string(indexBytes)
		if dsn := os.Getenv("SENTRY_FRONTEND_DSN"); dsn != "" {
			html = strings.Replace(html, "</head>",
				fmt.Sprintf("<meta name=\"sentry-dsn\" content=\"%s\">\n</head>", dsn), 1)
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(html))
	})
}

// GET /api/v1/audit - List audit events with optional filtering
// Query params: limit, offset, actor, action, resource_type
func (s *Server) handleAuditList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeErr(r.Context(), w, http.StatusMethodNotAllowed, "method not allowed", "")
		return
	}

	q := r.URL.Query()

	// Parse pagination
	limit := 50
	if l := q.Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 && parsed <= 1000 {
			limit = parsed
		}
	}

	offset := 0
	if o := q.Get("offset"); o != "" {
		if parsed, err := strconv.Atoi(o); err == nil && parsed >= 0 {
			offset = parsed
		}
	}

	opts := audit.ListOptions{
		Limit:        limit,
		Offset:       offset,
		Actor:        q.Get("actor"),
		Action:       q.Get("action"),
		ResourceType: q.Get("resource_type"),
	}

	events, total, err := s.auditLogger.List(r.Context(), opts)
	if err != nil {
		s.writeErr(r.Context(), w, http.StatusInternalServerError, "failed to list audit events", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"events": events,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

