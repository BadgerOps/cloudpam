package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"
)

func TestDiscoveryLimiterPacesAConfiguredProvider(t *testing.T) {
	t.Setenv("CLOUDPAM_DISCOVERY_AWS_RPS", "1")
	var logs bytes.Buffer
	l := discoveryLimiter(bufferedLogger(&logs), "CLOUDPAM_DISCOVERY_AWS_RPS")
	if l == nil {
		t.Fatal("discoveryLimiter() = nil, want a limiter for 1 rps")
	}
	if err := l.Wait(context.Background()); err != nil {
		t.Fatalf("first Wait() = %v, want nil", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := l.Wait(ctx); err == nil {
		t.Fatal("second Wait() within one second = nil, want the call held")
	}
	if !strings.Contains(logs.String(), "discovery rate limit configured") {
		t.Fatalf("logs = %s, want the configured limit reported", logs.String())
	}
}

func TestDiscoveryLimiterLeavesAnUnsetOrInvalidProviderUnlimited(t *testing.T) {
	for _, raw := range []string{"", "fast", "-1"} {
		t.Run(raw, func(t *testing.T) {
			t.Setenv("CLOUDPAM_DISCOVERY_GCP_RPS", raw)
			var logs bytes.Buffer
			if l := discoveryLimiter(bufferedLogger(&logs), "CLOUDPAM_DISCOVERY_GCP_RPS"); l != nil {
				t.Fatalf("discoveryLimiter(%q) = %v, want nil (unlimited)", raw, l)
			}
			warned := strings.Contains(logs.String(), "invalid discovery rate limit")
			if warned != (raw != "") {
				t.Fatalf("logs = %s, want a warning only for an invalid value", logs.String())
			}
		})
	}
}
