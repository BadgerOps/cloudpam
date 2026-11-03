package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"cloudpam/internal/domain"
	"cloudpam/internal/storage"
)

// A reclaim or resize checks a pool's links under the placement guard, so a link request must
// wait for the guard rather than land between that check and the delete.
func TestLinkResource_WaitsForThePlacementGuard(t *testing.T) {
	d, st, ds, _ := setupDiscoveryTestServer()
	account, err := st.CreateAccount(t.Context(), domain.CreateAccount{Key: "aws:1", Name: "prod", Provider: "aws"})
	if err != nil {
		t.Fatal(err)
	}
	pool := createRootPool(t, d.srv, "10.5.0.0/16")
	res := discoveredForPlacementTest(account.ID, domain.ResourceTypeVPC, "vpc-1", "10.5.0.0/16", nil)
	upsertDiscoveredForImportTest(t, ds, res)

	release := make(chan struct{})
	held := make(chan struct{})
	go func() {
		_ = d.srv.WithPlacement(context.Background(), func(context.Context, storage.PlacementStore) error {
			close(held)
			<-release
			return nil
		})
	}()
	<-held

	done := make(chan int, 1)
	go func() {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/discovery/resources/"+res.ID.String()+"/link", strings.NewReader(`{"pool_id":`+strconv.FormatInt(pool.ID, 10)+`}`))
		req.Header.Set("Content-Type", "application/json")
		d.srv.mux.ServeHTTP(rr, req)
		done <- rr.Code
	}()

	select {
	case code := <-done:
		t.Fatalf("link finished with %d while the placement guard was held", code)
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	if code := <-done; code != http.StatusOK {
		t.Fatalf("link after the guard was released = %d, want 200", code)
	}
	got, err := ds.GetDiscoveredResource(t.Context(), res.ID)
	if err != nil || got.PoolID == nil || *got.PoolID != pool.ID {
		t.Fatalf("resource %+v err=%v, want linked to pool %d", got, err, pool.ID)
	}
}
