// Package testutil provides testing utilities for CloudPAM integration tests.
package testutil

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"cloudpam/internal/audit"
	"cloudpam/internal/auth"
	cloudpamhttp "cloudpam/internal/http"
	"cloudpam/internal/observability"
	"cloudpam/internal/storage"
)

// TestServerConfig holds configuration for creating a test server.
type TestServerConfig struct {
	// EnableAuth enables API key authentication middleware.
	EnableAuth bool
	// RequireAuth makes authentication required (vs optional).
	RequireAuth bool
	// EnableRateLimit enables rate limiting middleware.
	EnableRateLimit bool
	// RateLimitConfig configures rate limiting if enabled.
	RateLimitConfig cloudpamhttp.RateLimitConfig
	// EnableMetrics enables metrics collection.
	EnableMetrics bool
	// EnableAudit enables audit logging.
	EnableAudit bool
}

// DefaultTestServerConfig returns a basic test server configuration.
func DefaultTestServerConfig() TestServerConfig {
	return TestServerConfig{
		EnableAuth:      false,
		RequireAuth:     false,
		EnableRateLimit: false,
		EnableMetrics:   false,
		EnableAudit:     false,
	}
}

// TestServerComponents holds all the components created for a test server.
type TestServerComponents struct {
	// Server is the test HTTP server.
	Server *httptest.Server
	// Store is the storage backend.
	Store *storage.MemoryStore
	// KeyStore is the API key store.
	KeyStore *auth.MemoryKeyStore
	// AuditLogger is the audit logger.
	AuditLogger *audit.MemoryAuditLogger
	// Metrics is the metrics collector.
	Metrics *observability.Metrics
	// Logger is the structured logger.
	Logger observability.Logger
	// Cleanup tears down the test server.
	Cleanup func()
}

// NewTestServer creates a fully configured test server with all dependencies.
// It returns the server components and a cleanup function.
func NewTestServer(t *testing.T, cfg TestServerConfig) *TestServerComponents {
	t.Helper()

	// Create storage
	store := storage.NewMemoryStore()

	// Create logger (discard output in tests)
	logger := observability.NewLogger(observability.Config{
		Level:  "debug",
		Format: "json",
		Output: io.Discard,
	})

	// Create metrics if enabled
	var metrics *observability.Metrics
	if cfg.EnableMetrics {
		metrics = observability.NewMetrics(observability.MetricsConfig{
			Namespace: "cloudpam_test",
			Version:   "test",
		})
	}

	// Create key store
	keyStore := auth.NewMemoryKeyStore()

	// Create audit logger
	var auditLogger *audit.MemoryAuditLogger
	if cfg.EnableAudit {
		auditLogger = audit.NewMemoryAuditLogger(audit.WithMaxEvents(1000))
	}

	// Create the base server
	mux := http.NewServeMux()
	srv := cloudpamhttp.NewServer(mux, store, logger, metrics, auditLogger)
	srv.RegisterRoutes()

	// Create auth server for key management endpoints
	authSrv := cloudpamhttp.NewAuthServer(srv, keyStore, auditLogger)
	authSrv.RegisterAuthRoutes()

	// Build middleware chain
	var handler http.Handler = mux

	// Apply audit middleware if enabled
	if cfg.EnableAudit && auditLogger != nil {
		// Create an adapter that converts audit.MemoryAuditLogger to cloudpamhttp.AuditLogger
		adapter := &auditLoggerAdapter{logger: auditLogger}
		handler = cloudpamhttp.AuditMiddleware(adapter, logger.Slog())(handler)
	}

	// Apply auth middleware if enabled
	if cfg.EnableAuth {
		handler = cloudpamhttp.AuthMiddleware(keyStore, cfg.RequireAuth, logger.Slog())(handler)
	}

	// Apply rate limiting if enabled
	if cfg.EnableRateLimit {
		handler = cloudpamhttp.RateLimitMiddleware(cfg.RateLimitConfig, logger.Slog())(handler)
	}

	// Apply metrics middleware if enabled
	if cfg.EnableMetrics && metrics != nil {
		handler = observability.MetricsMiddleware(metrics)(handler)
	}

	// Apply request ID middleware
	handler = cloudpamhttp.RequestIDMiddleware()(handler)

	// Apply logging middleware
	handler = cloudpamhttp.LoggingMiddleware(logger.Slog())(handler)

	// Create test server
	testServer := httptest.NewServer(handler)

	cleanup := func() {
		testServer.Close()
		_ = store.Close()
	}

	return &TestServerComponents{
		Server:      testServer,
		Store:       store,
		KeyStore:    keyStore,
		AuditLogger: auditLogger,
		Metrics:     metrics,
		Logger:      logger,
		Cleanup:     cleanup,
	}
}

// auditLoggerAdapter adapts audit.MemoryAuditLogger to cloudpamhttp.AuditLogger interface.
type auditLoggerAdapter struct {
	logger *audit.MemoryAuditLogger
}

func (a *auditLoggerAdapter) Log(ctx context.Context, event *cloudpamhttp.AuditEvent) error {
	if event == nil {
		return nil
	}

	// Convert cloudpamhttp.AuditEvent to audit.AuditEvent
	auditEvent := &audit.AuditEvent{
		ID:           event.ID,
		Timestamp:    event.Timestamp,
		Actor:        event.Actor,
		ActorType:    event.ActorType,
		Action:       event.Action,
		ResourceType: event.ResourceType,
		ResourceID:   event.ResourceID,
		ResourceName: event.ResourceName,
		RequestID:    event.RequestID,
		IPAddress:    event.IPAddress,
		StatusCode:   event.StatusCode,
	}

	if event.Changes != nil {
		auditEvent.Changes = &audit.Changes{
			Before: event.Changes.Before,
			After:  event.Changes.After,
		}
	}

	return a.logger.Log(ctx, auditEvent)
}

// CreateTestAPIKey creates a new API key for testing and stores it.
// Returns the plaintext key and the stored APIKey record.
func CreateTestAPIKey(t *testing.T, keyStore auth.KeyStore, name string, scopes []string) (string, *auth.APIKey) {
	t.Helper()

	plaintext, apiKey, err := auth.GenerateAPIKey(auth.GenerateAPIKeyOptions{
		Name:   name,
		Scopes: scopes,
	})
	if err != nil {
		t.Fatalf("failed to generate API key: %v", err)
	}

	if err := keyStore.Create(context.Background(), apiKey); err != nil {
		t.Fatalf("failed to store API key: %v", err)
	}

	return plaintext, apiKey
}

// AuthenticatedRequest creates an HTTP request with Bearer token authentication.
func AuthenticatedRequest(method, url, apiKey string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return nil, err
	}

	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	return req, nil
}

// MustAuthenticatedRequest creates an HTTP request with Bearer token authentication.
// It panics if request creation fails.
func MustAuthenticatedRequest(t *testing.T, method, url, apiKey string, body io.Reader) *http.Request {
	t.Helper()
	req, err := AuthenticatedRequest(method, url, apiKey, body)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	return req
}

// DoRequest performs an HTTP request and returns the response.
func DoRequest(t *testing.T, client *http.Client, req *http.Request) *http.Response {
	t.Helper()

	if client == nil {
		client = http.DefaultClient
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}

	return resp
}

// AssertStatus checks that the response has the expected status code.
func AssertStatus(t *testing.T, got, expected int) {
	t.Helper()

	if got != expected {
		t.Errorf("expected status %d, got %d", expected, got)
	}
}

// AssertJSON checks that the response body matches the expected JSON structure.
// The expected value should be a pointer to the struct to unmarshal into.
func AssertJSON(t *testing.T, body io.Reader, expected interface{}) {
	t.Helper()

	data, err := io.ReadAll(body)
	if err != nil {
		t.Fatalf("failed to read body: %v", err)
	}

	if err := json.Unmarshal(data, expected); err != nil {
		t.Fatalf("failed to unmarshal JSON: %v\nBody: %s", err, string(data))
	}
}

// AssertJSONEqual checks that the response body equals the expected JSON.
func AssertJSONEqual(t *testing.T, body io.Reader, expected interface{}) {
	t.Helper()

	data, err := io.ReadAll(body)
	if err != nil {
		t.Fatalf("failed to read body: %v", err)
	}

	expectedJSON, err := json.Marshal(expected)
	if err != nil {
		t.Fatalf("failed to marshal expected: %v", err)
	}

	// Compare by unmarshaling both to interface{} and comparing
	var gotValue, expectedValue interface{}
	if err := json.Unmarshal(data, &gotValue); err != nil {
		t.Fatalf("failed to unmarshal actual: %v\nBody: %s", err, string(data))
	}
	if err := json.Unmarshal(expectedJSON, &expectedValue); err != nil {
		t.Fatalf("failed to unmarshal expected: %v", err)
	}

	gotNorm, _ := json.Marshal(gotValue)
	expectedNorm, _ := json.Marshal(expectedValue)

	if !bytes.Equal(gotNorm, expectedNorm) {
		t.Errorf("JSON mismatch:\nExpected: %s\nGot: %s", expectedNorm, gotNorm)
	}
}

// AssertContains checks that the response body contains the expected string.
func AssertContains(t *testing.T, body io.Reader, expected string) {
	t.Helper()

	data, err := io.ReadAll(body)
	if err != nil {
		t.Fatalf("failed to read body: %v", err)
	}

	if !bytes.Contains(data, []byte(expected)) {
		t.Errorf("expected body to contain %q, got: %s", expected, string(data))
	}
}

// AssertHeader checks that the response has the expected header value.
func AssertHeader(t *testing.T, resp *http.Response, key, expected string) {
	t.Helper()

	got := resp.Header.Get(key)
	if got != expected {
		t.Errorf("expected header %s=%q, got %q", key, expected, got)
	}
}

// AssertHeaderExists checks that the response has the specified header.
func AssertHeaderExists(t *testing.T, resp *http.Response, key string) {
	t.Helper()

	if resp.Header.Get(key) == "" {
		t.Errorf("expected header %s to exist", key)
	}
}

// JSONBody creates an io.Reader from a JSON-serializable value.
func JSONBody(t *testing.T, v interface{}) io.Reader {
	t.Helper()

	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("failed to marshal JSON: %v", err)
	}

	return bytes.NewReader(data)
}

// ReadJSONResponse reads and unmarshals a JSON response body.
func ReadJSONResponse(t *testing.T, resp *http.Response, v interface{}) {
	t.Helper()

	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response body: %v", err)
	}

	if err := json.Unmarshal(data, v); err != nil {
		t.Fatalf("failed to unmarshal response: %v\nBody: %s", err, string(data))
	}
}

// HTTPClient returns the test server's client configured for the server.
func (c *TestServerComponents) HTTPClient() *http.Client {
	return c.Server.Client()
}

// URL returns the full URL for a given path.
func (c *TestServerComponents) URL(path string) string {
	return c.Server.URL + path
}
