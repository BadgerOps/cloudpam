package api

import (
	"context"
	"encoding/json"
	"errors"
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

type gatedListStore struct {
	*storage.MemoryStore
	gate chan struct{}
}

func (s gatedListStore) ListPools(ctx context.Context) ([]domain.Pool, error) {
	<-s.gate
	return s.MemoryStore.ListPools(ctx)
}

type slowFirstCreateStore struct {
	*storage.MemoryStore
	calls int
}

func (s *slowFirstCreateStore) CreatePool(ctx context.Context, in domain.CreatePool) (domain.Pool, error) {
	s.calls++
	if s.calls == 1 {
		time.Sleep(2 * placementBudget)
	}
	return s.MemoryStore.CreatePool(ctx, in)
}

func TestPlacePools_BudgetStopsBatchBetweenInserts(t *testing.T) {
	old := placementBudget
	placementBudget = 20 * time.Millisecond
	t.Cleanup(func() { placementBudget = old })

	st := &slowFirstCreateStore{MemoryStore: storage.NewMemoryStore()}
	logger := observability.NewLogger(observability.Config{Level: "info", Format: "json", Output: io.Discard})
	srv := NewServer(stdhttp.NewServeMux(), st, logger, nil, nil)
	batch := []poolPlacement{
		{In: domain.CreatePool{Name: "first", CIDR: "10.1.0.0/16"}},
		{In: domain.CreatePool{Name: "second", CIDR: "10.2.0.0/16"}},
		{In: domain.CreatePool{Name: "third", CIDR: "10.3.0.0/16"}},
	}

	_, err := srv.placePools(t.Context(), siblingsOnly, batch)
	if !errors.Is(err, storage.ErrBusy) {
		t.Errorf("placePools error = %v, want storage.ErrBusy", err)
	}
	pools, err := st.ListPools(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(pools) != 1 || pools[0].Name != "first" {
		t.Fatalf("pools stored after the budget expired = %+v, want only first", pools)
	}
}

// A request that spends its whole budget, whether queued on the placement gate or inside the store,
// answers 503 and stores nothing.
func TestPlacePools_BudgetCoversTheMutexWait(t *testing.T) {
	old := placementBudget
	placementBudget = 300 * time.Millisecond
	t.Cleanup(func() { placementBudget = old })

	st := storage.NewMemoryStore()
	gate := make(chan struct{})
	logger := observability.NewLogger(observability.Config{Level: "info", Format: "json", Output: io.Discard})
	srv := NewServer(stdhttp.NewServeMux(), gatedListStore{st, gate}, logger, nil, nil)
	srv.registerUnprotectedTestRoutes()

	send := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(stdhttp.MethodPost, "/api/v1/pools", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		srv.mux.ServeHTTP(rr, req)
		return rr
	}
	first := make(chan *httptest.ResponseRecorder, 1)
	go func() { first <- send(`{"name":"held","cidr":"10.1.0.0/16"}`) }()
	for !srv.placeGate.held() {
		time.Sleep(5 * time.Millisecond)
	}

	queued := send(`{"name":"queued","cidr":"10.2.0.0/16"}`)
	close(gate)
	held := <-first

	if queued.Code != stdhttp.StatusServiceUnavailable {
		t.Fatalf("request queued past its budget: got %d %s, want 503", queued.Code, queued.Body.String())
	}
	if queued.Header().Get("Retry-After") == "" {
		t.Fatal("503 without Retry-After")
	}
	if held.Code != stdhttp.StatusServiceUnavailable {
		t.Fatalf("request held in the store past its budget: got %d %s, want 503", held.Code, held.Body.String())
	}
	pools, err := st.ListPools(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(pools) != 0 {
		t.Fatalf("placements past their budget stored %d pools", len(pools))
	}
}

type post struct{ path, body string }

func postTogether(srv *Server, posts []post) map[int]int {
	return postAcross([]*Server{srv}, posts)
}

func postAcross(servers []*Server, posts []post) map[int]int {
	codes := make([]int, len(posts))
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i, p := range posts {
		srv := servers[i%len(servers)]
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

func placementOutcome(err error) string {
	var overlap *placementConflict
	var missing *missingParentError
	var outside *containmentError
	switch {
	case err == nil:
		return "stored"
	case errors.As(err, &overlap):
		return "overlaps " + overlap.With.Name
	case errors.As(err, &missing):
		return "no parent"
	case errors.As(err, &outside):
		return "outside"
	case errors.Is(err, storage.ErrConflict):
		return "duplicate"
	}
	return err.Error()
}

func TestPlace(t *testing.T) {
	rootID, aID, missingID := int64(1), int64(2), int64(99)
	pool := func(ref, parentRef, cidr string, parent *int64) poolPlacement {
		return poolPlacement{Ref: ref, ParentRef: parentRef, In: domain.CreatePool{Name: ref, CIDR: cidr, ParentID: parent}}
	}
	cases := []struct {
		name   string
		policy overlapPolicy
		batch  []poolPlacement
		whole  string
		each   []string
	}{
		{"sibling overlap", siblingsOnly, []poolPlacement{pool("n", "", "10.1.0.0/17", &rootID)}, "", []string{"overlaps a"}},
		{"disjoint sibling", siblingsOnly, []poolPlacement{pool("n", "", "10.2.0.0/16", &rootID)}, "", []string{"stored"}},
		{"identical sibling is left to the store", siblingsOnly, []poolPlacement{pool("n", "", "10.1.0.0/16", &rootID)}, "", []string{"duplicate"}},
		{"pools under other parents are ignored", siblingsOnly, []poolPlacement{pool("n", "", "10.1.2.0/24", &aID)}, "", []string{"stored"}},
		{"top level", siblingsOnly, []poolPlacement{pool("n", "", "10.0.0.0/9", nil)}, "", []string{"overlaps root"}},
		{"outside its parent", siblingsOnly, []poolPlacement{pool("n", "", "192.168.0.0/24", &rootID)}, "", []string{"outside"}},
		{"missing parent", siblingsOnly, []poolPlacement{pool("n", "", "10.3.0.0/16", &missingID)}, "", []string{"no parent"}},
		{"an entry stored earlier is a sibling", siblingsOnly, []poolPlacement{
			pool("x", "", "172.16.0.0/16", nil),
			pool("y", "", "172.16.0.0/17", nil),
		}, "", []string{"stored", "overlaps x"}},
		{"a child is checked against its parent entry", siblingsOnly, []poolPlacement{
			pool("p", "", "172.16.0.0/16", nil),
			pool("c", "p", "172.17.0.0/24", nil),
		}, "", []string{"stored", "outside"}},
		{"an unstored parent entry falls back to the stored parent", siblingsOnly, []poolPlacement{
			pool("r", "", "10.0.0.0/8", nil),
			pool("c", "r", "10.9.0.0/16", &rootID),
		}, "", []string{"duplicate", "stored"}},
		{"any live pool", anyLivePool, []poolPlacement{pool("n", "", "10.200.0.0/24", nil)}, "overlaps root", nil},
		{"any live pool, identical", anyLivePool, []poolPlacement{pool("n", "", "192.0.2.0/24", nil)}, "overlaps solo", nil},
		{"plan siblings overlap", anyLivePool, []poolPlacement{
			pool("r", "", "192.168.0.0/16", nil),
			pool("x", "r", "192.168.0.0/24", nil),
			pool("y", "r", "192.168.0.0/25", nil),
		}, "overlaps x", nil},
		{"plan parent and child do not conflict", anyLivePool, []poolPlacement{
			pool("r", "", "192.168.0.0/16", nil),
			pool("x", "r", "192.168.0.0/24", nil),
		}, "", []string{"stored", "stored"}},
		{"plan containment is checked before overlap", anyLivePool, []poolPlacement{
			pool("r", "", "192.168.0.0/16", nil),
			pool("x", "r", "10.200.0.0/24", nil),
		}, "outside", nil},
		{"skip_conflicts still checks containment", overlapAllowed, []poolPlacement{
			pool("r", "", "192.168.0.0/16", nil),
			pool("x", "r", "10.200.0.0/24", nil),
		}, "outside", nil},
		{"skip_conflicts stores an overlap", overlapAllowed, []poolPlacement{pool("n", "", "10.0.0.0/9", nil)}, "", []string{"stored"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			st := storage.NewMemoryStore()
			for _, in := range []domain.CreatePool{
				{Name: "root", CIDR: "10.0.0.0/8"},
				{Name: "a", CIDR: "10.1.0.0/16", ParentID: &rootID},
				{Name: "deep", CIDR: "10.1.1.0/24", ParentID: &aID},
				{Name: "solo", CIDR: "192.0.2.0/24"},
			} {
				if _, err := st.CreatePool(t.Context(), in); err != nil {
					t.Fatal(err)
				}
			}
			out, err := place(t.Context(), st, tc.policy, tc.batch)
			if tc.whole != "" {
				if got := placementOutcome(err); got != tc.whole {
					t.Fatalf("want the whole batch refused with %q, got %q", tc.whole, got)
				}
				pools, _ := st.ListPools(t.Context())
				if len(pools) != 4 {
					t.Fatalf("a refused batch stored %d pools", len(pools)-4)
				}
				return
			}
			if err != nil {
				t.Fatalf("want per-entry results, got %v", err)
			}
			for i, r := range out {
				if got := placementOutcome(r.Err); got != tc.each[i] {
					t.Fatalf("entry %d: want %q, got %q", i, tc.each[i], got)
				}
			}
		})
	}
}
