package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"cloudpam/internal/domain"
	"cloudpam/internal/observability"
	"cloudpam/internal/storage"
)

type slowListStore struct{ *storage.MemoryStore }

func (s slowListStore) ListPools(ctx context.Context) ([]domain.Pool, error) {
	pools, err := s.MemoryStore.ListPools(ctx)
	time.Sleep(2 * time.Millisecond)
	return pools, err
}

func setupSlowListServer() (*Server, *storage.MemoryStore) {
	st := storage.NewMemoryStore()
	logger := observability.NewLogger(observability.Config{Level: "info", Format: "json", Output: io.Discard})
	srv := NewServer(stdhttp.NewServeMux(), slowListStore{st}, logger, nil, nil)
	srv.registerUnprotectedTestRoutes()
	return srv, st
}

type post struct{ path, body string }

func postTogether(srv *Server, posts []post) map[int]int {
	codes := make([]int, len(posts))
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i, p := range posts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			req := httptest.NewRequest(stdhttp.MethodPost, p.path, strings.NewReader(p.body))
			req.Header.Set("Content-Type", "application/json")
			rr := httptest.NewRecorder()
			srv.mux.ServeHTTP(rr, req)
			codes[i] = rr.Code
		}()
	}
	close(start)
	wg.Wait()
	counts := map[int]int{}
	for _, c := range codes {
		counts[c]++
	}
	return counts
}

func createRootPool(t *testing.T, srv *Server, cidr string) poolDTO {
	t.Helper()
	rr := doJSON(t, srv.mux, stdhttp.MethodPost, "/api/v1/pools", fmt.Sprintf(`{"name":"root","cidr":%q}`, cidr), stdhttp.StatusCreated)
	var root poolDTO
	if err := json.Unmarshal(rr.Body.Bytes(), &root); err != nil {
		t.Fatalf("unmarshal root: %v", err)
	}
	return root
}

func TestCreatePool_ConcurrentOverlappingSiblingsStoreOne(t *testing.T) {
	srv, st := setupSlowListServer()
	root := createRootPool(t, srv, "10.0.0.0/8")

	var posts []post
	for bits := 16; bits <= 27; bits++ {
		posts = append(posts, post{"/api/v1/pools", fmt.Sprintf(`{"name":"c%d","cidr":"10.1.0.0/%d","parent_id":%d}`, bits, bits, root.ID)})
	}
	got := postTogether(srv, posts)
	if got[stdhttp.StatusCreated] != 1 || got[stdhttp.StatusBadRequest] != len(posts)-1 {
		t.Fatalf("want one 201 and %d 400s, got %v", len(posts)-1, got)
	}
	children, err := st.GetPoolChildren(t.Context(), root.ID)
	if err != nil {
		t.Fatalf("children: %v", err)
	}
	if len(children) != 1 {
		t.Fatalf("want 1 child stored, got %d: %+v", len(children), children)
	}
}

func TestCreatePool_ConcurrentIdenticalCIDRStoreOne(t *testing.T) {
	srv, st := setupSlowListServer()
	var posts []post
	for i := range 8 {
		posts = append(posts, post{"/api/v1/pools", fmt.Sprintf(`{"name":"dup%d","cidr":"10.0.0.0/8"}`, i)})
	}
	got := postTogether(srv, posts)
	if got[stdhttp.StatusCreated] != 1 || got[stdhttp.StatusConflict] != len(posts)-1 {
		t.Fatalf("want one 201 and %d 409s, got %v", len(posts)-1, got)
	}
	pools, err := st.ListPools(t.Context())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(pools) != 1 {
		t.Fatalf("want 1 pool stored, got %d", len(pools))
	}
}

func TestCreatePool_ExactDuplicateIs409(t *testing.T) {
	srv, st := setupTestServer()
	root := createRootPool(t, srv, "10.0.0.0/8")
	doJSON(t, srv.mux, stdhttp.MethodPost, "/api/v1/pools", `{"name":"root-again","cidr":"10.0.0.0/8"}`, stdhttp.StatusConflict)

	child := fmt.Sprintf(`{"name":"c","cidr":"10.1.0.0/16","parent_id":%d}`, root.ID)
	doJSON(t, srv.mux, stdhttp.MethodPost, "/api/v1/pools", child, stdhttp.StatusCreated)
	doJSON(t, srv.mux, stdhttp.MethodPost, "/api/v1/pools", child, stdhttp.StatusConflict)

	pools, err := st.ListPools(t.Context())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(pools) != 2 {
		t.Fatalf("want 2 pools stored, got %d: %+v", len(pools), pools)
	}
}

func TestConcurrentPoolsAndSchemaApplyStoreOne(t *testing.T) {
	srv, st := setupSlowListServer()
	var posts []post
	for i := range 6 {
		posts = append(posts,
			post{"/api/v1/pools", fmt.Sprintf(`{"name":"p%d","cidr":"10.1.0.0/%d"}`, i, 16+i)},
			post{"/api/v1/schema/apply", fmt.Sprintf(`{"pools":[{"ref":"r","name":"s%d","cidr":"10.1.0.0/%d","type":"supernet"}],"skip_conflicts":false}`, i, 22+i)},
		)
	}
	got := postTogether(srv, posts)
	if wins := got[stdhttp.StatusCreated] + got[stdhttp.StatusOK]; wins != 1 {
		t.Fatalf("want exactly one success, got %v", got)
	}
	pools, err := st.ListPools(t.Context())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(pools) != 1 {
		t.Fatalf("want 1 pool stored, got %d: %+v", len(pools), pools)
	}
}

func TestSchemaApply_OverlappingSiblingsInOneBatch(t *testing.T) {
	body := func(skip bool) string {
		return fmt.Sprintf(`{"pools":[
			{"ref":"root","name":"Root","cidr":"10.0.0.0/8","type":"supernet"},
			{"ref":"a","name":"A","cidr":"10.1.0.0/16","type":"region","parent_ref":"root"},
			{"ref":"b","name":"B","cidr":"10.1.0.0/17","type":"region","parent_ref":"root"}
		],"skip_conflicts":%t}`, skip)
	}

	srv, st := setupTestServer()
	doJSON(t, srv.mux, stdhttp.MethodPost, "/api/v1/schema/apply", body(false), stdhttp.StatusConflict)
	pools, err := st.ListPools(t.Context())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(pools) != 0 {
		t.Fatalf("refused batch stored %d pools", len(pools))
	}

	rr := doJSON(t, srv.mux, stdhttp.MethodPost, "/api/v1/schema/apply", body(true), stdhttp.StatusOK)
	var resp schemaApplyResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Created != 3 {
		t.Fatalf("skip_conflicts:true: want 3 created, got %+v", resp)
	}
}
