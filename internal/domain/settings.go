package domain

// SecuritySettings holds runtime security configuration.
type SecuritySettings struct {
	SessionDurationHours          int      `json:"session_duration_hours"`
	MaxSessionsPerUser            int      `json:"max_sessions_per_user"`
	PasswordMinLength             int      `json:"password_min_length"`
	PasswordMaxLength             int      `json:"password_max_length"`
	LoginRateLimitPerMin          int      `json:"login_rate_limit_per_minute"`
	AccountLockoutAttempts        int      `json:"account_lockout_attempts"`
	AccountLockoutCooldownMinutes int      `json:"account_lockout_cooldown_minutes"`
	TrustedProxies                []string `json:"trusted_proxies"`
	LocalAuthEnabled              bool     `json:"local_auth_enabled"`
}

// DefaultSecuritySettings returns safe defaults.
func DefaultSecuritySettings() SecuritySettings {
	return SecuritySettings{
		SessionDurationHours:          24,
		MaxSessionsPerUser:            10,
		PasswordMinLength:             12,
		PasswordMaxLength:             72,
		LoginRateLimitPerMin:          5,
		AccountLockoutAttempts:        0,
		AccountLockoutCooldownMinutes: 15,
		TrustedProxies:                []string{},
		LocalAuthEnabled:              true,
	}
}

// NormalizeSecuritySettings fills defaults for fields added after older
// settings documents may already exist in storage.
func NormalizeSecuritySettings(settings *SecuritySettings) *SecuritySettings {
	if settings == nil {
		defaults := DefaultSecuritySettings()
		return &defaults
	}
	if settings.AccountLockoutCooldownMinutes <= 0 {
		settings.AccountLockoutCooldownMinutes = DefaultSecuritySettings().AccountLockoutCooldownMinutes
	}
	if settings.TrustedProxies == nil {
		settings.TrustedProxies = []string{}
	}
	return settings
}
