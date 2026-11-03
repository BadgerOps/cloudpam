package api

import (
	"context"
	"log/slog"
	"math"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"cloudpam/internal/auth"

	"golang.org/x/time/rate"
)

// RateLimitConfig is the server-wide token bucket limit. It is the same shape
// an API key stores as its own limit.
type RateLimitConfig = auth.RateLimit

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

// bucketKey names one token bucket. The prefix keeps an API key id, a user
// id and an address from ever naming the same bucket.
type bucketKey string

func ipBucket(host string) bucketKey   { return bucketKey("ip:" + host) }
func apiKeyBucket(id string) bucketKey { return bucketKey("key:" + id) }
func userBucket(id string) bucketKey   { return bucketKey("user:" + id) }

type bucket struct {
	limiter  *rate.Limiter
	limit    auth.RateLimit
	lastSeen time.Time
}

// rateLimiter owns every bucket. A bucket is rebuilt when its limit changes
// and dropped after rateLimiterVisitorTTL without a request.
type rateLimiter struct {
	defaults RateLimitConfig
	logger   *slog.Logger

	mu        sync.Mutex
	buckets   map[bucketKey]*bucket
	lastSweep time.Time
}

func newRateLimiter(defaults RateLimitConfig, logger *slog.Logger) *rateLimiter {
	if logger == nil {
		logger = slog.Default()
	}
	return &rateLimiter{defaults: defaults, logger: logger, buckets: make(map[bucketKey]*bucket)}
}

func (l *rateLimiter) bucket(now time.Time, key bucketKey, limit auth.RateLimit) *rate.Limiter {
	l.mu.Lock()
	defer l.mu.Unlock()

	b, ok := l.buckets[key]
	if !ok || b.limit != limit {
		b = &bucket{limiter: rate.NewLimiter(rate.Limit(limit.RequestsPerSecond), limit.Burst), limit: limit}
		l.buckets[key] = b
	}
	b.lastSeen = now

	if l.lastSweep.IsZero() || now.Sub(l.lastSweep) > minimumCleanupInterval {
		for k, stale := range l.buckets {
			if now.Sub(stale.lastSeen) > rateLimiterVisitorTTL {
				delete(l.buckets, k)
			}
		}
		l.lastSweep = now
	}
	return b.limiter
}

// decision is one bucket's answer for one request.
type decision struct {
	allowed   bool
	limit     auth.RateLimit
	remaining int
	resetAt   time.Time
}

// take charges one token to key's bucket. A limit that is not enabled admits
// everything and keeps no bucket.
func (l *rateLimiter) take(now time.Time, key bucketKey, limit auth.RateLimit) decision {
	return l.decide(now, key, limit, true)
}

// peek reports whether key's bucket has a token without taking it.
func (l *rateLimiter) peek(now time.Time, key bucketKey, limit auth.RateLimit) decision {
	return l.decide(now, key, limit, false)
}

func (l *rateLimiter) decide(now time.Time, key bucketKey, limit auth.RateLimit, consume bool) decision {
	if !limit.Enabled() {
		return decision{allowed: true}
	}
	lim := l.bucket(now, key, limit)
	d := decision{limit: limit, resetAt: now.Add(time.Duration(float64(time.Second) / limit.RequestsPerSecond))}
	if consume {
		d.allowed = lim.AllowN(now, 1)
	} else {
		d.allowed = lim.TokensAt(now) >= 1
	}
	d.remaining = int(math.Floor(lim.TokensAt(now)))
	if d.remaining < 0 {
		d.remaining = 0
	}
	return d
}

func (d decision) retryAfterSeconds() int {
	s := int(math.Ceil(1 / d.limit.RequestsPerSecond))
	if s < 1 {
		return 1
	}
	return s
}

// writeHeaders sets the X-RateLimit-* headers for the bucket that answered.
func (d decision) writeHeaders(w http.ResponseWriter) {
	if !d.limit.Enabled() {
		return
	}
	w.Header().Set("X-RateLimit-Limit", strconv.FormatFloat(d.limit.RequestsPerSecond, 'f', -1, 64))
	w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(d.remaining))
	w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(d.resetAt.Unix(), 10))
}

func (l *rateLimiter) reject(w http.ResponseWriter, r *http.Request, key bucketKey, d decision) {
	attrs := appendRequestID(r.Context(), []any{
		"method", r.Method,
		"path", r.URL.Path,
		"status", http.StatusTooManyRequests,
		"bucket", string(key),
	})
	l.logger.WarnContext(r.Context(), "rate limit exceeded", attrs...)
	w.Header().Set("Retry-After", strconv.Itoa(d.retryAfterSeconds()))
	writeJSON(w, http.StatusTooManyRequests, apiError{Error: "too many requests"})
}

// principalClaim is the outer limiter's hand-off for a request that carries
// credentials. The auth middleware charges the principal's bucket through it
// once the credential validates; a request nobody claimed is charged to the
// IP afterwards, so a rejected credential still costs the address a token.
type principalClaim struct {
	limiter *rateLimiter
	charged bool
}

const principalClaimContextKey contextKey = "rateLimitClaim"

// hasCredentials reports whether the request carries something the auth
// middleware could turn into a principal.
func hasCredentials(r *http.Request) bool {
	if r.Header.Get("Authorization") != "" {
		return true
	}
	c, err := r.Cookie("session")
	return err == nil && c.Value != ""
}

// RateLimitMiddleware enforces a token bucket per principal. A request with
// no credentials is charged to its client address. A request with
// credentials is charged to the API key or user that the auth middleware
// resolves, under the key's own limit when it has one; its address only has
// to hold a token, and pays one if the credential is rejected. Every limited
// response carries X-RateLimit-Limit, X-RateLimit-Remaining and
// X-RateLimit-Reset, and a 429 carries Retry-After.
//
// With cfg disabled, addresses and users are not limited, but an API key with
// its own limit still is.
func RateLimitMiddleware(cfg RateLimitConfig, logger *slog.Logger) Middleware {
	l := newRateLimiter(cfg, logger)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			now := time.Now()
			ip := ipBucket(clientKey(r))

			if !hasCredentials(r) {
				d := l.take(now, ip, cfg)
				d.writeHeaders(w)
				if !d.allowed {
					l.reject(w, r, ip, d)
					return
				}
				next.ServeHTTP(w, r)
				return
			}

			d := l.peek(now, ip, cfg)
			d.writeHeaders(w)
			if !d.allowed {
				l.reject(w, r, ip, d)
				return
			}
			claim := &principalClaim{limiter: l}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), principalClaimContextKey, claim)))
			if !claim.charged {
				l.take(time.Now(), ip, cfg)
			}
		})
	}
}

// allowPrincipal charges the authenticated principal's bucket and writes 429
// when it is empty. It reports whether the request may proceed. Without a
// claim in the context there is no limiter in front of this handler, and the
// request proceeds.
func allowPrincipal(w http.ResponseWriter, r *http.Request) bool {
	claim, _ := r.Context().Value(principalClaimContextKey).(*principalClaim)
	if claim == nil {
		return true
	}
	key, limit, ok := principalBucket(r.Context(), claim.limiter.defaults)
	if !ok {
		return true
	}
	claim.charged = true
	d := claim.limiter.take(time.Now(), key, limit)
	d.writeHeaders(w)
	if !d.allowed {
		claim.limiter.reject(w, r, key, d)
		return false
	}
	return true
}

// principalBucket names the bucket an authenticated request is charged to
// and the limit it runs under.
func principalBucket(ctx context.Context, defaults RateLimitConfig) (bucketKey, auth.RateLimit, bool) {
	if k := auth.APIKeyFromContext(ctx); k != nil {
		if k.RateLimit != nil {
			return apiKeyBucket(k.ID), *k.RateLimit, true
		}
		return apiKeyBucket(k.ID), defaults, true
	}
	if s := auth.SessionFromContext(ctx); s != nil {
		return userBucket(s.UserID), defaults, true
	}
	return "", auth.RateLimit{}, false
}
