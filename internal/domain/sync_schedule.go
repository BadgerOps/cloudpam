package domain

// SyncSchedule holds per-account server discovery settings. Zero disables scheduling.
// NextRunAt is a Unix timestamp; zero means no run is scheduled.
type SyncSchedule struct {
	AccountID       int64  `json:"account_id"`
	IntervalSeconds int64  `json:"interval_seconds"`
	NextRunAt       int64  `json:"next_run_at"`
	LastAgentID     string `json:"-"`
}

type PutSyncScheduleRequest struct {
	IntervalSeconds int64 `json:"interval_seconds"`
}
