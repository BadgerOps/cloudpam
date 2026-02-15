//go:build !sqlite && !postgres

package auth

// These helpers are used by sqlite/postgres build-tagged files.
// In the default (no-tag) build they are unused but must exist
// to satisfy interface contracts when both tags are absent.

//nolint:unused
const defaultOrgID = "00000000-0000-0000-0000-000000000001"

//nolint:unused
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

//nolint:unused
func isUniqueViolation(_ error) bool {
	return false
}

//nolint:unused
func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
