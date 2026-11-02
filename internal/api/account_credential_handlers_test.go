package api

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	"cloudpam/internal/audit"
	"cloudpam/internal/auth"
	"cloudpam/internal/discovery"
	"cloudpam/internal/domain"
	"cloudpam/internal/observability"
	"cloudpam/internal/secrets"
	"cloudpam/internal/storage"
)

const (
	credTestRoleARN    = "arn:aws:iam::123456789012:role/CloudPAMDiscovery"
	credTestExternalID = "LEAK-EXTERNAL-ID-9f3a"
)

// fakeCredentialCollector is an aws collector whose bound identity call is scripted.
type fakeCredentialCollector struct {
	identity string
	err      error
}

func (c *fakeCredentialCollector) Provider() string { return "aws" }

func (c *fakeCredentialCollector) Discover(context.Context, domain.Account) ([]domain.DiscoveredResource, error) {
	return nil, nil
}

func (c *fakeCredentialCollector) WithCredential(context.Context, domain.Account, discovery.CredentialPayload) (discovery.CredentialedCollector, error) {
	return c, nil
}

func (c *fakeCredentialCollector) Identity(context.Context, domain.Account) (string, error) {
	if c.err != nil {
		return "", c.err
	}
	return c.identity, nil
}

type credentialFixture struct {
	mux   *http.ServeMux
	store *storage.MemoryStore
	audit *audit.MemoryAuditLogger
	vault *discovery.AccountCredentials
	aws   *fakeCredentialCollector
}

func newCredentialServer(t *testing.T, durable bool) *credentialFixture {
	t.Helper()
	key, err := secrets.GenerateKey()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	st := storage.NewMemoryStore()
	mux := http.NewServeMux()
	logger := observability.NewLogger(observability.Config{Level: "info", Format: "json", Output: io.Discard})
	auditLogger := audit.NewMemoryAuditLogger()
	srv := NewServer(mux, st, logger, nil, auditLogger)
	srv.handleOpenAPIRouteFunc("/api/v1/accounts", srv.handleAccounts)
	srv.handleOpenAPIRouteFunc("/api/v1/accounts/", srv.handleAccountsSubroutes)
	srv.handleOpenAPIRouteFunc("/api/v1/search", srv.handleSearch)
	srv.handleOpenAPIRouteFunc("/api/v1/export", srv.handleExport)

	vault := discovery.NewAccountCredentials(st, key, durable)
	sync := discovery.NewSyncService(storage.NewMemoryDiscoveryStore(st))
	sync.SetCredentials(vault)
	aws := &fakeCredentialCollector{identity: "arn:aws:sts::123456789012:assumed-role/CloudPAMDiscovery/cloudpam"}
	sync.RegisterCollector(aws)
	NewAccountCredentialServer(srv, vault, sync).RegisterAccountCredentialRoutes()
	return &credentialFixture{mux: mux, store: st, audit: auditLogger, vault: vault, aws: aws}
}

func (f *credentialFixture) account(t *testing.T, provider string) domain.Account {
	t.Helper()
	a, err := f.store.CreateAccount(t.Context(), domain.CreateAccount{Key: provider + ":" + rand.Text()[:12], Name: provider + " account", Provider: provider})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	return a
}

func (f *credentialFixture) do(t *testing.T, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, r)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rr := httptest.NewRecorder()
	f.mux.ServeHTTP(rr, req)
	return rr
}

func credentialPath(a domain.Account) string {
	return "/api/v1/accounts/" + itoa(a.ID) + "/credentials"
}

func awsCredentialBody(externalID string) string {
	b, _ := json.Marshal(domain.PutAccountCredentialRequest{
		Type:          domain.CredentialTypeAWSAssumeRole,
		AWSAssumeRole: &domain.AWSAssumeRoleInput{RoleARN: credTestRoleARN, ExternalID: externalID},
	})
	return string(b)
}

// gcpCredentialBody returns a PUT body with a fresh service-account key and a
// marker: one full base64 line of the private key PEM, which no response may contain.
func gcpCredentialBody(t *testing.T) (body, marker string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	pemKey := string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
	marker = strings.Split(pemKey, "\n")[1]
	keyJSON, _ := json.Marshal(map[string]string{
		"type":           "service_account",
		"project_id":     "proj-1",
		"private_key_id": "kid-1",
		"private_key":    pemKey,
		"client_email":   "sa@proj-1.iam.gserviceaccount.com",
	})
	b, _ := json.Marshal(domain.PutAccountCredentialRequest{
		Type:              domain.CredentialTypeGCPServiceAccount,
		GCPServiceAccount: &domain.GCPServiceAccountInput{KeyJSON: string(keyJSON)},
	})
	return string(b), marker
}

func decodeCredential(t *testing.T, rr *httptest.ResponseRecorder) domain.AccountCredential {
	t.Helper()
	var c domain.AccountCredential
	if err := json.Unmarshal(rr.Body.Bytes(), &c); err != nil {
		t.Fatalf("decode credential: %v: %s", err, rr.Body.String())
	}
	return c
}

func wantStatus(t *testing.T, rr *httptest.ResponseRecorder, code int, bodyContains string) {
	t.Helper()
	if rr.Code != code {
		t.Fatalf("status = %d, want %d: %s", rr.Code, code, rr.Body.String())
	}
	if bodyContains != "" && !strings.Contains(rr.Body.String(), bodyContains) {
		t.Fatalf("body %q does not contain %q", rr.Body.String(), bodyContains)
	}
}

func TestAccountCredentialPutThenGet(t *testing.T) {
	f := newCredentialServer(t, true)
	a := f.account(t, "aws")

	rr := f.do(t, http.MethodPut, credentialPath(a), awsCredentialBody(credTestExternalID))
	wantStatus(t, rr, http.StatusOK, "")
	put := decodeCredential(t, rr)
	if put.AccountID != a.ID || put.Type != domain.CredentialTypeAWSAssumeRole || put.Summary.RoleARN != credTestRoleARN || !put.Summary.HasExternalID || put.LastTest != nil {
		t.Fatalf("PUT returned %+v", put)
	}

	got := decodeCredential(t, f.do(t, http.MethodGet, credentialPath(a), ""))
	if got.Type != put.Type || got.Summary != put.Summary || !got.CreatedAt.Equal(put.CreatedAt) {
		t.Fatalf("GET = %+v, PUT = %+v", got, put)
	}
}

func TestAccountCredentialPutGCPSummary(t *testing.T) {
	f := newCredentialServer(t, true)
	a := f.account(t, "gcp")
	body, _ := gcpCredentialBody(t)

	rr := f.do(t, http.MethodPut, credentialPath(a), body)
	wantStatus(t, rr, http.StatusOK, "")
	got := decodeCredential(t, rr)
	if got.Type != domain.CredentialTypeGCPServiceAccount || got.Summary.ClientEmail != "sa@proj-1.iam.gserviceaccount.com" || got.Summary.ProjectID != "proj-1" {
		t.Fatalf("PUT returned %+v", got)
	}
}

func TestAccountCredentialGetNotFound(t *testing.T) {
	f := newCredentialServer(t, true)
	a := f.account(t, "aws")

	wantStatus(t, f.do(t, http.MethodGet, credentialPath(a), ""), http.StatusNotFound, "no credential stored")
	wantStatus(t, f.do(t, http.MethodGet, "/api/v1/accounts/999999/credentials", ""), http.StatusNotFound, "account not found")
	wantStatus(t, f.do(t, http.MethodGet, "/api/v1/accounts/abc/credentials", ""), http.StatusBadRequest, "invalid account id")
}

func TestAccountCredentialPutRejectsBadInput(t *testing.T) {
	f := newCredentialServer(t, true)
	aws := f.account(t, "aws")
	gcp := f.account(t, "gcp")
	gcpBody, _ := gcpCredentialBody(t)

	cases := []struct {
		name    string
		account domain.Account
		body    string
		want    string
	}{
		{"type mismatch", gcp, awsCredentialBody(""), "does not match the account provider"},
		{"invalid arn", aws, `{"type":"aws_assume_role","aws_assume_role":{"role_arn":"not-an-arn"}}`, "role_arn"},
		{"invalid key json", gcp, `{"type":"gcp_service_account","gcp_service_account":{"key_json":"{\"type\":\"user\"}"}}`, "key_json"},
		{"both objects", aws, `{"type":"aws_assume_role","aws_assume_role":{"role_arn":"` + credTestRoleARN + `"},"gcp_service_account":{"key_json":"{}"}}`, "only object"},
		{"unknown field", aws, `{"type":"aws_assume_role","aws_assume_role":{"role_arn":"` + credTestRoleARN + `"},"role_arn":"x"}`, `unknown field \"role_arn\"`},
		{"unknown type", aws, `{"type":"azure_sp"}`, "must be one of"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			wantStatus(t, f.do(t, http.MethodPut, credentialPath(tc.account), tc.body), http.StatusBadRequest, tc.want)
			wantStatus(t, f.do(t, http.MethodGet, credentialPath(tc.account), ""), http.StatusNotFound, "")
		})
	}
	wantStatus(t, f.do(t, http.MethodPut, "/api/v1/accounts/999999/credentials", gcpBody), http.StatusNotFound, "account not found")
}

func TestAccountCredentialPutEphemeralKey(t *testing.T) {
	f := newCredentialServer(t, false)
	a := f.account(t, "aws")

	wantStatus(t, f.do(t, http.MethodPut, credentialPath(a), awsCredentialBody("")), http.StatusConflict, "CLOUDPAM_OIDC_ENCRYPTION_KEY")
	wantStatus(t, f.do(t, http.MethodGet, credentialPath(a), ""), http.StatusNotFound, "")
}

func TestAccountCredentialDelete(t *testing.T) {
	f := newCredentialServer(t, true)
	a := f.account(t, "aws")
	wantStatus(t, f.do(t, http.MethodPut, credentialPath(a), awsCredentialBody("")), http.StatusOK, "")

	wantStatus(t, f.do(t, http.MethodDelete, credentialPath(a), ""), http.StatusNoContent, "")
	wantStatus(t, f.do(t, http.MethodGet, credentialPath(a), ""), http.StatusNotFound, "no credential stored")
	wantStatus(t, f.do(t, http.MethodDelete, credentialPath(a), ""), http.StatusNotFound, "no credential stored")
}

func TestAccountCredentialTest(t *testing.T) {
	f := newCredentialServer(t, true)
	a := f.account(t, "aws")

	wantStatus(t, f.do(t, http.MethodPost, credentialPath(a)+"/test", ""), http.StatusNotFound, "no credential stored")

	wantStatus(t, f.do(t, http.MethodPut, credentialPath(a), awsCredentialBody(credTestExternalID)), http.StatusOK, "")
	rr := f.do(t, http.MethodPost, credentialPath(a)+"/test", "")
	wantStatus(t, rr, http.StatusOK, "")
	var ok domain.CredentialTestResult
	_ = json.Unmarshal(rr.Body.Bytes(), &ok)
	if ok.Status != domain.CredentialTestOK || ok.Identity != f.aws.identity || ok.Error != "" || ok.TestedAt.IsZero() {
		t.Fatalf("test result = %+v", ok)
	}
	got := decodeCredential(t, f.do(t, http.MethodGet, credentialPath(a), ""))
	if got.LastTest == nil || got.LastTest.Status != domain.CredentialTestOK {
		t.Fatalf("GET after test = %+v", got)
	}

	f.aws.err = &discovery.ProbeError{Step: discovery.ProbeStepAssumeRole, Status: 403, Code: "AccessDenied"}
	rr = f.do(t, http.MethodPost, credentialPath(a)+"/test", "")
	wantStatus(t, rr, http.StatusOK, "")
	var failed domain.CredentialTestResult
	_ = json.Unmarshal(rr.Body.Bytes(), &failed)
	if failed.Status != domain.CredentialTestFailed || failed.Identity != "" || failed.Error != "assume_role: AccessDenied (http 403)" {
		t.Fatalf("failed test result = %+v", failed)
	}
	got = decodeCredential(t, f.do(t, http.MethodGet, credentialPath(a), ""))
	if got.LastTest == nil || got.LastTest.Status != domain.CredentialTestFailed || got.LastTest.Error != failed.Error {
		t.Fatalf("GET after failed test = %+v", got)
	}
}

func TestAccountCredentialNeverLeaks(t *testing.T) {
	f := newCredentialServer(t, true)
	aws := f.account(t, "aws")
	gcp := f.account(t, "gcp")
	gcpBody, pemMarker := gcpCredentialBody(t)
	wantStatus(t, f.do(t, http.MethodPut, credentialPath(aws), awsCredentialBody(credTestExternalID)), http.StatusOK, "")
	wantStatus(t, f.do(t, http.MethodPut, credentialPath(gcp), gcpBody), http.StatusOK, "")

	bodies := map[string]string{}
	for _, path := range []string{
		credentialPath(aws),
		credentialPath(gcp),
		"/api/v1/accounts/" + itoa(aws.ID),
		"/api/v1/accounts/" + itoa(gcp.ID),
		"/api/v1/accounts",
		"/api/v1/search?q=account",
		"/api/v1/search?q=" + credTestExternalID,
	} {
		rr := f.do(t, http.MethodGet, path, "")
		wantStatus(t, rr, http.StatusOK, "")
		bodies[path] = rr.Body.String()
	}
	rr := f.do(t, http.MethodGet, "/api/v1/export?datasets=accounts", "")
	wantStatus(t, rr, http.StatusOK, "")
	zr, err := zip.NewReader(bytes.NewReader(rr.Body.Bytes()), int64(rr.Body.Len()))
	if err != nil {
		t.Fatalf("open export zip: %v", err)
	}
	for _, file := range zr.File {
		rc, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		content, _ := io.ReadAll(rc)
		_ = rc.Close()
		bodies["export:"+file.Name] = string(content)
	}

	if !strings.Contains(bodies[credentialPath(aws)], credTestRoleARN) {
		t.Fatalf("GET credentials should show the role arn summary: %s", bodies[credentialPath(aws)])
	}
	for name, body := range bodies {
		for _, secret := range []string{credTestExternalID, pemMarker, "payload_encrypted"} {
			if strings.Contains(body, secret) {
				t.Errorf("%s leaks %q", name, secret)
			}
		}
	}
}

func TestAccountCredentialAuditEvents(t *testing.T) {
	f := newCredentialServer(t, true)
	aws := f.account(t, "aws")
	gcp := f.account(t, "gcp")
	gcpBody, pemMarker := gcpCredentialBody(t)
	wantStatus(t, f.do(t, http.MethodPut, credentialPath(aws), awsCredentialBody(credTestExternalID)), http.StatusOK, "")
	wantStatus(t, f.do(t, http.MethodPut, credentialPath(gcp), gcpBody), http.StatusOK, "")
	wantStatus(t, f.do(t, http.MethodPost, credentialPath(aws)+"/test", ""), http.StatusOK, "")
	wantStatus(t, f.do(t, http.MethodDelete, credentialPath(aws), ""), http.StatusNoContent, "")

	events, _, err := f.audit.List(t.Context(), audit.ListOptions{ResourceType: audit.ResourceAccountCredential})
	if err != nil {
		t.Fatal(err)
	}
	var actions []string
	for _, e := range events {
		actions = append(actions, e.Action+":"+e.ResourceID)
		b, err := json.Marshal(e)
		if err != nil {
			t.Fatal(err)
		}
		for _, secret := range []string{credTestExternalID, pemMarker, "payload_encrypted"} {
			if strings.Contains(string(b), secret) {
				t.Errorf("audit event %s leaks %q: %s", e.Action, secret, b)
			}
		}
		switch {
		case e.Action == audit.ActionUpdate && e.ResourceID == itoa(aws.ID) && !strings.Contains(string(b), `"type":"aws_assume_role"`):
			t.Errorf("update event = %s, want the credential type in changes", b)
		case e.Action == audit.ActionTest && !strings.Contains(string(b), `"status":"ok"`):
			t.Errorf("test event = %s, want the test status in changes", b)
		}
	}
	sort.Strings(actions)
	want := []string{
		audit.ActionDelete + ":" + itoa(aws.ID),
		audit.ActionTest + ":" + itoa(aws.ID),
		audit.ActionUpdate + ":" + itoa(aws.ID),
		audit.ActionUpdate + ":" + itoa(gcp.ID),
	}
	if strings.Join(actions, ",") != strings.Join(want, ",") {
		t.Fatalf("audit events = %v, want %v", actions, want)
	}
}

// newProtectedCredentialServer wires the credential routes behind RBAC with one API key of the given scopes.
func newProtectedCredentialServer(t *testing.T, scopes []string) (*credentialFixture, string) {
	t.Helper()
	key, err := secrets.GenerateKey()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	st := storage.NewMemoryStore()
	mux := http.NewServeMux()
	logger := observability.NewLogger(observability.Config{Level: "info", Format: "json", Output: io.Discard})
	srv := NewServer(mux, st, logger, nil, nil)
	slogger := slog.New(slog.NewJSONHandler(io.Discard, nil))

	keyStore := auth.NewMemoryKeyStore()
	plaintext, apiKey, err := auth.GenerateAPIKey(auth.GenerateAPIKeyOptions{Name: "cred-test", Scopes: scopes})
	if err != nil {
		t.Fatalf("generate api key: %v", err)
	}
	if err := keyStore.Create(t.Context(), apiKey); err != nil {
		t.Fatalf("store api key: %v", err)
	}
	sessionStore, userStore := auth.NewMemorySessionStore(), auth.NewMemoryUserStore()
	srv.RegisterProtectedRoutes(keyStore, sessionStore, userStore, slogger)

	vault := discovery.NewAccountCredentials(st, key, true)
	sync := discovery.NewSyncService(storage.NewMemoryDiscoveryStore(st))
	sync.SetCredentials(vault)
	aws := &fakeCredentialCollector{identity: "arn:aws:sts::123456789012:assumed-role/CloudPAMDiscovery/cloudpam"}
	sync.RegisterCollector(aws)
	dualMW := DualAuthMiddleware(keyStore, sessionStore, userStore, true, slogger)
	NewAccountCredentialServer(srv, vault, sync).RegisterProtectedAccountCredentialRoutes(dualMW, slogger)
	return &credentialFixture{mux: mux, store: st, vault: vault, aws: aws}, plaintext
}

func TestAccountCredentialPermissions(t *testing.T) {
	t.Run("reader", func(t *testing.T) {
		f, key := newProtectedCredentialServer(t, []string{"accounts:read"})
		a := f.account(t, "aws")
		if _, err := f.vault.Put(t.Context(), a, domain.PutAccountCredentialRequest{
			Type: domain.CredentialTypeAWSAssumeRole, AWSAssumeRole: &domain.AWSAssumeRoleInput{RoleARN: credTestRoleARN},
		}); err != nil {
			t.Fatal(err)
		}
		wantStatus(t, doProtected(t, f.mux, http.MethodGet, credentialPath(a), "", key), http.StatusOK, credTestRoleARN)
		wantStatus(t, doProtected(t, f.mux, http.MethodPut, credentialPath(a), awsCredentialBody(""), key), http.StatusForbidden, "")
		wantStatus(t, doProtected(t, f.mux, http.MethodDelete, credentialPath(a), "", key), http.StatusForbidden, "")
		wantStatus(t, doProtected(t, f.mux, http.MethodPost, credentialPath(a)+"/test", "", key), http.StatusForbidden, "")
		wantStatus(t, doProtected(t, f.mux, http.MethodGet, credentialPath(a), "", ""), http.StatusUnauthorized, "")
	})
	t.Run("updater", func(t *testing.T) {
		f, key := newProtectedCredentialServer(t, []string{"accounts:read", "accounts:update"})
		a := f.account(t, "aws")
		wantStatus(t, doProtected(t, f.mux, http.MethodPut, credentialPath(a), awsCredentialBody(""), key), http.StatusOK, "")
		wantStatus(t, doProtected(t, f.mux, http.MethodPost, credentialPath(a)+"/test", "", key), http.StatusOK, `"status":"ok"`)
		wantStatus(t, doProtected(t, f.mux, http.MethodGet, credentialPath(a), "", key), http.StatusOK, "")
		wantStatus(t, doProtected(t, f.mux, http.MethodDelete, credentialPath(a), "", key), http.StatusNoContent, "")
	})
}
