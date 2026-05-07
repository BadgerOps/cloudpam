package domain

// SecuritySettings holds runtime security configuration.
type SecuritySettings struct {
	SessionDurationHours          int                 `json:"session_duration_hours"`
	MaxSessionsPerUser            int                 `json:"max_sessions_per_user"`
	PasswordMinLength             int                 `json:"password_min_length"`
	PasswordMaxLength             int                 `json:"password_max_length"`
	LoginRateLimitPerMin          int                 `json:"login_rate_limit_per_minute"`
	AccountLockoutAttempts        int                 `json:"account_lockout_attempts"`
	AccountLockoutCooldownMinutes int                 `json:"account_lockout_cooldown_minutes"`
	TrustedProxies                []string            `json:"trusted_proxies"`
	LocalAuthEnabled              bool                `json:"local_auth_enabled"`
	APIKeyDefaultExpiryDays       int                 `json:"api_key_default_expiry_days"`
	APIKeyMaxLifetimeDays         int                 `json:"api_key_max_lifetime_days"`
	APIKeyRotationReminderDays    int                 `json:"api_key_rotation_reminder_days"`
	APIKeyAllowedScopesByRole     map[string][]string `json:"api_key_allowed_scopes_by_role"`
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
		APIKeyDefaultExpiryDays:       0,
		APIKeyMaxLifetimeDays:         0,
		APIKeyRotationReminderDays:    14,
		APIKeyAllowedScopesByRole:     DefaultAPIKeyAllowedScopesByRole(),
	}
}

// DefaultAPIKeyAllowedScopesByRole returns the default maximum API key scopes
// each built-in role may issue. Admins can issue all scopes; other roles are
// constrained to scopes that match their effective privileges.
func DefaultAPIKeyAllowedScopesByRole() map[string][]string {
	return map[string][]string{
		"admin": {
			"pools:read", "pools:write",
			"accounts:read", "accounts:write",
			"keys:read", "keys:write",
			"discovery:read", "discovery:write",
			"audit:read",
			"*",
		},
		"operator": {
			"pools:read", "pools:write",
			"accounts:read", "accounts:write",
			"discovery:read", "discovery:write",
		},
		"viewer": {
			"pools:read",
			"accounts:read",
			"discovery:read",
		},
		"auditor": {
			"audit:read",
		},
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
	if settings.APIKeyRotationReminderDays < 0 {
		settings.APIKeyRotationReminderDays = DefaultSecuritySettings().APIKeyRotationReminderDays
	}
	defaultScopes := DefaultAPIKeyAllowedScopesByRole()
	if settings.APIKeyAllowedScopesByRole == nil {
		settings.APIKeyAllowedScopesByRole = defaultScopes
	} else {
		for role, scopes := range defaultScopes {
			if _, ok := settings.APIKeyAllowedScopesByRole[role]; !ok {
				settings.APIKeyAllowedScopesByRole[role] = append([]string(nil), scopes...)
			}
		}
		for role, scopes := range settings.APIKeyAllowedScopesByRole {
			if scopes == nil {
				settings.APIKeyAllowedScopesByRole[role] = []string{}
			}
		}
	}
	return settings
}
