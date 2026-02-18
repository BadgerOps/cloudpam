//go:build sqlite

package sqlite

import (
	"context"
	"time"

	"cloudpam/internal/domain"
	"cloudpam/internal/storage"
)

var _ storage.UtilizationStore = (*Store)(nil)

func (s *Store) RecordSnapshot(ctx context.Context, snap domain.UtilizationSnapshot) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO utilization_snapshots (pool_id, total_ips, used_ips, available_ips, utilization, child_count, captured_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		snap.PoolID, snap.TotalIPs, snap.UsedIPs, snap.AvailableIPs,
		snap.Utilization, snap.ChildCount, snap.CapturedAt.Format(time.RFC3339))
	return err
}

func (s *Store) ListSnapshots(ctx context.Context, poolID int64, from, to time.Time) ([]domain.UtilizationSnapshot, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, pool_id, total_ips, used_ips, available_ips, utilization, child_count, captured_at
		FROM utilization_snapshots
		WHERE pool_id = ? AND captured_at >= ? AND captured_at <= ?
		ORDER BY captured_at ASC`,
		poolID, from.Format(time.RFC3339), to.Format(time.RFC3339))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.UtilizationSnapshot
	for rows.Next() {
		var snap domain.UtilizationSnapshot
		var capturedAt string
		if err := rows.Scan(&snap.ID, &snap.PoolID, &snap.TotalIPs, &snap.UsedIPs,
			&snap.AvailableIPs, &snap.Utilization, &snap.ChildCount, &capturedAt); err != nil {
			return nil, err
		}
		if t, e := time.Parse(time.RFC3339, capturedAt); e == nil {
			snap.CapturedAt = t
		}
		out = append(out, snap)
	}
	return out, rows.Err()
}

func (s *Store) LatestSnapshot(ctx context.Context, poolID int64) (*domain.UtilizationSnapshot, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, pool_id, total_ips, used_ips, available_ips, utilization, child_count, captured_at
		FROM utilization_snapshots
		WHERE pool_id = ?
		ORDER BY captured_at DESC
		LIMIT 1`, poolID)

	var snap domain.UtilizationSnapshot
	var capturedAt string
	if err := row.Scan(&snap.ID, &snap.PoolID, &snap.TotalIPs, &snap.UsedIPs,
		&snap.AvailableIPs, &snap.Utilization, &snap.ChildCount, &capturedAt); err != nil {
		if err.Error() == "sql: no rows in result set" {
			return nil, nil
		}
		return nil, err
	}
	if t, e := time.Parse(time.RFC3339, capturedAt); e == nil {
		snap.CapturedAt = t
	}
	return &snap, nil
}
