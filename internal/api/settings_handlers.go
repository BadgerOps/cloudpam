package api

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"cloudpam/internal/auth"
	"cloudpam/internal/domain"
	"cloudpam/internal/storage"
)

// SettingsServer handles settings API endpoints.
type SettingsServer struct {
	*Server
	settingsStore storage.SettingsStore
}

// NewSettingsServer creates a new SettingsServer.
func NewSettingsServer(srv *Server, store storage.SettingsStore) *SettingsServer {
	return &SettingsServer{Server: srv, settingsStore: store}
}

// RegisterProtectedSettingsRoutes registers settings endpoints with RBAC.
func (ss *SettingsServer) RegisterProtectedSettingsRoutes(dualMW func(http.Handler) http.Handler, slogger *slog.Logger) {
	adminRead := RequirePermissionMiddleware(auth.ResourceSettings, auth.ActionRead, slogger)
	adminWrite := RequirePermissionMiddleware(auth.ResourceSettings, auth.ActionWrite, slogger)

	ss.mux.Handle("GET /api/v1/settings/security",
		dualMW(adminRead(http.HandlerFunc(ss.handleGetSecuritySettings))))
	ss.mux.Handle("PATCH /api/v1/settings/security",
		dualMW(adminWrite(http.HandlerFunc(ss.handleUpdateSecuritySettings))))
}

// RegisterSettingsRoutes registers settings endpoints without RBAC (for tests).
func (ss *SettingsServer) RegisterSettingsRoutes() {
	ss.mux.HandleFunc("GET /api/v1/settings/security", ss.handleGetSecuritySettings)
	ss.mux.HandleFunc("PATCH /api/v1/settings/security", ss.handleUpdateSecuritySettings)
}

func (ss *SettingsServer) handleGetSecuritySettings(w http.ResponseWriter, r *http.Request) {
	settings, err := ss.settingsStore.GetSecuritySettings(r.Context())
	if err != nil {
		ss.writeErr(r.Context(), w, http.StatusInternalServerError, "failed to load settings", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, settings)
}

func (ss *SettingsServer) handleUpdateSecuritySettings(w http.ResponseWriter, r *http.Request) {
	var input domain.SecuritySettings
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		ss.writeErr(r.Context(), w, http.StatusBadRequest, "invalid request body", err.Error())
		return
	}

	// Validate bounds
	if input.SessionDurationHours < 1 || input.SessionDurationHours > 720 {
		ss.writeErr(r.Context(), w, http.StatusBadRequest, "invalid session_duration_hours", "must be between 1 and 720")
		return
	}
	if input.MaxSessionsPerUser < 1 || input.MaxSessionsPerUser > 100 {
		ss.writeErr(r.Context(), w, http.StatusBadRequest, "invalid max_sessions_per_user", "must be between 1 and 100")
		return
	}
	if input.PasswordMinLength < 8 || input.PasswordMinLength > 72 {
		ss.writeErr(r.Context(), w, http.StatusBadRequest, "invalid password_min_length", "must be between 8 and 72")
		return
	}
	if input.PasswordMaxLength < input.PasswordMinLength || input.PasswordMaxLength > 72 {
		ss.writeErr(r.Context(), w, http.StatusBadRequest, "invalid password_max_length", "must be between min_length and 72")
		return
	}
	if input.LoginRateLimitPerMin < 1 || input.LoginRateLimitPerMin > 60 {
		ss.writeErr(r.Context(), w, http.StatusBadRequest, "invalid login_rate_limit_per_minute", "must be between 1 and 60")
		return
	}
	if input.AccountLockoutAttempts < 0 || input.AccountLockoutAttempts > 100 {
		ss.writeErr(r.Context(), w, http.StatusBadRequest, "invalid account_lockout_attempts", "must be between 0 and 100")
		return
	}

	if err := ss.settingsStore.UpdateSecuritySettings(r.Context(), &input); err != nil {
		ss.writeErr(r.Context(), w, http.StatusInternalServerError, "failed to save settings", err.Error())
		return
	}

	ss.logAudit(r.Context(), "update", "settings", "security", "security_settings", http.StatusOK)
	writeJSON(w, http.StatusOK, input)
}
