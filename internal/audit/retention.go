package audit

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// RetentionInterval is how often the retention job applies the policy.
const RetentionInterval = time.Hour

// RetentionPolicySource returns the current retention age. It is read on
// every pass so a settings change applies without a restart. A zero or
// negative age keeps events forever.
type RetentionPolicySource func(ctx context.Context) (time.Duration, error)

// Retention purges audit events older than the policy allows.
//
// The first pass that finds an active policy is a dry run: it logs how many
// events a purge would remove and deletes nothing, so an operator sees the
// number before any history goes. Every later pass purges.
type Retention struct {
	logger AuditLogger
	policy RetentionPolicySource
	log    *slog.Logger
	now    func() time.Time

	purgeArmed bool
}

// NewRetention builds a retention job over logger that reads its policy from policy.
func NewRetention(logger AuditLogger, policy RetentionPolicySource, log *slog.Logger) *Retention {
	if log == nil {
		log = slog.Default()
	}
	return &Retention{logger: logger, policy: policy, log: log, now: time.Now}
}

// RunOnce applies the policy one time and returns how many events it deleted.
// A disabled policy and the dry run both return 0.
func (r *Retention) RunOnce(ctx context.Context) (int64, error) {
	maxAge, err := r.policy(ctx)
	if err != nil {
		return 0, fmt.Errorf("load audit retention policy: %w", err)
	}
	if maxAge <= 0 {
		return 0, nil
	}
	cutoff := r.now().UTC().Add(-maxAge)

	if !r.purgeArmed {
		_, total, err := r.logger.List(ctx, ListOptions{Until: &cutoff, Limit: 1})
		if err != nil {
			return 0, fmt.Errorf("count audit events older than %s: %w", cutoff.Format(time.RFC3339), err)
		}
		r.log.Info("audit retention dry run: the next pass purges",
			"max_age", maxAge.String(),
			"cutoff", cutoff.Format(time.RFC3339),
			"would_delete", total,
			"next_pass_in", RetentionInterval.String())
		r.purgeArmed = true
		return 0, nil
	}

	deleted, err := r.logger.DeleteOlderThan(ctx, cutoff)
	if err != nil {
		return 0, fmt.Errorf("purge audit events older than %s: %w", cutoff.Format(time.RFC3339), err)
	}
	if deleted > 0 {
		r.log.Info("audit retention purge", "deleted", deleted, "cutoff", cutoff.Format(time.RFC3339))
	}
	return deleted, nil
}

// Run applies the policy now and then every interval until ctx ends. Errors
// are logged and the loop continues, so one failed pass never stops retention.
func (r *Retention) Run(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if _, err := r.RunOnce(ctx); err != nil && ctx.Err() == nil {
			r.log.Warn("audit retention error", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
