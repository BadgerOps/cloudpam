package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"cloudpam/internal/auth"
	"cloudpam/internal/domain"
)

// oidcDoCov issues a request against the OIDC server mux.
func oidcDoCov(t *testing.T, env *testOIDCEnv, method, path string, body string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	var r *strings.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	var req *http.Request
	if r != nil {
		req = httptest.NewRequest(method, path, r)
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	env.oidcServer.mux.ServeHTTP(rec, req)
	return rec
}

func TestOIDCPublicEndpointMethodsCov(t *testing.T) {
	env := setupOIDCTestEnv(t)

	cases := []struct {
		name   string
		method string
		path   string
		allow  string
	}{
		{"login", http.MethodPost, "/api/v1/auth/oidc/login", http.MethodGet},
		{"callback", http.MethodPost, "/api/v1/auth/oidc/callback", http.MethodGet},
		{"providers", http.MethodPost, "/api/v1/auth/oidc/providers", http.MethodGet},
		{"refresh", http.MethodGet, "/api/v1/auth/oidc/refresh", http.MethodPost},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := oidcDoCov(t, env, tc.method, tc.path, "")
			assertStatusCov(t, rec, http.StatusMethodNotAllowed)
			if allow := rec.Header().Get("Allow"); allow != tc.allow {
				t.Fatalf("Allow = %q, want %q", allow, tc.allow)
			}
			if e := decodeErrCov(t, rec); e.Error != "method not allowed" {
				t.Fatalf("error = %q", e.Error)
			}
		})
	}
}

func TestOIDCLoginDisabledProviderCov(t *testing.T) {
	env := setupOIDCTestEnv(t)

	provider, err := env.oidcServer.oidcStore.GetProvider(t.Context(), env.providerID)
	if err != nil {
		t.Fatalf("get provider: %v", err)
	}
	provider.Enabled = false
	if err := env.oidcServer.oidcStore.UpdateProvider(t.Context(), provider); err != nil {
		t.Fatalf("update provider: %v", err)
	}

	rec := oidcDoCov(t, env, http.MethodGet, "/api/v1/auth/oidc/login?provider_id="+env.providerID, "")
	assertStatusCov(t, rec, http.StatusBadRequest)
	if e := decodeErrCov(t, rec); e.Error != "provider is disabled" {
		t.Fatalf("error = %q", e.Error)
	}
}

func TestOIDCLoginForwardsPromptCov(t *testing.T) {
	env := setupOIDCTestEnv(t)

	rec := oidcDoCov(t, env, http.MethodGet, "/api/v1/auth/oidc/login?provider_id="+env.providerID+"&prompt=none", "")
	assertStatusCov(t, rec, http.StatusFound)
	location := rec.Header().Get("Location")
	if !strings.Contains(location, "prompt=none") {
		t.Fatalf("redirect must forward prompt=none: %s", location)
	}
}

func TestOIDCCallbackParamValidationCov(t *testing.T) {
	env := setupOIDCTestEnv(t)

	t.Run("missing code and state", func(t *testing.T) {
		rec := oidcDoCov(t, env, http.MethodGet, "/api/v1/auth/oidc/callback", "")
		assertStatusCov(t, rec, http.StatusBadRequest)
		if e := decodeErrCov(t, rec); e.Error != "missing code or state" {
			t.Fatalf("error = %q", e.Error)
		}
	})

	t.Run("missing code only", func(t *testing.T) {
		rec := oidcDoCov(t, env, http.MethodGet, "/api/v1/auth/oidc/callback?state=abc", "")
		assertStatusCov(t, rec, http.StatusBadRequest)
	})

	t.Run("malformed state", func(t *testing.T) {
		state := "no-colon-here"
		rec := oidcDoCov(t, env, http.MethodGet, "/api/v1/auth/oidc/callback?code=c&state="+state, "",
			&http.Cookie{Name: "oidc_state", Value: state})
		assertStatusCov(t, rec, http.StatusBadRequest)
		if e := decodeErrCov(t, rec); e.Error != "malformed state" {
			t.Fatalf("error = %q", e.Error)
		}
	})

	t.Run("empty provider id in state", func(t *testing.T) {
		state := ":nonce"
		rec := oidcDoCov(t, env, http.MethodGet, "/api/v1/auth/oidc/callback?code=c&state="+state, "",
			&http.Cookie{Name: "oidc_state", Value: state})
		assertStatusCov(t, rec, http.StatusBadRequest)
		if e := decodeErrCov(t, rec); e.Error != "malformed state" {
			t.Fatalf("error = %q", e.Error)
		}
	})

	t.Run("unknown provider in state", func(t *testing.T) {
		state := "no-such-provider:nonce"
		rec := oidcDoCov(t, env, http.MethodGet, "/api/v1/auth/oidc/callback?code=c&state="+state, "",
			&http.Cookie{Name: "oidc_state", Value: state})
		assertStatusCov(t, rec, http.StatusNotFound)
		if e := decodeErrCov(t, rec); e.Error != "provider not found" {
			t.Fatalf("error = %q", e.Error)
		}
	})

	t.Run("state cookie is cleared", func(t *testing.T) {
		state := env.providerID + ":nonce"
		rec := oidcDoCov(t, env, http.MethodGet, "/api/v1/auth/oidc/callback?code=bad-code&state="+state, "",
			&http.Cookie{Name: "oidc_state", Value: state})
		// The exchange fails against the mock IdP, but the state cookie must
		// already have been expired before we get there.
		var cleared bool
		for _, c := range rec.Result().Cookies() {
			if c.Name == "oidc_state" && c.MaxAge == -1 {
				cleared = true
			}
		}
		if !cleared {
			t.Fatalf("expected the oidc_state cookie to be cleared, got %#v", rec.Result().Cookies())
		}
	})
}

func TestOIDCCallbackIdPErrorCov(t *testing.T) {
	env := setupOIDCTestEnv(t)

	t.Run("browser redirect", func(t *testing.T) {
		rec := oidcDoCov(t, env, http.MethodGet, "/api/v1/auth/oidc/callback?error=login_required", "")
		assertStatusCov(t, rec, http.StatusFound)
		if loc := rec.Header().Get("Location"); loc != "/?error=login_required" {
			t.Fatalf("Location = %q", loc)
		}
	})

	t.Run("iframe posts a message", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/oidc/callback?error=login_required", nil)
		req.Header.Set("Sec-Fetch-Dest", "iframe")
		rec := httptest.NewRecorder()
		env.oidcServer.mux.ServeHTTP(rec, req)
		assertStatusCov(t, rec, http.StatusOK)
		body := rec.Body.String()
		if !strings.Contains(body, `type:"oidc-refresh"`) || !strings.Contains(body, "success:false") {
			t.Fatalf("unexpected iframe body: %s", body)
		}
		if ct := rec.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
			t.Fatalf("Content-Type = %q", ct)
		}
	})

	t.Run("iframe escapes the error parameter", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/oidc/callback?error=%3Cscript%3Ealert(1)%3C/script%3E", nil)
		req.Header.Set("Sec-Fetch-Dest", "iframe")
		rec := httptest.NewRecorder()
		env.oidcServer.mux.ServeHTTP(rec, req)
		assertStatusCov(t, rec, http.StatusOK)
		if strings.Contains(rec.Body.String(), "<script>alert(1)</script>") {
			t.Fatalf("error parameter was not escaped: %s", rec.Body.String())
		}
	})
}

func TestOIDCListPublicProvidersOnlyEnabledCov(t *testing.T) {
	env := setupOIDCTestEnv(t)

	rec := oidcDoCov(t, env, http.MethodGet, "/api/v1/auth/oidc/providers", "")
	assertStatusCov(t, rec, http.StatusOK)
	var resp struct {
		Providers []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"providers"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Providers) != 1 || resp.Providers[0].ID != env.providerID {
		t.Fatalf("unexpected providers: %+v", resp.Providers)
	}

	provider, err := env.oidcServer.oidcStore.GetProvider(t.Context(), env.providerID)
	if err != nil {
		t.Fatalf("get provider: %v", err)
	}
	provider.Enabled = false
	if err := env.oidcServer.oidcStore.UpdateProvider(t.Context(), provider); err != nil {
		t.Fatalf("update provider: %v", err)
	}

	rec = oidcDoCov(t, env, http.MethodGet, "/api/v1/auth/oidc/providers", "")
	assertStatusCov(t, rec, http.StatusOK)
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Providers) != 0 {
		t.Fatalf("disabled providers must not be listed: %+v", resp.Providers)
	}
}

func TestOIDCRefreshCov(t *testing.T) {
	env := setupOIDCTestEnv(t)

	t.Run("no session cookie", func(t *testing.T) {
		rec := oidcDoCov(t, env, http.MethodPost, "/api/v1/auth/oidc/refresh", "")
		assertStatusCov(t, rec, http.StatusUnauthorized)
		if e := decodeErrCov(t, rec); e.Error != "no session" {
			t.Fatalf("error = %q", e.Error)
		}
	})

	t.Run("empty session cookie", func(t *testing.T) {
		rec := oidcDoCov(t, env, http.MethodPost, "/api/v1/auth/oidc/refresh", "",
			&http.Cookie{Name: "session", Value: ""})
		assertStatusCov(t, rec, http.StatusUnauthorized)
		if e := decodeErrCov(t, rec); e.Error != "no session" {
			t.Fatalf("error = %q", e.Error)
		}
	})

	t.Run("unknown session", func(t *testing.T) {
		rec := oidcDoCov(t, env, http.MethodPost, "/api/v1/auth/oidc/refresh", "",
			&http.Cookie{Name: "session", Value: "not-a-real-session"})
		assertStatusCov(t, rec, http.StatusUnauthorized)
		if e := decodeErrCov(t, rec); e.Error != "invalid session" {
			t.Fatalf("error = %q", e.Error)
		}
	})

	newSession := func(t *testing.T, userID string) string {
		t.Helper()
		sess, err := auth.NewSession(userID, auth.RoleViewer, auth.DefaultSessionDuration, nil)
		if err != nil {
			t.Fatalf("new session: %v", err)
		}
		if err := env.oidcServer.sessionStore.Create(t.Context(), sess); err != nil {
			t.Fatalf("create session: %v", err)
		}
		return sess.ID
	}

	t.Run("session for an unknown user", func(t *testing.T) {
		id := newSession(t, "ghost-user")
		rec := oidcDoCov(t, env, http.MethodPost, "/api/v1/auth/oidc/refresh", "",
			&http.Cookie{Name: "session", Value: id})
		assertStatusCov(t, rec, http.StatusUnauthorized)
		if e := decodeErrCov(t, rec); e.Error != "user not found" {
			t.Fatalf("error = %q", e.Error)
		}
	})

	t.Run("local user is not refreshable", func(t *testing.T) {
		local := &auth.User{
			ID: "local-1", Username: "local", Role: auth.RoleViewer, IsActive: true,
			AuthProvider: "local", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
		}
		if err := env.oidcServer.userStore.Create(t.Context(), local); err != nil {
			t.Fatalf("create user: %v", err)
		}
		id := newSession(t, local.ID)
		rec := oidcDoCov(t, env, http.MethodPost, "/api/v1/auth/oidc/refresh", "",
			&http.Cookie{Name: "session", Value: id})
		assertStatusCov(t, rec, http.StatusBadRequest)
		if e := decodeErrCov(t, rec); e.Error != "not an oidc session" {
			t.Fatalf("error = %q", e.Error)
		}
	})

	t.Run("oidc user with an unknown issuer", func(t *testing.T) {
		u := &auth.User{
			ID: "oidc-orphan", Username: "orphan", Role: auth.RoleViewer, IsActive: true,
			AuthProvider: "oidc", OIDCIssuer: "https://nowhere.example.test", OIDCSubject: "s1",
			CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
		}
		if err := env.oidcServer.userStore.Create(t.Context(), u); err != nil {
			t.Fatalf("create user: %v", err)
		}
		id := newSession(t, u.ID)
		rec := oidcDoCov(t, env, http.MethodPost, "/api/v1/auth/oidc/refresh", "",
			&http.Cookie{Name: "session", Value: id})
		assertStatusCov(t, rec, http.StatusNotFound)
		if e := decodeErrCov(t, rec); e.Error != "oidc provider not found" {
			t.Fatalf("error = %q", e.Error)
		}
	})

	t.Run("returns a silent re-auth url", func(t *testing.T) {
		provider, err := env.oidcServer.oidcStore.GetProvider(t.Context(), env.providerID)
		if err != nil {
			t.Fatalf("get provider: %v", err)
		}
		u := &auth.User{
			ID: "oidc-user", Username: "alice", Role: auth.RoleViewer, IsActive: true,
			AuthProvider: "oidc", OIDCIssuer: provider.IssuerURL, OIDCSubject: "user-123",
			CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
		}
		if err := env.oidcServer.userStore.Create(t.Context(), u); err != nil {
			t.Fatalf("create user: %v", err)
		}
		id := newSession(t, u.ID)
		rec := oidcDoCov(t, env, http.MethodPost, "/api/v1/auth/oidc/refresh", "",
			&http.Cookie{Name: "session", Value: id})
		assertStatusCov(t, rec, http.StatusOK)

		var resp struct {
			RedirectURL string `json:"redirect_url"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		want := "/api/v1/auth/oidc/login?provider_id=" + env.providerID + "&prompt=none"
		if resp.RedirectURL != want {
			t.Fatalf("redirect_url = %q, want %q", resp.RedirectURL, want)
		}
	})
}

func TestOIDCAdminUpdateIgnoresBadFieldTypesCov(t *testing.T) {
	oidcSrv := setupOIDCAdminTestEnv(t)

	created := createOIDCProviderCov(t, oidcSrv, `{
		"name":"Acme","issuer_url":"https://acme.example.test",
		"client_id":"cid","client_secret":"secret",
		"scopes":"openid","default_role":"viewer","auto_provision":true,"enabled":true
	}`)

	// Wrong JSON types for every field are silently ignored — the stored
	// provider must be unchanged apart from updated_at.
	body := `{"name":123,"issuer_url":false,"client_id":[],"client_secret":5,"scopes":{},
	          "role_mapping":"nope","default_role":9,"auto_provision":"yes","enabled":"no"}`
	req := httptest.NewRequest(http.MethodPatch, "/api/v1/settings/oidc/providers/"+created.ID, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	oidcSrv.mux.ServeHTTP(rec, req)
	assertStatusCov(t, rec, http.StatusOK)

	var updated domain.OIDCProvider
	if err := json.Unmarshal(rec.Body.Bytes(), &updated); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if updated.Name != "Acme" || updated.IssuerURL != "https://acme.example.test" || updated.ClientID != "cid" {
		t.Fatalf("mistyped fields must be ignored, got %+v", updated)
	}
	if !updated.AutoProvision || !updated.Enabled {
		t.Fatalf("boolean fields must be unchanged, got %+v", updated)
	}
	if updated.ClientSecretMasked != "****" {
		t.Fatalf("client secret must be masked, got %q", updated.ClientSecretMasked)
	}
}

func TestOIDCAdminUpdateBlankSecretKeptCov(t *testing.T) {
	oidcSrv := setupOIDCAdminTestEnv(t)

	created := createOIDCProviderCov(t, oidcSrv, `{
		"name":"Acme","issuer_url":"https://acme2.example.test",
		"client_id":"cid","client_secret":"original-secret","enabled":true
	}`)
	before, err := oidcSrv.oidcStore.GetProvider(t.Context(), created.ID)
	if err != nil {
		t.Fatalf("get provider: %v", err)
	}
	original := before.ClientSecretEncrypted

	req := httptest.NewRequest(http.MethodPatch, "/api/v1/settings/oidc/providers/"+created.ID,
		strings.NewReader(`{"client_secret":"","name":"Acme Renamed"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	oidcSrv.mux.ServeHTTP(rec, req)
	assertStatusCov(t, rec, http.StatusOK)

	after, err := oidcSrv.oidcStore.GetProvider(t.Context(), created.ID)
	if err != nil {
		t.Fatalf("get provider: %v", err)
	}
	if after.ClientSecretEncrypted != original {
		t.Fatal("an empty client_secret must leave the stored secret untouched")
	}
	if after.Name != "Acme Renamed" {
		t.Fatalf("name = %q, want the updated value", after.Name)
	}
}

func TestOIDCAdminUpdateMalformedBodyCov(t *testing.T) {
	oidcSrv := setupOIDCAdminTestEnv(t)
	created := createOIDCProviderCov(t, oidcSrv, `{
		"name":"Acme","issuer_url":"https://acme3.example.test",
		"client_id":"cid","client_secret":"secret"
	}`)

	req := httptest.NewRequest(http.MethodPatch, "/api/v1/settings/oidc/providers/"+created.ID, strings.NewReader(`{`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	oidcSrv.mux.ServeHTTP(rec, req)
	assertStatusCov(t, rec, http.StatusBadRequest)
	if e := decodeErrCov(t, rec); e.Error != "invalid request body" {
		t.Fatalf("error = %q", e.Error)
	}
}

func TestOIDCAdminCreateMalformedBodyCov(t *testing.T) {
	oidcSrv := setupOIDCAdminTestEnv(t)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/settings/oidc/providers", strings.NewReader(`{`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	oidcSrv.mux.ServeHTTP(rec, req)
	assertStatusCov(t, rec, http.StatusBadRequest)
	if e := decodeErrCov(t, rec); e.Error != "invalid request body" {
		t.Fatalf("error = %q", e.Error)
	}
}

// createOIDCProviderCov posts a provider definition and returns the created record.
func createOIDCProviderCov(t *testing.T, oidcSrv *OIDCServer, body string) domain.OIDCProvider {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/settings/oidc/providers", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	oidcSrv.mux.ServeHTTP(rec, req)
	assertStatusCov(t, rec, http.StatusCreated)

	var provider domain.OIDCProvider
	if err := json.Unmarshal(rec.Body.Bytes(), &provider); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return provider
}
