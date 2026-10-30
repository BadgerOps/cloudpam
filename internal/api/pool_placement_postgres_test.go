//go:build postgres

package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	stdhttp "net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"cloudpam/internal/observability"
	"cloudpam/internal/storage"
	pgstore "cloudpam/internal/storage/postgres"
)

type replicaPair struct {
	a, b *Server
	db   *pgxpool.Pool
}

func newReplicaPair(t *testing.T) replicaPair {
	t.Helper()
	ctx := context.Background()
	dsn := freshDatabase(t)

	storeA, err := pgstore.New(dsn)
	if err != nil {
		t.Fatalf("replica a store: %v", err)
	}
	t.Cleanup(func() { _ = storeA.Close() })

	poolB, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("replica b pool: %v", err)
	}
	t.Cleanup(poolB.Close)

	db, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("test pool: %v", err)
	}
	t.Cleanup(db.Close)

	return replicaPair{a: newReplica(storeA), b: newReplica(pgstore.NewFromPool(poolB)), db: db}
}

func newReplica(st storage.Store) *Server {
	logger := observability.NewLogger(observability.Config{Level: "info", Format: "json", Output: io.Discard})
	srv := NewServer(stdhttp.NewServeMux(), st, logger, nil, nil)
	srv.registerUnprotectedTestRoutes()
	return srv
}

// freshDatabase gives each test its own database: the storage/postgres tests run in parallel
// with this package and delete every pool in the shared one.
func freshDatabase(t *testing.T) string {
	t.Helper()
	base := os.Getenv("DATABASE_URL")
	if base == "" {
		t.Fatal("DATABASE_URL must be set for postgres tests")
	}
	u, err := url.Parse(base)
	if err != nil || u.Scheme == "" {
		t.Fatalf("DATABASE_URL must be a postgres:// URL, got %q", base)
	}

	ctx := context.Background()
	admin, err := pgx.Connect(ctx, base)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = admin.Close(ctx) }()

	name := fmt.Sprintf("cloudpam_api_%d", time.Now().UnixNano())
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		t.Fatalf("create database: %v", err)
	}
	t.Cleanup(func() {
		c, err := pgx.Connect(context.Background(), base)
		if err != nil {
			t.Errorf("connect for drop: %v", err)
			return
		}
		defer func() { _ = c.Close(context.Background()) }()
		if _, err := c.Exec(context.Background(), "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)"); err != nil {
			t.Errorf("drop database %s: %v", name, err)
		}
	})
	// Placement must pin READ COMMITTED: under this default the snapshot would predate the lock wait.
	if _, err := admin.Exec(ctx, "ALTER DATABASE "+name+" SET default_transaction_isolation = 'repeatable read'"); err != nil {
		t.Fatalf("set default isolation: %v", err)
	}

	u.Path = "/" + name
	return u.String()
}

func serveAsync(ctx context.Context, srv *Server, path, body string) <-chan *httptest.ResponseRecorder {
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		req := httptest.NewRequestWithContext(ctx, stdhttp.MethodPost, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		srv.mux.ServeHTTP(rr, req)
		done <- rr
	}()
	return done
}

// holdPoolInserts blocks every INSERT into pools while reads still run, so both replicas get past
// their overlap check before either can store anything unless something serializes them.
func holdPoolInserts(t *testing.T, db *pgxpool.Pool) (release func()) {
	t.Helper()
	ctx := context.Background()
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatalf("begin gate: %v", err)
	}
	if _, err := tx.Exec(ctx, "LOCK TABLE pools IN SHARE ROW EXCLUSIVE MODE"); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("lock pools: %v", err)
	}
	var once sync.Once
	release = func() { once.Do(func() { _ = tx.Rollback(ctx) }) }
	t.Cleanup(release)
	return release
}

func waitForLockWaiters(t *testing.T, db *pgxpool.Pool, want int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		var waiting int
		if err := db.QueryRow(context.Background(), `
			SELECT count(*) FROM pg_stat_activity
			WHERE datname = current_database() AND wait_event_type = 'Lock'`).Scan(&waiting); err != nil {
			t.Fatalf("pg_stat_activity: %v", err)
		}
		if waiting >= want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("want %d sessions waiting on a lock, have %d after 5s", want, waiting)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func waitForIdleBackends(t *testing.T, db *pgxpool.Pool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		var busy int
		if err := db.QueryRow(context.Background(), `
			SELECT count(*) FROM pg_stat_activity
			WHERE datname = current_database() AND backend_type = 'client backend'
			  AND pid <> pg_backend_pid() AND state <> 'idle'`).Scan(&busy); err != nil {
			t.Fatalf("pg_stat_activity: %v", err)
		}
		if busy == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d backends still busy after 5s", busy)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func listPoolsVia(t *testing.T, srv *Server) []poolDTO {
	t.Helper()
	rr := doJSON(t, srv.mux, stdhttp.MethodGet, "/api/v1/pools", "", stdhttp.StatusOK)
	var pools []poolDTO
	if err := json.Unmarshal(rr.Body.Bytes(), &pools); err != nil {
		t.Fatalf("unmarshal pools: %v", err)
	}
	return pools
}

func assertNoOverlappingSiblings(t *testing.T, srv *Server) {
	t.Helper()
	pools := listPoolsVia(t, srv)
	for i, p := range pools {
		for _, q := range pools[i+1:] {
			if !sameParent(p.ParentID, q.ParentID) {
				continue
			}
			if netip.MustParsePrefix(p.CIDR).Overlaps(netip.MustParsePrefix(q.CIDR)) {
				t.Errorf("overlapping siblings stored: #%d %s and #%d %s", p.ID, p.CIDR, q.ID, q.CIDR)
			}
		}
	}
}

func TestCreatePool_TwoReplicasPastTheCheckTogether_StoreOne(t *testing.T) {
	r := newReplicaPair(t)
	root := createRootPool(t, r.a, "10.0.0.0/8")
	ctx := context.Background()

	release := holdPoolInserts(t, r.db)
	fromA := serveAsync(ctx, r.a, "/api/v1/pools", fmt.Sprintf(`{"name":"a","cidr":"10.1.0.0/16","parent_id":%d}`, root.ID))
	fromB := serveAsync(ctx, r.b, "/api/v1/pools", fmt.Sprintf(`{"name":"b","cidr":"10.1.0.0/17","parent_id":%d}`, root.ID))
	waitForLockWaiters(t, r.db, 2)
	release()

	a, b := <-fromA, <-fromB
	codes := map[int]int{a.Code: 1}
	codes[b.Code]++
	if codes[stdhttp.StatusCreated] != 1 || codes[stdhttp.StatusBadRequest] != 1 {
		t.Errorf("want one 201 and one 400, got replica a %d %s, replica b %d %s", a.Code, a.Body, b.Code, b.Body)
	}
	assertNoOverlappingSiblings(t, r.a)
}

func TestCreatePool_TwoReplicasRacing_StoreOnePerRound(t *testing.T) {
	r := newReplicaPair(t)
	root := createRootPool(t, r.a, "10.0.0.0/8")

	for round := 1; round <= 20; round++ {
		var posts []post
		for bits := 16; bits <= 27; bits++ {
			posts = append(posts, post{"/api/v1/pools", fmt.Sprintf(`{"name":"r%dc%d","cidr":"10.%d.0.0/%d","parent_id":%d}`, round, bits, round, bits, root.ID)})
		}
		got := postAcross([]*Server{r.a, r.b}, posts)
		if got[stdhttp.StatusCreated] != 1 || got[stdhttp.StatusBadRequest] != len(posts)-1 {
			t.Errorf("round %d: want one 201 and %d 400s, got %v", round, len(posts)-1, got)
		}
	}
	assertNoOverlappingSiblings(t, r.a)
}

func TestSchemaApplyAndCreatePool_TwoReplicas_StoreOne(t *testing.T) {
	r := newReplicaPair(t)
	ctx := context.Background()

	for i, pair := range []struct{ schema, pools *Server }{{r.a, r.b}, {r.b, r.a}} {
		n := i + 1
		release := holdPoolInserts(t, r.db)
		schema := serveAsync(ctx, pair.schema, "/api/v1/schema/apply", fmt.Sprintf(`{"pools":[
			{"ref":"r","name":"s%d","cidr":"10.%d.0.0/16","type":"supernet"},
			{"ref":"c","parent_ref":"r","name":"s%dc","cidr":"10.%d.1.0/24","type":"subnet"}
		],"skip_conflicts":false}`, n, n, n, n))
		pools := serveAsync(ctx, pair.pools, "/api/v1/pools", fmt.Sprintf(`{"name":"p%d","cidr":"10.%d.0.0/17"}`, n, n))
		waitForLockWaiters(t, r.db, 2)
		release()

		s, p := <-schema, <-pools
		schemaWon := s.Code == stdhttp.StatusOK && p.Code == stdhttp.StatusBadRequest
		poolsWon := s.Code == stdhttp.StatusConflict && p.Code == stdhttp.StatusCreated
		if !schemaWon && !poolsWon {
			t.Errorf("round %d: want exactly one winner, got schema/apply %d %s, pools %d %s", n, s.Code, s.Body, p.Code, p.Body)
		}
	}
	assertNoOverlappingSiblings(t, r.a)
}

func TestSchemaApply_CancelledMidBatch_StoresNothing(t *testing.T) {
	r := newReplicaPair(t)
	ctx := context.Background()

	blocker, err := r.db.Begin(ctx)
	if err != nil {
		t.Fatalf("begin blocker: %v", err)
	}
	t.Cleanup(func() { _ = blocker.Rollback(ctx) })
	if _, err := blocker.Exec(ctx, `INSERT INTO pools (organization_id, name, cidr)
		VALUES ('00000000-0000-0000-0000-000000000001', 'blocker', '10.2.0.0/16')`); err != nil {
		t.Fatalf("blocker insert: %v", err)
	}

	reqCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := serveAsync(reqCtx, r.a, "/api/v1/schema/apply", `{"pools":[
		{"ref":"one","name":"one","cidr":"10.1.0.0/16","type":"supernet"},
		{"ref":"two","name":"two","cidr":"10.2.0.0/16","type":"supernet"},
		{"ref":"three","name":"three","cidr":"10.3.0.0/16","type":"supernet"}
	],"skip_conflicts":false}`)
	waitForLockWaiters(t, r.db, 1)
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("schema/apply did not return after its context was cancelled")
	}

	if err := blocker.Rollback(ctx); err != nil {
		t.Fatalf("rollback blocker: %v", err)
	}
	waitForIdleBackends(t, r.db)
	if pools := listPoolsVia(t, r.a); len(pools) != 0 {
		t.Fatalf("cancelled batch stored %d pools: %+v", len(pools), pools)
	}
}
