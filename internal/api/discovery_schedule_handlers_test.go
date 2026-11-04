package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"cloudpam/internal/audit"
	"cloudpam/internal/auth"
	"cloudpam/internal/discovery"
	"cloudpam/internal/domain"
	"cloudpam/internal/observability"
	"cloudpam/internal/storage"
)

func TestDiscoveryScheduleValidationAndAudit(t *testing.T) {
	f := newConnectivityFixture(t, probingCollector{})
	a := f.account(t, "aws")
	path := fmt.Sprintf("/api/v1/discovery/schedules/%d", a.ID)
	request := func(method, body string) *httptest.ResponseRecorder {
		rr := httptest.NewRecorder()
		f.mux.ServeHTTP(rr, httptest.NewRequest(method, path, strings.NewReader(body)))
		return rr
	}
	rr := request("GET", "")
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), `"interval_seconds":0`) {
		t.Fatalf("default: %d %s", rr.Code, rr.Body)
	}
	for _, body := range []string{`{}`, `{"interval_seconds":null}`, `{"interval_seconds":-1}`, `{"interval_seconds":59}`, `{"interval_seconds":604801}`, `{"interval_seconds":1.5}`, `{"interval_seconds":60,"unknown":true}`} {
		if rr := request("PUT", body); rr.Code != 400 {
			t.Errorf("%s: %d %s", body, rr.Code, rr.Body)
		}
	}
	rr = request("PUT", `{"interval_seconds":60}`)
	if rr.Code != 200 {
		t.Fatalf("save: %d %s", rr.Code, rr.Body)
	}
	var schedule domain.SyncSchedule
	if err := json.Unmarshal(rr.Body.Bytes(), &schedule); err != nil {
		t.Fatal(err)
	}
	if schedule.AccountID != a.ID || schedule.IntervalSeconds != 60 || schedule.NextRunAt == 0 {
		t.Fatalf("schedule=%+v", schedule)
	}
	rr = request("GET", "")
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), `"interval_seconds":60`) {
		t.Fatalf("get: %d %s", rr.Code, rr.Body)
	}
	events, _, err := f.audit.List(context.Background(), audit.ListOptions{Action: audit.ActionUpdate})
	if err != nil || len(events) != 1 {
		t.Fatalf("audit: %v %v", events, err)
	}
	rr = request("PUT", `{"interval_seconds":0}`)
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), `"next_run_at":0`) {
		t.Fatalf("disable: %d %s", rr.Code, rr.Body)
	}
	path = "/api/v1/discovery/schedules/999"
	if rr := request("GET", ""); rr.Code != 404 {
		t.Fatalf("missing: %d", rr.Code)
	}
	path = "/api/v1/discovery/schedules/bad"
	if rr := request("GET", ""); rr.Code != 400 {
		t.Fatalf("invalid: %d", rr.Code)
	}
	azure := f.account(t, "azure")
	path = fmt.Sprintf("/api/v1/discovery/schedules/%d", azure.ID)
	if rr := request("PUT", `{"interval_seconds":60}`); rr.Code != 422 {
		t.Fatalf("unsupported: %d %s", rr.Code, rr.Body)
	}
}
func TestDiscoverySchedulePermissions(t *testing.T) {
	for _, tc := range []struct {
		scope       string
		read, write int
	}{{"discovery:read", 200, 403}, {"discovery:write", 200, 200}} {
		t.Run(tc.scope, func(t *testing.T) {
			st := storage.NewMemoryStore()
			mux := http.NewServeMux()
			logger := slog.New(slog.NewTextHandler(io.Discard, nil))
			srv := NewServer(mux, st, observability.NewLogger(observability.Config{Output: io.Discard}), nil, nil)
			ds := storage.NewMemoryDiscoveryStore(st)
			svc := discovery.NewSyncService(ds)
			keys := auth.NewMemoryKeyStore()
			plain, key, err := auth.GenerateAPIKey(auth.GenerateAPIKeyOptions{Name: "schedule", Scopes: []string{tc.scope}})
			if err != nil {
				t.Fatal(err)
			}
			if err := keys.Create(t.Context(), key); err != nil {
				t.Fatal(err)
			}
			mw := DualAuthMiddleware(keys, auth.NewMemorySessionStore(), auth.NewMemoryUserStore(), true, logger)
			NewDiscoveryServer(srv, ds, svc, keys).RegisterProtectedDiscoveryRoutes(mw, logger)
			a, err := st.CreateAccount(t.Context(), domain.CreateAccount{Name: "a", Key: "aws:1", Provider: "aws"})
			if err != nil {
				t.Fatal(err)
			}
			path := fmt.Sprintf("/api/v1/discovery/schedules/%d", a.ID)
			wantStatus(t, doProtected(t, mux, "GET", path, "", plain), tc.read, "")
			wantStatus(t, doProtected(t, mux, "PUT", path, `{"interval_seconds":60}`, plain), tc.write, "")
			wantStatus(t, doProtected(t, mux, "GET", path, "", ""), 401, "")
		})
	}
}
