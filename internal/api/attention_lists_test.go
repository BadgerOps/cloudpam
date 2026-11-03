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

// The dashboard reads items.length on filtered lists, so an empty match must
// serialize as [] rather than null.
func TestEmptyFilteredListsSerializeItemsAsArray(t *testing.T) {
	driftMux, _, _, _ := setupDriftTestServer()
	recMux, _ := setupRecommendationServer()
	tests := []struct {
		name string
		mux  *http.ServeMux
		path string
	}{
		{"drift", driftMux, "/api/v1/drift?status=open&severity=critical"},
		{"recommendations", recMux, "/api/v1/recommendations?status=pending&priority=high"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rr := assertStatusCov(t, doReqCov(t, tt.mux, http.MethodGet, tt.path, ""), http.StatusOK)
			if !strings.Contains(rr.Body.String(), `"items":[]`) {
				t.Fatalf("expected items to be [], got %s", rr.Body.String())
			}
		})
	}
}

func TestDiscoverySyncLatestPerAccount(t *testing.T) {
	discSrv, st, ds, _ := setupDiscoveryTestServer()
	mux := discSrv.srv.mux

	rr := assertStatusCov(t, doReqCov(t, mux, http.MethodGet, "/api/v1/discovery/sync/latest", ""), http.StatusOK)
	if !strings.Contains(rr.Body.String(), `"items":[]`) {
		t.Fatalf("expected items to be [] with no accounts, got %s", rr.Body.String())
	}

	synced, err := st.CreateAccount(t.Context(), domain.CreateAccount{Key: "aws:1", Name: "synced", Provider: "aws"})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}
	if _, err := st.CreateAccount(t.Context(), domain.CreateAccount{Key: "aws:2", Name: "never", Provider: "aws"}); err != nil {
		t.Fatalf("create account: %v", err)
	}
	base := time.Now().UTC()
	older := domain.SyncJob{ID: uuid.New(), AccountID: synced.ID, Status: domain.SyncJobStatusFailed, Source: "local", CreatedAt: base}
	newer := domain.SyncJob{ID: uuid.New(), AccountID: synced.ID, Status: domain.SyncJobStatusCompleted, Source: "local", CreatedAt: base.Add(time.Minute)}
	for _, job := range []domain.SyncJob{older, newer} {
		if _, err := ds.CreateSyncJob(t.Context(), job); err != nil {
			t.Fatalf("create sync job: %v", err)
		}
	}

	rr = assertStatusCov(t, doReqCov(t, mux, http.MethodGet, "/api/v1/discovery/sync/latest", ""), http.StatusOK)
	var resp domain.SyncJobsResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Items) != 1 || resp.Items[0].ID != newer.ID {
		t.Fatalf("expected only the newest job for the synced account, got %#v", resp.Items)
	}

	assertStatusCov(t, doReqCov(t, mux, http.MethodPost, "/api/v1/discovery/sync/latest", ""), http.StatusMethodNotAllowed)
}
