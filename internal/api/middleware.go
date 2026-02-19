package api

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"net"
	"net/http"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"cloudpam/internal/auth"

	"github.com/getsentry/sentry-go"
	"github.com/google/uuid"
	"golang.org/x/time/rate"
)

const (
	requestIDHeader        = "X-Request-ID"
	maxRequestIDLength     = 64
	rateLimiterVisitorTTL  = 5 * time.Minute
	defaultRateLimitRPS    = 100.0
	defaultRateLimitBurst  = 200
	minimumCleanupInterval = 30 * time.Second

	// SECURITY TRADE-OFF: This cache balances database load against deactivation
	// responsiveness. A 30-second TTL means a deactivated user could retain access
	// for up to 30 seconds after deactivation. Shorter TTL = more DB hits per
	// request; longer TTL = wider security window after account deactivation.
	//
	// For deployments requiring instant revocation, set activeStatusCacheTTL to 0,
	// which checks the DB on every session-authenticated request.
	//
	// At 100 req/s with 30s cache: ~1 DB lookup per 30s per user
	// At 100 req/s with 0s cache:  ~100 DB lookups per second per user
	activeStatusCacheTTL = 30 * time.Second
)

// activeStatusEntry caches the result of a user active-status check.
type activeStatusEntry struct {
	isActive  bool
	checkedAt time.Time
}

// userActiveCache provides a short-TTL cache for user active-status lookups,
// avoiding a database hit on every session-authenticated request.
type userActiveCache struct {
	entries sync.Map // map[string]activeStatusEntry (keyed by user ID)
}

// check returns (isActive, cacheHit). If the cached entry is older than ttl,
// it returns cacheHit=false so the caller can refresh from the database.
func (c *userActiveCache) check(userID string, ttl time.Duration) (bool, bool) {
	val, ok := c.entries.Load(userID)
	if !ok {
		return false, false
	}
	entry := val.(activeStatusEntry)
	if ttl > 0 && time.Since(entry.checkedAt) > ttl {
		return false, false
	}
	// ttl == 0 means always miss (instant revocation mode)
	if ttl == 0 {
		return false, false
	}
	return entry.isActive, true
}

// set stores a cached active-status result for the given user ID.
func (c *userActiveCache) set(userID string, isActive bool) {
	c.entries.Store(userID, activeStatusEntry{
		isActive:  isActive,
		checkedAt: time.Now(),
	})
}

// Middleware represents an HTTP middleware that wraps a handler.
type Middleware func(http.Handler) http.Handler

// ApplyMiddlewares applies the provided middleware in order, where the first middleware
// in the list is the outermost handler.
func ApplyMiddlewares(h http.Handler, middlewares ...Middleware) http.Handler {
	for i := len(middlewares) - 1; i >= 0; i-- {
		h = middlewares[i](h)
	}
	return h
}

// RateLimitConfig configures the token bucket rate limiter.
type RateLimitConfig struct {
	RequestsPerSecond float64
	Burst             int
}

// Enabled reports whether rate limiting should be enforced.
func (c RateLimitConfig) Enabled() bool {
	return c.RequestsPerSecond > 0 && c.Burst > 0
}

// DefaultRateLimitConfig returns the default rate limiting configuration.
// It reads RATE_LIMIT_RPS and RATE_LIMIT_BURST from environment variables,
// falling back to 100 RPS and 200 burst if not set.
func DefaultRateLimitConfig() RateLimitConfig {
	rps := defaultRateLimitRPS
	burst := defaultRateLimitBurst

	if v := os.Getenv("RATE_LIMIT_RPS"); v != "" {
		if parsed, err := strconv.ParseFloat(v, 64); err == nil && parsed > 0 {
			rps = parsed
		}
	}
	if v := os.Getenv("RATE_LIMIT_BURST"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 {
			burst = parsed
		}
	}

	return RateLimitConfig{
		RequestsPerSecond: rps,
		Burst:             burst,
	}
}

// RequestIDMiddleware ensures every request carries a stable request ID.
func RequestIDMiddleware() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestID := sanitizeRequestID(r.Header.Get(requestIDHeader))
			if requestID == "" {
				requestID = uuid.New().String()
			}
			ctx := WithRequestID(r.Context(), requestID)
			r = r.WithContext(ctx)
			w.Header().Set(requestIDHeader, requestID)
			next.ServeHTTP(w, r)
		})
	}
}

func sanitizeRequestID(raw string) string {
	id := strings.TrimSpace(raw)
	if id == "" || len(id) > maxRequestIDLength {
		return ""
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '-', r == '_', r == '.':
		default:
			return ""
		}
	}
	return id
}

// LoggingMiddleware records structured request logs and wires Sentry tracing.
func LoggingMiddleware(logger *slog.Logger) Middleware {
	if logger == nil {
		logger = slog.Default()
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()

			hub := sentry.GetHubFromContext(ctx)
			if hub == nil {
				hub = sentry.CurrentHub().Clone()
				ctx = sentry.SetHubOnContext(ctx, hub)
				r = r.WithContext(ctx)
			}

			transaction := sentry.StartTransaction(
				ctx,
				fmt.Sprintf("%s %s", r.Method, r.URL.Path),
				sentry.WithOpName("http.server"),
				sentry.ContinueFromRequest(r),
				sentry.WithTransactionSource(sentry.SourceURL),
			)
			defer transaction.Finish()
			r = r.WithContext(transaction.Context())
			ctx = r.Context()

			hub.Scope().SetRequest(r)
			hub.Scope().SetContext("request", map[string]any{
				"url":    r.URL.String(),
				"method": r.Method,
			})

			start := time.Now()
			recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			var panicRecovered any

			defer func() {
				if rec := recover(); rec != nil {
					panicRecovered = rec
					transaction.Status = sentry.SpanStatusInternalError
					hub.RecoverWithContext(ctx, rec)
					attrs := appendRequestID(ctx, []any{
						"method", r.Method,
						"path", r.URL.Path,
					})
					attrs = append(attrs, "panic", rec)
					logger.ErrorContext(ctx, "panic recovered", attrs...)
					writeJSON(recorder, http.StatusInternalServerError, apiError{Error: "internal server error"})
				}
			}()

			next.ServeHTTP(recorder, r)

			if panicRecovered != nil {
				return
			}

			transaction.Status = sentry.HTTPtoSpanStatus(recorder.status)
			duration := time.Since(start)
			attrs := []any{
				"method", r.Method,
				"path", r.URL.Path,
				"status", recorder.status,
				"duration_ms", duration.Milliseconds(),
			}
			attrs = appendRequestID(r.Context(), attrs)

			switch {
			case recorder.status >= 500:
				logger.ErrorContext(r.Context(), "request completed", attrs...)
			case recorder.status >= 400:
				logger.WarnContext(r.Context(), "request completed", attrs...)
			default:
				logger.InfoContext(r.Context(), "request completed", attrs...)
			}
		})
	}
}

type clientLimiter struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// RateLimitMiddleware enforces per-client rate limiting using a token bucket.
// It adds the following headers to all responses:
//   - X-RateLimit-Limit: maximum requests per second
//   - X-RateLimit-Remaining: approximate remaining tokens
//   - X-RateLimit-Reset: Unix timestamp when a token will be available
//
// When the rate limit is exceeded, it returns 429 Too Many Requests with a Retry-After header.
func RateLimitMiddleware(cfg RateLimitConfig, logger *slog.Logger) Middleware {
	if !cfg.Enabled() {
		return func(next http.Handler) http.Handler { return next }
	}
	if logger == nil {
		logger = slog.Default()
	}

	var (
		mu          sync.Mutex
		visitors    = make(map[string]*clientLimiter)
		lastCleanup time.Time
	)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			now := time.Now()
			key := clientKey(r)

			mu.Lock()
			v, ok := visitors[key]
			if !ok {
				v = &clientLimiter{
					limiter:  rate.NewLimiter(rate.Limit(cfg.RequestsPerSecond), cfg.Burst),
					lastSeen: now,
				}
				visitors[key] = v
			} else {
				v.lastSeen = now
			}

			if lastCleanup.IsZero() || now.Sub(lastCleanup) > minimumCleanupInterval {
				for k, limiter := range visitors {
					if now.Sub(limiter.lastSeen) > rateLimiterVisitorTTL {
						delete(visitors, k)
					}
				}
				lastCleanup = now
			}
			mu.Unlock()

			// Set rate limit headers
			w.Header().Set("X-RateLimit-Limit", strconv.FormatFloat(cfg.RequestsPerSecond, 'f', -1, 64))

			// Calculate remaining tokens using Tokens() which returns float64
			tokens := v.limiter.Tokens()
			remaining := int(math.Floor(tokens))
			if remaining < 0 {
				remaining = 0
			}
			w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(remaining))

			// Reset time: when the next token will be available
			resetTime := now.Add(time.Duration(float64(time.Second) / cfg.RequestsPerSecond))
			w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(resetTime.Unix(), 10))

			if !v.limiter.AllowN(now, 1) {
				attrs := appendRequestID(r.Context(), []any{
					"method", r.Method,
					"path", r.URL.Path,
					"status", http.StatusTooManyRequests,
				})
				logger.WarnContext(r.Context(), "rate limit exceeded", attrs...)
				// Calculate retry-after in seconds
				retryAfter := int(math.Ceil(1 / cfg.RequestsPerSecond))
				if retryAfter < 1 {
					retryAfter = 1
				}
				w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
				writeJSON(w, http.StatusTooManyRequests, apiError{Error: "too many requests"})
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func clientKey(r *http.Request) string {
	return clientKeyWithProxies(r, nil)
}

// TrustedProxyConfig holds trusted proxy CIDR list for X-Forwarded-For handling.
type TrustedProxyConfig struct {
	CIDRs []netip.Prefix
}

// ParseTrustedProxies parses a comma-separated list of CIDRs.
func ParseTrustedProxies(raw string) (*TrustedProxyConfig, error) {
	if raw == "" {
		return &TrustedProxyConfig{}, nil
	}
	var cidrs []netip.Prefix
	for _, s := range strings.Split(raw, ",") {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		prefix, err := netip.ParsePrefix(s)
		if err != nil {
			return nil, fmt.Errorf("invalid trusted proxy CIDR %q: %w", s, err)
		}
		cidrs = append(cidrs, prefix)
	}
	return &TrustedProxyConfig{CIDRs: cidrs}, nil
}

// IsTrusted checks if the remote address is from a trusted proxy.
func (tc *TrustedProxyConfig) IsTrusted(remoteAddr string) bool {
	if tc == nil || len(tc.CIDRs) == 0 {
		return false
	}
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return false
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	for _, cidr := range tc.CIDRs {
		if cidr.Contains(addr) {
			return true
		}
	}
	return false
}

// clientKeyWithProxies extracts the client IP, only trusting X-Forwarded-For from trusted proxies.
func clientKeyWithProxies(r *http.Request, proxies *TrustedProxyConfig) string {
	if proxies != nil && proxies.IsTrusted(r.RemoteAddr) {
		if xff := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); xff != "" {
			parts := strings.SplitN(xff, ",", 2)
			if ip := strings.TrimSpace(parts[0]); ip != "" {
				return ip
			}
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// LoginRateLimitConfig configures per-IP login rate limiting.
type LoginRateLimitConfig struct {
	AttemptsPerMinute int
	ProxyConfig       *TrustedProxyConfig
}

// LoginRateLimitMiddleware wraps a handler with per-IP login rate limiting.
func LoginRateLimitMiddleware(cfg LoginRateLimitConfig) func(http.Handler) http.Handler {
	type ipEntry struct {
		limiter  *rate.Limiter
		lastSeen time.Time
	}
	var mu sync.Mutex
	clients := make(map[string]*ipEntry)

	go func() {
		for {
			time.Sleep(5 * time.Minute)
			mu.Lock()
			for ip, entry := range clients {
				if time.Since(entry.lastSeen) > 10*time.Minute {
					delete(clients, ip)
				}
			}
			mu.Unlock()
		}
	}()

	rps := rate.Limit(float64(cfg.AttemptsPerMinute) / 60.0)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := clientKeyWithProxies(r, cfg.ProxyConfig)

			mu.Lock()
			entry, ok := clients[ip]
			if !ok {
				entry = &ipEntry{limiter: rate.NewLimiter(rps, cfg.AttemptsPerMinute)}
				clients[ip] = entry
			}
			entry.lastSeen = time.Now()
			mu.Unlock()

			if !entry.limiter.Allow() {
				w.Header().Set("Retry-After", "60")
				writeJSON(w, http.StatusTooManyRequests, apiError{Error: "too many login attempts", Detail: "try again later"})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// AuthMiddleware validates API key authentication.
// If required is true, requests without valid authentication will receive 401.
// If required is false, authentication is optional but will be validated if present.
//
// The middleware:
// 1. Extracts the API key from the Authorization: Bearer header
// 2. Validates the key format and looks it up by prefix
// 3. Verifies the key hash, expiration, and revocation status
// 4. Stores the authenticated key in the request context
// 5. Updates the key's last used timestamp on successful authentication
func AuthMiddleware(keyStore auth.KeyStore, required bool, logger *slog.Logger) Middleware {
	if logger == nil {
		logger = slog.Default()
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()

			// Extract Authorization header
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				if required {
					logAuthFailure(logger, r, "missing authorization header")
					writeJSON(w, http.StatusUnauthorized, apiError{Error: "unauthorized", Detail: "missing authorization header"})
					return
				}
				// Optional auth, proceed without authentication
				next.ServeHTTP(w, r)
				return
			}

			// Parse Bearer token
			if !strings.HasPrefix(authHeader, "Bearer ") {
				if required {
					logAuthFailure(logger, r, "invalid authorization format")
					writeJSON(w, http.StatusUnauthorized, apiError{Error: "unauthorized", Detail: "invalid authorization format"})
					return
				}
				next.ServeHTTP(w, r)
				return
			}

			apiKey := strings.TrimPrefix(authHeader, "Bearer ")
			apiKey = strings.TrimSpace(apiKey)

			// Validate key format and extract prefix (before database lookup)
			prefix, err := auth.ParseAPIKeyPrefix(apiKey)
			if err != nil {
				logAuthFailure(logger, r, "invalid API key format")
				writeJSON(w, http.StatusUnauthorized, apiError{Error: "unauthorized", Detail: "invalid API key format"})
				return
			}

			// Look up key by prefix
			storedKey, err := keyStore.GetByPrefix(ctx, prefix)
			if err != nil {
				logAuthError(logger, r, "key store error", err)
				writeJSON(w, http.StatusInternalServerError, apiError{Error: "internal error"})
				return
			}

			if storedKey == nil {
				logAuthFailure(logger, r, "API key not found")
				writeJSON(w, http.StatusUnauthorized, apiError{Error: "unauthorized", Detail: "invalid API key"})
				return
			}

			// Validate the key
			if err := auth.ValidateAPIKey(apiKey, storedKey); err != nil {
				switch err {
				case auth.ErrKeyRevoked:
					logAuthFailure(logger, r, "API key revoked")
					writeJSON(w, http.StatusUnauthorized, apiError{Error: "unauthorized", Detail: "API key has been revoked"})
				case auth.ErrKeyExpired:
					logAuthFailure(logger, r, "API key expired")
					writeJSON(w, http.StatusUnauthorized, apiError{Error: "unauthorized", Detail: "API key has expired"})
				case auth.ErrInvalidKey:
					logAuthFailure(logger, r, "invalid API key")
					writeJSON(w, http.StatusUnauthorized, apiError{Error: "unauthorized", Detail: "invalid API key"})
				default:
					logAuthError(logger, r, "key validation error", err)
					writeJSON(w, http.StatusInternalServerError, apiError{Error: "internal error"})
				}
				return
			}

			// Update last used timestamp (non-blocking, don't fail request if this errors)
			// Use background context since update should complete even if request is cancelled.
			// Capture the key ID to avoid race with context modification below.
			keyID := storedKey.ID
			go func() {
				_ = keyStore.UpdateLastUsed(context.Background(), keyID, time.Now())
			}()

			// Store authenticated key in context
			ctx = auth.ContextWithAPIKey(ctx, storedKey)
			r = r.WithContext(ctx)

			// Log successful authentication (only prefix, never full key)
			attrs := appendRequestID(ctx, []any{
				"method", r.Method,
				"path", r.URL.Path,
				"api_key_id", storedKey.ID,
				"api_key_prefix", storedKey.Prefix,
				"api_key_name", storedKey.Name,
			})
			logger.DebugContext(ctx, "authenticated request", attrs...)

			next.ServeHTTP(w, r)
		})
	}
}

// DualAuthMiddleware validates both session-based and API key authentication.
// It tries these strategies in order:
// 1. "session" cookie -> session lookup
// 2. Authorization: Bearer with "cpam_" prefix -> API key (existing flow)
// If required is true, unauthenticated requests get 401.
// Non-cpam_ Bearer tokens are rejected (session IDs must not appear in headers).
func DualAuthMiddleware(
	keyStore auth.KeyStore,
	sessionStore auth.SessionStore,
	userStore auth.UserStore,
	required bool,
	logger *slog.Logger,
) Middleware {
	if logger == nil {
		logger = slog.Default()
	}

	activeCache := &userActiveCache{}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()

			// Strategy 1: Check session cookie
			if cookie, err := r.Cookie("session"); err == nil && cookie.Value != "" {
				session, err := sessionStore.Get(ctx, cookie.Value)
				if err == nil && session != nil && session.IsValid() {
					// Check if the user's account is still active (cached).
					isActive, cacheHit := activeCache.check(session.UserID, activeStatusCacheTTL)
					if !cacheHit {
						// Cache miss or expired — fetch from DB.
						user, _ := userStore.GetByID(ctx, session.UserID)
						if user == nil {
							writeJSON(w, http.StatusUnauthorized, apiError{Error: "account disabled"})
							return
						}
						isActive = user.IsActive
						activeCache.set(session.UserID, isActive)
						if !isActive {
							writeJSON(w, http.StatusUnauthorized, apiError{Error: "account disabled"})
							return
						}
						ctx = auth.ContextWithSession(ctx, session)
						ctx = auth.ContextWithRole(ctx, session.Role)
						ctx = auth.ContextWithUser(ctx, user)
					} else {
						if !isActive {
							writeJSON(w, http.StatusUnauthorized, apiError{Error: "account disabled"})
							return
						}
						ctx = auth.ContextWithSession(ctx, session)
						ctx = auth.ContextWithRole(ctx, session.Role)
						if user, _ := userStore.GetByID(ctx, session.UserID); user != nil {
							ctx = auth.ContextWithUser(ctx, user)
						}
					}
					r = r.WithContext(ctx)
					next.ServeHTTP(w, r)
					return
				}
			}

			// Strategy 2: Check Authorization header
			authHeader := r.Header.Get("Authorization")
			if authHeader != "" && strings.HasPrefix(authHeader, "Bearer ") {
				token := strings.TrimPrefix(authHeader, "Bearer ")
				token = strings.TrimSpace(token)

				if strings.HasPrefix(token, "cpam_") {
					// Strategy 2: API key authentication
					prefix, err := auth.ParseAPIKeyPrefix(token)
					if err != nil {
						if required {
							logAuthFailure(logger, r, "invalid API key format")
							writeJSON(w, http.StatusUnauthorized, apiError{Error: "unauthorized", Detail: "invalid API key format"})
							return
						}
						next.ServeHTTP(w, r)
						return
					}

					storedKey, err := keyStore.GetByPrefix(ctx, prefix)
					if err != nil {
						logAuthError(logger, r, "key store error", err)
						writeJSON(w, http.StatusInternalServerError, apiError{Error: "internal error"})
						return
					}
					if storedKey == nil {
						if required {
							logAuthFailure(logger, r, "API key not found")
							writeJSON(w, http.StatusUnauthorized, apiError{Error: "unauthorized", Detail: "invalid API key"})
							return
						}
						next.ServeHTTP(w, r)
						return
					}

					if err := auth.ValidateAPIKey(token, storedKey); err != nil {
						switch err {
						case auth.ErrKeyRevoked:
							logAuthFailure(logger, r, "API key revoked")
							writeJSON(w, http.StatusUnauthorized, apiError{Error: "unauthorized", Detail: "API key has been revoked"})
						case auth.ErrKeyExpired:
							logAuthFailure(logger, r, "API key expired")
							writeJSON(w, http.StatusUnauthorized, apiError{Error: "unauthorized", Detail: "API key has expired"})
						case auth.ErrInvalidKey:
							logAuthFailure(logger, r, "invalid API key")
							writeJSON(w, http.StatusUnauthorized, apiError{Error: "unauthorized", Detail: "invalid API key"})
						default:
							logAuthError(logger, r, "key validation error", err)
							writeJSON(w, http.StatusInternalServerError, apiError{Error: "internal error"})
						}
						return
					}

					keyID := storedKey.ID
					go func() {
						_ = keyStore.UpdateLastUsed(context.Background(), keyID, time.Now())
					}()

					ctx = auth.ContextWithAPIKey(ctx, storedKey)
					r = r.WithContext(ctx)
					next.ServeHTTP(w, r)
					return
				}

				// Unrecognized Bearer token format - reject
				if required {
					logAuthFailure(logger, r, "invalid bearer token format")
					writeJSON(w, http.StatusUnauthorized, apiError{Error: "invalid bearer token", Detail: "bearer tokens must be API keys (cpam_ prefix)"})
					return
				}
			}

			if required {
				logAuthFailure(logger, r, "missing authentication")
				writeJSON(w, http.StatusUnauthorized, apiError{Error: "unauthorized", Detail: "missing authentication"})
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// RequireScopeMiddleware checks that the authenticated API key has the required scope.
// This middleware must be used after AuthMiddleware.
// Returns 403 Forbidden if the scope is missing.
func RequireScopeMiddleware(scope string, logger *slog.Logger) Middleware {
	if logger == nil {
		logger = slog.Default()
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()

			key := auth.APIKeyFromContext(ctx)
			if key == nil {
				// Not authenticated - let AuthMiddleware handle this
				writeJSON(w, http.StatusUnauthorized, apiError{Error: "unauthorized"})
				return
			}

			if !key.HasScope(scope) {
				attrs := appendRequestID(ctx, []any{
					"method", r.Method,
					"path", r.URL.Path,
					"api_key_id", key.ID,
					"required_scope", scope,
					"key_scopes", key.Scopes,
				})
				logger.WarnContext(ctx, "insufficient scope", attrs...)
				writeJSON(w, http.StatusForbidden, apiError{
					Error:  "forbidden",
					Detail: fmt.Sprintf("missing required scope: %s", scope),
				})
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// RequireAnyScopeMiddleware checks that the authenticated API key has at least one of the required scopes.
// This middleware must be used after AuthMiddleware.
// Returns 403 Forbidden if no matching scope is found.
func RequireAnyScopeMiddleware(scopes []string, logger *slog.Logger) Middleware {
	if logger == nil {
		logger = slog.Default()
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()

			key := auth.APIKeyFromContext(ctx)
			if key == nil {
				writeJSON(w, http.StatusUnauthorized, apiError{Error: "unauthorized"})
				return
			}

			if !key.HasAnyScope(scopes...) {
				attrs := appendRequestID(ctx, []any{
					"method", r.Method,
					"path", r.URL.Path,
					"api_key_id", key.ID,
					"required_scopes", scopes,
					"key_scopes", key.Scopes,
				})
				logger.WarnContext(ctx, "insufficient scope", attrs...)
				writeJSON(w, http.StatusForbidden, apiError{
					Error:  "forbidden",
					Detail: fmt.Sprintf("missing required scope: one of %v", scopes),
				})
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func logAuthFailure(logger *slog.Logger, r *http.Request, reason string) {
	attrs := appendRequestID(r.Context(), []any{
		"method", r.Method,
		"path", r.URL.Path,
		"reason", reason,
	})
	logger.WarnContext(r.Context(), "authentication failed", attrs...)
}

func logAuthError(logger *slog.Logger, r *http.Request, msg string, err error) {
	attrs := appendRequestID(r.Context(), []any{
		"method", r.Method,
		"path", r.URL.Path,
		"error", err.Error(),
	})
	logger.ErrorContext(r.Context(), msg, attrs...)
}

// AuditMiddleware captures audit events for mutating requests (POST, PATCH, DELETE).
// It extracts actor information from the auth context and logs events after the response.
// GET requests and health/metrics endpoints are not audited.
func AuditMiddleware(auditLogger AuditLogger, logger *slog.Logger) Middleware {
	if auditLogger == nil {
		return func(next http.Handler) http.Handler { return next }
	}
	if logger == nil {
		logger = slog.Default()
	}

	// Paths to skip auditing
	skipPaths := map[string]bool{
		"/healthz":      true,
		"/readyz":       true,
		"/metrics":      true,
		"/openapi.yaml": true,
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()

			// Skip GET requests and health endpoints
			if r.Method == http.MethodGet || skipPaths[r.URL.Path] {
				next.ServeHTTP(w, r)
				return
			}

			// Capture response status
			recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

			// Call the next handler
			next.ServeHTTP(recorder, r)

			// Determine resource type and action from path and method
			resourceType, resourceID := parseResourceFromPath(r.URL.Path)
			if resourceType == "" {
				// Not a resource we track
				return
			}

			action := methodToAction(r.Method)
			if action == "" {
				return
			}

			// Extract actor from auth context (prefer user over API key)
			actor := "anonymous"
			actorType := "anonymous"
			if user := auth.UserFromContext(ctx); user != nil {
				actor = user.Username
				actorType = "user"
			} else if key := auth.APIKeyFromContext(ctx); key != nil {
				actor = key.Prefix
				actorType = "api_key"
			}

			// Create audit event
			event := &AuditEvent{
				Actor:        actor,
				ActorType:    actorType,
				Action:       action,
				ResourceType: resourceType,
				ResourceID:   resourceID,
				RequestID:    RequestIDFromContext(ctx),
				IPAddress:    clientKey(r),
				StatusCode:   recorder.status,
			}

			// Log the audit event
			if err := auditLogger.Log(ctx, event); err != nil {
				attrs := appendRequestID(ctx, []any{
					"error", err.Error(),
					"resource_type", resourceType,
					"resource_id", resourceID,
					"action", action,
				})
				logger.ErrorContext(ctx, "failed to log audit event", attrs...)
			}
		})
	}
}

// AuditLogger is the interface for audit logging.
// This is defined here to avoid import cycles with internal/audit.
type AuditLogger interface {
	Log(ctx context.Context, event *AuditEvent) error
}

// AuditEvent represents an audit event for the middleware.
// This mirrors the audit.AuditEvent type to avoid import cycles.
type AuditEvent struct {
	ID           string
	Timestamp    time.Time
	Actor        string
	ActorType    string
	Action       string
	ResourceType string
	ResourceID   string
	ResourceName string
	Changes      *AuditChanges
	RequestID    string
	IPAddress    string
	StatusCode   int
}

// AuditChanges captures before/after state for updates.
type AuditChanges struct {
	Before map[string]any
	After  map[string]any
}

// parseResourceFromPath extracts resource type and ID from a URL path.
func parseResourceFromPath(path string) (resourceType, resourceID string) {
	// Match patterns like:
	// /api/v1/pools -> pools, ""
	// /api/v1/pools/123 -> pools, "123"
	// /api/v1/accounts -> accounts, ""
	// /api/v1/accounts/456 -> accounts, "456"
	// /api/v1/auth/keys -> api_keys, ""
	// /api/v1/auth/keys/abc -> api_keys, "abc"

	parts := strings.Split(strings.Trim(path, "/"), "/")

	if len(parts) < 3 || parts[0] != "api" || parts[1] != "v1" {
		return "", ""
	}

	switch parts[2] {
	case "pools":
		if len(parts) >= 4 && parts[3] != "" {
			// Skip /pools/{id}/blocks
			if len(parts) >= 5 && parts[4] == "blocks" {
				return "", ""
			}
			return "pool", parts[3]
		}
		return "pool", ""
	case "accounts":
		if len(parts) >= 4 && parts[3] != "" {
			return "account", parts[3]
		}
		return "account", ""
	case "auth":
		if len(parts) >= 4 {
			switch parts[3] {
			case "keys":
				if len(parts) >= 5 && parts[4] != "" {
					return "api_key", parts[4]
				}
				return "api_key", ""
			case "users":
				if len(parts) >= 5 && parts[4] != "" {
					return "user", parts[4]
				}
				return "user", ""
			case "login":
				return "session", ""
			case "logout":
				return "session", ""
			}
		}
	}

	return "", ""
}

// methodToAction maps HTTP methods to audit actions.
func methodToAction(method string) string {
	switch method {
	case http.MethodPost:
		return "create"
	case http.MethodPatch, http.MethodPut:
		return "update"
	case http.MethodDelete:
		return "delete"
	default:
		return ""
	}
}

// RequirePermissionMiddleware returns middleware that checks for a specific RBAC permission.
// It verifies that the authenticated user has the required permission for the resource and action.
// Must be used after AuthMiddleware.
//
// If the user lacks the required permission, returns 403 Forbidden.
// Authorization failures are logged for auditing purposes.
func RequirePermissionMiddleware(resource, action string, logger *slog.Logger) Middleware {
	if logger == nil {
		logger = slog.Default()
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()

			// Get the effective role from context
			role := auth.GetEffectiveRole(ctx)
			if role == auth.RoleNone {
				// No role means not authenticated or no valid scopes
				writeJSON(w, http.StatusUnauthorized, apiError{Error: "unauthorized"})
				return
			}

			// Check permission
			if !auth.HasPermission(role, resource, action) {
				// Log authorization failure
				attrs := appendRequestID(ctx, []any{
					"method", r.Method,
					"path", r.URL.Path,
					"role", string(role),
					"required_resource", resource,
					"required_action", action,
				})

				// Include API key info if available
				if key := auth.APIKeyFromContext(ctx); key != nil {
					attrs = append(attrs, "api_key_id", key.ID)
					attrs = append(attrs, "api_key_prefix", key.Prefix)
				}

				logger.WarnContext(ctx, "authorization denied", attrs...)

				// Return generic forbidden message (don't leak permission details)
				writeJSON(w, http.StatusForbidden, apiError{Error: "forbidden"})
				return
			}

			// Permission granted, continue
			next.ServeHTTP(w, r)
		})
	}
}

// RequireAnyPermissionMiddleware returns middleware that checks for any of the specified permissions.
// If the user has at least one of the required permissions, access is granted.
// Must be used after AuthMiddleware.
func RequireAnyPermissionMiddleware(permissions []auth.Permission, logger *slog.Logger) Middleware {
	if logger == nil {
		logger = slog.Default()
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()

			role := auth.GetEffectiveRole(ctx)
			if role == auth.RoleNone {
				writeJSON(w, http.StatusUnauthorized, apiError{Error: "unauthorized"})
				return
			}

			// Check if any permission is granted
			for _, perm := range permissions {
				if auth.HasPermission(role, perm.Resource, perm.Action) {
					next.ServeHTTP(w, r)
					return
				}
			}

			// No permissions matched - log and deny
			permStrings := make([]string, len(permissions))
			for i, p := range permissions {
				permStrings[i] = p.String()
			}

			attrs := appendRequestID(ctx, []any{
				"method", r.Method,
				"path", r.URL.Path,
				"role", string(role),
				"required_permissions", permStrings,
			})

			if key := auth.APIKeyFromContext(ctx); key != nil {
				attrs = append(attrs, "api_key_id", key.ID)
				attrs = append(attrs, "api_key_prefix", key.Prefix)
			}

			logger.WarnContext(ctx, "authorization denied", attrs...)
			writeJSON(w, http.StatusForbidden, apiError{Error: "forbidden"})
		})
	}
}

// RequireRoleMiddleware returns middleware that checks for a minimum role level.
// It verifies that the authenticated user has at least the specified role.
// Must be used after AuthMiddleware.
func RequireRoleMiddleware(minRole auth.Role, logger *slog.Logger) Middleware {
	if logger == nil {
		logger = slog.Default()
	}

	// Define role hierarchy (higher index = higher privilege)
	roleLevel := map[auth.Role]int{
		auth.RoleNone:     0,
		auth.RoleAuditor:  1,
		auth.RoleViewer:   2,
		auth.RoleOperator: 3,
		auth.RoleAdmin:    4,
	}

	minLevel := roleLevel[minRole]

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()

			role := auth.GetEffectiveRole(ctx)
			if role == auth.RoleNone {
				writeJSON(w, http.StatusUnauthorized, apiError{Error: "unauthorized"})
				return
			}

			currentLevel := roleLevel[role]
			if currentLevel < minLevel {
				attrs := appendRequestID(ctx, []any{
					"method", r.Method,
					"path", r.URL.Path,
					"role", string(role),
					"required_role", string(minRole),
				})

				if key := auth.APIKeyFromContext(ctx); key != nil {
					attrs = append(attrs, "api_key_id", key.ID)
					attrs = append(attrs, "api_key_prefix", key.Prefix)
				}

				logger.WarnContext(ctx, "insufficient role", attrs...)
				writeJSON(w, http.StatusForbidden, apiError{Error: "forbidden"})
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
