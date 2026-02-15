//go:build sqlite && postgres

package auth

import "strings"

const defaultOrgID = "00000000-0000-0000-0000-000000000001"

// isUniqueViolation checks for both SQLite and PostgreSQL unique constraint violations.
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE constraint failed") ||
		strings.Contains(msg, "23505") ||
		strings.Contains(msg, "unique constraint") ||
		strings.Contains(msg, "duplicate key") ||
		strings.Contains(msg, "duplicate")
}

func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
