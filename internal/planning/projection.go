package planning

import (
	"math"
	"sort"
	"time"

	"cloudpam/internal/domain"
)

// ProjectionWindowDays is how far back the growth fit looks.
const ProjectionWindowDays = 90

// minHistoryDays is the shortest span of history the fit accepts.
const minHistoryDays = 7

const daysPerMonth = 30.4375

// maxExhaustionDays bounds the exhaustion date well inside time.Duration's
// roughly 292-year range; a longer runway gets no date.
const maxExhaustionDays = 100 * 365

var forecastMonths = []int{3, 6, 12}

// UsagePoint is one observation of a pool's used address count.
type UsagePoint struct {
	At      time.Time
	UsedIPs int64
}

// ProjectGrowth fits a least-squares line through the usage points inside
// the window ending at now and extrapolates it to a runway and a forecast.
// The last point is taken as the current usage; a pool already at capacity
// is exhausted whatever its history.
func ProjectGrowth(points []UsagePoint, totalIPs int64, now time.Time) domain.GrowthProjection {
	windowStart := now.AddDate(0, 0, -ProjectionWindowDays)
	proj := domain.GrowthProjection{
		Status:      domain.ProjectionInsufficientHistory,
		WindowDays:  ProjectionWindowDays,
		WindowStart: windowStart,
		Forecast:    []domain.ProjectedUsage{},
	}

	var window []UsagePoint
	for _, p := range points {
		if !p.At.Before(windowStart) && !p.At.After(now) {
			window = append(window, p)
		}
	}
	sort.Slice(window, func(i, j int) bool { return window[i].At.Before(window[j].At) })
	proj.SampleCount = len(window)
	if len(window) > 0 && window[len(window)-1].UsedIPs >= totalIPs {
		var zero float64
		proj.Status = domain.ProjectionExhausted
		proj.RunwayDays = &zero
		proj.Forecast = forecast(window[len(window)-1].UsedIPs, 0, totalIPs)
		return proj
	}
	if len(window) < 2 {
		return proj
	}
	span := window[len(window)-1].At.Sub(window[0].At)
	if span < minHistoryDays*24*time.Hour {
		return proj
	}

	rate := slopePerDay(window)
	proj.GrowthPerDay = rate
	current := window[len(window)-1].UsedIPs
	proj.Forecast = forecast(current, rate, totalIPs)
	if rate <= 0 {
		proj.Status = domain.ProjectionNoGrowth
		return proj
	}

	available := math.Max(float64(totalIPs-current), 0)
	runway := available / rate
	proj.Status = domain.ProjectionOK
	proj.RunwayDays = &runway
	if runway <= maxExhaustionDays {
		exhaustion := now.Add(time.Duration(runway * float64(24*time.Hour)))
		proj.ExhaustionDate = &exhaustion
	}
	return proj
}

// slopePerDay is the ordinary least-squares slope of used IPs over days.
func slopePerDay(points []UsagePoint) float64 {
	origin := points[0].At
	n := float64(len(points))
	var sumX, sumY, sumXY, sumXX float64
	for _, p := range points {
		x := p.At.Sub(origin).Hours() / 24
		y := float64(p.UsedIPs)
		sumX += x
		sumY += y
		sumXY += x * y
		sumXX += x * x
	}
	denom := n*sumXX - sumX*sumX
	if denom == 0 {
		return 0
	}
	return (n*sumXY - sumX*sumY) / denom
}

func forecast(current int64, rate float64, totalIPs int64) []domain.ProjectedUsage {
	out := make([]domain.ProjectedUsage, 0, len(forecastMonths))
	for _, months := range forecastMonths {
		used := float64(current) + rate*float64(months)*daysPerMonth
		used = math.Min(math.Max(used, 0), float64(totalIPs))
		var utilization float64
		if totalIPs > 0 {
			utilization = used / float64(totalIPs) * 100
		}
		out = append(out, domain.ProjectedUsage{
			Months:      months,
			UsedIPs:     int64(math.Round(used)),
			Utilization: utilization,
		})
	}
	return out
}
