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

// A reclaim or consolidation can delete the pool while a link request waits for the guard, so the
// link must recheck the pool under the guard instead of linking to the deleted one.
func TestLinkResource_RejectsAPoolDeletedWhileWaiting(t *testing.T) {
	d, st, ds, _ := setupDiscoveryTestServer()
	account, err := st.CreateAccount(t.Context(), domain.CreateAccount{Key: "aws:1", Name: "prod", Provider: "aws"})
	if err != nil {
		t.Fatal(err)
	}
	pool := createRootPool(t, d.srv, "10.6.0.0/16")
	res := discoveredForPlacementTest(account.ID, domain.ResourceTypeVPC, "vpc-1", "10.6.0.0/16", nil)
	upsertDiscoveredForImportTest(t, ds, res)

	release := make(chan struct{})
	held := make(chan struct{})
	holder := make(chan error, 1)
	go func() {
		holder <- d.srv.WithPlacement(context.Background(), func(work context.Context, ps storage.PlacementStore) error {
			close(held)
			<-release
			_, err := ps.DeletePool(work, pool.ID)
			return err
		})
	}()
	<-held

	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/discovery/resources/"+res.ID.String()+"/link", strings.NewReader(`{"pool_id":`+strconv.FormatInt(pool.ID, 10)+`}`))
		req.Header.Set("Content-Type", "application/json")
		d.srv.mux.ServeHTTP(rr, req)
		done <- rr
	}()
	time.Sleep(100 * time.Millisecond)
	close(release)
	if err := <-holder; err != nil {
		t.Fatalf("delete under the guard: %v", err)
	}

	rr := <-done
	if rr.Code != http.StatusNotFound || !strings.Contains(rr.Body.String(), "pool not found") {
		t.Fatalf("link to a pool deleted while it waited = %d %s, want 404 pool not found", rr.Code, rr.Body.String())
	}
	got, err := ds.GetDiscoveredResource(t.Context(), res.ID)
	if err != nil || got.PoolID != nil {
		t.Fatalf("resource %+v err=%v, want it left unlinked", got, err)
	}
}

// A reclaim checks that a pool is unchanged and then deletes it under the placement guard, so a
// PATCH must wait for the guard and then find the pool gone, not update it between those steps.
func TestUpdatePool_WaitsForThePlacementGuard(t *testing.T) {
	d, _, _, _ := setupDiscoveryTestServer()
	pool := createRootPool(t, d.srv, "10.7.0.0/16")

	release := make(chan struct{})
	held := make(chan struct{})
	holder := make(chan error, 1)
	go func() {
		holder <- d.srv.WithPlacement(context.Background(), func(work context.Context, ps storage.PlacementStore) error {
			close(held)
			<-release
			_, err := ps.DeletePool(work, pool.ID)
			return err
		})
	}()
	<-held

	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPatch, "/api/v1/pools/"+strconv.FormatInt(pool.ID, 10), strings.NewReader(`{"name":"renamed"}`))
		req.Header.Set("Content-Type", "application/json")
		d.srv.mux.ServeHTTP(rr, req)
		done <- rr
	}()

	select {
	case rr := <-done:
		t.Fatalf("PATCH finished with %d while the placement guard was held", rr.Code)
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	if err := <-holder; err != nil {
		t.Fatalf("delete under the guard: %v", err)
	}
	if rr := <-done; rr.Code != http.StatusNotFound {
		t.Fatalf("PATCH of a pool deleted while it waited = %d %s, want 404", rr.Code, rr.Body.String())
	}
}
