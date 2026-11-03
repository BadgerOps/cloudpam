package api

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"cloudpam/internal/auth"
	"cloudpam/internal/domain"
	"cloudpam/internal/planning"
	"cloudpam/internal/storage"
)

// updateOnlyPrincipals are the two ways a caller can hold pools:update without pools:delete.
var updateOnlyPrincipals = map[string]func(context.Context) context.Context{
	"api key": func(ctx context.Context) context.Context {
		return auth.ContextWithAPIKey(ctx, &auth.APIKey{ID: "editor", Scopes: []string{"pools:read", "pools:create", "pools:update"}})
	},
	"custom role": func(ctx context.Context) context.Context {
		return auth.ContextWithRole(ctx, "pool-editor")
	},
}

type deletePermissionFixture struct {
	mux      *http.ServeMux
	st       *storage.MemoryStore
	recStore storage.RecommendationStore
	parent   domain.Pool
}

func setupDeletePermissionFixture(t *testing.T, principal func(context.Context) context.Context) deletePermissionFixture {
	t.Helper()
	roleStore := auth.NewMemoryRoleStore(auth.NewMemoryUserStore())
	if err := roleStore.CreateRole(context.Background(), &auth.RoleDefinition{
		Name: "pool-editor",
		Permissions: []auth.Permission{
			{Resource: auth.ResourcePools, Action: auth.ActionRead},
			{Resource: auth.ResourcePools, Action: auth.ActionCreate},
			{Resource: auth.ResourcePools, Action: auth.ActionUpdate},
		},
	}); err != nil {
		t.Fatal(err)
	}
	auth.SetRoleStoreProvider(roleStore)
	t.Cleanup(func() { auth.SetRoleStoreProvider(nil) })

	st := storage.NewMemoryStore()
	mux := http.NewServeMux()
	srv := NewServerWithSlog(mux, st, nil)
	recStore := storage.NewMemoryRecommendationStore(st)
	recSvc := planning.NewRecommendationService(planning.NewAnalysisService(st), recStore, st, storage.NewMemoryDiscoveryStore(st), srv)
	asPrincipal := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(principal(r.Context())))
		})
	}
	NewRecommendationServer(srv, recSvc, recStore).RegisterProtectedRecommendationRoutes(asPrincipal, slog.New(slog.NewTextHandler(io.Discard, nil)))

	parent, err := st.CreatePool(context.Background(), domain.CreatePool{Name: "Corp", CIDR: "10.0.0.0/16"})
	if err != nil {
		t.Fatal(err)
	}
	return deletePermissionFixture{mux: mux, st: st, recStore: recStore, parent: parent}
}

func (f deletePermissionFixture) child(t *testing.T, name, cidr string) domain.Pool {
	t.Helper()
	p, err := f.st.CreatePool(context.Background(), domain.CreatePool{Name: name, CIDR: cidr, ParentID: &f.parent.ID})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func (f deletePermissionFixture) store(t *testing.T, rec domain.Recommendation) domain.Recommendation {
	t.Helper()
	rec.ID = "rec-" + string(rec.Type)
	rec.Status = domain.RecommendationStatusPending
	rec.Priority = domain.RecommendationPriorityLow
	rec.Title = string(rec.Type)
	rec.CreatedAt, rec.UpdatedAt = time.Now().UTC(), time.Now().UTC()
	if err := f.recStore.CreateRecommendation(context.Background(), rec); err != nil {
		t.Fatal(err)
	}
	return rec
}

func (f deletePermissionFixture) apply(rec domain.Recommendation) *httptest.ResponseRecorder {
	rr := httptest.NewRecorder()
	f.mux.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/api/v1/recommendations/"+rec.ID+"/apply", nil))
	return rr
}

func (f deletePermissionFixture) live(t *testing.T, id int64) bool {
	t.Helper()
	_, found, err := f.st.GetPool(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return found
}

func (f deletePermissionFixture) pending(t *testing.T, rec domain.Recommendation) bool {
	t.Helper()
	got, err := f.recStore.GetRecommendation(context.Background(), rec.ID)
	if err != nil {
		t.Fatal(err)
	}
	return got.Status == domain.RecommendationStatusPending
}

// Reclaim and consolidation delete pools, so pools:update alone must not apply them.
func TestRecommendationApply_DestructiveTypesNeedPoolsDelete(t *testing.T) {
	for name, principal := range updateOnlyPrincipals {
		t.Run(name+" reclaim", func(t *testing.T) {
			f := setupDeletePermissionFixture(t, principal)
			idle := f.child(t, "idle", "10.0.9.0/24")
			rec := f.store(t, domain.Recommendation{
				PoolID: idle.ID, Type: domain.RecommendationTypeReclaim, SuggestedCIDR: idle.CIDR,
				Metadata: map[string]string{"last_updated": idle.UpdatedAt.UTC().Format(time.RFC3339Nano)},
			})
			if rr := f.apply(rec); rr.Code != http.StatusForbidden {
				t.Fatalf("reclaim apply with pools:update only = %d %s, want 403", rr.Code, rr.Body.String())
			}
			if !f.live(t, idle.ID) || !f.pending(t, rec) {
				t.Fatalf("a refused reclaim deleted pool %d or left the recommendation applied", idle.ID)
			}
		})
		t.Run(name+" consolidation", func(t *testing.T) {
			f := setupDeletePermissionFixture(t, principal)
			a := f.child(t, "a", "10.0.1.0/25")
			b := f.child(t, "b", "10.0.1.128/25")
			rec := f.store(t, domain.Recommendation{
				PoolID: f.parent.ID, Type: domain.RecommendationTypeConsolidation, SuggestedCIDR: "10.0.1.0/24",
				Metadata: map[string]string{
					"source_pool_ids": strconv.FormatInt(a.ID, 10) + "," + strconv.FormatInt(b.ID, 10),
					"source_cidrs":    a.CIDR + "," + b.CIDR,
				},
			})
			if rr := f.apply(rec); rr.Code != http.StatusForbidden {
				t.Fatalf("consolidation apply with pools:update only = %d %s, want 403", rr.Code, rr.Body.String())
			}
			kept, _, _ := f.st.GetPool(context.Background(), a.ID)
			if !f.live(t, b.ID) || kept.CIDR != a.CIDR || !f.pending(t, rec) {
				t.Fatalf("a refused consolidation changed the pools (kept %+v, absorbed live %v)", kept, f.live(t, b.ID))
			}
		})
	}
}

// Allocation and resize create or shrink pools and delete none, so pools:update still applies them.
func TestRecommendationApply_NonDestructiveTypesNeedOnlyPoolsUpdate(t *testing.T) {
	for name, principal := range updateOnlyPrincipals {
		t.Run(name+" allocation", func(t *testing.T) {
			f := setupDeletePermissionFixture(t, principal)
			rec := f.store(t, domain.Recommendation{PoolID: f.parent.ID, Type: domain.RecommendationTypeAllocation, SuggestedCIDR: "10.0.4.0/24"})
			if rr := f.apply(rec); rr.Code != http.StatusOK {
				t.Fatalf("allocation apply with pools:update = %d %s, want 200", rr.Code, rr.Body.String())
			}
		})
		t.Run(name+" resize", func(t *testing.T) {
			f := setupDeletePermissionFixture(t, principal)
			wide := f.child(t, "wide", "10.0.16.0/20")
			rec := f.store(t, domain.Recommendation{
				PoolID: wide.ID, Type: domain.RecommendationTypeResize, SuggestedCIDR: "10.0.16.0/24",
				Metadata: map[string]string{"current_cidr": wide.CIDR},
			})
			if rr := f.apply(rec); rr.Code != http.StatusOK {
				t.Fatalf("resize apply with pools:update = %d %s, want 200", rr.Code, rr.Body.String())
			}
		})
	}
}

// A principal that also holds pools:delete applies a reclaim.
func TestRecommendationApply_ReclaimWithPoolsDelete(t *testing.T) {
	f := setupDeletePermissionFixture(t, func(ctx context.Context) context.Context {
		return auth.ContextWithAPIKey(ctx, &auth.APIKey{ID: "deleter", Scopes: []string{"pools:read", "pools:update", "pools:delete"}})
	})
	idle := f.child(t, "idle", "10.0.9.0/24")
	rec := f.store(t, domain.Recommendation{
		PoolID: idle.ID, Type: domain.RecommendationTypeReclaim, SuggestedCIDR: idle.CIDR,
		Metadata: map[string]string{"last_updated": idle.UpdatedAt.UTC().Format(time.RFC3339Nano)},
	})
	if rr := f.apply(rec); rr.Code != http.StatusOK {
		t.Fatalf("reclaim apply with pools:delete = %d %s, want 200", rr.Code, rr.Body.String())
	}
	if f.live(t, idle.ID) {
		t.Fatalf("reclaimed pool %d is still live", idle.ID)
	}
}
