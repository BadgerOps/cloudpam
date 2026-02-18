package domain

// SecuritySettings holds runtime security configuration.
type SecuritySettings struct {
	SessionDurationHours   int      `json:"session_duration_hours"`
	MaxSessionsPerUser     int      `json:"max_sessions_per_user"`
	PasswordMinLength      int      `json:"password_min_length"`
	PasswordMaxLength      int      `json:"password_max_length"`
	LoginRateLimitPerMin   int      `json:"login_rate_limit_per_minute"`
	AccountLockoutAttempts int      `json:"account_lockout_attempts"`
	TrustedProxies         []string `json:"trusted_proxies"`
	LocalAuthEnabled       bool     `json:"local_auth_enabled"`
}

// DefaultSecuritySettings returns safe defaults.
func DefaultSecuritySettings() SecuritySettings {
	return SecuritySettings{
		SessionDurationHours:   24,
		MaxSessionsPerUser:     10,
		PasswordMinLength:      12,
		PasswordMaxLength:      72,
		LoginRateLimitPerMin:   5,
		AccountLockoutAttempts: 0,
		TrustedProxies:         []string{},
		LocalAuthEnabled:       true,
	}
}
