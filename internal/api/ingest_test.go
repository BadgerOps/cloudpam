package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"cloudpam/internal/discovery"
	"cloudpam/internal/domain"
	"cloudpam/internal/storage"
)

func TestIngestFailsWhenResourceProcessingFails(t *testing.T) {
	discSrv, st, _, _ := setupDiscoveryTestServer()
	account, err := st.CreateAccount(t.Context(), domain.CreateAccount{
		Key:      "aws:123456789012",
		Name:     "prod",
		Provider: "aws",
	})
	if err != nil {
		t.Fatalf("create account: %v", err)
	}

	failingStore := &apiMarkStaleFailStore{
		MemoryDiscoveryStore: storage.NewMemoryDiscoveryStore(st),
		err:                  errors.New("mark stale failed"),
	}
	discSrv.store = failingStore
	discSrv.syncService = discovery.NewSyncService(failingStore)

	body := fmt.Sprintf("{\"account_id\":%d,\"resources\":[{\"provider\":\"aws\",\"region\":\"us-east-1\",\"resource_type\":\"vpc\",\"resource_id\":\"vpc-1\",\"name\":\"prod\",\"cidr\":\"10.0.0.0/16\",\"status\":\"active\"}]}", account.ID)
	doJSON(t, discSrv.srv.mux, http.MethodPost, "/api/v1/discovery/ingest", body, http.StatusInternalServerError)

	jobs, err := failingStore.ListSyncJobs(t.Context(), account.ID, 1)
	if err != nil {
		t.Fatalf("list sync jobs: %v", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("len(jobs) = %d, want 1", len(jobs))
	}
	if jobs[0].Status != domain.SyncJobStatusFailed {
		t.Fatalf("job status = %q, want %q", jobs[0].Status, domain.SyncJobStatusFailed)
	}
	if jobs[0].ErrorMessage == "" {
		t.Fatal("job error message is empty")
	}
}

type apiMarkStaleFailStore struct {
	*storage.MemoryDiscoveryStore
	err error
}

func (s *apiMarkStaleFailStore) MarkStaleResources(context.Context, int64, time.Time) (int, error) {
	return 0, s.err
}
