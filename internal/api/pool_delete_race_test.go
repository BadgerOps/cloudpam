package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	stdhttp "net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"cloudpam/internal/domain"
	"cloudpam/internal/observability"
	"cloudpam/internal/storage"
)

// pauseAfterListStore holds the first ListPools call made after pause() until resume is closed,
// after that call has read the pools. A placement held there has already seen the parent.
type pauseAfterListStore struct {
	*storage.MemoryStore
	armed  atomic.Bool
	listed chan struct{}
	resume chan struct{}
}

func (s *pauseAfterListStore) pause() {
	s.listed = make(chan struct{})
	s.resume = make(chan struct{})
	s.armed.Store(true)
}

func (s *pauseAfterListStore) ListPools(ctx context.Context) ([]domain.Pool, error) {
	pools, err := s.MemoryStore.ListPools(ctx)
	if s.armed.CompareAndSwap(true, false) {
		close(s.listed)
		<-s.resume
	}
	return pools, err
}

func serveRequest(srv *Server, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rr := httptest.NewRecorder()
	srv.mux.ServeHTTP(rr, req)
	return rr
}

func orphanedPools(t *testing.T, st storage.Store) []domain.Pool {
	t.Helper()
	pools, err := st.ListPools(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	live := make(map[int64]bool, len(pools))
	for _, p := range pools {
		live[p.ID] = true
	}
	var orphans []domain.Pool
	for _, p := range pools {
		if p.ParentID != nil && !live[*p.ParentID] {
			orphans = append(orphans, p)
		}
	}
	return orphans
}

// A delete that arrives while a child create has already read its parent must not leave the
// child stored under a deleted parent (#269).
func TestDeleteDuringChildCreate_NeverOrphansTheChild(t *testing.T) {
	for _, tc := range []struct {
		name       string
		deletePath func(root poolDTO, accountID int64) string
		wantDelete int
		wantLive   int
	}{
		{"pool delete", func(root poolDTO, _ int64) string { return fmt.Sprintf("/api/v1/pools/%d", root.ID) }, stdhttp.StatusConflict, 2},
		{"pool cascade delete", func(root poolDTO, _ int64) string { return fmt.Sprintf("/api/v1/pools/%d?force=true", root.ID) }, stdhttp.StatusNoContent, 0},
		{"account cascade delete", func(_ poolDTO, accountID int64) string {
			return fmt.Sprintf("/api/v1/accounts/%d?force=true", accountID)
		}, stdhttp.StatusNoContent, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st := &pauseAfterListStore{MemoryStore: storage.NewMemoryStore()}
			logger := observability.NewLogger(observability.Config{Level: "info", Format: "json", Output: io.Discard})
			srv := NewServer(stdhttp.NewServeMux(), st, logger, nil, nil)
			srv.registerUnprotectedTestRoutes()

			account, err := st.CreateAccount(t.Context(), domain.CreateAccount{Key: "aws:1", Name: "prod", Provider: "aws"})
			if err != nil {
				t.Fatal(err)
			}
			rr := doJSON(t, srv.mux, stdhttp.MethodPost, "/api/v1/pools", fmt.Sprintf(`{"name":"root","cidr":"10.0.0.0/8","account_id":%d}`, account.ID), stdhttp.StatusCreated)
			var root poolDTO
			if err := json.Unmarshal(rr.Body.Bytes(), &root); err != nil {
				t.Fatal(err)
			}

			st.pause()
			created := make(chan *httptest.ResponseRecorder, 1)
			go func() {
				created <- serveRequest(srv, stdhttp.MethodPost, "/api/v1/pools", fmt.Sprintf(`{"name":"child","cidr":"10.1.0.0/16","parent_id":%d}`, root.ID))
			}()
			<-st.listed

			deleted := make(chan *httptest.ResponseRecorder, 1)
			go func() { deleted <- serveRequest(srv, stdhttp.MethodDelete, tc.deletePath(root, account.ID), "") }()
			var del *httptest.ResponseRecorder
			select {
			case del = <-deleted:
			case <-time.After(200 * time.Millisecond):
			}
			close(st.resume)
			if c := <-created; c.Code != stdhttp.StatusCreated {
				t.Fatalf("create child: %d %s", c.Code, c.Body.String())
			}
			if del == nil {
				del = <-deleted
			}

			if orphans := orphanedPools(t, st); len(orphans) > 0 {
				t.Fatalf("pools stored under a deleted parent: %+v", orphans)
			}
			if del.Code != tc.wantDelete {
				t.Fatalf("delete: got %d %s, want %d", del.Code, del.Body.String(), tc.wantDelete)
			}
			if n := len(storedCIDRs(t, st)); n != tc.wantLive {
				t.Fatalf("live pools = %d, want %d", n, tc.wantLive)
			}
		})
	}
}
