package storagetest

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"cloudpam/internal/domain"
	"cloudpam/internal/storage"
)

// RunSyncScheduleSuite checks durable coordination shared by all backends.
func RunSyncScheduleSuite(t *testing.T, accounts storage.Store, ds storage.DiscoveryStore, ss storage.SyncScheduleStore) {
	t.Helper()
	ctx := context.Background()
	a, err := accounts.CreateAccount(ctx, domain.CreateAccount{Name: "scheduled", Key: "aws:123456789012", Provider: "aws"})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	v, err := ss.GetSyncSchedule(ctx, a.ID)
	if err != nil || v.IntervalSeconds != 0 {
		t.Fatalf("default: %+v %v", v, err)
	}
	if err := ss.RecordSyncAgent(ctx, a.ID, "agent-owner"); err != nil {
		t.Fatal(err)
	}
	if err := ss.PutSyncSchedule(ctx, a.ID, 60, now); err != nil {
		t.Fatal(err)
	}
	due, err := ss.DueSyncSchedules(ctx, now)
	if err != nil || len(due) != 0 {
		t.Fatalf("not due: %v %v", due, err)
	}
	due, err = ss.DueSyncSchedules(ctx, now.Add(time.Minute))
	if err != nil || len(due) != 1 {
		t.Fatalf("due: %v %v", due, err)
	}
	if due[0].LastAgentID != "agent-owner" {
		t.Fatalf("lost agent ownership: %+v", due[0])
	}
	var winners atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, err := ss.ClaimSync(ctx, a.ID, "winner", now.Add(time.Minute), now.Add(15*time.Minute), true)
			if err != nil {
				t.Error(err)
			}
			if ok {
				winners.Add(1)
			}
		}()
	}
	wg.Wait()
	if winners.Load() != 1 {
		t.Fatalf("claim winners=%d", winners.Load())
	}
	if err := ss.PutSyncSchedule(ctx, a.ID, 0, now); err != nil {
		t.Fatal(err)
	}
	if err := ss.ReleaseSync(ctx, a.ID, "wrong-token"); err != nil {
		t.Fatal(err)
	}
	ok, err := ss.ClaimSync(ctx, a.ID, "manual", now, now.Add(time.Minute), false)
	if err != nil || ok {
		t.Fatalf("edit or wrong release lost lease: %v %v", ok, err)
	}
	if err := ss.ReleaseSync(ctx, a.ID, "winner"); err != nil {
		t.Fatal(err)
	}
	ok, err = ss.ClaimSync(ctx, a.ID, "manual", now, now.Add(time.Minute), false)
	if err != nil || !ok {
		t.Fatalf("manual disabled schedule: %v %v", ok, err)
	}
	ok, err = ss.ClaimSync(ctx, a.ID, "replacement", now.Add(2*time.Minute), now.Add(3*time.Minute), false)
	if err != nil || !ok {
		t.Fatalf("expired lease: %v %v", ok, err)
	}
	if err := ss.ReleaseSync(ctx, a.ID, "manual"); err != nil {
		t.Fatal(err)
	}
	ok, err = ss.ClaimSync(ctx, a.ID, "other", now.Add(2*time.Minute), now.Add(3*time.Minute), false)
	if err != nil || ok {
		t.Fatalf("old token unlocked replacement: %v %v", ok, err)
	}

	// Observation-only writes preserve cloud fields and change timestamp.
	r := domain.DiscoveredResource{AccountID: a.ID, ResourceID: "vpc-1", Provider: "aws", ResourceType: domain.ResourceTypeVPC, Status: domain.DiscoveryStatusActive, Name: "stable", DiscoveredAt: now, LastSeenAt: now, UpdatedAt: now}
	if err := ds.UpsertDiscoveredResource(ctx, r); err != nil {
		t.Fatal(err)
	}
	items, _, err := ds.ListDiscoveredResources(ctx, a.ID, domain.DiscoveryFilters{})
	if err != nil || len(items) != 1 {
		t.Fatalf("resources: %v %v", items, err)
	}
	r = items[0]
	if err := ds.TouchDiscoveredResource(ctx, r.ID, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	got, err := ds.GetDiscoveredResource(ctx, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != r.Name || !got.UpdatedAt.Equal(now) || !got.LastSeenAt.Equal(now.Add(time.Minute)) {
		t.Fatalf("touch: %+v", got)
	}
	n, err := ds.MarkStaleResources(ctx, a.ID, now.Add(2*time.Minute))
	if err != nil || n != 1 {
		t.Fatalf("stale: %d %v", n, err)
	}
	got, err = ds.GetDiscoveredResource(ctx, r.ID)
	if err != nil || !got.UpdatedAt.Equal(now.Add(2*time.Minute)) {
		t.Fatalf("stale timestamp: %+v %v", got, err)
	}

	job, err := ds.CreateSyncJob(ctx, domain.SyncJob{AccountID: a.ID, Status: domain.SyncJobStatusRunning, Source: "local", IdentitySource: "stored_credential", Trigger: "scheduled", Attempts: 1})
	if err != nil {
		t.Fatal(err)
	}
	job.Attempts = 3
	job.Status = domain.SyncJobStatusCompleted
	if err := ds.UpdateSyncJob(ctx, job); err != nil {
		t.Fatal(err)
	}
	read, err := ds.GetSyncJob(ctx, job.ID)
	if err != nil || read.Attempts != 3 || read.Trigger != "scheduled" || read.IdentitySource != "stored_credential" {
		t.Fatalf("job: %+v %v", read, err)
	}
	jobs, err := ds.ListSyncJobs(ctx, a.ID, 10)
	if err != nil || len(jobs) != 1 || jobs[0].Attempts != 3 {
		t.Fatalf("list jobs: %+v %v", jobs, err)
	}
}
