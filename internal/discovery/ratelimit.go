package discovery

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"

	"golang.org/x/time/rate"
)

// MaxRPS bounds a provider rate limit so the burst fits an int.
const MaxRPS = 1_000_000

// Limiter paces the provider API calls a collector makes. A nil Limiter never
// waits, which is how an unconfigured provider runs.
type Limiter struct {
	bucket *rate.Limiter
}

// NewLimiter allows rps provider calls per second, with a burst of one second's
// worth of calls. rps of zero or less returns nil.
func NewLimiter(rps float64) *Limiter {
	if rps <= 0 {
		return nil
	}
	rps = math.Min(rps, MaxRPS)
	return &Limiter{bucket: rate.NewLimiter(rate.Limit(rps), int(math.Ceil(rps)))}
}

// Wait blocks until the next provider call may be made. It fails when ctx ends
// first, or when the wait would outlast the ctx deadline.
func (l *Limiter) Wait(ctx context.Context) error {
	if l == nil {
		return nil
	}
	return l.bucket.Wait(ctx)
}

// ParseRPS reads a requests-per-second setting. Empty means unlimited.
func ParseRPS(s string) (float64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}
	rps, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, fmt.Errorf("requests per second %q is not a number", s)
	}
	return rps, CheckRPS(rps)
}

// CheckRPS rejects a requests-per-second setting outside 0 to MaxRPS.
func CheckRPS(rps float64) error {
	if math.IsNaN(rps) || rps < 0 || rps > MaxRPS {
		return fmt.Errorf("requests per second must be between 0 and %d, got %v", MaxRPS, rps)
	}
	return nil
}
