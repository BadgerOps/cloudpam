//go:build sqlite

package sqlite

import (
	"context"
	"database/sql"
	"time"

	"cloudpam/internal/domain"
	"cloudpam/internal/storage"
)

var _ storage.SyncScheduleStore = (*Store)(nil)

func (s *Store) GetSyncSchedule(ctx context.Context, id int64) (domain.SyncSchedule, error) {
	v := domain.SyncSchedule{AccountID: id}
	err := s.db.QueryRowContext(ctx, `SELECT interval_seconds,next_run_at,last_agent_id FROM sync_schedules WHERE account_id=?`, id).Scan(&v.IntervalSeconds, &v.NextRunAt, &v.LastAgentID)
	if err == sql.ErrNoRows {
		err = nil
	}
	return v, err
}
func (s *Store) PutSyncSchedule(ctx context.Context, id, interval int64, now time.Time) error {
	next := int64(0)
	if interval > 0 {
		next = now.Unix() + interval
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO sync_schedules(account_id,interval_seconds,next_run_at) VALUES(?,?,?) ON CONFLICT(account_id) DO UPDATE SET interval_seconds=excluded.interval_seconds,next_run_at=excluded.next_run_at`, id, interval, next)
	return err
}
func (s *Store) DueSyncSchedules(ctx context.Context, now time.Time) ([]domain.SyncSchedule, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT account_id,interval_seconds,next_run_at,last_agent_id FROM sync_schedules WHERE interval_seconds>0 AND next_run_at<=? AND lease_until<=? ORDER BY next_run_at`, now.Unix(), now.Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.SyncSchedule{}
	for rows.Next() {
		var v domain.SyncSchedule
		if err := rows.Scan(&v.AccountID, &v.IntervalSeconds, &v.NextRunAt, &v.LastAgentID); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (s *Store) ClaimSync(ctx context.Context, id int64, token string, now, until time.Time, scheduled bool) (bool, error) {
	_, err := s.db.ExecContext(ctx, `INSERT INTO sync_schedules(account_id) VALUES(?) ON CONFLICT(account_id) DO NOTHING`, id)
	if err != nil {
		return false, err
	}
	res, err := s.db.ExecContext(ctx, `UPDATE sync_schedules SET lease_token=?,lease_until=?,next_run_at=CASE WHEN interval_seconds>0 THEN ?+interval_seconds ELSE 0 END WHERE account_id=? AND lease_until<=? AND (?=false OR (interval_seconds>0 AND next_run_at<=?))`, token, until.Unix(), now.Unix(), id, now.Unix(), scheduled, now.Unix())
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}
func (s *Store) ReleaseSync(ctx context.Context, id int64, token string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE sync_schedules SET lease_token='',lease_until=0 WHERE account_id=? AND lease_token=?`, id, token)
	return err
}

func (s *Store) RecordSyncAgent(ctx context.Context, id int64, agentID string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO sync_schedules(account_id,last_agent_id) VALUES(?,?) ON CONFLICT(account_id) DO UPDATE SET last_agent_id=excluded.last_agent_id`, id, agentID)
	return err
}
