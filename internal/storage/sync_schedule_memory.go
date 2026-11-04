package storage

import (
	"context"
	"sort"
	"time"

	"cloudpam/internal/domain"
)

type memorySyncSchedule struct {
	domain.SyncSchedule
	token string
	until int64
}

func (m *MemoryDiscoveryStore) GetSyncSchedule(_ context.Context, id int64) (domain.SyncSchedule, error) {
	m.store.mu.RLock()
	defer m.store.mu.RUnlock()
	if v, ok := m.schedules[id]; ok {
		return v.SyncSchedule, nil
	}
	return domain.SyncSchedule{AccountID: id}, nil
}
func (m *MemoryDiscoveryStore) PutSyncSchedule(_ context.Context, id, interval int64, now time.Time) error {
	m.store.mu.Lock()
	defer m.store.mu.Unlock()
	v := m.schedules[id]
	v.AccountID = id
	v.IntervalSeconds = interval
	v.NextRunAt = 0
	if interval > 0 {
		v.NextRunAt = now.Unix() + interval
	}
	m.schedules[id] = v
	return nil
}
func (m *MemoryDiscoveryStore) DueSyncSchedules(_ context.Context, now time.Time) ([]domain.SyncSchedule, error) {
	m.store.mu.RLock()
	defer m.store.mu.RUnlock()
	out := []domain.SyncSchedule{}
	for _, v := range m.schedules {
		if v.IntervalSeconds > 0 && v.NextRunAt <= now.Unix() && v.until <= now.Unix() {
			out = append(out, v.SyncSchedule)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].NextRunAt < out[j].NextRunAt })
	return out, nil
}
func (m *MemoryDiscoveryStore) ClaimSync(_ context.Context, id int64, token string, now, until time.Time, scheduled bool) (bool, error) {
	m.store.mu.Lock()
	defer m.store.mu.Unlock()
	v := m.schedules[id]
	if v.until > now.Unix() || (scheduled && (v.IntervalSeconds == 0 || v.NextRunAt > now.Unix())) {
		return false, nil
	}
	v.AccountID = id
	v.token = token
	v.until = until.Unix()
	if v.IntervalSeconds > 0 {
		v.NextRunAt = now.Unix() + v.IntervalSeconds
	}
	m.schedules[id] = v
	return true, nil
}
func (m *MemoryDiscoveryStore) ReleaseSync(_ context.Context, id int64, token string) error {
	m.store.mu.Lock()
	defer m.store.mu.Unlock()
	v := m.schedules[id]
	if v.token == token {
		v.token = ""
		v.until = 0
		m.schedules[id] = v
	}
	return nil
}

func (m *MemoryDiscoveryStore) RecordSyncAgent(_ context.Context, id int64, agentID string) error {
	m.store.mu.Lock()
	defer m.store.mu.Unlock()
	v := m.schedules[id]
	v.AccountID = id
	v.LastAgentID = agentID
	m.schedules[id] = v
	return nil
}
