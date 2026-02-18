package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"cloudpam/internal/audit"
	"cloudpam/internal/auth"

	"github.com/google/uuid"
)

// UserServer extends Server with user management capabilities.
type UserServer struct {
	*Server
	keyStore     auth.KeyStore
	userStore    auth.UserStore
	sessionStore auth.SessionStore
	auditLogger  audit.AuditLogger
}

// NewUserServer creates a new UserServer.
func NewUserServer(s *Server, keyStore auth.KeyStore, userStore auth.UserStore, sessionStore auth.SessionStore, auditLogger audit.AuditLogger) *UserServer {
	return &UserServer{
		Server:       s,
		keyStore:     keyStore,
		userStore:    userStore,
		sessionStore: sessionStore,
		auditLogger:  auditLogger,
	}
}

// userRouteConfig holds optional configuration for user route registration.
type userRouteConfig struct {
	loginRateLimit func(http.Handler) http.Handler
}

// UserRouteOption configures user route registration.
type UserRouteOption func(*userRouteConfig)

// WithLoginRateLimit sets login rate limiting middleware.
func WithLoginRateLimit(mw func(http.Handler) http.Handler) UserRouteOption {
	return func(cfg *userRouteConfig) {
		cfg.loginRateLimit = mw
	}
}

// RegisterUserRoutes registers user auth routes without RBAC (development mode).
func (us *UserServer) RegisterUserRoutes() {
	us.mux.HandleFunc("/api/v1/auth/login", us.handleLogin)
	us.mux.HandleFunc("/api/v1/auth/logout", us.handleLogout)
	us.mux.HandleFunc("/api/v1/auth/me", us.handleMe)
	us.mux.HandleFunc("/api/v1/auth/users", us.handleUsers)
	us.mux.HandleFunc("/api/v1/auth/users/", us.handleUserByID)
}

// RegisterProtectedUserRoutes registers user auth routes with RBAC.
func (us *UserServer) RegisterProtectedUserRoutes(logger *slog.Logger, opts ...UserRouteOption) {
	if logger == nil {
		logger = slog.Default()
	}

	var cfg userRouteConfig
	for _, o := range opts {
		o(&cfg)
	}

	// Login is always public (no auth required), but rate limited.
	loginHandler := http.Handler(http.HandlerFunc(us.handleLogin))
	if cfg.loginRateLimit != nil {
		loginHandler = cfg.loginRateLimit(loginHandler)
	}
	us.mux.Handle("/api/v1/auth/login", loginHandler)

	// Dual auth middleware (session or API key).
	dualMW := DualAuthMiddleware(us.keyStore, us.sessionStore, us.userStore, true, logger)

	// Logout and me require authentication.
	us.mux.Handle("/api/v1/auth/logout", dualMW(http.HandlerFunc(us.handleLogout)))
	us.mux.Handle("/api/v1/auth/me", dualMW(http.HandlerFunc(us.handleMe)))

	// User CRUD — admin only.
	usersCreateMW := RequirePermissionMiddleware(auth.ResourceUsers, auth.ActionCreate, logger)
	usersReadMW := RequirePermissionMiddleware(auth.ResourceUsers, auth.ActionRead, logger)
	usersListMW := RequirePermissionMiddleware(auth.ResourceUsers, auth.ActionList, logger)
	usersUpdateMW := RequirePermissionMiddleware(auth.ResourceUsers, auth.ActionUpdate, logger)
	usersDeleteMW := RequirePermissionMiddleware(auth.ResourceUsers, auth.ActionDelete, logger)

	us.mux.Handle("/api/v1/auth/users", dualMW(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			usersListMW(http.HandlerFunc(us.listUsers)).ServeHTTP(w, r)
		case http.MethodPost:
			usersCreateMW(http.HandlerFunc(us.createUser)).ServeHTTP(w, r)
		default:
			w.Header().Set("Allow", strings.Join([]string{http.MethodGet, http.MethodPost}, ", "))
			us.writeErr(r.Context(), w, http.StatusMethodNotAllowed, "method not allowed", "")
		}
	})))

	us.mux.Handle("/api/v1/auth/users/", dualMW(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/api/v1/auth/users/")
		parts := strings.SplitN(path, "/", 2)
		id := strings.Trim(parts[0], "/")
		if id == "" {
			us.writeErr(r.Context(), w, http.StatusNotFound, "not found", "")
			return
		}

		// Check for /password sub-route.
		if len(parts) == 2 && strings.TrimSuffix(parts[1], "/") == "password" {
			if r.Method == http.MethodPatch {
				// Self or admin can change password.
				us.changePassword(w, r, id)
				return
			}
			w.Header().Set("Allow", http.MethodPatch)
			us.writeErr(r.Context(), w, http.StatusMethodNotAllowed, "method not allowed", "")
			return
		}

		switch r.Method {
		case http.MethodGet:
			usersReadMW(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				us.getUser(w, r, id)
			})).ServeHTTP(w, r)
		case http.MethodPatch:
			usersUpdateMW(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				us.updateUser(w, r, id)
			})).ServeHTTP(w, r)
		case http.MethodDelete:
			usersDeleteMW(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				us.deactivateUser(w, r, id)
			})).ServeHTTP(w, r)
		default:
			w.Header().Set("Allow", strings.Join([]string{http.MethodGet, http.MethodPatch, http.MethodDelete}, ", "))
			us.writeErr(r.Context(), w, http.StatusMethodNotAllowed, "method not allowed", "")
		}
	})))
}

// handleLogin authenticates a user via username/password and creates a session.
// POST /api/v1/auth/login
func (us *UserServer) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		us.writeErr(r.Context(), w, http.StatusMethodNotAllowed, "method not allowed", "")
		return
	}

	ctx := r.Context()

	var input struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		us.writeErr(ctx, w, http.StatusBadRequest, "invalid json", "")
		return
	}

	input.Username = strings.TrimSpace(input.Username)
	if input.Username == "" || input.Password == "" {
		us.writeErr(ctx, w, http.StatusBadRequest, "username and password are required", "")
		return
	}

	// Look up user.
	user, err := us.userStore.GetByUsername(ctx, input.Username)
	if err != nil {
		us.writeErr(ctx, w, http.StatusInternalServerError, "internal error", err.Error())
		return
	}
	if user == nil {
		us.logAuditEvent(ctx, audit.ActionLoginFailed, audit.ResourceSession, "", input.Username, http.StatusUnauthorized)
		us.writeErr(ctx, w, http.StatusUnauthorized, "invalid credentials", "")
		return
	}

	// Check active.
	if !user.IsActive {
		us.logAuditEvent(ctx, audit.ActionLoginFailed, audit.ResourceSession, user.ID, user.Username, http.StatusForbidden)
		us.writeErr(ctx, w, http.StatusForbidden, "account disabled", "")
		return
	}

	// Verify password.
	if err := auth.VerifyPassword(input.Password, user.PasswordHash); err != nil {
		us.logAuditEvent(ctx, audit.ActionLoginFailed, audit.ResourceSession, user.ID, user.Username, http.StatusUnauthorized)
		us.writeErr(ctx, w, http.StatusUnauthorized, "invalid credentials", "")
		return
	}

	// Create session.
	session, err := auth.NewSession(user.ID, user.Role, auth.DefaultSessionDuration, nil)
	if err != nil {
		us.writeErr(ctx, w, http.StatusInternalServerError, "failed to create session", err.Error())
		return
	}
	if err := us.sessionStore.Create(ctx, session); err != nil {
		us.writeErr(ctx, w, http.StatusInternalServerError, "failed to store session", err.Error())
		return
	}

	// Update last login.
	now := time.Now().UTC()
	_ = us.userStore.UpdateLastLogin(ctx, user.ID, now)

	// Set session cookie.
	// Only set Secure flag when served over HTTPS so cookies work on http://localhost.
	isSecure := r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
	sameSite := http.SameSiteLaxMode
	if isSecure {
		sameSite = http.SameSiteStrictMode
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    session.ID,
		Path:     "/",
		HttpOnly: true,
		Secure:   isSecure,
		SameSite: sameSite,
		MaxAge:   int(auth.DefaultSessionDuration.Seconds()),
	})

	us.logAuditEvent(ctx, audit.ActionLogin, audit.ResourceSession, session.ID, user.Username, http.StatusOK)

	writeJSON(w, http.StatusOK, struct {
		User      *auth.User `json:"user"`
		ExpiresAt time.Time  `json:"expires_at"`
	}{
		User:      user,
		ExpiresAt: session.ExpiresAt,
	})
}

// handleLogout invalidates the current session.
// POST /api/v1/auth/logout
func (us *UserServer) handleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		us.writeErr(r.Context(), w, http.StatusMethodNotAllowed, "method not allowed", "")
		return
	}

	ctx := r.Context()
	session := auth.SessionFromContext(ctx)
	if session != nil {
		_ = us.sessionStore.Delete(ctx, session.ID)
		actor := "unknown"
		if user := auth.UserFromContext(ctx); user != nil {
			actor = user.Username
		}
		us.logAuditEvent(ctx, audit.ActionLogout, audit.ResourceSession, session.ID, actor, http.StatusOK)
	}

	// Clear cookie.
	isSecure := r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
	sameSite := http.SameSiteLaxMode
	if isSecure {
		sameSite = http.SameSiteStrictMode
	}
	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   isSecure,
		SameSite: sameSite,
		MaxAge:   -1,
	})

	w.WriteHeader(http.StatusNoContent)
}

// handleMe returns information about the currently authenticated user or API key.
// GET /api/v1/auth/me
func (us *UserServer) handleMe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		us.writeErr(r.Context(), w, http.StatusMethodNotAllowed, "method not allowed", "")
		return
	}

	ctx := r.Context()
	role := auth.GetEffectiveRole(ctx)

	type meResponse struct {
		AuthType string     `json:"auth_type"`
		Role     auth.Role  `json:"role"`
		User     *auth.User `json:"user,omitempty"`
		KeyID    string     `json:"key_id,omitempty"`
		KeyName  string     `json:"key_name,omitempty"`
	}

	if user := auth.UserFromContext(ctx); user != nil {
		writeJSON(w, http.StatusOK, meResponse{
			AuthType: "session",
			Role:     role,
			User:     user,
		})
		return
	}

	if key := auth.APIKeyFromContext(ctx); key != nil {
		writeJSON(w, http.StatusOK, meResponse{
			AuthType: "api_key",
			Role:     role,
			KeyID:    key.ID,
			KeyName:  key.Name,
		})
		return
	}

	us.writeErr(ctx, w, http.StatusUnauthorized, "not authenticated", "")
}

// handleUsers dispatches to listUsers or createUser (unprotected mode).
func (us *UserServer) handleUsers(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		us.listUsers(w, r)
	case http.MethodPost:
		us.createUser(w, r)
	default:
		w.Header().Set("Allow", strings.Join([]string{http.MethodGet, http.MethodPost}, ", "))
		us.writeErr(r.Context(), w, http.StatusMethodNotAllowed, "method not allowed", "")
	}
}

// handleUserByID dispatches user-by-ID routes (unprotected mode).
func (us *UserServer) handleUserByID(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/auth/users/")
	parts := strings.SplitN(path, "/", 2)
	id := strings.Trim(parts[0], "/")
	if id == "" {
		us.writeErr(r.Context(), w, http.StatusNotFound, "not found", "")
		return
	}

	if len(parts) == 2 && strings.TrimSuffix(parts[1], "/") == "password" {
		if r.Method == http.MethodPatch {
			us.changePassword(w, r, id)
			return
		}
		w.Header().Set("Allow", http.MethodPatch)
		us.writeErr(r.Context(), w, http.StatusMethodNotAllowed, "method not allowed", "")
		return
	}

	switch r.Method {
	case http.MethodGet:
		us.getUser(w, r, id)
	case http.MethodPatch:
		us.updateUser(w, r, id)
	case http.MethodDelete:
		us.deactivateUser(w, r, id)
	default:
		w.Header().Set("Allow", strings.Join([]string{http.MethodGet, http.MethodPatch, http.MethodDelete}, ", "))
		us.writeErr(r.Context(), w, http.StatusMethodNotAllowed, "method not allowed", "")
	}
}

// listUsers returns all users.
// GET /api/v1/auth/users
func (us *UserServer) listUsers(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	users, err := us.userStore.List(ctx)
	if err != nil {
		us.writeErr(ctx, w, http.StatusInternalServerError, "failed to list users", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, struct {
		Users []*auth.User `json:"users"`
	}{Users: users})
}

// createUser creates a new user.
// POST /api/v1/auth/users
func (us *UserServer) createUser(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var input struct {
		Username    string `json:"username"`
		Email       string `json:"email"`
		DisplayName string `json:"display_name"`
		Role        string `json:"role"`
		Password    string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		us.writeErr(ctx, w, http.StatusBadRequest, "invalid json", "")
		return
	}

	input.Username = strings.TrimSpace(input.Username)
	input.Email = strings.TrimSpace(input.Email)
	input.DisplayName = strings.TrimSpace(input.DisplayName)
	input.Password = strings.TrimSpace(input.Password)

	if input.Username == "" {
		us.writeErr(ctx, w, http.StatusBadRequest, "username is required", "")
		return
	}
	if len(input.Username) > 255 {
		us.writeErr(ctx, w, http.StatusBadRequest, "username too long", "maximum 255 characters")
		return
	}
	if input.Password == "" {
		us.writeErr(ctx, w, http.StatusBadRequest, "password is required", "")
		return
	}
	if err := auth.ValidatePassword(input.Password, 0); err != nil {
		us.writeErr(ctx, w, http.StatusBadRequest, "password too weak", err.Error())
		return
	}

	role := auth.ParseRole(input.Role)
	if role == auth.RoleNone {
		role = auth.RoleViewer // default
	}

	hash, err := auth.HashPassword(input.Password)
	if err != nil {
		us.writeErr(ctx, w, http.StatusInternalServerError, "failed to hash password", err.Error())
		return
	}

	now := time.Now().UTC()
	user := &auth.User{
		ID:           uuid.New().String(),
		Username:     input.Username,
		Email:        input.Email,
		DisplayName:  input.DisplayName,
		Role:         role,
		PasswordHash: hash,
		IsActive:     true,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	if err := us.userStore.Create(ctx, user); err != nil {
		if err == auth.ErrUserExists {
			us.writeErr(ctx, w, http.StatusConflict, "user already exists", "")
			return
		}
		us.writeErr(ctx, w, http.StatusInternalServerError, "failed to create user", err.Error())
		return
	}

	us.logAuditEvent(ctx, audit.ActionCreate, audit.ResourceUser, user.ID, user.Username, http.StatusCreated)

	writeJSON(w, http.StatusCreated, user)
}

// getUser returns a single user by ID.
// GET /api/v1/auth/users/{id}
func (us *UserServer) getUser(w http.ResponseWriter, r *http.Request, id string) {
	ctx := r.Context()

	user, err := us.userStore.GetByID(ctx, id)
	if err != nil {
		us.writeErr(ctx, w, http.StatusInternalServerError, "failed to get user", err.Error())
		return
	}
	if user == nil {
		us.writeErr(ctx, w, http.StatusNotFound, "user not found", "")
		return
	}

	writeJSON(w, http.StatusOK, user)
}

// updateUser modifies a user's role, display name, email, or active status.
// PATCH /api/v1/auth/users/{id}
func (us *UserServer) updateUser(w http.ResponseWriter, r *http.Request, id string) {
	ctx := r.Context()

	user, err := us.userStore.GetByID(ctx, id)
	if err != nil {
		us.writeErr(ctx, w, http.StatusInternalServerError, "failed to get user", err.Error())
		return
	}
	if user == nil {
		us.writeErr(ctx, w, http.StatusNotFound, "user not found", "")
		return
	}

	var input struct {
		Email       *string `json:"email"`
		DisplayName *string `json:"display_name"`
		Role        *string `json:"role"`
		IsActive    *bool   `json:"is_active"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		us.writeErr(ctx, w, http.StatusBadRequest, "invalid json", "")
		return
	}

	if input.Email != nil {
		user.Email = strings.TrimSpace(*input.Email)
	}
	if input.DisplayName != nil {
		user.DisplayName = strings.TrimSpace(*input.DisplayName)
	}
	if input.Role != nil {
		role := auth.ParseRole(*input.Role)
		if role == auth.RoleNone {
			us.writeErr(ctx, w, http.StatusBadRequest, "invalid role", *input.Role)
			return
		}
		user.Role = role
	}
	if input.IsActive != nil {
		user.IsActive = *input.IsActive
		// If deactivating, kill sessions.
		if !user.IsActive {
			_ = us.sessionStore.DeleteByUserID(ctx, user.ID)
		}
	}

	user.UpdatedAt = time.Now().UTC()

	if err := us.userStore.Update(ctx, user); err != nil {
		us.writeErr(ctx, w, http.StatusInternalServerError, "failed to update user", err.Error())
		return
	}

	us.logAuditEvent(ctx, audit.ActionUpdate, audit.ResourceUser, user.ID, user.Username, http.StatusOK)

	writeJSON(w, http.StatusOK, user)
}

// deactivateUser soft-deletes a user by setting is_active=false and clearing sessions.
// DELETE /api/v1/auth/users/{id}
func (us *UserServer) deactivateUser(w http.ResponseWriter, r *http.Request, id string) {
	ctx := r.Context()

	user, err := us.userStore.GetByID(ctx, id)
	if err != nil {
		us.writeErr(ctx, w, http.StatusInternalServerError, "failed to get user", err.Error())
		return
	}
	if user == nil {
		us.writeErr(ctx, w, http.StatusNotFound, "user not found", "")
		return
	}

	user.IsActive = false
	user.UpdatedAt = time.Now().UTC()

	if err := us.userStore.Update(ctx, user); err != nil {
		us.writeErr(ctx, w, http.StatusInternalServerError, "failed to deactivate user", err.Error())
		return
	}

	// Delete all sessions for this user.
	_ = us.sessionStore.DeleteByUserID(ctx, user.ID)

	us.logAuditEvent(ctx, audit.ActionDelete, audit.ResourceUser, user.ID, user.Username, http.StatusOK)

	w.WriteHeader(http.StatusNoContent)
}

// changePassword changes a user's password.
// PATCH /api/v1/auth/users/{id}/password
// Self-service (user can change own password) or admin can change anyone's.
func (us *UserServer) changePassword(w http.ResponseWriter, r *http.Request, id string) {
	ctx := r.Context()

	// Authorization: self or admin.
	currentUser := auth.UserFromContext(ctx)
	role := auth.GetEffectiveRole(ctx)
	isSelf := currentUser != nil && currentUser.ID == id
	isAdmin := auth.HasPermission(role, auth.ResourceUsers, auth.ActionUpdate)

	if !isSelf && !isAdmin {
		writeJSON(w, http.StatusForbidden, apiError{Error: "forbidden"})
		return
	}

	user, err := us.userStore.GetByID(ctx, id)
	if err != nil {
		us.writeErr(ctx, w, http.StatusInternalServerError, "failed to get user", err.Error())
		return
	}
	if user == nil {
		us.writeErr(ctx, w, http.StatusNotFound, "user not found", "")
		return
	}

	var input struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		us.writeErr(ctx, w, http.StatusBadRequest, "invalid json", "")
		return
	}

	if input.NewPassword == "" {
		us.writeErr(ctx, w, http.StatusBadRequest, "new_password is required", "")
		return
	}
	if err := auth.ValidatePassword(input.NewPassword, 0); err != nil {
		us.writeErr(ctx, w, http.StatusBadRequest, "password too weak", err.Error())
		return
	}

	// Self-service requires current password.
	if isSelf && !isAdmin {
		if input.CurrentPassword == "" {
			us.writeErr(ctx, w, http.StatusBadRequest, "current_password is required", "")
			return
		}
		if err := auth.VerifyPassword(input.CurrentPassword, user.PasswordHash); err != nil {
			us.writeErr(ctx, w, http.StatusUnauthorized, "invalid current password", "")
			return
		}
	}

	hash, err := auth.HashPassword(input.NewPassword)
	if err != nil {
		us.writeErr(ctx, w, http.StatusInternalServerError, "failed to hash password", err.Error())
		return
	}

	user.PasswordHash = hash
	user.UpdatedAt = time.Now().UTC()

	if err := us.userStore.Update(ctx, user); err != nil {
		us.writeErr(ctx, w, http.StatusInternalServerError, "failed to update password", err.Error())
		return
	}

	us.logAuditEvent(ctx, audit.ActionUpdate, audit.ResourceUser, user.ID, user.Username, http.StatusOK)

	w.WriteHeader(http.StatusNoContent)
}

// logAuditEvent is a helper to log audit events with actor context.
func (us *UserServer) logAuditEvent(ctx context.Context, action, resourceType, resourceID, resourceName string, statusCode int) {
	if us.auditLogger == nil {
		return
	}

	actor := "anonymous"
	actorType := audit.ActorTypeAnonymous
	if user := auth.UserFromContext(ctx); user != nil {
		actor = user.Username
		actorType = audit.ActorTypeUser
	} else if key := auth.APIKeyFromContext(ctx); key != nil {
		actor = key.Prefix
		actorType = audit.ActorTypeAPIKey
	}

	event := &audit.AuditEvent{
		Actor:        actor,
		ActorType:    actorType,
		Action:       action,
		ResourceType: resourceType,
		ResourceID:   resourceID,
		ResourceName: resourceName,
		StatusCode:   statusCode,
	}
	if reqID := RequestIDFromContext(ctx); reqID != "" {
		event.RequestID = reqID
	}
	_ = us.auditLogger.Log(ctx, event)
}
