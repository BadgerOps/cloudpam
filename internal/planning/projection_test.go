package planning

import (
	"encoding/json"
	"math"
	"testing"
	"time"

	"cloudpam/internal/domain"
)

var projNow = time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)

func daysAgo(d float64) time.Time {
	return projNow.Add(-time.Duration(d * float64(24*time.Hour)))
}

func TestProjectGrowthLinearRunway(t *testing.T) {
	// 256 IPs per day for 30 days.
	points := []UsagePoint{
		{At: daysAgo(30), UsedIPs: 0},
		{At: daysAgo(20), UsedIPs: 2560},
		{At: daysAgo(10), UsedIPs: 5120},
		{At: daysAgo(0), UsedIPs: 7680},
	}
	proj := ProjectGrowth(points, 65536, projNow)

	if proj.Status != domain.ProjectionOK {
		t.Fatalf("status = %q, want ok", proj.Status)
	}
	if proj.SampleCount != 4 {
		t.Errorf("sample_count = %d, want 4", proj.SampleCount)
	}
	if math.Abs(proj.GrowthPerDay-256) > 1e-6 {
		t.Errorf("growth_ips_per_day = %v, want 256", proj.GrowthPerDay)
	}
	wantRunway := float64(65536-7680) / 256
	if proj.RunwayDays == nil || math.Abs(*proj.RunwayDays-wantRunway) > 1e-6 {
		t.Fatalf("runway_days = %v, want %v", proj.RunwayDays, wantRunway)
	}
	wantExhaustion := projNow.Add(time.Duration(wantRunway * float64(24*time.Hour)))
	if proj.ExhaustionDate == nil || !proj.ExhaustionDate.Equal(wantExhaustion) {
		t.Errorf("exhaustion_date = %v, want %v", proj.ExhaustionDate, wantExhaustion)
	}
	if len(proj.Forecast) != 3 {
		t.Fatalf("forecast has %d points, want 3", len(proj.Forecast))
	}
	for i, months := range []int{3, 6, 12} {
		want := int64(math.Round(7680 + 256*float64(months)*daysPerMonth))
		if want > 65536 {
			want = 65536
		}
		if proj.Forecast[i].Months != months || proj.Forecast[i].UsedIPs != want {
			t.Errorf("forecast[%d] = %+v, want months=%d used=%d", i, proj.Forecast[i], months, want)
		}
	}
	if proj.Forecast[2].Utilization != 100 {
		t.Errorf("12-month utilization = %v, want capped at 100", proj.Forecast[2].Utilization)
	}
}

func TestProjectGrowthFlatUsage(t *testing.T) {
	points := []UsagePoint{
		{At: daysAgo(60), UsedIPs: 512},
		{At: daysAgo(0), UsedIPs: 512},
	}
	proj := ProjectGrowth(points, 65536, projNow)
	if proj.Status != domain.ProjectionNoGrowth {
		t.Fatalf("status = %q, want no_growth", proj.Status)
	}
	if proj.RunwayDays != nil || proj.ExhaustionDate != nil {
		t.Errorf("flat usage must not carry a runway: %+v", proj)
	}
	if len(proj.Forecast) != 3 || proj.Forecast[2].UsedIPs != 512 {
		t.Errorf("flat forecast = %+v, want 512 at every horizon", proj.Forecast)
	}
}

func TestProjectGrowthShrinkingUsage(t *testing.T) {
	points := []UsagePoint{
		{At: daysAgo(30), UsedIPs: 4096},
		{At: daysAgo(0), UsedIPs: 1024},
	}
	proj := ProjectGrowth(points, 65536, projNow)
	if proj.Status != domain.ProjectionNoGrowth {
		t.Fatalf("status = %q, want no_growth", proj.Status)
	}
	if proj.GrowthPerDay >= 0 {
		t.Errorf("growth_ips_per_day = %v, want negative", proj.GrowthPerDay)
	}
	if proj.Forecast[2].UsedIPs != 0 {
		t.Errorf("12-month forecast = %d, want clamped to 0", proj.Forecast[2].UsedIPs)
	}
}

func TestProjectGrowthInsufficientHistory(t *testing.T) {
	cases := map[string][]UsagePoint{
		"no points":      nil,
		"one point":      {{At: daysAgo(10), UsedIPs: 256}},
		"span under 7d":  {{At: daysAgo(6), UsedIPs: 0}, {At: daysAgo(0), UsedIPs: 2048}},
		"outside window": {{At: daysAgo(200), UsedIPs: 0}, {At: daysAgo(100), UsedIPs: 2048}},
	}
	for name, points := range cases {
		t.Run(name, func(t *testing.T) {
			proj := ProjectGrowth(points, 65536, projNow)
			if proj.Status != domain.ProjectionInsufficientHistory {
				t.Fatalf("status = %q, want insufficient_history", proj.Status)
			}
			if proj.RunwayDays != nil || len(proj.Forecast) != 0 {
				t.Errorf("insufficient history must carry no runway or forecast: %+v", proj)
			}
			if proj.WindowDays != ProjectionWindowDays {
				t.Errorf("window_days = %d, want %d", proj.WindowDays, ProjectionWindowDays)
			}
		})
	}
}

func TestProjectGrowthIgnoresPointsOutsideWindow(t *testing.T) {
	// A burst 200 days ago must not pull the fitted rate up.
	points := []UsagePoint{
		{At: daysAgo(200), UsedIPs: 0},
		{At: daysAgo(150), UsedIPs: 60000},
		{At: daysAgo(80), UsedIPs: 60000},
		{At: daysAgo(0), UsedIPs: 60800},
	}
	proj := ProjectGrowth(points, 65536, projNow)
	if proj.SampleCount != 2 {
		t.Fatalf("sample_count = %d, want 2", proj.SampleCount)
	}
	if math.Abs(proj.GrowthPerDay-10) > 1e-6 {
		t.Errorf("growth_ips_per_day = %v, want 10", proj.GrowthPerDay)
	}
}

func TestProjectGrowthFullPool(t *testing.T) {
	points := []UsagePoint{
		{At: daysAgo(30), UsedIPs: 32768},
		{At: daysAgo(0), UsedIPs: 65536},
	}
	proj := ProjectGrowth(points, 65536, projNow)
	if proj.Status != domain.ProjectionOK || proj.RunwayDays == nil || *proj.RunwayDays != 0 {
		t.Fatalf("full pool must report zero runway: %+v", proj)
	}
	if !proj.ExhaustionDate.Equal(projNow) {
		t.Errorf("exhaustion_date = %v, want now", proj.ExhaustionDate)
	}
}

func TestProjectGrowthLongRunwayOmitsExhaustionDate(t *testing.T) {
	// A /8 gaining one /24 every 30 days has thousands of years of runway.
	points := []UsagePoint{
		{At: daysAgo(60), UsedIPs: 0},
		{At: daysAgo(30), UsedIPs: 256},
		{At: daysAgo(0), UsedIPs: 512},
	}
	proj := ProjectGrowth(points, 1<<24, projNow)

	if proj.Status != domain.ProjectionOK {
		t.Fatalf("status = %q, want ok", proj.Status)
	}
	wantRunway := float64(1<<24-512) / (256.0 / 30)
	if proj.RunwayDays == nil || math.Abs(*proj.RunwayDays-wantRunway) > 1e-3 {
		t.Fatalf("runway_days = %v, want %v", proj.RunwayDays, wantRunway)
	}
	if proj.ExhaustionDate != nil {
		t.Errorf("exhaustion_date = %v, want omitted beyond the date horizon", proj.ExhaustionDate)
	}
	if _, err := json.Marshal(proj); err != nil {
		t.Errorf("marshal projection: %v", err)
	}
}

func TestProjectGrowthExhaustionDateWithinHorizon(t *testing.T) {
	// One IP per day into a pool with 50 years of free space.
	free := int64(50 * 365)
	points := []UsagePoint{
		{At: daysAgo(10), UsedIPs: 0},
		{At: daysAgo(0), UsedIPs: 10},
	}
	proj := ProjectGrowth(points, free+10, projNow)

	if proj.ExhaustionDate == nil {
		t.Fatal("exhaustion_date omitted, want a date about 50 years out")
	}
	if !proj.ExhaustionDate.After(projNow) {
		t.Errorf("exhaustion_date = %v, want after now", proj.ExhaustionDate)
	}
	if want := projNow.AddDate(0, 0, int(free)); proj.ExhaustionDate.Sub(want).Abs() > 24*time.Hour {
		t.Errorf("exhaustion_date = %v, want about %v", proj.ExhaustionDate, want)
	}
}
