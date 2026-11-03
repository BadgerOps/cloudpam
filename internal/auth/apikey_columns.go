//go:build sqlite || postgres

package auth

// rateLimitColumns splits an optional limit into the two nullable columns a
// SQL store keeps it in.
func rateLimitColumns(l *RateLimit) (rps *float64, burst *int) {
	if l == nil {
		return nil, nil
	}
	return &l.RequestsPerSecond, &l.Burst
}

// rateLimitFromColumns is the inverse of rateLimitColumns. A row with only
// one column set has no limit.
func rateLimitFromColumns(rps *float64, burst *int) *RateLimit {
	if rps == nil || burst == nil {
		return nil
	}
	return &RateLimit{RequestsPerSecond: *rps, Burst: *burst}
}
