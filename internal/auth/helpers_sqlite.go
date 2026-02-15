//go:build sqlite && !postgres

package auth

const defaultOrgID = "00000000-0000-0000-0000-000000000001"

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// isUniqueViolation checks if the error is a SQLite UNIQUE constraint violation.
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return contains(msg, "UNIQUE constraint failed") || contains(msg, "duplicate")
}

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
