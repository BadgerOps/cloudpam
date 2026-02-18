package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
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
