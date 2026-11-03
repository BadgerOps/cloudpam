package api

import (
	"context"
	"encoding/json"
	stdhttp "net/http"
	"strconv"
	"testing"
	"time"

	"cloudpam/internal/domain"
)

func createPoolID(t *testing.T, srv *Server, body string) int64 {
	t.Helper()
	rr := doJSON(t, srv.mux, stdhttp.MethodPost, "/api/v1/pools", body, stdhttp.StatusCreated)
	var p poolDTO
	if err := json.Unmarshal(rr.Body.Bytes(), &p); err != nil {
		t.Fatalf("unmarshal pool: %v", err)
	}
	return p.ID
}

func getStats(t *testing.T, srv *Server, id int64) domain.PoolStats {
	t.Helper()
	rr := doJSON(t, srv.mux, stdhttp.MethodGet, "/api/v1/pools/"+strconv.FormatInt(id, 10)+"/stats", "", stdhttp.StatusOK)
	var stats domain.PoolStats
	if err := json.Unmarshal(rr.Body.Bytes(), &stats); err != nil {
		t.Fatalf("unmarshal stats: %v", err)
	}
	return stats
}

func TestPoolStatsProjectionFromSnapshots(t *testing.T) {
	srv, st := setupTestServer()
	root := createPoolID(t, srv, `{"name":"root","cidr":"10.0.0.0/16"}`)
	parent := strconv.FormatInt(root, 10)
	for i, cidr := range []string{"10.0.0.0/24", "10.0.1.0/24", "10.0.2.0/24", "10.0.3.0/24"} {
		createPoolID(t, srv, `{"name":"child`+strconv.Itoa(i)+`","cidr":"`+cidr+`","parent_id":`+parent+`}`)
	}

	// One /24 was allocated every ten days; the live stats are the fourth point.
	now := time.Now()
	for i, used := range []int64{256, 512, 768} {
		if err := st.RecordSnapshot(context.Background(), domain.UtilizationSnapshot{
			PoolID:     root,
			TotalIPs:   65536,
			UsedIPs:    used,
			CapturedAt: now.AddDate(0, 0, -30+10*i),
		}); err != nil {
			t.Fatalf("record snapshot: %v", err)
		}
	}

	stats := getStats(t, srv, root)
	if stats.UsedIPs != 1024 {
		t.Fatalf("used_ips = %d, want 1024", stats.UsedIPs)
	}
	proj := stats.Projection
	if proj == nil {
		t.Fatal("stats carry no projection")
	}
	if proj.Status != domain.ProjectionOK {
		t.Fatalf("projection status = %q, want ok: %+v", proj.Status, proj)
	}
	if proj.SampleCount != 4 {
		t.Errorf("sample_count = %d, want 3 snapshots plus the live point", proj.SampleCount)
	}
	// The live point lands a few milliseconds after "now", so allow a small tolerance.
	if proj.GrowthPerDay < 25 || proj.GrowthPerDay > 26 {
		t.Errorf("growth_ips_per_day = %v, want about 25.6", proj.GrowthPerDay)
	}
	if proj.RunwayDays == nil || *proj.RunwayDays < 2500 || *proj.RunwayDays > 2600 {
		t.Errorf("runway_days = %v, want about 2520", proj.RunwayDays)
	}
	if proj.ExhaustionDate == nil || !proj.ExhaustionDate.After(now) {
		t.Errorf("exhaustion_date = %v, want after now", proj.ExhaustionDate)
	}
	if len(proj.Forecast) != 3 || proj.Forecast[0].Months != 3 || proj.Forecast[0].UsedIPs <= 1024 {
		t.Errorf("forecast = %+v, want three horizons growing past 1024", proj.Forecast)
	}
}

func TestPoolStatsProjectionWithoutHistory(t *testing.T) {
	srv, _ := setupTestServer()
	root := createPoolID(t, srv, `{"name":"root","cidr":"10.0.0.0/16"}`)

	stats := getStats(t, srv, root)
	if stats.Projection == nil {
		t.Fatal("stats carry no projection")
	}
	if stats.Projection.Status != domain.ProjectionInsufficientHistory {
		t.Errorf("status = %q, want insufficient_history", stats.Projection.Status)
	}
	if stats.Projection.SampleCount != 1 {
		t.Errorf("sample_count = %d, want only the live point", stats.Projection.SampleCount)
	}
	if stats.Projection.RunwayDays != nil {
		t.Errorf("runway_days = %v, want unset", *stats.Projection.RunwayDays)
	}
}

func TestPoolStatsProjectionStaysOutOfHierarchy(t *testing.T) {
	srv, _ := setupTestServer()
	createPoolID(t, srv, `{"name":"root","cidr":"10.0.0.0/16"}`)

	rr := doJSON(t, srv.mux, stdhttp.MethodGet, "/api/v1/pools/hierarchy", "", stdhttp.StatusOK)
	var resp struct {
		Pools []struct {
			Stats map[string]json.RawMessage `json:"stats"`
		} `json:"pools"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal hierarchy: %v", err)
	}
	if len(resp.Pools) != 1 {
		t.Fatalf("hierarchy has %d pools, want 1", len(resp.Pools))
	}
	if _, ok := resp.Pools[0].Stats["projection"]; ok {
		t.Error("hierarchy stats must omit the projection")
	}
}
