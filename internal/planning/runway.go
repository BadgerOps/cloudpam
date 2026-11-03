package planning

import (
	"context"
	"fmt"
	"time"

	"cloudpam/internal/domain"
	"cloudpam/internal/storage"
)

// ProjectPoolGrowth fits the pool's utilization snapshots inside the
// projection window, with the live stats as the newest point, and returns
// the projection. It returns nil when the store keeps no snapshot history.
func (s *AnalysisService) ProjectPoolGrowth(ctx context.Context, poolID int64, stats domain.PoolStats, now time.Time) (*domain.GrowthProjection, error) {
	history, ok := s.store.(storage.UtilizationStore)
	if !ok {
		return nil, nil
	}
	snapshots, err := history.ListSnapshots(ctx, poolID, now.AddDate(0, 0, -ProjectionWindowDays), now)
	if err != nil {
		return nil, fmt.Errorf("list utilization snapshots for pool %d: %w", poolID, err)
	}
	points := make([]UsagePoint, 0, len(snapshots)+1)
	for _, snap := range snapshots {
		points = append(points, UsagePoint{At: snap.CapturedAt, UsedIPs: snap.UsedIPs})
	}
	points = append(points, UsagePoint{At: now, UsedIPs: stats.UsedIPs})
	proj := ProjectGrowth(points, stats.TotalIPs, now)
	return &proj, nil
}
