package domain

import "time"

// Delivery states reported by GET /api/v1/settings/log-destinations.
const (
	LogDestinationStateInvalid = "invalid"
	LogDestinationStateIdle    = "idle"
	LogDestinationStateOK      = "ok"
	LogDestinationStateFailing = "failing"
)

// LogDestination is one audit forwarding target and its delivery health.
// An invalid destination was configured but rejected at startup, so nothing
// is forwarded to it; config_error says why.
type LogDestination struct {
	Type          string     `json:"type"`
	Format        string     `json:"format"`
	Network       string     `json:"network"`
	Address       string     `json:"address"`
	AppName       string     `json:"app_name,omitempty"`
	State         string     `json:"state"`
	ConfigError   string     `json:"config_error,omitempty"`
	Delivered     uint64     `json:"delivered"`
	Failed        uint64     `json:"failed"`
	LastSuccessAt *time.Time `json:"last_success_at,omitempty"`
	LastFailureAt *time.Time `json:"last_failure_at,omitempty"`
	LastError     string     `json:"last_error,omitempty"`
}

// LogDestinationsResponse lists the active audit forwarding targets.
type LogDestinationsResponse struct {
	Destinations []LogDestination `json:"destinations"`
}
