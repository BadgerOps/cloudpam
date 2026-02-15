//go:build postgres && !sqlite

package auth

const defaultOrgID = "00000000-0000-0000-0000-000000000001"

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return contains(s, "23505") || contains(s, "unique constraint") || contains(s, "duplicate key")
}

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
