package storage

import (
	"context"
	"sort"
	"sync"
	"time"

	"cloudpam/internal/domain"
)

// MemoryUtilizationStore is an in-memory implementation of UtilizationStore.
type MemoryUtilizationStore struct {
	mu        sync.RWMutex
	snapshots []domain.UtilizationSnapshot
	nextID    int64
}

func NewMemoryUtilizationStore() *MemoryUtilizationStore {
	return &MemoryUtilizationStore{nextID: 1}
}

var _ UtilizationStore = (*MemoryUtilizationStore)(nil)

func (s *MemoryUtilizationStore) RecordSnapshot(_ context.Context, snap domain.UtilizationSnapshot) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	snap.ID = s.nextID
	s.nextID++
	s.snapshots = append(s.snapshots, snap)
	return nil
}

func (s *MemoryUtilizationStore) ListSnapshots(_ context.Context, poolID int64, from, to time.Time) ([]domain.UtilizationSnapshot, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []domain.UtilizationSnapshot
	for _, snap := range s.snapshots {
		if snap.PoolID == poolID &&
			!snap.CapturedAt.Before(from) &&
			!snap.CapturedAt.After(to) {
			out = append(out, snap)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CapturedAt.Before(out[j].CapturedAt)
	})
	return out, nil
}

func (s *MemoryUtilizationStore) LatestSnapshot(_ context.Context, poolID int64) (*domain.UtilizationSnapshot, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var latest *domain.UtilizationSnapshot
	for i := range s.snapshots {
		snap := &s.snapshots[i]
		if snap.PoolID == poolID {
			if latest == nil || snap.CapturedAt.After(latest.CapturedAt) {
				latest = snap
			}
		}
	}
	return latest, nil
}
