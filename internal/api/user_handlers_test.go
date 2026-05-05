package api

import (
	"context"
	"encoding/json"
	"io"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"cloudpam/internal/audit"
	"cloudpam/internal/auth"
	"cloudpam/internal/observability"
	"cloudpam/internal/storage"
)

func setupUserTestServer() (*UserServer, auth.SessionStore, auth.UserStore) {
	us, sessionStore, userStore, _ := setupUserTestServerWithAudit()
	return us, sessionStore, userStore
}

func setupUserTestServerWithAudit() (*UserServer, auth.SessionStore, auth.UserStore, *audit.MemoryAuditLogger) {
	st := storage.NewMemoryStore()
	mux := stdhttp.NewServeMux()
	logger := observability.NewLogger(observability.Config{
		Level:  "info",
		Format: "json",
		Output: io.Discard,
	})
	auditLogger := audit.NewMemoryAuditLogger()
	srv := NewServer(mux, st, logger, nil, auditLogger)

	keyStore := auth.NewMemoryKeyStore()
	sessionStore := auth.NewMemorySessionStore()
	userStore := auth.NewMemoryUserStore()

	us := NewUserServer(srv, keyStore, userStore, sessionStore, auditLogger)
	srv.registerUnprotectedTestRoutes()
	us.RegisterUserRoutes()

	return us, sessionStore, userStore, auditLogger
}

func TestRevokeSessions_Success(t *testing.T) {
	us, sessionStore, userStore := setupUserTestServer()

	// Create a test user.
	ctx := context.Background()
	hash, err := auth.HashPassword("TestPass123!")
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	user := &auth.User{
		ID:           "user-1",
		Username:     "testuser",
		Email:        "test@example.com",
		Role:         auth.RoleViewer,
		PasswordHash: hash,
		IsActive:     true,
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}
	if err := userStore.Create(ctx, user); err != nil {
		t.Fatalf("create user: %v", err)
	}

	// Create 3 sessions for the user.
	for i := 0; i < 3; i++ {
		sess, err := auth.NewSession(user.ID, user.Role, auth.DefaultSessionDuration, nil)
		if err != nil {
			t.Fatalf("new session: %v", err)
		}
		if err := sessionStore.Create(ctx, sess); err != nil {
			t.Fatalf("create session: %v", err)
		}
	}

	// Verify 3 sessions exist.
	sessions, err := sessionStore.ListByUserID(ctx, user.ID)
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if len(sessions) != 3 {
		t.Fatalf("expected 3 sessions, got %d", len(sessions))
	}

	// Call revoke-sessions endpoint (as admin context).
	req := httptest.NewRequest(stdhttp.MethodPost, "/api/v1/auth/users/"+user.ID+"/revoke-sessions", nil)
	req = req.WithContext(auth.ContextWithRole(auth.ContextWithUser(req.Context(), user), auth.RoleAdmin))
	rr := httptest.NewRecorder()
	us.mux.ServeHTTP(rr, req)

	if rr.Code != stdhttp.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	// Verify response.
	var resp map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp["status"] != "sessions revoked" {
		t.Errorf("expected status 'sessions revoked', got %q", resp["status"])
	}

	// Verify all sessions are deleted.
	sessions, err = sessionStore.ListByUserID(ctx, user.ID)
	if err != nil {
		t.Fatalf("list sessions after revoke: %v", err)
	}
	if len(sessions) != 0 {
		t.Errorf("expected 0 sessions after revoke, got %d", len(sessions))
	}
}

func TestRevokeSessions_SelfService(t *testing.T) {
	us, sessionStore, userStore := setupUserTestServer()

	ctx := context.Background()
	hash, _ := auth.HashPassword("TestPass123!")
	user := &auth.User{
		ID:           "user-2",
		Username:     "selfuser",
		Email:        "self@example.com",
		Role:         auth.RoleViewer,
		PasswordHash: hash,
		IsActive:     true,
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}
	if err := userStore.Create(ctx, user); err != nil {
		t.Fatalf("create user: %v", err)
	}

	// Create a session.
	sess, _ := auth.NewSession(user.ID, user.Role, auth.DefaultSessionDuration, nil)
	_ = sessionStore.Create(ctx, sess)

	// User revokes own sessions (non-admin).
	req := httptest.NewRequest(stdhttp.MethodPost, "/api/v1/auth/users/"+user.ID+"/revoke-sessions", nil)
	req = req.WithContext(auth.ContextWithRole(auth.ContextWithUser(req.Context(), user), auth.RoleViewer))
	rr := httptest.NewRecorder()
	us.mux.ServeHTTP(rr, req)

	if rr.Code != stdhttp.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	sessions, _ := sessionStore.ListByUserID(ctx, user.ID)
	if len(sessions) != 0 {
		t.Errorf("expected 0 sessions, got %d", len(sessions))
	}
}

func TestRevokeSessions_Forbidden(t *testing.T) {
	us, sessionStore, userStore := setupUserTestServer()

	ctx := context.Background()
	hash, _ := auth.HashPassword("TestPass123!")

	targetUser := &auth.User{
		ID:           "user-target",
		Username:     "target",
		Role:         auth.RoleViewer,
		PasswordHash: hash,
		IsActive:     true,
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}
	callerUser := &auth.User{
		ID:           "user-caller",
		Username:     "caller",
		Role:         auth.RoleViewer,
		PasswordHash: hash,
		IsActive:     true,
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}
	_ = userStore.Create(ctx, targetUser)
	_ = userStore.Create(ctx, callerUser)

	sess, _ := auth.NewSession(targetUser.ID, targetUser.Role, auth.DefaultSessionDuration, nil)
	_ = sessionStore.Create(ctx, sess)

	// Non-admin caller trying to revoke another user's sessions.
	req := httptest.NewRequest(stdhttp.MethodPost, "/api/v1/auth/users/"+targetUser.ID+"/revoke-sessions", nil)
	req = req.WithContext(auth.ContextWithRole(auth.ContextWithUser(req.Context(), callerUser), auth.RoleViewer))
	rr := httptest.NewRecorder()
	us.mux.ServeHTTP(rr, req)

	if rr.Code != stdhttp.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rr.Code, rr.Body.String())
	}

	// Verify session still exists.
	sessions, _ := sessionStore.ListByUserID(ctx, targetUser.ID)
	if len(sessions) != 1 {
		t.Errorf("expected 1 session (not revoked), got %d", len(sessions))
	}
}

func TestListByUserID(t *testing.T) {
	store := auth.NewMemorySessionStore()
	ctx := context.Background()

	// Create sessions for two users.
	for i := 0; i < 3; i++ {
		sess, _ := auth.NewSession("user-a", auth.RoleViewer, auth.DefaultSessionDuration, nil)
		_ = store.Create(ctx, sess)
	}
	for i := 0; i < 2; i++ {
		sess, _ := auth.NewSession("user-b", auth.RoleViewer, auth.DefaultSessionDuration, nil)
		_ = store.Create(ctx, sess)
	}

	sessionsA, err := store.ListByUserID(ctx, "user-a")
	if err != nil {
		t.Fatalf("ListByUserID: %v", err)
	}
	if len(sessionsA) != 3 {
		t.Errorf("expected 3 sessions for user-a, got %d", len(sessionsA))
	}

	sessionsB, err := store.ListByUserID(ctx, "user-b")
	if err != nil {
		t.Fatalf("ListByUserID: %v", err)
	}
	if len(sessionsB) != 2 {
		t.Errorf("expected 2 sessions for user-b, got %d", len(sessionsB))
	}

	// Non-existent user returns empty.
	sessionsC, err := store.ListByUserID(ctx, "user-c")
	if err != nil {
		t.Fatalf("ListByUserID: %v", err)
	}
	if len(sessionsC) != 0 {
		t.Errorf("expected 0 sessions for user-c, got %d", len(sessionsC))
	}
}

func TestLogin_LocalAuthDisabled_Rejected(t *testing.T) {
	us, _, userStore := setupUserTestServer()

	ctx := context.Background()
	hash, _ := auth.HashPassword("TestPass123!")
	user := &auth.User{
		ID:           "user-local-auth",
		Username:     "localuser",
		Email:        "local@example.com",
		Role:         auth.RoleViewer,
		PasswordHash: hash,
		IsActive:     true,
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}
	if err := userStore.Create(ctx, user); err != nil {
		t.Fatalf("create user: %v", err)
	}

	// Set up settings store with local auth disabled.
	settingsStore := storage.NewMemorySettingsStore()
	settings, _ := settingsStore.GetSecuritySettings(ctx)
	settings.LocalAuthEnabled = false
	_ = settingsStore.UpdateSecuritySettings(ctx, settings)
	us.SetSettingsStore(settingsStore)

	// Attempt login with valid credentials.
	body := `{"username":"localuser","password":"TestPass123!"}`
	req := httptest.NewRequest(stdhttp.MethodPost, "/api/v1/auth/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	us.mux.ServeHTTP(rr, req)

	if rr.Code != stdhttp.StatusForbidden {
		t.Fatalf("expected 403 when local auth disabled, got %d: %s", rr.Code, rr.Body.String())
	}

	var errResp apiError
	if err := json.Unmarshal(rr.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("unmarshal error response: %v", err)
	}
	if errResp.Error != "local authentication is disabled" {
		t.Errorf("expected error 'local authentication is disabled', got %q", errResp.Error)
	}
	if errResp.Detail != "use SSO to sign in" {
		t.Errorf("expected detail 'use SSO to sign in', got %q", errResp.Detail)
	}
}

func TestLogin_LocalAuthEnabled_Allowed(t *testing.T) {
	us, _, userStore := setupUserTestServer()

	ctx := context.Background()
	hash, _ := auth.HashPassword("TestPass123!")
	user := &auth.User{
		ID:           "user-local-auth-ok",
		Username:     "enableduser",
		Email:        "enabled@example.com",
		Role:         auth.RoleViewer,
		PasswordHash: hash,
		IsActive:     true,
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}
	if err := userStore.Create(ctx, user); err != nil {
		t.Fatalf("create user: %v", err)
	}

	// Set up settings store with local auth enabled (default).
	settingsStore := storage.NewMemorySettingsStore()
	us.SetSettingsStore(settingsStore)

	// Attempt login with valid credentials — should succeed.
	body := `{"username":"enableduser","password":"TestPass123!"}`
	req := httptest.NewRequest(stdhttp.MethodPost, "/api/v1/auth/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	us.mux.ServeHTTP(rr, req)

	if rr.Code != stdhttp.StatusOK {
		t.Fatalf("expected 200 when local auth enabled, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestLogin_AccountLockout_AuditAndManualUnlock(t *testing.T) {
	us, _, userStore, auditLogger := setupUserTestServerWithAudit()

	ctx := context.Background()
	hash, _ := auth.HashPassword("TestPass123!")
	user := &auth.User{
		ID:           "user-lockout",
		Username:     "lockoutuser",
		Email:        "lockout@example.com",
		Role:         auth.RoleViewer,
		PasswordHash: hash,
		IsActive:     true,
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}
	if err := userStore.Create(ctx, user); err != nil {
		t.Fatalf("create user: %v", err)
	}

	settingsStore := storage.NewMemorySettingsStore()
	settings, _ := settingsStore.GetSecuritySettings(ctx)
	settings.AccountLockoutAttempts = 2
	settings.AccountLockoutCooldownMinutes = 15
	_ = settingsStore.UpdateSecuritySettings(ctx, settings)
	us.SetSettingsStore(settingsStore)

	body := `{"username":"lockoutuser","password":"wrong"}`
	req := httptest.NewRequest(stdhttp.MethodPost, "/api/v1/auth/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	us.mux.ServeHTTP(rr, req)
	if rr.Code != stdhttp.StatusUnauthorized {
		t.Fatalf("first failure: expected 401, got %d: %s", rr.Code, rr.Body.String())
	}

	req = httptest.NewRequest(stdhttp.MethodPost, "/api/v1/auth/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr = httptest.NewRecorder()
	us.mux.ServeHTTP(rr, req)
	if rr.Code != stdhttp.StatusLocked {
		t.Fatalf("second failure: expected 423, got %d: %s", rr.Code, rr.Body.String())
	}

	lockedUser, err := userStore.GetByID(ctx, user.ID)
	if err != nil {
		t.Fatalf("get locked user: %v", err)
	}
	if lockedUser.FailedLoginAttempts != 2 {
		t.Errorf("failed attempts = %d, want 2", lockedUser.FailedLoginAttempts)
	}
	if lockedUser.LockedAt == nil || lockedUser.LockoutUntil == nil {
		t.Fatalf("expected locked_at and lockout_until to be set")
	}

	events, _, err := auditLogger.List(ctx, audit.ListOptions{Limit: 10})
	if err != nil {
		t.Fatalf("list audit: %v", err)
	}
	if countAuditActions(events, audit.ActionLoginFailed) != 2 {
		t.Fatalf("expected 2 login_failed audit events, got %#v", events)
	}
	if countAuditActions(events, audit.ActionAccountLocked) != 1 {
		t.Fatalf("expected 1 account_locked audit event, got %#v", events)
	}

	admin := &auth.User{ID: "admin", Username: "admin", Role: auth.RoleAdmin, IsActive: true}
	req = httptest.NewRequest(stdhttp.MethodPost, "/api/v1/auth/users/"+user.ID+"/unlock", nil)
	req = req.WithContext(auth.ContextWithRole(auth.ContextWithUser(req.Context(), admin), auth.RoleAdmin))
	rr = httptest.NewRecorder()
	us.mux.ServeHTTP(rr, req)
	if rr.Code != stdhttp.StatusOK {
		t.Fatalf("unlock: expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	unlockedUser, _ := userStore.GetByID(ctx, user.ID)
	if unlockedUser.FailedLoginAttempts != 0 || unlockedUser.LockedAt != nil || unlockedUser.LockoutUntil != nil || unlockedUser.LastFailedLoginAt != nil {
		t.Fatalf("expected lockout state cleared, got %+v", unlockedUser)
	}
	events, _, _ = auditLogger.List(ctx, audit.ListOptions{Limit: 10})
	if countAuditActions(events, audit.ActionAccountUnlocked) != 1 {
		t.Fatalf("expected 1 account_unlocked audit event, got %#v", events)
	}
}

func TestLogin_ExpiredLockoutAutoUnlocksAndAllowsLogin(t *testing.T) {
	us, _, userStore, auditLogger := setupUserTestServerWithAudit()

	ctx := context.Background()
	hash, _ := auth.HashPassword("TestPass123!")
	now := time.Now().UTC()
	lockedAt := now.Add(-20 * time.Minute)
	lockoutUntil := now.Add(-5 * time.Minute)
	lastFailed := lockedAt
	user := &auth.User{
		ID:                  "user-auto-unlock",
		Username:            "autounlock",
		Email:               "autounlock@example.com",
		Role:                auth.RoleViewer,
		PasswordHash:        hash,
		IsActive:            true,
		FailedLoginAttempts: 2,
		LastFailedLoginAt:   &lastFailed,
		LockedAt:            &lockedAt,
		LockoutUntil:        &lockoutUntil,
		CreatedAt:           now,
		UpdatedAt:           now,
	}
	if err := userStore.Create(ctx, user); err != nil {
		t.Fatalf("create user: %v", err)
	}
	settingsStore := storage.NewMemorySettingsStore()
	us.SetSettingsStore(settingsStore)

	body := `{"username":"autounlock","password":"TestPass123!"}`
	req := httptest.NewRequest(stdhttp.MethodPost, "/api/v1/auth/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	us.mux.ServeHTTP(rr, req)
	if rr.Code != stdhttp.StatusOK {
		t.Fatalf("login: expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	got, _ := userStore.GetByID(ctx, user.ID)
	if got.FailedLoginAttempts != 0 || got.LockedAt != nil || got.LockoutUntil != nil || got.LastFailedLoginAt != nil {
		t.Fatalf("expected lockout state cleared after auto-unlock, got %+v", got)
	}
	events, _, _ := auditLogger.List(ctx, audit.ListOptions{Limit: 10})
	if countAuditActions(events, audit.ActionAccountUnlocked) != 1 {
		t.Fatalf("expected auto account_unlocked audit event, got %#v", events)
	}
	if countAuditActions(events, audit.ActionLogin) != 1 {
		t.Fatalf("expected successful login audit event, got %#v", events)
	}
}

func TestSessionLimit_Enforcement(t *testing.T) {
	us, sessionStore, userStore := setupUserTestServer()

	ctx := context.Background()
	hash, _ := auth.HashPassword("TestPass123!")
	user := &auth.User{
		ID:           "user-limit",
		Username:     "limituser",
		Email:        "limit@example.com",
		Role:         auth.RoleViewer,
		PasswordHash: hash,
		IsActive:     true,
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}
	if err := userStore.Create(ctx, user); err != nil {
		t.Fatalf("create user: %v", err)
	}

	// Pre-create 11 sessions (over the limit of 10).
	for i := 0; i < 11; i++ {
		sess, _ := auth.NewSession(user.ID, user.Role, auth.DefaultSessionDuration, nil)
		// Stagger creation times so oldest can be identified.
		sess.CreatedAt = time.Now().UTC().Add(time.Duration(i) * time.Second)
		_ = sessionStore.Create(ctx, sess)
	}

	// Login, which should trigger eviction.
	body := `{"username":"limituser","password":"TestPass123!"}`
	req := httptest.NewRequest(stdhttp.MethodPost, "/api/v1/auth/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	us.mux.ServeHTTP(rr, req)

	if rr.Code != stdhttp.StatusOK {
		t.Fatalf("login: expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	// After login: 11 existing + 1 new = 12, then evict 2 oldest => 10.
	sessions, err := sessionStore.ListByUserID(ctx, user.ID)
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if len(sessions) != 10 {
		t.Errorf("expected 10 sessions after eviction, got %d", len(sessions))
	}
}

func countAuditActions(events []*audit.AuditEvent, action string) int {
	count := 0
	for _, event := range events {
		if event.Action == action {
			count++
		}
	}
	return count
}
