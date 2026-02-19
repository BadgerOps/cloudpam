package api

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"
)

const (
	csrfTokenLength = 32
	csrfHeaderName  = "X-CSRF-Token"
	csrfCookieName  = "csrf_token"
)

// CSRFMiddleware adds CSRF protection for session-authenticated state-changing requests.
// API key authenticated requests are exempt (no cookies = no CSRF risk).
// Login and setup endpoints are exempt (no session yet).
func CSRFMiddleware() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Skip for safe methods
			if r.Method == "GET" || r.Method == "HEAD" || r.Method == "OPTIONS" {
				// Set CSRF token cookie if not present
				if _, err := r.Cookie(csrfCookieName); err != nil {
					token := generateCSRFToken()
					http.SetCookie(w, &http.Cookie{
						Name:     csrfCookieName,
						Value:    token,
						Path:     "/",
						HttpOnly: false, // JS needs to read it
						Secure:   r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https",
						SameSite: http.SameSiteLaxMode,
					})
				}
				next.ServeHTTP(w, r)
				return
			}

			// For state-changing methods: skip CSRF check if using API key auth
			if authHeader := r.Header.Get("Authorization"); authHeader != "" && strings.HasPrefix(authHeader, "Bearer cpam_") {
				next.ServeHTTP(w, r)
				return
			}

			// Skip CSRF for login, setup, and OIDC endpoints (OIDC uses state parameter for security)
			if r.URL.Path == "/api/v1/auth/login" || r.URL.Path == "/api/v1/auth/setup" || strings.HasPrefix(r.URL.Path, "/api/v1/auth/oidc/") {
				next.ServeHTTP(w, r)
				return
			}

			// Validate CSRF token from header matches cookie
			cookie, err := r.Cookie(csrfCookieName)
			if err != nil {
				writeJSON(w, http.StatusForbidden, apiError{Error: "CSRF token missing", Detail: "csrf_token cookie required"})
				return
			}
			headerToken := r.Header.Get(csrfHeaderName)
			if headerToken == "" || headerToken != cookie.Value {
				writeJSON(w, http.StatusForbidden, apiError{Error: "CSRF token invalid", Detail: "X-CSRF-Token header must match csrf_token cookie"})
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func generateCSRFToken() string {
	b := make([]byte, csrfTokenLength)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
