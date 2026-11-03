package audit

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func newRetentionFixture(t *testing.T, maxAge time.Duration) (*Retention, *MemoryAuditLogger, *bytes.Buffer) {
	t.Helper()
	logger := NewMemoryAuditLogger()
	now := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	ctx := context.Background()
	for _, age := range []time.Duration{100 * 24 * time.Hour, 91 * 24 * time.Hour, 89 * 24 * time.Hour, time.Hour} {
		if err := logger.Log(ctx, &AuditEvent{Timestamp: now.Add(-age), Action: ActionCreate, ResourceType: ResourcePool, ResourceID: "1"}); err != nil {
			t.Fatalf("Log: %v", err)
		}
	}

	var out bytes.Buffer
	policy := func(context.Context) (time.Duration, error) { return maxAge, nil }
	r := NewRetention(logger, policy, slog.New(slog.NewTextHandler(&out, nil)))
	r.now = func() time.Time { return now }
	return r, logger, &out
}

func remaining(t *testing.T, logger *MemoryAuditLogger) int {
	t.Helper()
	_, total, err := logger.List(context.Background(), ListOptions{Limit: 10})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	return total
}

func TestRetentionDryRunsBeforeTheFirstPurge(t *testing.T) {
	r, logger, out := newRetentionFixture(t, 90*24*time.Hour)
	ctx := context.Background()

	deleted, err := r.RunOnce(ctx)
	if err != nil {
		t.Fatalf("first pass: %v", err)
	}
	if deleted != 0 {
		t.Fatalf("dry run deleted %d events", deleted)
	}
	if got := remaining(t, logger); got != 4 {
		t.Fatalf("dry run left %d events, want 4", got)
	}
	line := out.String()
	if !strings.Contains(line, "audit retention dry run") || !strings.Contains(line, "would_delete=2") {
		t.Fatalf("dry run log line = %q, want the message and would_delete=2", line)
	}

	deleted, err = r.RunOnce(ctx)
	if err != nil {
		t.Fatalf("second pass: %v", err)
	}
	if deleted != 2 {
		t.Fatalf("purge deleted %d events, want 2", deleted)
	}
	if got := remaining(t, logger); got != 2 {
		t.Fatalf("purge left %d events, want 2", got)
	}
	if !strings.Contains(out.String(), "audit retention purge") {
		t.Fatalf("purge was not logged: %q", out.String())
	}
}

func TestRetentionDisabledPolicyDeletesNothing(t *testing.T) {
	r, logger, out := newRetentionFixture(t, 0)

	for range 3 {
		if deleted, err := r.RunOnce(context.Background()); err != nil || deleted != 0 {
			t.Fatalf("RunOnce = (%d, %v), want (0, nil)", deleted, err)
		}
	}
	if got := remaining(t, logger); got != 4 {
		t.Fatalf("disabled policy left %d events, want 4", got)
	}
	if out.Len() != 0 {
		t.Fatalf("disabled policy logged %q", out.String())
	}
	if r.purgeArmed {
		t.Fatal("a disabled policy must not arm the purge; enabling it later still gets a dry run")
	}
}

func TestRetentionReadsThePolicyOnEveryPass(t *testing.T) {
	r, logger, _ := newRetentionFixture(t, 0)
	maxAge := time.Duration(0)
	r.policy = func(context.Context) (time.Duration, error) { return maxAge, nil }
	ctx := context.Background()

	if _, err := r.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	maxAge = 90 * 24 * time.Hour
	if deleted, _ := r.RunOnce(ctx); deleted != 0 {
		t.Fatalf("the first enabled pass must be a dry run, deleted %d", deleted)
	}
	if deleted, _ := r.RunOnce(ctx); deleted != 2 {
		t.Fatalf("second enabled pass deleted %d, want 2", deleted)
	}
	if got := remaining(t, logger); got != 2 {
		t.Fatalf("left %d events, want 2", got)
	}
}

func TestRetentionPolicyErrorIsReturned(t *testing.T) {
	r, logger, _ := newRetentionFixture(t, 0)
	r.policy = func(context.Context) (time.Duration, error) { return 0, errors.New("settings unavailable") }

	if _, err := r.RunOnce(context.Background()); err == nil || !strings.Contains(err.Error(), "settings unavailable") {
		t.Fatalf("err = %v, want the policy error", err)
	}
	if got := remaining(t, logger); got != 4 {
		t.Fatalf("a policy error must not delete, left %d", got)
	}
}

func TestRetentionRunStopsWithContext(t *testing.T) {
	r, _, _ := newRetentionFixture(t, 0)
	polled := make(chan struct{}, 1)
	r.policy = func(context.Context) (time.Duration, error) {
		select {
		case polled <- struct{}{}:
		default:
		}
		return 0, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		r.Run(ctx, time.Hour)
		close(done)
	}()

	select {
	case <-polled:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not apply the policy on start")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not stop after cancel")
	}
}
