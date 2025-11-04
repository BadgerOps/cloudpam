package http

import (
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/google/uuid"
	"golang.org/x/time/rate"
)

const (
	requestIDHeader        = "X-Request-ID"
	maxRequestIDLength     = 64
	rateLimiterVisitorTTL  = 5 * time.Minute
	defaultRateLimitPerMin = 100.0
	defaultRateLimitBurst  = 20
	minimumCleanupInterval = 30 * time.Second
)

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
func DefaultRateLimitConfig() RateLimitConfig {
	return RateLimitConfig{
		RequestsPerSecond: defaultRateLimitPerMin / 60.0,
		Burst:             defaultRateLimitBurst,
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

			if !v.limiter.AllowN(now, 1) {
				attrs := appendRequestID(r.Context(), []any{
					"method", r.Method,
					"path", r.URL.Path,
					"status", http.StatusTooManyRequests,
				})
				logger.WarnContext(r.Context(), "rate limit exceeded", attrs...)
				w.Header().Set("Retry-After", "1")
				writeJSON(w, http.StatusTooManyRequests, apiError{Error: "too many requests"})
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func clientKey(r *http.Request) string {
	if xff := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); xff != "" {
		if idx := strings.Index(xff, ","); idx != -1 {
			xff = xff[:idx]
		}
		if ip := strings.TrimSpace(xff); ip != "" {
			return ip
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil && host != "" {
		return host
	}
	return r.RemoteAddr
}
