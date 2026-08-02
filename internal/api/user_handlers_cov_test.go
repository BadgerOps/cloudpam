package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"cloudpam/internal/auth"
)

// newUserCov creates and stores a user with the given role and password.
func newUserCov(t *testing.T, store auth.UserStore, id, username string, role auth.Role, password string) *auth.User {
	t.Helper()
	hash, err := auth.HashPassword(password)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	now := time.Now().UTC()
	u := &auth.User{
		ID:           id,
		Username:     username,
		Email:        username + "@example.test",
		DisplayName:  username,
		Role:         role,
		PasswordHash: hash,
		IsActive:     true,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := store.Create(t.Context(), u); err != nil {
		t.Fatalf("create user: %v", err)
	}
	return u
}

// doUserReqCov issues a request against the user server mux, optionally with a
// principal (user + role) attached to the request context.
func doUserReqCov(t *testing.T, us *UserServer, method, path, body string, principal *auth.User, role auth.Role) *httptest.ResponseRecorder {
	t.Helper()
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, r)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	ctx := req.Context()
	if principal != nil {
		ctx = auth.ContextWithUser(ctx, principal)
	}
	if role != auth.RoleNone {
		ctx = auth.ContextWithRole(ctx, role)
	}
	req = req.WithContext(ctx)
	rr := httptest.NewRecorder()
	us.mux.ServeHTTP(rr, req)
	return rr
}

// --- /api/v1/auth/users collection ---

func TestUsersCollectionMethodNotAllowedCov(t *testing.T) {
	us, _, _ := setupUserTestServer()
	rr := assertStatusCov(t, doUserReqCov(t, us, http.MethodPut, "/api/v1/auth/users", "", nil, auth.RoleNone), http.StatusMethodNotAllowed)
	if allow := rr.Header().Get("Allow"); allow != "GET, POST" {
		t.Fatalf("Allow = %q", allow)
	}
	if e := decodeErrCov(t, rr); e.Error != "method not allowed" {
		t.Fatalf("error = %q", e.Error)
	}
}

func TestListUsersCov(t *testing.T) {
	us, _, userStore := setupUserTestServer()

	rr := assertStatusCov(t, doUserReqCov(t, us, http.MethodGet, "/api/v1/auth/users", "", nil, auth.RoleNone), http.StatusOK)
	var empty struct {
		Users []*auth.User `json:"users"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &empty); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(empty.Users) != 0 {
		t.Fatalf("expected 0 users, got %d", len(empty.Users))
	}

	newUserCov(t, userStore, "u1", "alice", auth.RoleAdmin, "Str0ngPassw0rd!x")
	newUserCov(t, userStore, "u2", "bob", auth.RoleViewer, "Str0ngPassw0rd!x")

	rr = assertStatusCov(t, doUserReqCov(t, us, http.MethodGet, "/api/v1/auth/users", "", nil, auth.RoleNone), http.StatusOK)
	var listed struct {
		Users []*auth.User `json:"users"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &listed); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(listed.Users) != 2 {
		t.Fatalf("expected 2 users, got %d", len(listed.Users))
	}
	for _, u := range listed.Users {
		if len(u.PasswordHash) != 0 {
			t.Fatalf("password hash leaked in list response for %s", u.Username)
		}
	}
}

func TestCreateUserValidationCov(t *testing.T) {
	us, _, userStore := setupUserTestServer()
	newUserCov(t, userStore, "existing", "taken", auth.RoleViewer, "Str0ngPassw0rd!x")

	tests := []struct {
		name     string
		body     string
		wantCode int
		wantErr  string
	}{
		{"malformed json", `{`, http.StatusBadRequest, "invalid json"},
		{"missing username", `{"password":"Str0ngPassw0rd!x"}`, http.StatusBadRequest, "username is required"},
		{"username too long", `{"username":"` + strings.Repeat("a", 256) + `","password":"Str0ngPassw0rd!x"}`, http.StatusBadRequest, "username too long"},
		{"missing password", `{"username":"newbie"}`, http.StatusBadRequest, "password is required"},
		{"weak password", `{"username":"newbie","password":"short"}`, http.StatusBadRequest, "password too weak"},
		{"invalid role", `{"username":"newbie","password":"Str0ngPassw0rd!x","role":"wizard"}`, http.StatusBadRequest, "invalid role"},
		{"duplicate username", `{"username":"taken","password":"Str0ngPassw0rd!x"}`, http.StatusConflict, "user already exists"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rr := assertStatusCov(t, doUserReqCov(t, us, http.MethodPost, "/api/v1/auth/users", tc.body, nil, auth.RoleNone), tc.wantCode)
			if e := decodeErrCov(t, rr); e.Error != tc.wantErr {
				t.Fatalf("error = %q, want %q", e.Error, tc.wantErr)
			}
		})
	}
}

func TestCreateUserSuccessCov(t *testing.T) {
	us, _, userStore := setupUserTestServer()

	body := `{"username":"carol","email":" carol@example.test ","display_name":" Carol ","password":"Str0ngPassw0rd!x"}`
	rr := assertStatusCov(t, doUserReqCov(t, us, http.MethodPost, "/api/v1/auth/users", body, nil, auth.RoleNone), http.StatusCreated)
	var created auth.User
	if err := json.Unmarshal(rr.Body.Bytes(), &created); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if created.Username != "carol" {
		t.Fatalf("username = %q", created.Username)
	}
	if created.Email != "carol@example.test" || created.DisplayName != "Carol" {
		t.Fatalf("fields not trimmed: %+v", created)
	}
	if created.Role != auth.RoleViewer {
		t.Fatalf("role = %q, want the viewer default", created.Role)
	}
	if !created.IsActive {
		t.Fatal("new user should be active")
	}

	stored, err := userStore.GetByUsername(t.Context(), "carol")
	if err != nil || stored == nil {
		t.Fatalf("user not persisted: %v", err)
	}
	if err := auth.VerifyPassword("Str0ngPassw0rd!x", stored.PasswordHash); err != nil {
		t.Fatalf("stored password hash does not verify: %v", err)
	}

	// Explicit role is honoured.
	rr = assertStatusCov(t, doUserReqCov(t, us, http.MethodPost, "/api/v1/auth/users",
		`{"username":"dave","password":"Str0ngPassw0rd!x","role":"admin"}`, nil, auth.RoleNone), http.StatusCreated)
	var admin auth.User
	if err := json.Unmarshal(rr.Body.Bytes(), &admin); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if admin.Role != auth.RoleAdmin {
		t.Fatalf("role = %q, want admin", admin.Role)
	}
}

// --- /api/v1/auth/users/{id} ---

func TestUserByIDRoutingCov(t *testing.T) {
	us, _, userStore := setupUserTestServer()
	u := newUserCov(t, userStore, "u1", "alice", auth.RoleViewer, "Str0ngPassw0rd!x")

	tests := []struct {
		name      string
		method    string
		path      string
		wantCode  int
		wantErr   string
		wantAllow string
	}{
		{"empty id", http.MethodGet, "/api/v1/auth/users/", http.StatusNotFound, "not found", ""},
		{"unknown method on user", http.MethodPut, "/api/v1/auth/users/" + u.ID, http.StatusMethodNotAllowed, "method not allowed", "GET, PATCH, DELETE"},
		{"password wrong method", http.MethodPost, "/api/v1/auth/users/" + u.ID + "/password", http.StatusMethodNotAllowed, "method not allowed", http.MethodPatch},
		{"revoke wrong method", http.MethodGet, "/api/v1/auth/users/" + u.ID + "/revoke-sessions", http.StatusMethodNotAllowed, "method not allowed", http.MethodPost},
		{"unlock wrong method", http.MethodGet, "/api/v1/auth/users/" + u.ID + "/unlock", http.StatusMethodNotAllowed, "method not allowed", http.MethodPost},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rr := assertStatusCov(t, doUserReqCov(t, us, tc.method, tc.path, "", nil, auth.RoleNone), tc.wantCode)
			if e := decodeErrCov(t, rr); e.Error != tc.wantErr {
				t.Fatalf("error = %q, want %q", e.Error, tc.wantErr)
			}
			if tc.wantAllow != "" {
				if allow := rr.Header().Get("Allow"); allow != tc.wantAllow {
					t.Fatalf("Allow = %q, want %q", allow, tc.wantAllow)
				}
			}
		})
	}
}

func TestGetUserCov(t *testing.T) {
	us, _, userStore := setupUserTestServer()
	u := newUserCov(t, userStore, "u1", "alice", auth.RoleOperator, "Str0ngPassw0rd!x")

	rr := assertStatusCov(t, doUserReqCov(t, us, http.MethodGet, "/api/v1/auth/users/missing", "", nil, auth.RoleNone), http.StatusNotFound)
	if e := decodeErrCov(t, rr); e.Error != "user not found" {
		t.Fatalf("error = %q", e.Error)
	}

	rr = assertStatusCov(t, doUserReqCov(t, us, http.MethodGet, "/api/v1/auth/users/"+u.ID, "", nil, auth.RoleNone), http.StatusOK)
	var got auth.User
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.ID != u.ID || got.Username != "alice" || got.Role != auth.RoleOperator {
		t.Fatalf("unexpected user: %+v", got)
	}
}

func TestUpdateUserCov(t *testing.T) {
	us, sessionStore, userStore := setupUserTestServer()
	u := newUserCov(t, userStore, "u1", "alice", auth.RoleViewer, "Str0ngPassw0rd!x")

	t.Run("not found", func(t *testing.T) {
		rr := assertStatusCov(t, doUserReqCov(t, us, http.MethodPatch, "/api/v1/auth/users/nope", `{}`, nil, auth.RoleNone), http.StatusNotFound)
		if e := decodeErrCov(t, rr); e.Error != "user not found" {
			t.Fatalf("error = %q", e.Error)
		}
	})

	t.Run("malformed json", func(t *testing.T) {
		rr := assertStatusCov(t, doUserReqCov(t, us, http.MethodPatch, "/api/v1/auth/users/"+u.ID, `{`, nil, auth.RoleNone), http.StatusBadRequest)
		if e := decodeErrCov(t, rr); e.Error != "invalid json" {
			t.Fatalf("error = %q", e.Error)
		}
	})

	t.Run("invalid role", func(t *testing.T) {
		rr := assertStatusCov(t, doUserReqCov(t, us, http.MethodPatch, "/api/v1/auth/users/"+u.ID, `{"role":"sorcerer"}`, nil, auth.RoleNone), http.StatusBadRequest)
		if e := decodeErrCov(t, rr); e.Error != "invalid role" {
			t.Fatalf("error = %q", e.Error)
		}
	})

	t.Run("updates fields", func(t *testing.T) {
		body := `{"email":" new@example.test ","display_name":" New Name ","role":"operator"}`
		rr := assertStatusCov(t, doUserReqCov(t, us, http.MethodPatch, "/api/v1/auth/users/"+u.ID, body, nil, auth.RoleNone), http.StatusOK)
		var updated auth.User
		if err := json.Unmarshal(rr.Body.Bytes(), &updated); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if updated.Email != "new@example.test" || updated.DisplayName != "New Name" || updated.Role != auth.RoleOperator {
			t.Fatalf("unexpected update: %+v", updated)
		}
		stored, err := userStore.GetByID(t.Context(), u.ID)
		if err != nil {
			t.Fatalf("get user: %v", err)
		}
		if stored.Role != auth.RoleOperator {
			t.Fatalf("role not persisted: %q", stored.Role)
		}
	})

	t.Run("deactivating kills sessions", func(t *testing.T) {
		sess, err := auth.NewSession(u.ID, auth.RoleOperator, auth.DefaultSessionDuration, nil)
		if err != nil {
			t.Fatalf("new session: %v", err)
		}
		if err := sessionStore.Create(t.Context(), sess); err != nil {
			t.Fatalf("create session: %v", err)
		}

		assertStatusCov(t, doUserReqCov(t, us, http.MethodPatch, "/api/v1/auth/users/"+u.ID, `{"is_active":false}`, nil, auth.RoleNone), http.StatusOK)

		sessions, err := sessionStore.ListByUserID(t.Context(), u.ID)
		if err != nil {
			t.Fatalf("list sessions: %v", err)
		}
		if len(sessions) != 0 {
			t.Fatalf("expected sessions to be revoked, got %d", len(sessions))
		}
		stored, err := userStore.GetByID(t.Context(), u.ID)
		if err != nil {
			t.Fatalf("get user: %v", err)
		}
		if stored.IsActive {
			t.Fatal("user should be inactive")
		}
	})
}

func TestDeactivateUserCov(t *testing.T) {
	us, sessionStore, userStore := setupUserTestServer()
	u := newUserCov(t, userStore, "u1", "alice", auth.RoleViewer, "Str0ngPassw0rd!x")
	sess, err := auth.NewSession(u.ID, u.Role, auth.DefaultSessionDuration, nil)
	if err != nil {
		t.Fatalf("new session: %v", err)
	}
	if err := sessionStore.Create(t.Context(), sess); err != nil {
		t.Fatalf("create session: %v", err)
	}

	rr := assertStatusCov(t, doUserReqCov(t, us, http.MethodDelete, "/api/v1/auth/users/ghost", "", nil, auth.RoleNone), http.StatusNotFound)
	if e := decodeErrCov(t, rr); e.Error != "user not found" {
		t.Fatalf("error = %q", e.Error)
	}

	rr = assertStatusCov(t, doUserReqCov(t, us, http.MethodDelete, "/api/v1/auth/users/"+u.ID, "", nil, auth.RoleNone), http.StatusNoContent)
	if rr.Body.Len() != 0 {
		t.Fatalf("expected empty body, got %q", rr.Body.String())
	}
	stored, err := userStore.GetByID(t.Context(), u.ID)
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	if stored.IsActive {
		t.Fatal("user should be deactivated")
	}
	sessions, err := sessionStore.ListByUserID(t.Context(), u.ID)
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if len(sessions) != 0 {
		t.Fatalf("expected sessions removed, got %d", len(sessions))
	}
}

// --- password change ---

func TestChangePasswordCov(t *testing.T) {
	us, _, userStore := setupUserTestServer()
	self := newUserCov(t, userStore, "u1", "alice", auth.RoleViewer, "Str0ngPassw0rd!x")
	other := newUserCov(t, userStore, "u2", "bob", auth.RoleViewer, "Str0ngPassw0rd!x")

	path := func(id string) string { return "/api/v1/auth/users/" + id + "/password" }

	t.Run("forbidden for another user", func(t *testing.T) {
		rr := assertStatusCov(t, doUserReqCov(t, us, http.MethodPatch, path(other.ID),
			`{"new_password":"An0therStrongPwd!"}`, self, auth.RoleViewer), http.StatusForbidden)
		if e := decodeErrCov(t, rr); e.Error != "forbidden" {
			t.Fatalf("error = %q", e.Error)
		}
	})

	t.Run("admin on missing user", func(t *testing.T) {
		rr := assertStatusCov(t, doUserReqCov(t, us, http.MethodPatch, path("ghost"),
			`{"new_password":"An0therStrongPwd!"}`, nil, auth.RoleAdmin), http.StatusNotFound)
		if e := decodeErrCov(t, rr); e.Error != "user not found" {
			t.Fatalf("error = %q", e.Error)
		}
	})

	t.Run("malformed json", func(t *testing.T) {
		rr := assertStatusCov(t, doUserReqCov(t, us, http.MethodPatch, path(self.ID), `{`, self, auth.RoleViewer), http.StatusBadRequest)
		if e := decodeErrCov(t, rr); e.Error != "invalid json" {
			t.Fatalf("error = %q", e.Error)
		}
	})

	t.Run("missing new password", func(t *testing.T) {
		rr := assertStatusCov(t, doUserReqCov(t, us, http.MethodPatch, path(self.ID), `{}`, self, auth.RoleViewer), http.StatusBadRequest)
		if e := decodeErrCov(t, rr); e.Error != "new_password is required" {
			t.Fatalf("error = %q", e.Error)
		}
	})

	t.Run("weak new password", func(t *testing.T) {
		rr := assertStatusCov(t, doUserReqCov(t, us, http.MethodPatch, path(self.ID), `{"new_password":"abc"}`, self, auth.RoleViewer), http.StatusBadRequest)
		if e := decodeErrCov(t, rr); e.Error != "password too weak" {
			t.Fatalf("error = %q", e.Error)
		}
	})

	t.Run("self requires current password", func(t *testing.T) {
		rr := assertStatusCov(t, doUserReqCov(t, us, http.MethodPatch, path(self.ID),
			`{"new_password":"An0therStrongPwd!"}`, self, auth.RoleViewer), http.StatusBadRequest)
		if e := decodeErrCov(t, rr); e.Error != "current_password is required" {
			t.Fatalf("error = %q", e.Error)
		}
	})

	t.Run("self wrong current password", func(t *testing.T) {
		rr := assertStatusCov(t, doUserReqCov(t, us, http.MethodPatch, path(self.ID),
			`{"current_password":"WrongPassw0rd!x","new_password":"An0therStrongPwd!"}`, self, auth.RoleViewer), http.StatusUnauthorized)
		if e := decodeErrCov(t, rr); e.Error != "invalid current password" {
			t.Fatalf("error = %q", e.Error)
		}
	})

	t.Run("self success", func(t *testing.T) {
		assertStatusCov(t, doUserReqCov(t, us, http.MethodPatch, path(self.ID),
			`{"current_password":"Str0ngPassw0rd!x","new_password":"An0therStrongPwd!"}`, self, auth.RoleViewer), http.StatusNoContent)
		stored, err := userStore.GetByID(t.Context(), self.ID)
		if err != nil {
			t.Fatalf("get user: %v", err)
		}
		if err := auth.VerifyPassword("An0therStrongPwd!", stored.PasswordHash); err != nil {
			t.Fatalf("new password not stored: %v", err)
		}
	})

	t.Run("admin needs no current password", func(t *testing.T) {
		assertStatusCov(t, doUserReqCov(t, us, http.MethodPatch, path(other.ID),
			`{"new_password":"AdminSetPassw0rd!"}`, nil, auth.RoleAdmin), http.StatusNoContent)
		stored, err := userStore.GetByID(t.Context(), other.ID)
		if err != nil {
			t.Fatalf("get user: %v", err)
		}
		if err := auth.VerifyPassword("AdminSetPassw0rd!", stored.PasswordHash); err != nil {
			t.Fatalf("admin-set password not stored: %v", err)
		}
	})
}

// --- revoke sessions / unlock ---

func TestRevokeSessionsForbiddenCov(t *testing.T) {
	us, _, userStore := setupUserTestServer()
	caller := newUserCov(t, userStore, "u1", "alice", auth.RoleViewer, "Str0ngPassw0rd!x")
	target := newUserCov(t, userStore, "u2", "bob", auth.RoleViewer, "Str0ngPassw0rd!x")

	rr := assertStatusCov(t, doUserReqCov(t, us, http.MethodPost, "/api/v1/auth/users/"+target.ID+"/revoke-sessions",
		"", caller, auth.RoleViewer), http.StatusForbidden)
	if e := decodeErrCov(t, rr); e.Error != "forbidden" {
		t.Fatalf("error = %q", e.Error)
	}
}

func TestRevokeSessionsSelfCov(t *testing.T) {
	us, sessionStore, userStore := setupUserTestServer()
	caller := newUserCov(t, userStore, "u1", "alice", auth.RoleViewer, "Str0ngPassw0rd!x")
	sess, err := auth.NewSession(caller.ID, caller.Role, auth.DefaultSessionDuration, nil)
	if err != nil {
		t.Fatalf("new session: %v", err)
	}
	if err := sessionStore.Create(t.Context(), sess); err != nil {
		t.Fatalf("create session: %v", err)
	}

	rr := assertStatusCov(t, doUserReqCov(t, us, http.MethodPost, "/api/v1/auth/users/"+caller.ID+"/revoke-sessions",
		"", caller, auth.RoleViewer), http.StatusOK)
	var body map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if body["status"] != "sessions revoked" {
		t.Fatalf("status = %q", body["status"])
	}
	sessions, err := sessionStore.ListByUserID(t.Context(), caller.ID)
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if len(sessions) != 0 {
		t.Fatalf("expected sessions cleared, got %d", len(sessions))
	}
}

func TestUnlockUserCov(t *testing.T) {
	us, _, userStore := setupUserTestServer()
	locked := newUserCov(t, userStore, "u1", "alice", auth.RoleViewer, "Str0ngPassw0rd!x")
	now := time.Now().UTC()
	until := now.Add(time.Hour)
	locked.LockedAt = &now
	locked.LockoutUntil = &until
	locked.FailedLoginAttempts = 5
	if err := userStore.Update(t.Context(), locked); err != nil {
		t.Fatalf("update user: %v", err)
	}

	t.Run("forbidden for non admin", func(t *testing.T) {
		rr := assertStatusCov(t, doUserReqCov(t, us, http.MethodPost, "/api/v1/auth/users/"+locked.ID+"/unlock",
			"", nil, auth.RoleViewer), http.StatusForbidden)
		if e := decodeErrCov(t, rr); e.Error != "forbidden" {
			t.Fatalf("error = %q", e.Error)
		}
	})

	t.Run("not found", func(t *testing.T) {
		rr := assertStatusCov(t, doUserReqCov(t, us, http.MethodPost, "/api/v1/auth/users/ghost/unlock",
			"", nil, auth.RoleAdmin), http.StatusNotFound)
		if e := decodeErrCov(t, rr); e.Error != "user not found" {
			t.Fatalf("error = %q", e.Error)
		}
	})

	t.Run("admin unlocks", func(t *testing.T) {
		assertStatusCov(t, doUserReqCov(t, us, http.MethodPost, "/api/v1/auth/users/"+locked.ID+"/unlock",
			"", nil, auth.RoleAdmin), http.StatusOK)
		stored, err := userStore.GetByID(t.Context(), locked.ID)
		if err != nil {
			t.Fatalf("get user: %v", err)
		}
		if stored.LockedAt != nil || stored.LockoutUntil != nil || stored.FailedLoginAttempts != 0 {
			t.Fatalf("lockout state not cleared: %+v", stored)
		}
	})
}

// --- /api/v1/auth/me and /logout ---

func TestMeCov(t *testing.T) {
	us, _, userStore := setupUserTestServer()
	u := newUserCov(t, userStore, "u1", "alice", auth.RoleAdmin, "Str0ngPassw0rd!x")

	t.Run("method not allowed", func(t *testing.T) {
		rr := assertStatusCov(t, doUserReqCov(t, us, http.MethodPost, "/api/v1/auth/me", "", nil, auth.RoleNone), http.StatusMethodNotAllowed)
		if allow := rr.Header().Get("Allow"); allow != http.MethodGet {
			t.Fatalf("Allow = %q", allow)
		}
	})

	t.Run("unauthenticated", func(t *testing.T) {
		rr := assertStatusCov(t, doUserReqCov(t, us, http.MethodGet, "/api/v1/auth/me", "", nil, auth.RoleNone), http.StatusUnauthorized)
		if e := decodeErrCov(t, rr); e.Error != "not authenticated" {
			t.Fatalf("error = %q", e.Error)
		}
	})

	t.Run("session identity", func(t *testing.T) {
		rr := assertStatusCov(t, doUserReqCov(t, us, http.MethodGet, "/api/v1/auth/me", "", u, auth.RoleAdmin), http.StatusOK)
		var resp struct {
			AuthType     string     `json:"auth_type"`
			Role         auth.Role  `json:"role"`
			User         *auth.User `json:"user"`
			AuthProvider string     `json:"auth_provider"`
			Permissions  []string   `json:"permissions"`
		}
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if resp.AuthType != "session" || resp.User == nil || resp.User.ID != u.ID {
			t.Fatalf("unexpected response: %+v", resp)
		}
		if resp.AuthProvider != "local" {
			t.Fatalf("auth_provider = %q, want local", resp.AuthProvider)
		}
		if len(resp.Permissions) == 0 {
			t.Fatal("expected permissions for an admin session")
		}
	})

	t.Run("api key identity", func(t *testing.T) {
		key := &auth.APIKey{ID: "key-1", Name: "ci", Scopes: []string{"pools:read"}}
		req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
		req = req.WithContext(auth.ContextWithAPIKey(req.Context(), key))
		rr := httptest.NewRecorder()
		us.mux.ServeHTTP(rr, req)
		assertStatusCov(t, rr, http.StatusOK)

		var resp struct {
			AuthType    string   `json:"auth_type"`
			KeyID       string   `json:"key_id"`
			KeyName     string   `json:"key_name"`
			Permissions []string `json:"permissions"`
		}
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if resp.AuthType != "api_key" || resp.KeyID != "key-1" || resp.KeyName != "ci" {
			t.Fatalf("unexpected response: %+v", resp)
		}
		perms := map[string]bool{}
		for _, p := range resp.Permissions {
			perms[p] = true
		}
		if !perms["pools:read"] {
			t.Fatalf("permissions = %#v, want pools:read", resp.Permissions)
		}
		if perms["users:create"] || perms["pools:delete"] {
			t.Fatalf("read-only key must not gain write permissions: %#v", resp.Permissions)
		}
	})
}

func TestLogoutCov(t *testing.T) {
	us, sessionStore, userStore := setupUserTestServer()
	u := newUserCov(t, userStore, "u1", "alice", auth.RoleViewer, "Str0ngPassw0rd!x")

	t.Run("method not allowed", func(t *testing.T) {
		rr := assertStatusCov(t, doUserReqCov(t, us, http.MethodGet, "/api/v1/auth/logout", "", nil, auth.RoleNone), http.StatusMethodNotAllowed)
		if allow := rr.Header().Get("Allow"); allow != http.MethodPost {
			t.Fatalf("Allow = %q", allow)
		}
	})

	t.Run("clears cookie without a session", func(t *testing.T) {
		rr := assertStatusCov(t, doUserReqCov(t, us, http.MethodPost, "/api/v1/auth/logout", "", nil, auth.RoleNone), http.StatusNoContent)
		cookies := rr.Result().Cookies()
		if len(cookies) != 1 || cookies[0].Name != "session" || cookies[0].MaxAge != -1 {
			t.Fatalf("expected an expiring session cookie, got %#v", cookies)
		}
	})

	t.Run("deletes the active session", func(t *testing.T) {
		sess, err := auth.NewSession(u.ID, u.Role, auth.DefaultSessionDuration, nil)
		if err != nil {
			t.Fatalf("new session: %v", err)
		}
		if err := sessionStore.Create(t.Context(), sess); err != nil {
			t.Fatalf("create session: %v", err)
		}

		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
		ctx := auth.ContextWithUser(req.Context(), u)
		ctx = auth.ContextWithSession(ctx, sess)
		req = req.WithContext(ctx)
		rr := httptest.NewRecorder()
		us.mux.ServeHTTP(rr, req)
		assertStatusCov(t, rr, http.StatusNoContent)

		if got, err := sessionStore.Get(t.Context(), sess.ID); err == nil && got != nil {
			t.Fatal("session should have been deleted")
		}
	})

	t.Run("secure cookie behind https proxy", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
		req.Header.Set("X-Forwarded-Proto", "https")
		rr := httptest.NewRecorder()
		us.mux.ServeHTTP(rr, req)
		assertStatusCov(t, rr, http.StatusNoContent)
		cookies := rr.Result().Cookies()
		if len(cookies) != 1 || !cookies[0].Secure || cookies[0].SameSite != http.SameSiteStrictMode {
			t.Fatalf("expected a secure strict cookie, got %#v", cookies)
		}
	})
}

// --- login ---

func TestLoginCov(t *testing.T) {
	us, sessionStore, userStore := setupUserTestServer()
	u := newUserCov(t, userStore, "u1", "alice", auth.RoleViewer, "Str0ngPassw0rd!x")
	disabled := newUserCov(t, userStore, "u2", "bob", auth.RoleViewer, "Str0ngPassw0rd!x")
	disabled.IsActive = false
	if err := userStore.Update(t.Context(), disabled); err != nil {
		t.Fatalf("update user: %v", err)
	}

	errCases := []struct {
		name     string
		method   string
		body     string
		wantCode int
		wantErr  string
	}{
		{"method not allowed", http.MethodGet, "", http.StatusMethodNotAllowed, "method not allowed"},
		{"malformed json", http.MethodPost, `{`, http.StatusBadRequest, "invalid json"},
		{"missing fields", http.MethodPost, `{"username":"  "}`, http.StatusBadRequest, "username and password are required"},
		{"unknown user", http.MethodPost, `{"username":"nobody","password":"Str0ngPassw0rd!x"}`, http.StatusUnauthorized, "invalid credentials"},
		{"wrong password", http.MethodPost, `{"username":"alice","password":"WrongPassw0rd!x"}`, http.StatusUnauthorized, "invalid credentials"},
		{"inactive account", http.MethodPost, `{"username":"bob","password":"Str0ngPassw0rd!x"}`, http.StatusForbidden, "account disabled"},
	}
	for _, tc := range errCases {
		t.Run(tc.name, func(t *testing.T) {
			rr := assertStatusCov(t, doUserReqCov(t, us, tc.method, "/api/v1/auth/login", tc.body, nil, auth.RoleNone), tc.wantCode)
			if e := decodeErrCov(t, rr); e.Error != tc.wantErr {
				t.Fatalf("error = %q, want %q", e.Error, tc.wantErr)
			}
		})
	}

	t.Run("success sets session cookie", func(t *testing.T) {
		rr := assertStatusCov(t, doUserReqCov(t, us, http.MethodPost, "/api/v1/auth/login",
			`{"username":"alice","password":"Str0ngPassw0rd!x"}`, nil, auth.RoleNone), http.StatusOK)
		var resp struct {
			User        *auth.User `json:"user"`
			ExpiresAt   time.Time  `json:"expires_at"`
			Permissions []string   `json:"permissions"`
		}
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if resp.User == nil || resp.User.ID != u.ID {
			t.Fatalf("unexpected user in response: %+v", resp.User)
		}
		if !resp.ExpiresAt.After(time.Now().UTC()) {
			t.Fatalf("expires_at should be in the future: %v", resp.ExpiresAt)
		}
		cookies := rr.Result().Cookies()
		if len(cookies) != 1 || cookies[0].Name != "session" || cookies[0].Value == "" {
			t.Fatalf("expected a session cookie, got %#v", cookies)
		}
		if !cookies[0].HttpOnly {
			t.Fatal("session cookie must be HttpOnly")
		}
		sessions, err := sessionStore.ListByUserID(t.Context(), u.ID)
		if err != nil {
			t.Fatalf("list sessions: %v", err)
		}
		if len(sessions) == 0 {
			t.Fatal("expected a stored session")
		}
	})

	t.Run("locked account is rejected", func(t *testing.T) {
		stored, err := userStore.GetByID(t.Context(), u.ID)
		if err != nil {
			t.Fatalf("get user: %v", err)
		}
		now := time.Now().UTC()
		until := now.Add(time.Hour)
		stored.LockedAt = &now
		stored.LockoutUntil = &until
		if err := userStore.Update(t.Context(), stored); err != nil {
			t.Fatalf("update user: %v", err)
		}

		rr := assertStatusCov(t, doUserReqCov(t, us, http.MethodPost, "/api/v1/auth/login",
			`{"username":"alice","password":"Str0ngPassw0rd!x"}`, nil, auth.RoleNone), http.StatusLocked)
		if e := decodeErrCov(t, rr); e.Error != "account locked" {
			t.Fatalf("error = %q", e.Error)
		}
	})

	t.Run("expired lockout is cleared on login", func(t *testing.T) {
		stored, err := userStore.GetByID(t.Context(), u.ID)
		if err != nil {
			t.Fatalf("get user: %v", err)
		}
		past := time.Now().UTC().Add(-2 * time.Hour)
		expired := time.Now().UTC().Add(-time.Hour)
		stored.LockedAt = &past
		stored.LockoutUntil = &expired
		stored.FailedLoginAttempts = 3
		if err := userStore.Update(t.Context(), stored); err != nil {
			t.Fatalf("update user: %v", err)
		}

		assertStatusCov(t, doUserReqCov(t, us, http.MethodPost, "/api/v1/auth/login",
			`{"username":"alice","password":"Str0ngPassw0rd!x"}`, nil, auth.RoleNone), http.StatusOK)

		after, err := userStore.GetByID(t.Context(), u.ID)
		if err != nil {
			t.Fatalf("get user: %v", err)
		}
		if after.LockedAt != nil || after.LockoutUntil != nil || after.FailedLoginAttempts != 0 {
			t.Fatalf("lockout not cleared: %+v", after)
		}
	})
}
