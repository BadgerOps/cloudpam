package api

import (
	"encoding/json"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"cloudpam/internal/domain"
	"cloudpam/internal/storage"
)

func setupSettingsServer() *stdhttp.ServeMux {
	st := storage.NewMemoryStore()
	mux := stdhttp.NewServeMux()
	srv := NewServerWithSlog(mux, st, nil)

	settingsStore := storage.NewMemorySettingsStore()
	settingsSrv := NewSettingsServer(srv, settingsStore)
	settingsSrv.RegisterSettingsRoutes()

	return mux
}

func TestSettingsHandler_GetDefaults(t *testing.T) {
	mux := setupSettingsServer()

	req := httptest.NewRequest(stdhttp.MethodGet, "/api/v1/settings/security", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != stdhttp.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var settings domain.SecuritySettings
	if err := json.NewDecoder(rr.Body).Decode(&settings); err != nil {
		t.Fatalf("decode error: %v", err)
	}

	defaults := domain.DefaultSecuritySettings()
	if settings.SessionDurationHours != defaults.SessionDurationHours {
		t.Errorf("session_duration_hours: got %d, want %d", settings.SessionDurationHours, defaults.SessionDurationHours)
	}
	if settings.MaxSessionsPerUser != defaults.MaxSessionsPerUser {
		t.Errorf("max_sessions_per_user: got %d, want %d", settings.MaxSessionsPerUser, defaults.MaxSessionsPerUser)
	}
	if settings.PasswordMinLength != defaults.PasswordMinLength {
		t.Errorf("password_min_length: got %d, want %d", settings.PasswordMinLength, defaults.PasswordMinLength)
	}
	if settings.PasswordMaxLength != defaults.PasswordMaxLength {
		t.Errorf("password_max_length: got %d, want %d", settings.PasswordMaxLength, defaults.PasswordMaxLength)
	}
	if settings.LoginRateLimitPerMin != defaults.LoginRateLimitPerMin {
		t.Errorf("login_rate_limit_per_minute: got %d, want %d", settings.LoginRateLimitPerMin, defaults.LoginRateLimitPerMin)
	}
	if settings.AccountLockoutAttempts != defaults.AccountLockoutAttempts {
		t.Errorf("account_lockout_attempts: got %d, want %d", settings.AccountLockoutAttempts, defaults.AccountLockoutAttempts)
	}
}

func TestSettingsHandler_UpdateValid(t *testing.T) {
	mux := setupSettingsServer()

	body := `{
		"session_duration_hours": 48,
		"max_sessions_per_user": 5,
		"password_min_length": 16,
		"password_max_length": 64,
		"login_rate_limit_per_minute": 10,
		"account_lockout_attempts": 5,
		"trusted_proxies": ["10.0.0.0/8"]
	}`
	req := httptest.NewRequest(stdhttp.MethodPatch, "/api/v1/settings/security", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)

	if rr.Code != stdhttp.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}

	var settings domain.SecuritySettings
	if err := json.NewDecoder(rr.Body).Decode(&settings); err != nil {
		t.Fatalf("decode error: %v", err)
	}

	if settings.SessionDurationHours != 48 {
		t.Errorf("session_duration_hours: got %d, want 48", settings.SessionDurationHours)
	}
	if settings.MaxSessionsPerUser != 5 {
		t.Errorf("max_sessions_per_user: got %d, want 5", settings.MaxSessionsPerUser)
	}
	if settings.PasswordMinLength != 16 {
		t.Errorf("password_min_length: got %d, want 16", settings.PasswordMinLength)
	}

	// Verify GET returns updated values
	getReq := httptest.NewRequest(stdhttp.MethodGet, "/api/v1/settings/security", nil)
	getRR := httptest.NewRecorder()
	mux.ServeHTTP(getRR, getReq)

	var updated domain.SecuritySettings
	if err := json.NewDecoder(getRR.Body).Decode(&updated); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if updated.SessionDurationHours != 48 {
		t.Errorf("after update, session_duration_hours: got %d, want 48", updated.SessionDurationHours)
	}
}

func TestSettingsHandler_UpdateInvalidBounds(t *testing.T) {
	mux := setupSettingsServer()

	tests := []struct {
		name string
		body string
	}{
		{
			name: "session_duration_hours too low",
			body: `{"session_duration_hours":0,"max_sessions_per_user":10,"password_min_length":12,"password_max_length":72,"login_rate_limit_per_minute":5,"account_lockout_attempts":0}`,
		},
		{
			name: "session_duration_hours too high",
			body: `{"session_duration_hours":721,"max_sessions_per_user":10,"password_min_length":12,"password_max_length":72,"login_rate_limit_per_minute":5,"account_lockout_attempts":0}`,
		},
		{
			name: "max_sessions_per_user too low",
			body: `{"session_duration_hours":24,"max_sessions_per_user":0,"password_min_length":12,"password_max_length":72,"login_rate_limit_per_minute":5,"account_lockout_attempts":0}`,
		},
		{
			name: "password_min_length too low",
			body: `{"session_duration_hours":24,"max_sessions_per_user":10,"password_min_length":7,"password_max_length":72,"login_rate_limit_per_minute":5,"account_lockout_attempts":0}`,
		},
		{
			name: "password_max_length less than min",
			body: `{"session_duration_hours":24,"max_sessions_per_user":10,"password_min_length":16,"password_max_length":10,"login_rate_limit_per_minute":5,"account_lockout_attempts":0}`,
		},
		{
			name: "login_rate_limit_per_minute too low",
			body: `{"session_duration_hours":24,"max_sessions_per_user":10,"password_min_length":12,"password_max_length":72,"login_rate_limit_per_minute":0,"account_lockout_attempts":0}`,
		},
		{
			name: "account_lockout_attempts too high",
			body: `{"session_duration_hours":24,"max_sessions_per_user":10,"password_min_length":12,"password_max_length":72,"login_rate_limit_per_minute":5,"account_lockout_attempts":101}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(stdhttp.MethodPatch, "/api/v1/settings/security", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			rr := httptest.NewRecorder()
			mux.ServeHTTP(rr, req)

			if rr.Code != stdhttp.StatusBadRequest {
				t.Errorf("expected 400, got %d: %s", rr.Code, rr.Body.String())
			}
		})
	}
}
