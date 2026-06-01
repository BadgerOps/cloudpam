package storage

import (
	"context"
	"testing"
	"time"

	"cloudpam/internal/domain"
)

func TestMemoryUtilizationStoreLatestSnapshotReturnsCopy(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryUtilizationStore()
	capturedAt := time.Now().UTC()

	if err := store.RecordSnapshot(ctx, domain.UtilizationSnapshot{
		PoolID:      7,
		TotalIPs:    256,
		UsedIPs:     64,
		Utilization: 25,
		CapturedAt:  capturedAt,
	}); err != nil {
		t.Fatalf("RecordSnapshot() error = %v", err)
	}

	latest, err := store.LatestSnapshot(ctx, 7)
	if err != nil {
		t.Fatalf("LatestSnapshot() error = %v", err)
	}
	if latest == nil {
		t.Fatal("LatestSnapshot() = nil")
	}

	latest.UsedIPs = 128
	latest.Utilization = 50

	stored, err := store.LatestSnapshot(ctx, 7)
	if err != nil {
		t.Fatalf("LatestSnapshot() second call error = %v", err)
	}
	if stored.UsedIPs != 64 || stored.Utilization != 25 {
		t.Fatalf("stored snapshot was mutated through returned pointer: UsedIPs=%d Utilization=%f", stored.UsedIPs, stored.Utilization)
	}
}
