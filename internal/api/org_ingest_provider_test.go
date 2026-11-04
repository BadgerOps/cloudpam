package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"cloudpam/internal/domain"
)

func postOrgIngest(t *testing.T, discSrv *DiscoveryServer, body string) domain.BulkIngestResponse {
	t.Helper()
	rr := doJSON(t, discSrv.srv.mux, http.MethodPost, "/api/v1/discovery/ingest/org", body, http.StatusOK)
	var resp domain.BulkIngestResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return resp
}

func TestOrgIngestCreatesGCPProjectAccounts(t *testing.T) {
	discSrv, st, _, _ := setupDiscoveryTestServer()

	resp := postOrgIngest(t, discSrv, `{"accounts":[{"provider":"gcp","external_id":"my-project","account_name":"My Project","regions":["us-central1"],
		"resources":[{"provider":"gcp","region":"global","resource_type":"vpc","resource_id":"net-1","name":"default","cidr":"10.0.0.0/16","status":"active"}]}]}`)
	if resp.AccountsCreated != 1 || resp.AccountsProcessed != 1 || resp.TotalResources != 1 || len(resp.Errors) != 0 {
		t.Fatalf("resp = %+v", resp)
	}
	acct, err := st.GetAccountByKey(t.Context(), "gcp:my-project")
	if err != nil {
		t.Fatalf("get gcp account: %v", err)
	}
	if acct.Provider != "gcp" || acct.ExternalID != "my-project" || acct.Name != "My Project" {
		t.Fatalf("account = %+v", acct)
	}
}

func TestOrgIngestAcceptsTheLegacyAWSField(t *testing.T) {
	discSrv, st, _, _ := setupDiscoveryTestServer()

	resp := postOrgIngest(t, discSrv, `{"accounts":[
		{"provider":"aws","aws_account_id":"111111111111"},
		{"provider":"aws","aws_account_id":"222222222222","external_id":"222222222222"}
	]}`)
	if resp.AccountsCreated != 2 || len(resp.Errors) != 0 {
		t.Fatalf("resp = %+v", resp)
	}
	for _, key := range []string{"aws:111111111111", "aws:222222222222"} {
		if _, err := st.GetAccountByKey(t.Context(), key); err != nil {
			t.Fatalf("get %s: %v", key, err)
		}
	}
}

func TestOrgIngestRejectsBadEntries(t *testing.T) {
	discSrv, _, _, _ := setupDiscoveryTestServer()

	resp := postOrgIngest(t, discSrv, `{"accounts":[
		{"external_id":"no-provider"},
		{"provider":"azure","external_id":"sub-1"},
		{"provider":"aws","aws_account_id":"111111111111","external_id":"999999999999"},
		{"provider":"gcp"}
	]}`)
	if resp.AccountsProcessed != 0 || resp.AccountsCreated != 0 {
		t.Fatalf("resp = %+v, want nothing processed", resp)
	}
	want := []string{
		"skipped account: provider is required",
		`skipped account: unsupported provider "azure"`,
		`skipped account: external_id "999999999999" and aws_account_id "111111111111" disagree`,
		"skipped account: external_id is required",
	}
	if len(resp.Errors) != len(want) {
		t.Fatalf("errors = %#v", resp.Errors)
	}
	for i, w := range want {
		if resp.Errors[i] != w {
			t.Errorf("errors[%d] = %q, want %q", i, resp.Errors[i], w)
		}
	}
}

func TestOrgIngestMatchesAnExistingAccountByExternalID(t *testing.T) {
	discSrv, st, _, _ := setupDiscoveryTestServer()
	existing, err := st.CreateAccount(t.Context(), domain.CreateAccount{
		Key: "prod-net", Name: "Prod network", Provider: "gcp", ExternalID: "my-project",
	})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}

	resp := postOrgIngest(t, discSrv, `{"accounts":[{"provider":"gcp","external_id":"my-project",
		"resources":[{"provider":"gcp","region":"global","resource_type":"vpc","resource_id":"net-1","name":"default","cidr":"10.0.0.0/16","status":"active"}]}]}`)
	if resp.AccountsCreated != 0 || resp.AccountsProcessed != 1 || len(resp.Errors) != 0 {
		t.Fatalf("resp = %+v, want the existing account reused", resp)
	}
	if _, err := st.GetAccountByKey(t.Context(), "gcp:my-project"); err == nil {
		t.Fatal("a duplicate gcp:my-project account was created")
	}
	accounts, err := st.ListAccounts(t.Context())
	if err != nil {
		t.Fatalf("list accounts: %v", err)
	}
	if len(accounts) != 1 || accounts[0].ID != existing.ID {
		t.Fatalf("accounts = %+v", accounts)
	}
}

func TestOrgIngestRefusesAnAmbiguousMatch(t *testing.T) {
	discSrv, st, _, _ := setupDiscoveryTestServer()
	for _, key := range []string{"one", "two"} {
		if _, err := st.CreateAccount(t.Context(), domain.CreateAccount{Key: key, Name: key, Provider: "aws", ExternalID: "123456789012"}); err != nil {
			t.Fatalf("create %s: %v", key, err)
		}
	}
	resp := postOrgIngest(t, discSrv, `{"accounts":[{"provider":"aws","external_id":"123456789012"}]}`)
	if resp.AccountsProcessed != 0 || len(resp.Errors) != 1 || !strings.HasPrefix(resp.Errors[0], "skipped account: ambiguous accounts for aws 123456789012: ids ") {
		t.Fatalf("resp = %+v", resp)
	}
}

func TestOrgIngestIgnoresAnotherProvidersExternalID(t *testing.T) {
	discSrv, st, _, _ := setupDiscoveryTestServer()
	if _, err := st.CreateAccount(t.Context(), domain.CreateAccount{Key: "aws-thing", Name: "x", Provider: "aws", ExternalID: "shared-id"}); err != nil {
		t.Fatalf("create: %v", err)
	}
	resp := postOrgIngest(t, discSrv, `{"accounts":[{"provider":"gcp","external_id":"shared-id"}]}`)
	if resp.AccountsCreated != 1 {
		t.Fatalf("resp = %+v, want a new gcp account", resp)
	}
}

func TestOrgIngestReportsFailedAccountsWithoutTouchingTheirInventory(t *testing.T) {
	discSrv, st, ds, _ := setupDiscoveryTestServer()
	ctx := t.Context()
	failing, err := st.CreateAccount(ctx, domain.CreateAccount{Key: "gcp:broken", Name: "broken", Provider: "gcp", ExternalID: "broken"})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	now := time.Now().UTC()
	kept := domain.DiscoveredResource{
		ID: uuid.New(), AccountID: failing.ID, Provider: "gcp", Region: "global", ResourceType: domain.ResourceTypeVPC,
		ResourceID: "broken-net", Name: "net", CIDR: "10.9.0.0/16", Status: domain.DiscoveryStatusActive,
		DiscoveredAt: now, LastSeenAt: now, UpdatedAt: now,
	}
	if err := ds.UpsertDiscoveredResource(ctx, kept); err != nil {
		t.Fatalf("upsert resource: %v", err)
	}
	job, err := ds.CreateSyncJob(ctx, domain.SyncJob{ID: uuid.New(), AccountID: failing.ID, Status: domain.SyncJobStatusPending, Source: "agent", CreatedAt: now})
	if err != nil {
		t.Fatalf("create sync job: %v", err)
	}

	resp := postOrgIngest(t, discSrv, `{"sync_job_id":"`+job.ID.String()+`","accounts":[
		{"provider":"gcp","external_id":"prod","resources":[{"provider":"gcp","region":"global","resource_type":"vpc","resource_id":"prod-net","name":"n","cidr":"10.0.0.0/16","status":"active"}]},
		{"provider":"gcp","external_id":"dev"}
	],"failures":[{"provider":"gcp","external_id":"broken","error":"compute api disabled"}]}`)

	const want = "discovery failed for gcp:broken: compute api disabled"
	if resp.AccountsProcessed != 2 || resp.AccountsFailed != 1 || resp.TotalResources != 1 {
		t.Fatalf("resp = %+v", resp)
	}
	if len(resp.Errors) != 1 || resp.Errors[0] != want {
		t.Fatalf("errors = %#v, want [%q]", resp.Errors, want)
	}
	got, err := ds.GetSyncJob(ctx, job.ID)
	if err != nil {
		t.Fatalf("get sync job: %v", err)
	}
	if got.Status != domain.SyncJobStatusFailed || got.ErrorMessage != want || got.ResourcesFound != 1 {
		t.Fatalf("job = status %q, message %q, found %d", got.Status, got.ErrorMessage, got.ResourcesFound)
	}
	res, err := ds.GetDiscoveredResource(ctx, kept.ID)
	if err != nil {
		t.Fatalf("get resource: %v", err)
	}
	if res.Status != domain.DiscoveryStatusActive {
		t.Fatalf("failed account's resource status = %q, want active", res.Status)
	}
}

func TestOrgIngestAcceptsAFailuresOnlyRequest(t *testing.T) {
	discSrv, _, ds, _ := setupDiscoveryTestServer()
	job, err := ds.CreateSyncJob(t.Context(), domain.SyncJob{ID: uuid.New(), AccountID: 1, Status: domain.SyncJobStatusPending, Source: "agent", CreatedAt: time.Now().UTC()})
	if err != nil {
		t.Fatalf("create sync job: %v", err)
	}

	resp := postOrgIngest(t, discSrv, `{"sync_job_id":"`+job.ID.String()+`","accounts":[],
		"failures":[{"provider":"aws","error":"list organization accounts: access denied"}]}`)

	const want = "discovery failed for aws: list organization accounts: access denied"
	if resp.AccountsProcessed != 0 || resp.AccountsFailed != 1 || len(resp.Errors) != 1 || resp.Errors[0] != want {
		t.Fatalf("resp = %+v", resp)
	}
	got, err := ds.GetSyncJob(t.Context(), job.ID)
	if err != nil {
		t.Fatalf("get sync job: %v", err)
	}
	if got.Status != domain.SyncJobStatusFailed || got.ErrorMessage != want {
		t.Fatalf("job = status %q, message %q", got.Status, got.ErrorMessage)
	}

	rr := doJSON(t, discSrv.srv.mux, http.MethodPost, "/api/v1/discovery/ingest/org", `{"accounts":[],"failures":[]}`, http.StatusBadRequest)
	if !strings.Contains(rr.Body.String(), "accounts list is required") {
		t.Fatalf("body = %s", rr.Body.String())
	}
}

func TestOrgIngestClipsALongFailureError(t *testing.T) {
	discSrv, _, _, _ := setupDiscoveryTestServer()
	long := strings.Repeat("x", 1000)

	resp := postOrgIngest(t, discSrv, `{"failures":[{"provider":"gcp","external_id":"p","error":"`+long+`"}]}`)

	if want := "discovery failed for gcp:p: " + long[:300]; len(resp.Errors) != 1 || resp.Errors[0] != want {
		t.Fatalf("errors = %#v, want the error clipped to 300 characters", resp.Errors)
	}
}

func TestOrgIngestCompletesAJobWithAnEmptyReport(t *testing.T) {
	discSrv, _, ds, _ := setupDiscoveryTestServer()
	agentID := uuid.New()
	job, err := ds.CreateSyncJob(t.Context(), domain.SyncJob{ID: uuid.New(), AccountID: 1, Status: domain.SyncJobStatusPending, Source: "agent", AgentID: &agentID, CreatedAt: time.Now().UTC()})
	if err != nil {
		t.Fatalf("create sync job: %v", err)
	}
	if _, err := ds.ClaimPendingAgentSync(t.Context(), agentID); err != nil {
		t.Fatalf("claim sync job: %v", err)
	}

	resp := postOrgIngest(t, discSrv, `{"sync_job_id":"`+job.ID.String()+`"}`)

	if resp.AccountsProcessed != 0 || resp.AccountsFailed != 0 || len(resp.Errors) != 0 {
		t.Fatalf("resp = %+v", resp)
	}
	got, err := ds.GetSyncJob(t.Context(), job.ID)
	if err != nil {
		t.Fatalf("get sync job: %v", err)
	}
	if got.Status != domain.SyncJobStatusCompleted || got.ResourcesFound != 0 || got.CompletedAt == nil {
		t.Fatalf("job = %+v, want completed with no resources", got)
	}

	rr := doJSON(t, discSrv.srv.mux, http.MethodPost, "/api/v1/discovery/ingest/org", `{}`, http.StatusBadRequest)
	if !strings.Contains(rr.Body.String(), "accounts list is required") {
		t.Fatalf("body = %s", rr.Body.String())
	}
}
