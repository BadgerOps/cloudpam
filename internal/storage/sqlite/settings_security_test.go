//go:build sqlite

package sqlite

import (
	"context"
	"testing"
)

func TestSecuritySettingsWithoutARowKeepAuditHistory(t *testing.T) {
	s := newTestStore(t)

	settings, err := s.GetSecuritySettings(context.Background())
	if err != nil {
		t.Fatalf("GetSecuritySettings: %v", err)
	}
	if settings.AuditRetentionDays != 0 {
		t.Fatalf("audit_retention_days = %d with no saved settings row, want 0 so an upgraded database keeps its history", settings.AuditRetentionDays)
	}
}
