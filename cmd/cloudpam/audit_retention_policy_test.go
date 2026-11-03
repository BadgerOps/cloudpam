package main

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"cloudpam/internal/audit"
	"cloudpam/internal/storage"
)

func TestAuditRetentionIsOffUntilAnOperatorSetsIt(t *testing.T) {
	ctx := context.Background()
	settings := storage.NewMemorySettingsStore()
	auditLogger := audit.NewMemoryAuditLogger()
	old := &audit.AuditEvent{Timestamp: time.Now().Add(-400 * 24 * time.Hour), Action: audit.ActionCreate, ResourceType: audit.ResourcePool, ResourceID: "1"}
	if err := auditLogger.Log(ctx, old); err != nil {
		t.Fatalf("Log: %v", err)
	}
	remaining := func() int {
		t.Helper()
		_, total, err := auditLogger.List(ctx, audit.ListOptions{Limit: 1})
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		return total
	}

	r := audit.NewRetention(auditLogger, auditRetentionPolicy(settings), slog.New(slog.NewTextHandler(io.Discard, nil)))
	for range 3 {
		if deleted, err := r.RunOnce(ctx); err != nil || deleted != 0 {
			t.Fatalf("RunOnce with unsaved settings = (%d, %v), want (0, nil)", deleted, err)
		}
	}
	if remaining() != 1 {
		t.Fatal("history was purged although no operator ever set audit_retention_days")
	}

	current, err := settings.GetSecuritySettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	current.AuditRetentionDays = 30
	if err := settings.UpdateSecuritySettings(ctx, current); err != nil {
		t.Fatal(err)
	}
	if deleted, _ := r.RunOnce(ctx); deleted != 0 {
		t.Fatalf("first pass after opting in deleted %d, want a dry run", deleted)
	}
	if deleted, _ := r.RunOnce(ctx); deleted != 1 {
		t.Fatalf("second pass after opting in deleted %d, want 1", deleted)
	}
	if remaining() != 0 {
		t.Fatal("the purge left the expired event in place")
	}
}
