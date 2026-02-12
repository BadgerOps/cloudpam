package http

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/getsentry/sentry-go"

	"cloudpam/internal/audit"
	"cloudpam/internal/auth"
	"cloudpam/internal/observability"
	"cloudpam/internal/storage"
)

type apiError struct {
	Error  string `json:"error"`
	Detail string `json:"detail,omitempty"`
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

type Server struct {
	mux         *http.ServeMux
	store       storage.Store
	logger      observability.Logger
	metrics     *observability.Metrics
	auditLogger audit.AuditLogger
}

// NewServer creates a new HTTP server with the given dependencies.
// If logger is nil, a default logger will be used.
// If metrics is nil, metrics collection is disabled.
// If auditLogger is nil, a memory-based audit logger will be used.
func NewServer(mux *http.ServeMux, store storage.Store, logger observability.Logger, metrics *observability.Metrics, auditLogger audit.AuditLogger) *Server {
	if logger == nil {
		logger = observability.NewLogger(observability.DefaultConfig())
	}
	if auditLogger == nil {
		auditLogger = audit.NewMemoryAuditLogger()
	}
	return &Server{mux: mux, store: store, logger: logger, metrics: metrics, auditLogger: auditLogger}
}

// NewServerWithSlog creates a new HTTP server with a raw *slog.Logger.
// This is for backward compatibility with existing code.
func NewServerWithSlog(mux *http.ServeMux, store storage.Store, slogger *slog.Logger) *Server {
	var logger observability.Logger
	if slogger != nil {
		logger = observability.NewLoggerFromSlog(slogger)
	} else {
		logger = observability.NewLogger(observability.DefaultConfig())
	}
	return &Server{mux: mux, store: store, logger: logger, metrics: nil, auditLogger: audit.NewMemoryAuditLogger()}
}

func valueOrNil[T any](ptr *T) any {
	if ptr == nil {
		return nil
	}
	return *ptr
}

func (s *Server) writeErr(ctx context.Context, w http.ResponseWriter, code int, msg string, detail string) {
	fields := []any{
		"status", code,
		"error", msg,
	}
	if detail != "" {
		fields = append(fields, "detail", detail)
	}
	fields = appendRequestID(ctx, fields)
	if code >= 500 {
		s.logger.ErrorContext(ctx, "request failed", fields...)
		sentry.CaptureMessage(fmt.Sprintf("HTTP %d: %s (detail: %s)", code, msg, detail))
	} else {
		s.logger.WarnContext(ctx, "request failed", fields...)
	}
	writeJSON(w, code, apiError{Error: msg, Detail: detail})
}

// writeStoreErr maps a storage-layer error to the appropriate HTTP status code
// and writes the error response. It uses errors.Is() to detect sentinel errors
// from the storage package, falling back to 500 Internal Server Error for unknown errors.
func (s *Server) writeStoreErr(ctx context.Context, w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, storage.ErrNotFound):
		s.writeErr(ctx, w, http.StatusNotFound, err.Error(), "")
	case errors.Is(err, storage.ErrConflict):
		s.writeErr(ctx, w, http.StatusConflict, err.Error(), "")
	case errors.Is(err, storage.ErrValidation):
		s.writeErr(ctx, w, http.StatusBadRequest, err.Error(), "")
	default:
		s.writeErr(ctx, w, http.StatusInternalServerError, "internal error", err.Error())
	}
}

// logAudit logs an audit event for CRUD operations.
func (s *Server) logAudit(ctx context.Context, action, resourceType, resourceID, resourceName string, statusCode int) {
	if s.auditLogger == nil {
		return
	}
	event := &audit.AuditEvent{
		Actor:        "anonymous", // Will be overwritten if auth is enabled
		ActorType:    audit.ActorTypeAnonymous,
		Action:       action,
		ResourceType: resourceType,
		ResourceID:   resourceID,
		ResourceName: resourceName,
		StatusCode:   statusCode,
	}
	// Try to get request ID from context
	if reqID := ctx.Value(requestIDContextKey); reqID != nil {
		if id, ok := reqID.(string); ok {
			event.RequestID = id
		}
	}
	_ = s.auditLogger.Log(ctx, event)
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) { s.status = code; s.ResponseWriter.WriteHeader(code) }

// RegisterRoutes registers all HTTP routes without RBAC protection.
// This is for backward compatibility. Use RegisterProtectedRoutes for RBAC enforcement.
func (s *Server) RegisterRoutes() {
	s.mux.HandleFunc("/openapi.yaml", s.handleOpenAPISpec)
	s.mux.HandleFunc("/healthz", s.handleHealth)
	s.mux.HandleFunc("/readyz", s.handleReady)
	// Metrics endpoint
	if s.metrics != nil {
		s.mux.Handle("/metrics", s.metrics.Handler())
	}
	s.mux.HandleFunc("/api/v1/test-sentry", s.handleTestSentry)
	s.mux.HandleFunc("/api/v1/pools", s.handlePools)
	// Note: /api/v1/pools/hierarchy is handled by handlePoolsSubroutes
	s.mux.HandleFunc("/api/v1/pools/", s.handlePoolsSubroutes)
	s.mux.HandleFunc("/api/v1/accounts", s.handleAccounts)
	s.mux.HandleFunc("/api/v1/accounts/", s.handleAccountsSubroutes)
	s.mux.HandleFunc("/api/v1/blocks", s.handleBlocksList)
	// Data export (CSV in ZIP)
	s.mux.HandleFunc("/api/v1/export", s.handleExport)
	// Data import (CSV)
	s.mux.HandleFunc("/api/v1/import/accounts", s.handleImportAccounts)
	s.mux.HandleFunc("/api/v1/import/pools", s.handleImportPools)
	// Audit logs (unprotected access)
	s.mux.HandleFunc("/api/v1/audit", s.handleAuditList)
	// Schema planner wizard SPA
	s.mux.Handle("/wizard/", s.handleWizardAssets())
	// Schema planner API
	s.mux.HandleFunc("/api/v1/schema/check", s.handleSchemaCheck)
	s.mux.HandleFunc("/api/v1/schema/apply", s.handleSchemaApply)
	// Static index
	s.mux.HandleFunc("/", s.handleIndex)
}

// RegisterProtectedRoutes registers all HTTP routes with RBAC protection.
// Routes are protected based on the resource and action being performed.
// Public endpoints (health, metrics, static) remain unprotected.
// API endpoints require authentication and appropriate permissions.
func (s *Server) RegisterProtectedRoutes(keyStore auth.KeyStore, slogger *slog.Logger) {
	if slogger == nil {
		slogger = slog.Default()
	}

	// Public endpoints (no auth required)
	s.mux.HandleFunc("/openapi.yaml", s.handleOpenAPISpec)
	s.mux.HandleFunc("/healthz", s.handleHealth)
	s.mux.HandleFunc("/readyz", s.handleReady)
	if s.metrics != nil {
		s.mux.Handle("/metrics", s.metrics.Handler())
	}
	s.mux.HandleFunc("/api/v1/test-sentry", s.handleTestSentry)
	s.mux.Handle("/wizard/", s.handleWizardAssets())
	s.mux.HandleFunc("/", s.handleIndex)

	// Auth middleware (required for all API endpoints below)
	authMW := AuthMiddleware(keyStore, true, slogger)

	// Pool endpoints - require pools permissions
	s.mux.Handle("/api/v1/pools", authMW(s.protectedPoolsHandler(slogger)))
	s.mux.Handle("/api/v1/pools/", authMW(s.protectedPoolsSubroutesHandler(slogger)))

	// Account endpoints - require accounts permissions
	s.mux.Handle("/api/v1/accounts", authMW(s.protectedAccountsHandler(slogger)))
	s.mux.Handle("/api/v1/accounts/", authMW(s.protectedAccountsSubroutesHandler(slogger)))

	// Blocks list - requires pools:read (read-only view of pool allocations)
	poolsReadMW := RequirePermissionMiddleware(auth.ResourcePools, auth.ActionRead, slogger)
	s.mux.Handle("/api/v1/blocks", authMW(poolsReadMW(http.HandlerFunc(s.handleBlocksList))))

	// Export endpoint - requires pools:read and accounts:read
	exportPermMW := RequireAnyPermissionMiddleware([]auth.Permission{
		{Resource: auth.ResourcePools, Action: auth.ActionRead},
	}, slogger)
	s.mux.Handle("/api/v1/export", authMW(exportPermMW(http.HandlerFunc(s.handleExport))))

	// Schema planner endpoints - require pools:create
	poolsCreateMW := RequirePermissionMiddleware(auth.ResourcePools, auth.ActionCreate, slogger)
	s.mux.Handle("/api/v1/schema/check", authMW(poolsReadMW(http.HandlerFunc(s.handleSchemaCheck))))
	s.mux.Handle("/api/v1/schema/apply", authMW(poolsCreateMW(http.HandlerFunc(s.handleSchemaApply))))
}
