package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"cloudpam/internal/auth"
)

// principalLimitedHandler is the production order: the limiter outside, the
// auth middleware inside, a 200 handler behind both.
func principalLimitedHandler(t *testing.T, cfg RateLimitConfig, keyStore auth.KeyStore, sessionStore auth.SessionStore, userStore auth.UserStore) http.Handler {
	t.Helper()
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	return RateLimitMiddleware(cfg, newTestLogger())(DualAuthMiddleware(keyStore, sessionStore, userStore, true, newTestLogger())(ok))
}

func mintKey(t *testing.T, keyStore auth.KeyStore, name string, limit *auth.RateLimit) string {
	t.Helper()
	plaintext, key, err := auth.GenerateAPIKey(auth.GenerateAPIKeyOptions{Name: name, Scopes: []string{"pools:read"}, RateLimit: limit})
	if err != nil {
		t.Fatalf("GenerateAPIKey: %v", err)
	}
	if err := keyStore.Create(context.Background(), key); err != nil {
		t.Fatalf("Create key: %v", err)
	}
	return plaintext
}

func mintSession(t *testing.T, sessionStore auth.SessionStore, userStore auth.UserStore, sessionID, userID string) {
	t.Helper()
	ctx := context.Background()
	if u, _ := userStore.GetByID(ctx, userID); u == nil {
		if err := userStore.Create(ctx, &auth.User{ID: userID, Username: userID, Role: auth.RoleViewer, IsActive: true}); err != nil {
			t.Fatalf("Create user: %v", err)
		}
	}
	if err := sessionStore.Create(ctx, &auth.Session{ID: sessionID, UserID: userID, Role: auth.RoleViewer, CreatedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour)}); err != nil {
		t.Fatalf("Create session: %v", err)
	}
}

type principalRequest struct {
	bearer  string
	session string
}

func sendAs(h http.Handler, p principalRequest) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/pools", nil)
	req.RemoteAddr = "203.0.113.9:4000"
	if p.bearer != "" {
		req.Header.Set("Authorization", "Bearer "+p.bearer)
	}
	if p.session != "" {
		req.AddCookie(&http.Cookie{Name: "session", Value: p.session})
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

func expectCodes(t *testing.T, h http.Handler, p principalRequest, want ...int) {
	t.Helper()
	for i, code := range want {
		if got := sendAs(h, p).Code; got != code {
			t.Fatalf("request %d: status = %d, want %d", i+1, got, code)
		}
	}
}

func TestRateLimitEachAPIKeyHasItsOwnBucket(t *testing.T) {
	keyStore := auth.NewMemoryKeyStore()
	h := principalLimitedHandler(t, RateLimitConfig{RequestsPerSecond: 0.001, Burst: 1}, keyStore, auth.NewMemorySessionStore(), auth.NewMemoryUserStore())
	keyA := mintKey(t, keyStore, "a", nil)
	keyB := mintKey(t, keyStore, "b", nil)

	expectCodes(t, h, principalRequest{bearer: keyA}, http.StatusOK, http.StatusTooManyRequests)
	expectCodes(t, h, principalRequest{bearer: keyB}, http.StatusOK, http.StatusTooManyRequests)

	// Authenticated traffic from this address did not spend its address bucket.
	expectCodes(t, h, principalRequest{}, http.StatusUnauthorized, http.StatusTooManyRequests)
}

func TestRateLimitEachUserHasItsOwnBucket(t *testing.T) {
	sessionStore := auth.NewMemorySessionStore()
	userStore := auth.NewMemoryUserStore()
	h := principalLimitedHandler(t, RateLimitConfig{RequestsPerSecond: 0.001, Burst: 1}, auth.NewMemoryKeyStore(), sessionStore, userStore)
	mintSession(t, sessionStore, userStore, "s-alice-1", "alice")
	mintSession(t, sessionStore, userStore, "s-alice-2", "alice")
	mintSession(t, sessionStore, userStore, "s-bob", "bob")

	expectCodes(t, h, principalRequest{session: "s-alice-1"}, http.StatusOK)
	// A second browser for the same user shares that user's bucket.
	expectCodes(t, h, principalRequest{session: "s-alice-2"}, http.StatusTooManyRequests)
	expectCodes(t, h, principalRequest{session: "s-bob"}, http.StatusOK, http.StatusTooManyRequests)
}

func TestRateLimitKeyWithOwnLimitUsesIt(t *testing.T) {
	keyStore := auth.NewMemoryKeyStore()
	h := principalLimitedHandler(t, RateLimitConfig{RequestsPerSecond: 0.001, Burst: 5}, keyStore, auth.NewMemorySessionStore(), auth.NewMemoryUserStore())
	throttled := mintKey(t, keyStore, "throttled", &auth.RateLimit{RequestsPerSecond: 0.5, Burst: 1})
	plain := mintKey(t, keyStore, "plain", nil)

	first := sendAs(h, principalRequest{bearer: throttled})
	if first.Code != http.StatusOK {
		t.Fatalf("first throttled request: status = %d", first.Code)
	}
	if got := first.Header().Get("X-RateLimit-Limit"); got != "0.5" {
		t.Fatalf("X-RateLimit-Limit = %q, want the key's own rate", got)
	}
	second := sendAs(h, principalRequest{bearer: throttled})
	if second.Code != http.StatusTooManyRequests {
		t.Fatalf("second throttled request: status = %d", second.Code)
	}
	if got := second.Header().Get("Retry-After"); got != "2" {
		t.Fatalf("Retry-After = %q, want 2 (one token per two seconds)", got)
	}

	expectCodes(t, h, principalRequest{bearer: plain}, http.StatusOK, http.StatusOK, http.StatusOK, http.StatusOK, http.StatusOK, http.StatusTooManyRequests)
}

func TestRateLimitRejectedCredentialsCostTheAddress(t *testing.T) {
	h := principalLimitedHandler(t, RateLimitConfig{RequestsPerSecond: 0.001, Burst: 2}, auth.NewMemoryKeyStore(), auth.NewMemorySessionStore(), auth.NewMemoryUserStore())

	bad := principalRequest{bearer: "cpam_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"}
	expectCodes(t, h, bad, http.StatusUnauthorized, http.StatusUnauthorized, http.StatusTooManyRequests)
}

func TestRateLimitKeyLimitAppliesWhenServerLimitIsOff(t *testing.T) {
	keyStore := auth.NewMemoryKeyStore()
	h := principalLimitedHandler(t, RateLimitConfig{}, keyStore, auth.NewMemorySessionStore(), auth.NewMemoryUserStore())
	throttled := mintKey(t, keyStore, "throttled", &auth.RateLimit{RequestsPerSecond: 0.001, Burst: 1})
	plain := mintKey(t, keyStore, "plain", nil)

	expectCodes(t, h, principalRequest{bearer: throttled}, http.StatusOK, http.StatusTooManyRequests)
	expectCodes(t, h, principalRequest{bearer: plain}, http.StatusOK, http.StatusOK, http.StatusOK, http.StatusOK)
	unauth := sendAs(h, principalRequest{})
	if unauth.Code != http.StatusUnauthorized || unauth.Header().Get("X-RateLimit-Limit") != "" {
		t.Fatalf("unauthenticated request with the limiter off: status %d, limit header %q", unauth.Code, unauth.Header().Get("X-RateLimit-Limit"))
	}
}

func TestRateLimitAuthWithoutLimiterIsUnlimited(t *testing.T) {
	keyStore := auth.NewMemoryKeyStore()
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	h := DualAuthMiddleware(keyStore, auth.NewMemorySessionStore(), auth.NewMemoryUserStore(), true, newTestLogger())(ok)
	throttled := mintKey(t, keyStore, "throttled", &auth.RateLimit{RequestsPerSecond: 0.001, Burst: 1})

	expectCodes(t, h, principalRequest{bearer: throttled}, http.StatusOK, http.StatusOK, http.StatusOK)
}

// blockingHandler holds every request it admits until release is closed, so a
// test can keep requests in flight at once.
func blockingHandler(entered chan<- struct{}, release <-chan struct{}) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		entered <- struct{}{}
		<-release
		w.WriteHeader(http.StatusOK)
	})
}

func TestRateLimitConcurrentCredentialRequestsCannotOverspendTheAddress(t *testing.T) {
	const burst, sent = 2, 6
	entered := make(chan struct{}, sent)
	release := make(chan struct{})
	h := RateLimitMiddleware(RateLimitConfig{RequestsPerSecond: 0.001, Burst: burst}, newTestLogger())(blockingHandler(entered, release))

	rejected := make(chan struct{}, sent)
	for range sent {
		go func() {
			if sendAs(h, principalRequest{bearer: "cpam_invalid"}).Code == http.StatusTooManyRequests {
				rejected <- struct{}{}
			}
		}()
	}

	admitted, refused := 0, 0
	for admitted+refused < sent {
		select {
		case <-entered:
			admitted++
		case <-rejected:
			refused++
		case <-time.After(5 * time.Second):
			t.Fatalf("timed out: %d admitted, %d rejected", admitted, refused)
		}
	}
	close(release)
	if admitted != burst {
		t.Fatalf("admitted %d concurrent credential-bearing requests, want the address burst of %d", admitted, burst)
	}
}

func TestRateLimitLongAuthenticatedRequestReleasesTheAddress(t *testing.T) {
	keyStore := auth.NewMemoryKeyStore()
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	slow := DualAuthMiddleware(keyStore, auth.NewMemorySessionStore(), auth.NewMemoryUserStore(), true, newTestLogger())(blockingHandler(entered, release))
	h := RateLimitMiddleware(RateLimitConfig{RequestsPerSecond: 0.001, Burst: 1}, newTestLogger())(slow)
	key := mintKey(t, keyStore, "streaming", nil)

	done := make(chan int, 1)
	go func() { done <- sendAs(h, principalRequest{bearer: key}).Code }()
	<-entered

	// The key paid for the stream; its address token is free for anyone else.
	expectCodes(t, h, principalRequest{}, http.StatusUnauthorized)
	close(release)
	if code := <-done; code != http.StatusOK {
		t.Fatalf("streaming request: status = %d", code)
	}
}
