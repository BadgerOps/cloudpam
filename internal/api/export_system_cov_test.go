package api

import (
	"archive/zip"
	"bytes"
	"encoding/csv"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"cloudpam/internal/auth"
	"cloudpam/internal/domain"
	"cloudpam/internal/storage"
)

// readZipCSVsCov unpacks a ZIP response body into a map of filename → CSV records.
func readZipCSVsCov(t *testing.T, rr *httptest.ResponseRecorder) map[string][][]string {
	t.Helper()
	body := rr.Body.Bytes()
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	out := map[string][][]string{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("open %s: %v", f.Name, err)
		}
		records, err := csv.NewReader(rc).ReadAll()
		_ = rc.Close()
		if err != nil {
			t.Fatalf("read csv %s: %v", f.Name, err)
		}
		out[f.Name] = records
	}
	return out
}

// seedExportFixtureCov populates accounts and a parent/child pool pair.
func seedExportFixtureCov(t *testing.T, st *storage.MemoryStore) (domain.Account, domain.Pool, domain.Pool) {
	t.Helper()
	acct, err := st.CreateAccount(t.Context(), domain.CreateAccount{
		Key: "aws:1", Name: "prod", Provider: "aws", ExternalID: "111",
		Description: "prod account", Platform: "eks", Tier: "gold",
		Environment: "production", Regions: []string{"us-east-1", "us-west-2"},
	})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	parent, err := st.CreatePool(t.Context(), domain.CreatePool{Name: "root", CIDR: "10.0.0.0/16"})
	if err != nil {
		t.Fatalf("create parent pool: %v", err)
	}
	child, err := st.CreatePool(t.Context(), domain.CreatePool{
		Name: "child", CIDR: "10.0.1.0/24", ParentID: &parent.ID, AccountID: &acct.ID,
	})
	if err != nil {
		t.Fatalf("create child pool: %v", err)
	}
	return acct, parent, child
}

func TestExportDatasetValidationCov(t *testing.T) {
	srv, _ := setupTestServer()

	tests := []struct {
		name     string
		method   string
		path     string
		wantCode int
		wantErr  string
	}{
		{"method not allowed", http.MethodPost, "/api/v1/export?datasets=pools", http.StatusMethodNotAllowed, "method not allowed"},
		{"missing datasets", http.MethodGet, "/api/v1/export", http.StatusBadRequest, "datasets is required"},
		{"blank datasets", http.MethodGet, "/api/v1/export?datasets=%20%20", http.StatusBadRequest, "datasets is required"},
		{"only unknown datasets", http.MethodGet, "/api/v1/export?datasets=widgets,gizmos", http.StatusBadRequest, "no valid datasets requested"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rr := assertStatusCov(t, doReqCov(t, srv.mux, tc.method, tc.path, ""), tc.wantCode)
			if e := decodeErrCov(t, rr); e.Error != tc.wantErr {
				t.Fatalf("error = %q, want %q", e.Error, tc.wantErr)
			}
		})
	}
}

func TestExportDatasetCombinationsCov(t *testing.T) {
	srv, st := setupTestServer()
	seedExportFixtureCov(t, st)

	tests := []struct {
		name      string
		datasets  string
		wantFiles []string
	}{
		{"accounts only", "accounts", []string{"accounts.csv"}},
		{"pools only", "pools", []string{"pools.csv"}},
		{"blocks only", "blocks", []string{"blocks.csv"}},
		{"accounts and pools", "accounts,pools", []string{"accounts.csv", "pools.csv"}},
		{"all three", "accounts,pools,blocks", []string{"accounts.csv", "pools.csv", "blocks.csv"}},
		{"mixed case and unknown names", " Accounts , WIDGETS ,pools", []string{"accounts.csv", "pools.csv"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rr := assertStatusCov(t, doReqCov(t, srv.mux, http.MethodGet, "/api/v1/export?datasets="+strings.ReplaceAll(tc.datasets, " ", "%20"), ""), http.StatusOK)
			if ct := rr.Header().Get("Content-Type"); ct != "application/zip" {
				t.Fatalf("Content-Type = %q", ct)
			}
			if cd := rr.Header().Get("Content-Disposition"); !strings.Contains(cd, "cloudpam-export-") {
				t.Fatalf("Content-Disposition = %q", cd)
			}
			files := readZipCSVsCov(t, rr)
			if len(files) != len(tc.wantFiles) {
				t.Fatalf("zip contains %d files (%v), want %v", len(files), keysOfCov(files), tc.wantFiles)
			}
			for _, name := range tc.wantFiles {
				if _, ok := files[name]; !ok {
					t.Fatalf("missing %s in zip (%v)", name, keysOfCov(files))
				}
			}
		})
	}
}

func keysOfCov(m map[string][][]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestExportContentAndFieldSelectionCov(t *testing.T) {
	srv, st := setupTestServer()
	acct, parent, child := seedExportFixtureCov(t, st)

	t.Run("default account columns", func(t *testing.T) {
		rr := assertStatusCov(t, doReqCov(t, srv.mux, http.MethodGet, "/api/v1/export?datasets=accounts", ""), http.StatusOK)
		rows := readZipCSVsCov(t, rr)["accounts.csv"]
		if len(rows) != 2 {
			t.Fatalf("expected header + 1 row, got %d", len(rows))
		}
		hdr := rows[0]
		rec := map[string]string{}
		for i, col := range hdr {
			rec[col] = rows[1][i]
		}
		if rec["key"] != acct.Key || rec["name"] != "prod" || rec["provider"] != "aws" {
			t.Fatalf("unexpected account row: %#v", rec)
		}
		if rec["regions"] != "us-east-1|us-west-2" {
			t.Fatalf("regions = %q, want pipe-joined", rec["regions"])
		}
		if rec["platform"] != "eks" || rec["tier"] != "gold" || rec["environment"] != "production" {
			t.Fatalf("account metadata missing: %#v", rec)
		}
	})

	t.Run("selected account columns", func(t *testing.T) {
		rr := assertStatusCov(t, doReqCov(t, srv.mux, http.MethodGet,
			"/api/v1/export?datasets=accounts&accounts_fields=name,%20,regions,unknown_col", ""), http.StatusOK)
		rows := readZipCSVsCov(t, rr)["accounts.csv"]
		if got := rows[0]; len(got) != 3 || got[0] != "name" || got[1] != "regions" || got[2] != "unknown_col" {
			t.Fatalf("header = %#v, want [name regions unknown_col]", got)
		}
		if rows[1][0] != "prod" || rows[1][1] != "us-east-1|us-west-2" || rows[1][2] != "" {
			t.Fatalf("row = %#v (unknown column must be blank)", rows[1])
		}
	})

	t.Run("pool rows carry hierarchy", func(t *testing.T) {
		rr := assertStatusCov(t, doReqCov(t, srv.mux, http.MethodGet, "/api/v1/export?datasets=pools", ""), http.StatusOK)
		rows := readZipCSVsCov(t, rr)["pools.csv"]
		if len(rows) != 3 {
			t.Fatalf("expected header + 2 pools, got %d", len(rows))
		}
		idx := map[string]int{}
		for i, col := range rows[0] {
			idx[col] = i
		}
		if rows[1][idx["parent_id"]] != "" || rows[1][idx["account_id"]] != "" {
			t.Fatalf("root pool should have empty parent/account: %#v", rows[1])
		}
		if rows[2][idx["parent_id"]] != itoa(parent.ID) {
			t.Fatalf("child parent_id = %q, want %d", rows[2][idx["parent_id"]], parent.ID)
		}
		if rows[2][idx["cidr"]] != child.CIDR {
			t.Fatalf("child cidr = %q", rows[2][idx["cidr"]])
		}
	})

	t.Run("blocks join account metadata", func(t *testing.T) {
		rr := assertStatusCov(t, doReqCov(t, srv.mux, http.MethodGet, "/api/v1/export?datasets=blocks", ""), http.StatusOK)
		rows := readZipCSVsCov(t, rr)["blocks.csv"]
		if len(rows) != 2 {
			t.Fatalf("expected header + 1 block (only child pools), got %d", len(rows))
		}
		idx := map[string]int{}
		for i, col := range rows[0] {
			idx[col] = i
		}
		if rows[1][idx["parent_name"]] != "root" {
			t.Fatalf("parent_name = %q", rows[1][idx["parent_name"]])
		}
		if rows[1][idx["account_name"]] != "prod" || rows[1][idx["account_tier"]] != "gold" {
			t.Fatalf("account metadata not joined: %#v", rows[1])
		}
		if rows[1][idx["account_regions"]] != "us-east-1|us-west-2" {
			t.Fatalf("account_regions = %q", rows[1][idx["account_regions"]])
		}
	})

	t.Run("blocks respect pool and account filters", func(t *testing.T) {
		rr := assertStatusCov(t, doReqCov(t, srv.mux, http.MethodGet,
			"/api/v1/export?datasets=blocks&pools="+itoa(parent.ID)+"&accounts="+itoa(acct.ID), ""), http.StatusOK)
		rows := readZipCSVsCov(t, rr)["blocks.csv"]
		if len(rows) != 2 {
			t.Fatalf("expected the child block to match, got %d rows", len(rows))
		}

		rr = assertStatusCov(t, doReqCov(t, srv.mux, http.MethodGet,
			"/api/v1/export?datasets=blocks&pools=9999", ""), http.StatusOK)
		rows = readZipCSVsCov(t, rr)["blocks.csv"]
		if len(rows) != 1 {
			t.Fatalf("expected only a header for a non-matching pool filter, got %d rows", len(rows))
		}

		rr = assertStatusCov(t, doReqCov(t, srv.mux, http.MethodGet,
			"/api/v1/export?datasets=blocks&accounts=9999,not-a-number", ""), http.StatusOK)
		rows = readZipCSVsCov(t, rr)["blocks.csv"]
		if len(rows) != 1 {
			t.Fatalf("expected only a header for a non-matching account filter, got %d rows", len(rows))
		}
	})
}

func TestExportRequiredResourcesCov(t *testing.T) {
	tests := []struct {
		datasets string
		want     []string
	}{
		{"accounts", []string{auth.ResourceAccounts}},
		{"pools", []string{auth.ResourcePools}},
		{"blocks", []string{auth.ResourceAccounts, auth.ResourcePools}},
		{"accounts,pools", []string{auth.ResourceAccounts, auth.ResourcePools}},
		{"widgets", nil},
	}
	for _, tc := range tests {
		got := exportRequiredResources(parseExportDatasets(tc.datasets))
		if len(got) != len(tc.want) {
			t.Fatalf("%s: got %v, want %v", tc.datasets, got, tc.want)
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Fatalf("%s: got %v, want %v", tc.datasets, got, tc.want)
			}
		}
	}
}

// --- system handlers ---

func TestSystemInfoCov(t *testing.T) {
	srv, _ := setupTestServer()
	srv.SetAppVersion("v1.2.3")

	t.Run("method not allowed", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/system/info", nil)
		rr := httptest.NewRecorder()
		srv.handleSystemInfo(rr, req)
		assertStatusCov(t, rr, http.StatusMethodNotAllowed)
	})

	t.Run("reports version and upgrade mode", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/system/info", nil)
		rr := httptest.NewRecorder()
		srv.handleSystemInfo(rr, req)
		assertStatusCov(t, rr, http.StatusOK)

		var resp map[string]any
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if resp["version"] != "1.2.3" {
			t.Fatalf("version = %v, want 1.2.3 (v prefix stripped)", resp["version"])
		}
		if resp["release_url"] != "https://github.com/BadgerOps/cloudpam/releases/tag/v1.2.3" {
			t.Fatalf("release_url = %v", resp["release_url"])
		}
		if resp["auth_enabled"] != true {
			t.Fatalf("auth_enabled = %v", resp["auth_enabled"])
		}
		if resp["upgrade_mode"] != "manual" && resp["upgrade_mode"] != "file_trigger" {
			t.Fatalf("upgrade_mode = %v", resp["upgrade_mode"])
		}
		if _, ok := resp["in_app_upgrade_enabled"].(bool); !ok {
			t.Fatalf("in_app_upgrade_enabled missing or not a bool: %v", resp["in_app_upgrade_enabled"])
		}
	})

	t.Run("dev version falls back to the releases index", func(t *testing.T) {
		devSrv, _ := setupTestServer()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/system/info", nil)
		rr := httptest.NewRecorder()
		devSrv.handleSystemInfo(rr, req)
		assertStatusCov(t, rr, http.StatusOK)

		var resp map[string]any
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if resp["version"] != "dev" {
			t.Fatalf("version = %v, want dev", resp["version"])
		}
		if resp["release_url"] != "https://github.com/BadgerOps/cloudpam/releases" {
			t.Fatalf("release_url = %v", resp["release_url"])
		}
	})
}

func TestChangelogCov(t *testing.T) {
	srv, _ := setupTestServer()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/system/changelog", nil)
	rr := httptest.NewRecorder()
	srv.handleChangelog(rr, req)
	assertStatusCov(t, rr, http.StatusMethodNotAllowed)

	req = httptest.NewRequest(http.MethodGet, "/api/v1/system/changelog", nil)
	rr = httptest.NewRecorder()
	srv.handleChangelog(rr, req)
	assertStatusCov(t, rr, http.StatusOK)
	if ct := rr.Header().Get("Content-Type"); ct != "text/markdown; charset=utf-8" {
		t.Fatalf("Content-Type = %q", ct)
	}
	if rr.Body.Len() == 0 {
		t.Fatal("changelog body should not be empty")
	}
}

func TestSetupCov(t *testing.T) {
	t.Run("method not allowed", func(t *testing.T) {
		srv, _ := setupTestServer()
		rr := assertStatusCov(t, doReqCov(t, srv.mux, http.MethodGet, "/api/v1/auth/setup", ""), http.StatusMethodNotAllowed)
		if e := decodeErrCov(t, rr); e.Error != "method not allowed" {
			t.Fatalf("error = %q", e.Error)
		}
	})

	t.Run("setup already completed", func(t *testing.T) {
		srv, _ := setupTestServer()
		rr := assertStatusCov(t, doReqCov(t, srv.mux, http.MethodPost, "/api/v1/auth/setup",
			`{"username":"admin","password":"Str0ngPassw0rd!x"}`), http.StatusForbidden)
		if e := decodeErrCov(t, rr); e.Error != "setup already completed" {
			t.Fatalf("error = %q", e.Error)
		}
	})

	t.Run("user store not configured", func(t *testing.T) {
		srv, _ := setupTestServer()
		srv.SetNeedsSetup(true)
		rr := assertStatusCov(t, doReqCov(t, srv.mux, http.MethodPost, "/api/v1/auth/setup",
			`{"username":"admin","password":"Str0ngPassw0rd!x"}`), http.StatusServiceUnavailable)
		if e := decodeErrCov(t, rr); e.Error != "user store not configured" {
			t.Fatalf("error = %q", e.Error)
		}
	})

	t.Run("validation", func(t *testing.T) {
		tests := []struct {
			name    string
			body    string
			wantErr string
		}{
			{"malformed json", `{`, "invalid request body"},
			{"missing username", `{"password":"Str0ngPassw0rd!x"}`, "username is required"},
			{"blank username", `{"username":"   ","password":"Str0ngPassw0rd!x"}`, "username is required"},
			{"weak password", `{"username":"admin","password":"abc"}`, "password too weak"},
		}
		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				srv, _ := setupTestServer()
				srv.SetNeedsSetup(true)
				srv.SetUserStore(auth.NewMemoryUserStore())
				rr := assertStatusCov(t, doReqCov(t, srv.mux, http.MethodPost, "/api/v1/auth/setup", tc.body), http.StatusBadRequest)
				if e := decodeErrCov(t, rr); e.Error != tc.wantErr {
					t.Fatalf("error = %q, want %q", e.Error, tc.wantErr)
				}
			})
		}
	})

	t.Run("creates the admin account", func(t *testing.T) {
		srv, _ := setupTestServer()
		srv.SetNeedsSetup(true)
		userStore := auth.NewMemoryUserStore()
		srv.SetUserStore(userStore)

		rr := assertStatusCov(t, doReqCov(t, srv.mux, http.MethodPost, "/api/v1/auth/setup",
			`{"username":" root ","password":"Str0ngPassw0rd!x"}`), http.StatusCreated)
		var resp map[string]string
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if resp["username"] != "root" || resp["message"] != "admin account created" {
			t.Fatalf("unexpected response: %#v", resp)
		}

		created, err := userStore.GetByUsername(t.Context(), "root")
		if err != nil || created == nil {
			t.Fatalf("admin user not created: %v", err)
		}
		if created.Role != auth.RoleAdmin || !created.IsActive {
			t.Fatalf("unexpected admin user: %+v", created)
		}
		if err := auth.VerifyPassword("Str0ngPassw0rd!x", created.PasswordHash); err != nil {
			t.Fatalf("password not hashed correctly: %v", err)
		}

		// A second call is now rejected because setup is complete.
		rr = assertStatusCov(t, doReqCov(t, srv.mux, http.MethodPost, "/api/v1/auth/setup",
			`{"username":"root2","password":"Str0ngPassw0rd!x"}`), http.StatusForbidden)
		if e := decodeErrCov(t, rr); e.Error != "setup already completed" {
			t.Fatalf("error = %q", e.Error)
		}
	})

	t.Run("email defaults to username@localhost", func(t *testing.T) {
		srv, _ := setupTestServer()
		srv.SetNeedsSetup(true)
		userStore := auth.NewMemoryUserStore()
		srv.SetUserStore(userStore)

		assertStatusCov(t, doReqCov(t, srv.mux, http.MethodPost, "/api/v1/auth/setup",
			`{"username":"root","password":"Str0ngPassw0rd!x"}`), http.StatusCreated)
		created, err := userStore.GetByUsername(t.Context(), "root")
		if err != nil || created == nil {
			t.Fatalf("admin user not created: %v", err)
		}
		if created.Email != "root@localhost" {
			t.Fatalf("email = %q, want the localhost default", created.Email)
		}
	})

	// Regression for #244: the default email was built from the raw username
	// while the stored username was trimmed, embedding whitespace inside the
	// address of the most privileged account on the system.
	t.Run("default email is derived from the trimmed username", func(t *testing.T) {
		srv, _ := setupTestServer()
		srv.SetNeedsSetup(true)
		userStore := auth.NewMemoryUserStore()
		srv.SetUserStore(userStore)

		assertStatusCov(t, doReqCov(t, srv.mux, http.MethodPost, "/api/v1/auth/setup",
			`{"username":" root ","password":"Str0ngPassw0rd!x"}`), http.StatusCreated)
		created, err := userStore.GetByUsername(t.Context(), "root")
		if err != nil || created == nil {
			t.Fatalf("admin user not created: %v", err)
		}
		if created.Email != "root@localhost" {
			t.Fatalf("email = %q, want %q with no embedded whitespace", created.Email, "root@localhost")
		}
		if created.DisplayName != "root" {
			t.Fatalf("display name = %q, want the trimmed username", created.DisplayName)
		}
	})

	t.Run("explicit email is trimmed and preserved", func(t *testing.T) {
		srv, _ := setupTestServer()
		srv.SetNeedsSetup(true)
		userStore := auth.NewMemoryUserStore()
		srv.SetUserStore(userStore)

		assertStatusCov(t, doReqCov(t, srv.mux, http.MethodPost, "/api/v1/auth/setup",
			`{"username":"root","password":"Str0ngPassw0rd!x","email":"  ops@example.test  "}`), http.StatusCreated)
		created, err := userStore.GetByUsername(t.Context(), "root")
		if err != nil || created == nil {
			t.Fatalf("admin user not created: %v", err)
		}
		if created.Email != "ops@example.test" {
			t.Fatalf("email = %q", created.Email)
		}
	})

	t.Run("existing users close the setup window", func(t *testing.T) {
		srv, _ := setupTestServer()
		srv.SetNeedsSetup(true)
		userStore := auth.NewMemoryUserStore()
		newUserCov(t, userStore, "u1", "someone", auth.RoleViewer, "Str0ngPassw0rd!x")
		srv.SetUserStore(userStore)

		rr := assertStatusCov(t, doReqCov(t, srv.mux, http.MethodPost, "/api/v1/auth/setup",
			`{"username":"admin","password":"Str0ngPassw0rd!x"}`), http.StatusForbidden)
		if e := decodeErrCov(t, rr); e.Error != "setup already completed" {
			t.Fatalf("error = %q", e.Error)
		}
	})
}

func TestHealthReportsSetupStateCov(t *testing.T) {
	srv, _ := setupTestServer()
	srv.SetNeedsSetup(true)
	srv.SetAppVersion(" v2.0.0 ")

	rr := assertStatusCov(t, doReqCov(t, srv.mux, http.MethodGet, "/healthz", ""), http.StatusOK)
	var resp map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp["needs_setup"] != true {
		t.Fatalf("needs_setup = %v", resp["needs_setup"])
	}
	if resp["version"] != "2.0.0" {
		t.Fatalf("version = %v, want 2.0.0", resp["version"])
	}
	if resp["local_auth_enabled"] != false {
		t.Fatalf("local_auth_enabled = %v, want false by default", resp["local_auth_enabled"])
	}

	// A settings store overrides the server default.
	ss := storage.NewMemorySettingsStore()
	settings, err := ss.GetSecuritySettings(t.Context())
	if err != nil {
		t.Fatalf("get settings: %v", err)
	}
	settings.LocalAuthEnabled = true
	if err := ss.UpdateSecuritySettings(t.Context(), settings); err != nil {
		t.Fatalf("update settings: %v", err)
	}
	srv.SetSettingsStore(ss)

	rr = assertStatusCov(t, doReqCov(t, srv.mux, http.MethodGet, "/healthz", ""), http.StatusOK)
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp["local_auth_enabled"] != true {
		t.Fatalf("local_auth_enabled = %v, want the settings-store value", resp["local_auth_enabled"])
	}
}

func TestSPAAssetHandlingCov(t *testing.T) {
	srv, _ := setupTestServer()

	// Asset-like paths that do not exist must 404 rather than fall back to index.html.
	for _, path := range []string{"/assets/missing.js", "/missing.css", "/nope.png"} {
		rr := doReqCov(t, srv.mux, http.MethodGet, path, "")
		if rr.Code != http.StatusNotFound {
			t.Fatalf("%s: status = %d, want 404", path, rr.Code)
		}
	}

	// Client-side routes fall back to index.html.
	rr := doReqCov(t, srv.mux, http.MethodGet, "/pools", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("Content-Type = %q", ct)
	}
}

func TestSPAInjectsSentryDSNCov(t *testing.T) {
	t.Setenv("SENTRY_FRONTEND_DSN", "https://public@sentry.example.test/42")
	srv, _ := setupTestServer()

	rr := doReqCov(t, srv.mux, http.MethodGet, "/dashboard", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), `<meta name="sentry-dsn" content="https://public@sentry.example.test/42">`) {
		t.Fatal("expected the Sentry DSN meta tag to be injected into index.html")
	}
}

func TestAuditListPaginationCov(t *testing.T) {
	srv, _ := setupTestServer()

	rr := assertStatusCov(t, doReqCov(t, srv.mux, http.MethodPost, "/api/v1/audit", ""), http.StatusMethodNotAllowed)
	if e := decodeErrCov(t, rr); e.Error != "method not allowed" {
		t.Fatalf("error = %q", e.Error)
	}

	tests := []struct {
		name         string
		query        string
		wantLimit    float64
		wantOffset   float64
		expectStatus int
	}{
		{"defaults", "", 50, 0, http.StatusOK},
		{"explicit", "?limit=10&offset=5", 10, 5, http.StatusOK},
		{"limit zero falls back", "?limit=0", 50, 0, http.StatusOK},
		{"limit over max falls back", "?limit=5000", 50, 0, http.StatusOK},
		{"negative offset falls back", "?offset=-4", 50, 0, http.StatusOK},
		{"non numeric falls back", "?limit=abc&offset=xyz", 50, 0, http.StatusOK},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rr := assertStatusCov(t, doReqCov(t, srv.mux, http.MethodGet, "/api/v1/audit"+tc.query, ""), tc.expectStatus)
			var resp map[string]any
			if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if resp["limit"] != tc.wantLimit || resp["offset"] != tc.wantOffset {
				t.Fatalf("limit/offset = %v/%v, want %v/%v", resp["limit"], resp["offset"], tc.wantLimit, tc.wantOffset)
			}
		})
	}
}

var _ io.Reader = strings.NewReader("")
