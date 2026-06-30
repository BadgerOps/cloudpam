package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"cloudpam/internal/auth"
)

func TestCSRFMiddleware_GETSetsTokenCookie(t *testing.T) {
	handler := CSRFMiddleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/pools", nil)
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	// Check that csrf_token cookie was set
	var found bool
	for _, c := range rr.Result().Cookies() {
		if c.Name == csrfCookieName {
			found = true
			if c.Value == "" {
				t.Fatal("csrf_token cookie value is empty")
			}
			if c.HttpOnly {
				t.Fatal("csrf_token cookie should not be HttpOnly (JS needs to read it)")
			}
			break
		}
	}
	if !found {
		t.Fatal("expected csrf_token cookie to be set on GET request")
	}
}

func TestCSRFMiddleware_GETDoesNotSetCookieIfAlreadyPresent(t *testing.T) {
	handler := CSRFMiddleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/pools", nil)
	req.AddCookie(&http.Cookie{Name: csrfCookieName, Value: "existing-token"})
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	// Should not set a new cookie since one already exists
	for _, c := range rr.Result().Cookies() {
		if c.Name == csrfCookieName {
			t.Fatal("should not set csrf_token cookie when one already exists")
		}
	}
}

func TestCSRFMiddleware_POSTWithoutCSRFTokenReturns403(t *testing.T) {
	handler := CSRFMiddleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/pools", nil)
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

func TestCSRFMiddleware_POSTWithValidCSRFTokenSucceeds(t *testing.T) {
	handler := CSRFMiddleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	token := "test-csrf-token-value"
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/pools", nil)
	req.AddCookie(&http.Cookie{Name: csrfCookieName, Value: token})
	req.Header.Set(csrfHeaderName, token)
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

func TestCSRFMiddleware_POSTWithMismatchedTokenReturns403(t *testing.T) {
	handler := CSRFMiddleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/pools", nil)
	req.AddCookie(&http.Cookie{Name: csrfCookieName, Value: "cookie-token"})
	req.Header.Set(csrfHeaderName, "different-header-token")
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

func TestCSRFMiddleware_POSTWithAPIKeyBypassesCSRF(t *testing.T) {
	handler := CSRFMiddleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/pools", nil)
	req.Header.Set("Authorization", "Bearer cpam_testkey123abc")
	// No CSRF token provided - should still succeed
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 for API key auth (CSRF bypass), got %d", rr.Code)
	}
}

func TestCSRFMiddleware_POSTWithAPIKeyAndSessionRequiresCSRF(t *testing.T) {
	handler := CSRFMiddleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/pools", nil)
	req.Header.Set("Authorization", "Bearer cpam_forgedkey123abc")
	req.AddCookie(&http.Cookie{Name: "session", Value: "browser-session"})
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403 when a bearer header is mixed with a session cookie and no CSRF token, got %d", rr.Code)
	}
}

func TestCSRFMiddleware_ComposedAuthRejectsForgedAPIKeyHeaderWithSessionCookie(t *testing.T) {
	keyStore := auth.NewMemoryKeyStore()
	sessionStore := auth.NewMemorySessionStore()
	userStore := auth.NewMemoryUserStore()
	ctx := context.Background()

	user := &auth.User{
		ID:       "csrf-user",
		Username: "csrf-user",
		Role:     auth.RoleAdmin,
		IsActive: true,
	}
	if err := userStore.Create(ctx, user); err != nil {
		t.Fatalf("failed to create user: %v", err)
	}

	session := &auth.Session{
		ID:        "csrf-session",
		UserID:    user.ID,
		Role:      auth.RoleAdmin,
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(time.Hour),
	}
	if err := sessionStore.Create(ctx, session); err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	wrapped := CSRFMiddleware()(DualAuthMiddleware(keyStore, sessionStore, userStore, true, newTestLogger())(
		RequirePermissionMiddleware(auth.ResourcePools, auth.ActionCreate, newTestLogger())(
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				t.Fatal("handler should not be called without a CSRF token")
			}),
		),
	))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/pools", nil)
	req.Header.Set("Authorization", "Bearer cpam_forgedkey123abc")
	req.AddCookie(&http.Cookie{Name: "session", Value: session.ID})
	wrapped.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403 before auth when a forged bearer header is mixed with a session cookie, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestCSRFMiddleware_ComposedAuthAllowsValidAPIKeyWithoutCSRF(t *testing.T) {
	keyStore := auth.NewMemoryKeyStore()
	sessionStore := auth.NewMemorySessionStore()
	userStore := auth.NewMemoryUserStore()
	ctx := context.Background()

	plaintext, apiKey, err := auth.GenerateAPIKey(auth.GenerateAPIKeyOptions{
		Name:   "csrf-api-key",
		Scopes: []string{"pools:write"},
	})
	if err != nil {
		t.Fatalf("failed to generate API key: %v", err)
	}
	if err := keyStore.Create(ctx, apiKey); err != nil {
		t.Fatalf("failed to store API key: %v", err)
	}

	var called bool
	wrapped := CSRFMiddleware()(DualAuthMiddleware(keyStore, sessionStore, userStore, true, newTestLogger())(
		RequirePermissionMiddleware(auth.ResourcePools, auth.ActionCreate, newTestLogger())(
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				called = true
				w.WriteHeader(http.StatusOK)
			}),
		),
	))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/pools", nil)
	req.Header.Set("Authorization", "Bearer "+plaintext)
	wrapped.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 for valid API key without CSRF token, got %d: %s", rr.Code, rr.Body.String())
	}
	if !called {
		t.Fatal("handler should have been called for valid API key")
	}
}

func TestCSRFMiddleware_POSTToLoginBypassesCSRF(t *testing.T) {
	handler := CSRFMiddleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", nil)
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 for login bypass, got %d", rr.Code)
	}
}

func TestCSRFMiddleware_POSTToSetupBypassesCSRF(t *testing.T) {
	handler := CSRFMiddleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/setup", nil)
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 for setup bypass, got %d", rr.Code)
	}
}

func TestCSRFMiddleware_DELETERequiresCSRFToken(t *testing.T) {
	handler := CSRFMiddleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Without token - should fail
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/pools/1", nil)
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}

	// With valid token - should succeed
	token := "delete-csrf-token"
	rr2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodDelete, "/api/v1/pools/1", nil)
	req2.AddCookie(&http.Cookie{Name: csrfCookieName, Value: token})
	req2.Header.Set(csrfHeaderName, token)
	handler.ServeHTTP(rr2, req2)

	if rr2.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr2.Code)
	}
}

func TestCSRFMiddleware_PATCHRequiresCSRFToken(t *testing.T) {
	handler := CSRFMiddleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Without token - should fail
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/pools/1", nil)
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

func TestCSRFMiddleware_POSTWithCookieButNoHeaderReturns403(t *testing.T) {
	handler := CSRFMiddleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/pools", nil)
	req.AddCookie(&http.Cookie{Name: csrfCookieName, Value: "some-token"})
	// No X-CSRF-Token header
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rr.Code)
	}
}

func TestGenerateCSRFToken(t *testing.T) {
	token := generateCSRFToken()
	if len(token) != csrfTokenLength*2 { // hex encoding doubles length
		t.Fatalf("expected token length %d, got %d", csrfTokenLength*2, len(token))
	}

	// Ensure tokens are unique
	token2 := generateCSRFToken()
	if token == token2 {
		t.Fatal("expected unique tokens, got identical values")
	}
}
