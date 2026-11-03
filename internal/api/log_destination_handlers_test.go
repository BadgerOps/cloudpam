package api

import (
	"encoding/json"
	stdhttp "net/http"
	"net/http/httptest"
	"testing"
	"time"

	"cloudpam/internal/domain"
	"cloudpam/internal/storage"
)

func setupLogDestinationsServer(src func() []domain.LogDestination) *stdhttp.ServeMux {
	mux := stdhttp.NewServeMux()
	srv := NewServerWithSlog(mux, storage.NewMemoryStore(), nil)
	settingsSrv := NewSettingsServer(srv, storage.NewMemorySettingsStore())
	settingsSrv.SetLogDestinations(src)
	settingsSrv.RegisterSettingsRoutes()
	return mux
}

func getLogDestinations(t *testing.T, mux *stdhttp.ServeMux) domain.LogDestinationsResponse {
	t.Helper()
	req := httptest.NewRequest(stdhttp.MethodGet, "/api/v1/settings/log-destinations", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != stdhttp.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var resp domain.LogDestinationsResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	return resp
}

func TestLogDestinationsHandler_EmptyWhenForwardingOff(t *testing.T) {
	for name, src := range map[string]func() []domain.LogDestination{
		"unset": nil,
		"no-op": func() []domain.LogDestination { return nil },
		"empty": func() []domain.LogDestination { return []domain.LogDestination{} },
	} {
		t.Run(name, func(t *testing.T) {
			resp := getLogDestinations(t, setupLogDestinationsServer(src))
			if resp.Destinations == nil || len(resp.Destinations) != 0 {
				t.Fatalf("destinations = %#v, want an empty list", resp.Destinations)
			}
		})
	}
}

func TestLogDestinationsHandler_ReportsLiveHealth(t *testing.T) {
	failedAt := time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC)
	calls := 0
	mux := setupLogDestinationsServer(func() []domain.LogDestination {
		calls++
		return []domain.LogDestination{{
			Type:          "syslog",
			Format:        "cef",
			Network:       "tcp",
			Address:       "siem.example.com:514",
			AppName:       "cloudpam",
			State:         domain.LogDestinationStateFailing,
			Delivered:     uint64(calls),
			Failed:        1,
			LastFailureAt: &failedAt,
			LastError:     "dial tcp: connection refused",
		}}
	})

	first := getLogDestinations(t, mux)
	second := getLogDestinations(t, mux)
	if len(second.Destinations) != 1 {
		t.Fatalf("destinations = %d, want 1", len(second.Destinations))
	}
	got := second.Destinations[0]
	if got.Address != "siem.example.com:514" || got.Network != "tcp" || got.State != "failing" || got.LastError != "dial tcp: connection refused" {
		t.Fatalf("destination = %+v", got)
	}
	if got.LastFailureAt == nil || !got.LastFailureAt.Equal(failedAt) {
		t.Fatalf("last_failure_at = %v, want %v", got.LastFailureAt, failedAt)
	}
	if first.Destinations[0].Delivered == got.Delivered {
		t.Fatal("each request should read the current health, not a cached snapshot")
	}
}
