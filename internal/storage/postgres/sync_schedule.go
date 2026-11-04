//go:build postgres

package postgres

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"cloudpam/internal/domain"
	"cloudpam/internal/storage"
)

var _ storage.SyncScheduleStore = (*Store)(nil)

func (s *Store) GetSyncSchedule(ctx context.Context, id int64) (domain.SyncSchedule, error) {
	v := domain.SyncSchedule{AccountID: id}
	err := s.pool.QueryRow(ctx, `SELECT interval_seconds,next_run_at,last_agent_id FROM sync_schedules WHERE account_id=$1 AND EXISTS (SELECT 1 FROM accounts WHERE seq_id=$1 AND organization_id=$2 AND deleted_at IS NULL)`, id, s.orgID).Scan(&v.IntervalSeconds, &v.NextRunAt, &v.LastAgentID)
	if err == pgx.ErrNoRows {
		err = nil
	}
	return v, err
}
func (s *Store) PutSyncSchedule(ctx context.Context, id, interval int64, now time.Time) error {
	next := int64(0)
	if interval > 0 {
		next = now.Unix() + interval
	}
	_, err := s.pool.Exec(ctx, `INSERT INTO sync_schedules(account_id,interval_seconds,next_run_at) SELECT $1,$2,$3 FROM accounts WHERE seq_id=$1 AND organization_id=$4 AND deleted_at IS NULL ON CONFLICT(account_id) DO UPDATE SET interval_seconds=excluded.interval_seconds,next_run_at=excluded.next_run_at`, id, interval, next, s.orgID)
	return err
}
func (s *Store) DueSyncSchedules(ctx context.Context, now time.Time) ([]domain.SyncSchedule, error) {
	rows, err := s.pool.Query(ctx, `SELECT account_id,interval_seconds,next_run_at,last_agent_id FROM sync_schedules WHERE interval_seconds>0 AND next_run_at<=$1 AND lease_until<=$2 AND EXISTS (SELECT 1 FROM accounts WHERE seq_id=sync_schedules.account_id AND organization_id=$3 AND deleted_at IS NULL) ORDER BY next_run_at`, now.Unix(), now.Unix(), s.orgID)
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
	_, err := s.pool.Exec(ctx, `INSERT INTO sync_schedules(account_id) SELECT $1 FROM accounts WHERE seq_id=$1 AND organization_id=$2 AND deleted_at IS NULL ON CONFLICT(account_id) DO NOTHING`, id, s.orgID)
	if err != nil {
		return false, err
	}
	res, err := s.pool.Exec(ctx, `UPDATE sync_schedules SET lease_token=$1,lease_until=$2,next_run_at=CASE WHEN interval_seconds>0 THEN $3+interval_seconds ELSE 0 END WHERE account_id=$4 AND lease_until<=$5 AND ($6=false OR (interval_seconds>0 AND next_run_at<=$7)) AND EXISTS (SELECT 1 FROM accounts WHERE seq_id=$4 AND organization_id=$8 AND deleted_at IS NULL)`, token, until.Unix(), now.Unix(), id, now.Unix(), scheduled, now.Unix(), s.orgID)
	if err != nil {
		return false, err
	}
	return res.RowsAffected() == 1, nil
}
func (s *Store) ReleaseSync(ctx context.Context, id int64, token string) error {
	_, err := s.pool.Exec(ctx, `UPDATE sync_schedules SET lease_token='',lease_until=0 WHERE account_id=$1 AND lease_token=$2 AND EXISTS (SELECT 1 FROM accounts WHERE seq_id=$1 AND organization_id=$3)`, id, token, s.orgID)
	return err
}

func (s *Store) RecordSyncAgent(ctx context.Context, id int64, agentID string) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO sync_schedules(account_id,last_agent_id) SELECT $1,$2 FROM accounts WHERE seq_id=$1 AND organization_id=$3 AND deleted_at IS NULL ON CONFLICT(account_id) DO UPDATE SET last_agent_id=excluded.last_agent_id`, id, agentID, s.orgID)
	return err
}
