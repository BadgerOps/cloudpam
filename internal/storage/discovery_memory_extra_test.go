package storage

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"cloudpam/internal/domain"
)

// tPtr returns a pointer to v. Named distinctly to avoid colliding with the
// helpers already defined in store_test.go.
func tPtr[T any](v T) *T { return &v }

func newDiscoveryStoreForExtraTests() *MemoryDiscoveryStore {
	return NewMemoryDiscoveryStore(NewMemoryStore())
}

var extraDiscoveryBase = time.Date(2025, 3, 1, 12, 0, 0, 0, time.UTC)

func mustUpsertDiscoveredExtra(t *testing.T, s *MemoryDiscoveryStore, res domain.DiscoveredResource) domain.DiscoveredResource {
	t.Helper()
	if err := s.UpsertDiscoveredResource(context.Background(), res); err != nil {
		t.Fatalf("UpsertDiscoveredResource(%s): %v", res.ResourceID, err)
	}
	all, _, err := s.ListDiscoveredResources(context.Background(), res.AccountID, domain.DiscoveryFilters{PageSize: 1000})
	if err != nil {
		t.Fatalf("ListDiscoveredResources: %v", err)
	}
	for _, r := range all {
		if r.ResourceID == res.ResourceID {
			return r
		}
	}
	t.Fatalf("resource %q not found after upsert", res.ResourceID)
	return domain.DiscoveredResource{}
}

func TestMemoryDiscoveryStoreExtra_ResourceLifecycle(t *testing.T) {
	ctx := context.Background()
	s := newDiscoveryStoreForExtraTests()

	stored := mustUpsertDiscoveredExtra(t, s, domain.DiscoveredResource{
		AccountID:    1,
		Provider:     "aws",
		Region:       "us-east-1",
		ResourceType: domain.ResourceTypeVPC,
		ResourceID:   "vpc-1",
		Name:         "prod-vpc",
		CIDR:         "10.0.0.0/16",
		Status:       domain.DiscoveryStatusActive,
		DiscoveredAt: extraDiscoveryBase,
		LastSeenAt:   extraDiscoveryBase,
	})
	if stored.ID == uuid.Nil {
		t.Fatal("insert did not assign an ID")
	}

	got, err := s.GetDiscoveredResource(ctx, stored.ID)
	if err != nil {
		t.Fatalf("GetDiscoveredResource: %v", err)
	}
	if got.Name != "prod-vpc" || got.CIDR != "10.0.0.0/16" {
		t.Fatalf("GetDiscoveredResource = %+v, want prod-vpc/10.0.0.0/16", got)
	}
	if got.PoolID != nil {
		t.Fatalf("PoolID = %v, want nil", *got.PoolID)
	}

	if err := s.LinkResourceToPool(ctx, stored.ID, 42); err != nil {
		t.Fatalf("LinkResourceToPool: %v", err)
	}
	got, err = s.GetDiscoveredResource(ctx, stored.ID)
	if err != nil {
		t.Fatalf("GetDiscoveredResource after link: %v", err)
	}
	if got.PoolID == nil || *got.PoolID != 42 {
		t.Fatalf("PoolID after link = %v, want 42", got.PoolID)
	}

	if err := s.UnlinkResource(ctx, stored.ID); err != nil {
		t.Fatalf("UnlinkResource: %v", err)
	}
	got, err = s.GetDiscoveredResource(ctx, stored.ID)
	if err != nil {
		t.Fatalf("GetDiscoveredResource after unlink: %v", err)
	}
	if got.PoolID != nil {
		t.Fatalf("PoolID after unlink = %v, want nil", *got.PoolID)
	}

	if err := s.DeleteDiscoveredResource(ctx, stored.ID); err != nil {
		t.Fatalf("DeleteDiscoveredResource: %v", err)
	}
	if _, err := s.GetDiscoveredResource(ctx, stored.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetDiscoveredResource after delete error = %v, want ErrNotFound", err)
	}
	if err := s.DeleteDiscoveredResource(ctx, stored.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second DeleteDiscoveredResource error = %v, want ErrNotFound", err)
	}
}

func TestMemoryDiscoveryStoreExtra_NotFoundPaths(t *testing.T) {
	ctx := context.Background()
	s := newDiscoveryStoreForExtraTests()
	missing := uuid.New()

	if err := s.LinkResourceToPool(ctx, missing, 1); !errors.Is(err, ErrNotFound) {
		t.Errorf("LinkResourceToPool error = %v, want ErrNotFound", err)
	}
	if err := s.UnlinkResource(ctx, missing); !errors.Is(err, ErrNotFound) {
		t.Errorf("UnlinkResource error = %v, want ErrNotFound", err)
	}
	if err := s.DeleteDiscoveredResource(ctx, missing); !errors.Is(err, ErrNotFound) {
		t.Errorf("DeleteDiscoveredResource error = %v, want ErrNotFound", err)
	}
	if _, err := s.GetSyncJob(ctx, missing); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetSyncJob error = %v, want ErrNotFound", err)
	}
	if err := s.UpdateSyncJob(ctx, domain.SyncJob{ID: missing}); !errors.Is(err, ErrNotFound) {
		t.Errorf("UpdateSyncJob error = %v, want ErrNotFound", err)
	}
	if _, err := s.GetAgent(ctx, missing); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetAgent error = %v, want ErrNotFound", err)
	}
	if err := s.DeleteAgent(ctx, missing); !errors.Is(err, ErrNotFound) {
		t.Errorf("DeleteAgent error = %v, want ErrNotFound", err)
	}
	if _, err := s.ClaimPendingAgentSync(ctx, missing); !errors.Is(err, ErrNotFound) {
		t.Errorf("ClaimPendingAgentSync error = %v, want ErrNotFound", err)
	}
}

func TestMemoryDiscoveryStoreExtra_UpsertPreservesIdentityPoolAndDiscoveredAt(t *testing.T) {
	s := newDiscoveryStoreForExtraTests()

	original := mustUpsertDiscoveredExtra(t, s, domain.DiscoveredResource{
		AccountID:    7,
		ResourceID:   "vpc-abc",
		Name:         "old-name",
		CIDR:         "10.1.0.0/16",
		Status:       domain.DiscoveryStatusActive,
		PoolID:       tPtr(int64(9)),
		DiscoveredAt: extraDiscoveryBase,
		LastSeenAt:   extraDiscoveryBase,
	})

	// Update carrying a fresh (zero) ID, no pool link and no discovered_at.
	updated := mustUpsertDiscoveredExtra(t, s, domain.DiscoveredResource{
		AccountID:  7,
		ResourceID: "vpc-abc",
		Name:       "new-name",
		CIDR:       "10.1.0.0/16",
		Status:     domain.DiscoveryStatusStale,
		LastSeenAt: extraDiscoveryBase.Add(time.Hour),
	})

	if updated.ID != original.ID {
		t.Errorf("ID = %s, want preserved %s", updated.ID, original.ID)
	}
	if updated.PoolID == nil || *updated.PoolID != 9 {
		t.Errorf("PoolID = %v, want preserved 9", updated.PoolID)
	}
	if !updated.DiscoveredAt.Equal(extraDiscoveryBase) {
		t.Errorf("DiscoveredAt = %v, want preserved %v", updated.DiscoveredAt, extraDiscoveryBase)
	}
	if updated.Name != "new-name" || updated.Status != domain.DiscoveryStatusStale {
		t.Errorf("mutable fields not updated: %+v", updated)
	}
	if !updated.LastSeenAt.Equal(extraDiscoveryBase.Add(time.Hour)) {
		t.Errorf("LastSeenAt = %v, want updated", updated.LastSeenAt)
	}

	// Explicit values on the incoming record win over the preserved ones.
	later := extraDiscoveryBase.Add(48 * time.Hour)
	overridden := mustUpsertDiscoveredExtra(t, s, domain.DiscoveredResource{
		AccountID:    7,
		ResourceID:   "vpc-abc",
		Name:         "new-name",
		PoolID:       tPtr(int64(11)),
		DiscoveredAt: later,
	})
	if overridden.PoolID == nil || *overridden.PoolID != 11 {
		t.Errorf("PoolID = %v, want overridden 11", overridden.PoolID)
	}
	if !overridden.DiscoveredAt.Equal(later) {
		t.Errorf("DiscoveredAt = %v, want overridden %v", overridden.DiscoveredAt, later)
	}

	all, total, err := s.ListDiscoveredResources(context.Background(), 7, domain.DiscoveryFilters{})
	if err != nil {
		t.Fatalf("ListDiscoveredResources: %v", err)
	}
	if total != 1 || len(all) != 1 {
		t.Fatalf("upsert created duplicates: total=%d len=%d", total, len(all))
	}
}

func TestMemoryDiscoveryStoreExtra_UpsertKeyIncludesAccount(t *testing.T) {
	ctx := context.Background()
	s := newDiscoveryStoreForExtraTests()

	mustUpsertDiscoveredExtra(t, s, domain.DiscoveredResource{AccountID: 1, ResourceID: "shared-id", Name: "a"})
	mustUpsertDiscoveredExtra(t, s, domain.DiscoveredResource{AccountID: 2, ResourceID: "shared-id", Name: "b"})

	for accountID, wantName := range map[int64]string{1: "a", 2: "b"} {
		items, total, err := s.ListDiscoveredResources(ctx, accountID, domain.DiscoveryFilters{})
		if err != nil {
			t.Fatalf("ListDiscoveredResources(%d): %v", accountID, err)
		}
		if total != 1 || len(items) != 1 {
			t.Fatalf("account %d: total=%d len=%d, want 1", accountID, total, len(items))
		}
		if items[0].Name != wantName {
			t.Errorf("account %d name = %q, want %q", accountID, items[0].Name, wantName)
		}
	}
}

func TestMemoryDiscoveryStoreExtra_ListFilters(t *testing.T) {
	ctx := context.Background()
	s := newDiscoveryStoreForExtraTests()

	seed := []domain.DiscoveredResource{
		{AccountID: 1, Provider: "aws", Region: "us-east-1", ResourceType: domain.ResourceTypeVPC, ResourceID: "vpc-1", Name: "Prod VPC", CIDR: "10.0.0.0/16", Status: domain.DiscoveryStatusActive, PoolID: tPtr(int64(5))},
		{AccountID: 1, Provider: "aws", Region: "us-west-2", ResourceType: domain.ResourceTypeSubnet, ResourceID: "subnet-1", Name: "Staging Subnet", CIDR: "10.1.0.0/24", Status: domain.DiscoveryStatusStale},
		{AccountID: 1, Provider: "gcp", Region: "us-east-1", ResourceType: domain.ResourceTypeElasticIP, ResourceID: "eip-1", Name: "Edge IP", CIDR: "203.0.113.7/32", Status: domain.DiscoveryStatusActive},
		{AccountID: 2, Provider: "aws", Region: "us-east-1", ResourceType: domain.ResourceTypeVPC, ResourceID: "vpc-other", Name: "Other Account", CIDR: "172.16.0.0/16", Status: domain.DiscoveryStatusActive},
	}
	for i, r := range seed {
		r.DiscoveredAt = extraDiscoveryBase.Add(time.Duration(i) * time.Minute)
		r.LastSeenAt = r.DiscoveredAt
		mustUpsertDiscoveredExtra(t, s, r)
	}

	tests := []struct {
		name    string
		filters domain.DiscoveryFilters
		want    []string
	}{
		{"no filters returns account rows only", domain.DiscoveryFilters{}, []string{"vpc-1", "subnet-1", "eip-1"}},
		{"provider", domain.DiscoveryFilters{Provider: "gcp"}, []string{"eip-1"}},
		{"region", domain.DiscoveryFilters{Region: "us-west-2"}, []string{"subnet-1"}},
		{"resource type", domain.DiscoveryFilters{ResourceType: "vpc"}, []string{"vpc-1"}},
		{"status", domain.DiscoveryFilters{Status: "stale"}, []string{"subnet-1"}},
		{"query matches name case-insensitively", domain.DiscoveryFilters{Query: "prod"}, []string{"vpc-1"}},
		{"query matches resource id", domain.DiscoveryFilters{Query: "SUBNET-1"}, []string{"subnet-1"}},
		{"query matches cidr", domain.DiscoveryFilters{Query: "203.0.113"}, []string{"eip-1"}},
		{"query matches nothing", domain.DiscoveryFilters{Query: "nonexistent"}, nil},
		{"has pool true", domain.DiscoveryFilters{HasPool: tPtr(true)}, []string{"vpc-1"}},
		{"has pool false", domain.DiscoveryFilters{HasPool: tPtr(false)}, []string{"subnet-1", "eip-1"}},
		{"combined provider and status", domain.DiscoveryFilters{Provider: "aws", Status: "active"}, []string{"vpc-1"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			items, total, err := s.ListDiscoveredResources(ctx, 1, tt.filters)
			if err != nil {
				t.Fatalf("ListDiscoveredResources: %v", err)
			}
			if total != len(tt.want) {
				t.Errorf("total = %d, want %d", total, len(tt.want))
			}
			got := make(map[string]bool, len(items))
			for _, item := range items {
				got[item.ResourceID] = true
			}
			if len(got) != len(tt.want) {
				t.Fatalf("got %d items %v, want %v", len(got), got, tt.want)
			}
			for _, want := range tt.want {
				if !got[want] {
					t.Errorf("missing %q in %v", want, got)
				}
			}
		})
	}
}

func TestMemoryDiscoveryStoreExtra_ListSortsNewestFirstAndPaginates(t *testing.T) {
	ctx := context.Background()
	s := newDiscoveryStoreForExtraTests()

	// Insert out of chronological order to prove sorting is applied.
	for _, offset := range []int{2, 0, 4, 1, 3} {
		mustUpsertDiscoveredExtra(t, s, domain.DiscoveredResource{
			AccountID:    1,
			ResourceID:   string(rune('a' + offset)),
			DiscoveredAt: extraDiscoveryBase.Add(time.Duration(offset) * time.Hour),
		})
	}

	all, total, err := s.ListDiscoveredResources(ctx, 1, domain.DiscoveryFilters{})
	if err != nil {
		t.Fatalf("ListDiscoveredResources: %v", err)
	}
	if total != 5 || len(all) != 5 {
		t.Fatalf("total=%d len=%d, want 5 (default page size is 50)", total, len(all))
	}
	for i := 1; i < len(all); i++ {
		if all[i-1].DiscoveredAt.Before(all[i].DiscoveredAt) {
			t.Fatalf("results not sorted newest-first: %v then %v", all[i-1].DiscoveredAt, all[i].DiscoveredAt)
		}
	}

	tests := []struct {
		name      string
		page      int
		pageSize  int
		wantLen   int
		wantFirst string
	}{
		{"page zero is treated as first page", 0, 2, 2, "e"},
		{"explicit first page", 1, 2, 2, "e"},
		{"second page", 2, 2, 2, "c"},
		{"partial last page", 3, 2, 1, "a"},
		{"page past the end", 99, 2, 0, ""},
		{"negative page size falls back to default", 1, -5, 5, "e"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			items, gotTotal, err := s.ListDiscoveredResources(ctx, 1, domain.DiscoveryFilters{Page: tt.page, PageSize: tt.pageSize})
			if err != nil {
				t.Fatalf("ListDiscoveredResources: %v", err)
			}
			if gotTotal != 5 {
				t.Errorf("total = %d, want 5 (total must ignore pagination)", gotTotal)
			}
			if len(items) != tt.wantLen {
				t.Fatalf("len = %d, want %d", len(items), tt.wantLen)
			}
			if tt.wantLen > 0 && items[0].ResourceID != tt.wantFirst {
				t.Errorf("first item = %q, want %q", items[0].ResourceID, tt.wantFirst)
			}
		})
	}
}

func TestMemoryDiscoveryStoreExtra_MarkStaleResources(t *testing.T) {
	ctx := context.Background()
	s := newDiscoveryStoreForExtraTests()
	cutoff := extraDiscoveryBase

	seed := []domain.DiscoveredResource{
		{AccountID: 1, ResourceID: "old-active", Status: domain.DiscoveryStatusActive, LastSeenAt: cutoff.Add(-time.Hour)},
		{AccountID: 1, ResourceID: "fresh-active", Status: domain.DiscoveryStatusActive, LastSeenAt: cutoff.Add(time.Hour)},
		{AccountID: 1, ResourceID: "old-deleted", Status: domain.DiscoveryStatusDeleted, LastSeenAt: cutoff.Add(-time.Hour)},
		{AccountID: 2, ResourceID: "other-account", Status: domain.DiscoveryStatusActive, LastSeenAt: cutoff.Add(-time.Hour)},
	}
	for _, r := range seed {
		mustUpsertDiscoveredExtra(t, s, r)
	}

	count, err := s.MarkStaleResources(ctx, 1, cutoff)
	if err != nil {
		t.Fatalf("MarkStaleResources: %v", err)
	}
	if count != 1 {
		t.Fatalf("count = %d, want 1", count)
	}

	wantStatus := map[string]domain.DiscoveryStatus{
		"old-active":   domain.DiscoveryStatusStale,
		"fresh-active": domain.DiscoveryStatusActive,
		"old-deleted":  domain.DiscoveryStatusDeleted,
	}
	items, _, err := s.ListDiscoveredResources(ctx, 1, domain.DiscoveryFilters{})
	if err != nil {
		t.Fatalf("ListDiscoveredResources: %v", err)
	}
	for _, item := range items {
		if want, ok := wantStatus[item.ResourceID]; ok && item.Status != want {
			t.Errorf("%s status = %q, want %q", item.ResourceID, item.Status, want)
		}
	}

	other, _, err := s.ListDiscoveredResources(ctx, 2, domain.DiscoveryFilters{})
	if err != nil {
		t.Fatalf("ListDiscoveredResources(2): %v", err)
	}
	if len(other) != 1 || other[0].Status != domain.DiscoveryStatusActive {
		t.Errorf("other account resource was marked stale: %+v", other)
	}

	// A second run has nothing left to mark.
	if count, err = s.MarkStaleResources(ctx, 1, cutoff); err != nil || count != 0 {
		t.Errorf("second MarkStaleResources = (%d, %v), want (0, nil)", count, err)
	}
}

func TestMemoryDiscoveryStoreExtra_SyncJobLifecycle(t *testing.T) {
	ctx := context.Background()
	s := newDiscoveryStoreForExtraTests()

	created, err := s.CreateSyncJob(ctx, domain.SyncJob{AccountID: 1, Status: domain.SyncJobStatusPending, Source: "local"})
	if err != nil {
		t.Fatalf("CreateSyncJob: %v", err)
	}
	if created.ID == uuid.Nil {
		t.Error("CreateSyncJob did not assign an ID")
	}
	if created.CreatedAt.IsZero() {
		t.Error("CreateSyncJob did not assign CreatedAt")
	}

	// Explicit ID and CreatedAt are preserved.
	explicitID := uuid.New()
	explicit, err := s.CreateSyncJob(ctx, domain.SyncJob{ID: explicitID, AccountID: 1, CreatedAt: extraDiscoveryBase, Source: "local"})
	if err != nil {
		t.Fatalf("CreateSyncJob(explicit): %v", err)
	}
	if explicit.ID != explicitID || !explicit.CreatedAt.Equal(extraDiscoveryBase) {
		t.Errorf("explicit job = %+v, want ID %s and CreatedAt %v preserved", explicit, explicitID, extraDiscoveryBase)
	}

	got, err := s.GetSyncJob(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetSyncJob: %v", err)
	}
	if got.Status != domain.SyncJobStatusPending {
		t.Errorf("Status = %q, want pending", got.Status)
	}

	created.Status = domain.SyncJobStatusCompleted
	created.ResourcesFound = 12
	created.ErrorMessage = ""
	if err := s.UpdateSyncJob(ctx, created); err != nil {
		t.Fatalf("UpdateSyncJob: %v", err)
	}
	got, err = s.GetSyncJob(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetSyncJob after update: %v", err)
	}
	if got.Status != domain.SyncJobStatusCompleted || got.ResourcesFound != 12 {
		t.Errorf("job after update = %+v, want completed with 12 resources", got)
	}
}

func TestMemoryDiscoveryStoreExtra_ListSyncJobsOrderingAndLimit(t *testing.T) {
	ctx := context.Background()
	s := newDiscoveryStoreForExtraTests()

	for _, offset := range []int{1, 3, 0, 2} {
		if _, err := s.CreateSyncJob(ctx, domain.SyncJob{
			AccountID: 1,
			Source:    "local",
			CreatedAt: extraDiscoveryBase.Add(time.Duration(offset) * time.Minute),
		}); err != nil {
			t.Fatalf("CreateSyncJob: %v", err)
		}
	}
	if _, err := s.CreateSyncJob(ctx, domain.SyncJob{AccountID: 2, Source: "local", CreatedAt: extraDiscoveryBase}); err != nil {
		t.Fatalf("CreateSyncJob(other account): %v", err)
	}

	tests := []struct {
		name    string
		limit   int
		wantLen int
	}{
		{"zero limit returns everything", 0, 4},
		{"negative limit returns everything", -1, 4},
		{"limit smaller than result set", 2, 2},
		{"limit larger than result set", 100, 4},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jobs, err := s.ListSyncJobs(ctx, 1, tt.limit)
			if err != nil {
				t.Fatalf("ListSyncJobs: %v", err)
			}
			if len(jobs) != tt.wantLen {
				t.Fatalf("len = %d, want %d", len(jobs), tt.wantLen)
			}
			for i := 1; i < len(jobs); i++ {
				if jobs[i-1].CreatedAt.Before(jobs[i].CreatedAt) {
					t.Fatalf("jobs not sorted newest-first: %v then %v", jobs[i-1].CreatedAt, jobs[i].CreatedAt)
				}
			}
			for _, j := range jobs {
				if j.AccountID != 1 {
					t.Errorf("job from account %d leaked into results", j.AccountID)
				}
			}
		})
	}

	if jobs, err := s.ListSyncJobs(ctx, 999, 0); err != nil || len(jobs) != 0 {
		t.Errorf("ListSyncJobs(unknown account) = (%d jobs, %v), want (0, nil)", len(jobs), err)
	}
}

func TestMemoryDiscoveryStoreExtra_ClaimPendingAgentSync(t *testing.T) {
	ctx := context.Background()
	s := newDiscoveryStoreForExtraTests()
	agentID := uuid.New()
	otherAgent := uuid.New()

	oldest, err := s.CreateSyncJob(ctx, domain.SyncJob{
		AccountID: 1, Status: domain.SyncJobStatusPending, Source: "agent",
		AgentID: &agentID, CreatedAt: extraDiscoveryBase,
	})
	if err != nil {
		t.Fatalf("CreateSyncJob: %v", err)
	}
	newerCandidates := []domain.SyncJob{
		{AccountID: 1, Status: domain.SyncJobStatusPending, Source: "agent", AgentID: &agentID, CreatedAt: extraDiscoveryBase.Add(time.Hour)},
		{AccountID: 1, Status: domain.SyncJobStatusRunning, Source: "agent", AgentID: &agentID, CreatedAt: extraDiscoveryBase.Add(-time.Hour)},
		{AccountID: 1, Status: domain.SyncJobStatusPending, Source: "local", AgentID: &agentID, CreatedAt: extraDiscoveryBase.Add(-2 * time.Hour)},
		{AccountID: 1, Status: domain.SyncJobStatusPending, Source: "agent", AgentID: &otherAgent, CreatedAt: extraDiscoveryBase.Add(-3 * time.Hour)},
		{AccountID: 1, Status: domain.SyncJobStatusPending, Source: "agent", CreatedAt: extraDiscoveryBase.Add(-4 * time.Hour)},
	}
	for _, j := range newerCandidates {
		if _, err := s.CreateSyncJob(ctx, j); err != nil {
			t.Fatalf("CreateSyncJob: %v", err)
		}
	}

	claimed, err := s.ClaimPendingAgentSync(ctx, agentID)
	if err != nil {
		t.Fatalf("ClaimPendingAgentSync: %v", err)
	}
	if claimed.ID != oldest.ID {
		t.Errorf("claimed job %s, want oldest pending %s", claimed.ID, oldest.ID)
	}
	if claimed.Status != domain.SyncJobStatusRunning {
		t.Errorf("claimed status = %q, want running", claimed.Status)
	}
	if claimed.StartedAt == nil {
		t.Error("claimed job has no StartedAt")
	}

	persisted, err := s.GetSyncJob(ctx, oldest.ID)
	if err != nil {
		t.Fatalf("GetSyncJob: %v", err)
	}
	if persisted.Status != domain.SyncJobStatusRunning || persisted.StartedAt == nil {
		t.Errorf("claim was not persisted: %+v", persisted)
	}

	// The remaining pending job for the same agent is claimed next; then nothing.
	second, err := s.ClaimPendingAgentSync(ctx, agentID)
	if err != nil {
		t.Fatalf("second ClaimPendingAgentSync: %v", err)
	}
	if second.ID == oldest.ID {
		t.Error("already-claimed job was handed out twice")
	}
	if _, err := s.ClaimPendingAgentSync(ctx, agentID); !errors.Is(err, ErrNotFound) {
		t.Errorf("third ClaimPendingAgentSync error = %v, want ErrNotFound", err)
	}
}

func TestMemoryDiscoveryStoreExtra_AgentUpsertAndList(t *testing.T) {
	ctx := context.Background()
	s := newDiscoveryStoreForExtraTests()

	agent := domain.DiscoveryAgent{Name: "agent-a", AccountID: 1, Hostname: "host-a"}
	if err := s.UpsertAgent(ctx, agent); err != nil {
		t.Fatalf("UpsertAgent: %v", err)
	}
	agents, err := s.ListAgents(ctx, 1)
	if err != nil {
		t.Fatalf("ListAgents: %v", err)
	}
	if len(agents) != 1 {
		t.Fatalf("len(agents) = %d, want 1", len(agents))
	}
	generated := agents[0]
	if generated.ID == uuid.Nil {
		t.Error("UpsertAgent did not assign an ID")
	}
	if generated.CreatedAt.IsZero() {
		t.Error("UpsertAgent did not assign CreatedAt")
	}

	// Re-upserting with a zero CreatedAt preserves the original creation time.
	if err := s.UpsertAgent(ctx, domain.DiscoveryAgent{ID: generated.ID, Name: "agent-a-renamed", AccountID: 1}); err != nil {
		t.Fatalf("UpsertAgent(update): %v", err)
	}
	got, err := s.GetAgent(ctx, generated.ID)
	if err != nil {
		t.Fatalf("GetAgent: %v", err)
	}
	if !got.CreatedAt.Equal(generated.CreatedAt) {
		t.Errorf("CreatedAt = %v, want preserved %v", got.CreatedAt, generated.CreatedAt)
	}
	if got.Name != "agent-a-renamed" {
		t.Errorf("Name = %q, want agent-a-renamed", got.Name)
	}

	// An explicit CreatedAt is honoured.
	explicitID := uuid.New()
	if err := s.UpsertAgent(ctx, domain.DiscoveryAgent{ID: explicitID, Name: "agent-b", AccountID: 2, CreatedAt: extraDiscoveryBase}); err != nil {
		t.Fatalf("UpsertAgent(explicit): %v", err)
	}
	gotB, err := s.GetAgent(ctx, explicitID)
	if err != nil {
		t.Fatalf("GetAgent(b): %v", err)
	}
	if !gotB.CreatedAt.Equal(extraDiscoveryBase) {
		t.Errorf("CreatedAt = %v, want %v", gotB.CreatedAt, extraDiscoveryBase)
	}

	if scoped, err := s.ListAgents(ctx, 2); err != nil || len(scoped) != 1 || scoped[0].ID != explicitID {
		t.Errorf("ListAgents(2) = (%+v, %v), want only agent-b", scoped, err)
	}
	all, err := s.ListAgents(ctx, 0)
	if err != nil {
		t.Fatalf("ListAgents(0): %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("ListAgents(0) len = %d, want 2 (account 0 means all accounts)", len(all))
	}
	if all[0].CreatedAt.Before(all[1].CreatedAt) {
		t.Errorf("agents not sorted newest-first: %v then %v", all[0].CreatedAt, all[1].CreatedAt)
	}
}

func TestMemoryDiscoveryStoreExtra_DeleteAgentDetachesItsSyncJobs(t *testing.T) {
	ctx := context.Background()
	s := newDiscoveryStoreForExtraTests()

	agentID := uuid.New()
	otherAgent := uuid.New()
	if err := s.UpsertAgent(ctx, domain.DiscoveryAgent{ID: agentID, Name: "doomed", AccountID: 1}); err != nil {
		t.Fatalf("UpsertAgent: %v", err)
	}
	if err := s.UpsertAgent(ctx, domain.DiscoveryAgent{ID: otherAgent, Name: "survivor", AccountID: 1}); err != nil {
		t.Fatalf("UpsertAgent: %v", err)
	}

	owned, err := s.CreateSyncJob(ctx, domain.SyncJob{AccountID: 1, Source: "agent", AgentID: &agentID})
	if err != nil {
		t.Fatalf("CreateSyncJob: %v", err)
	}
	foreign, err := s.CreateSyncJob(ctx, domain.SyncJob{AccountID: 1, Source: "agent", AgentID: &otherAgent})
	if err != nil {
		t.Fatalf("CreateSyncJob: %v", err)
	}
	unowned, err := s.CreateSyncJob(ctx, domain.SyncJob{AccountID: 1, Source: "local"})
	if err != nil {
		t.Fatalf("CreateSyncJob: %v", err)
	}

	if err := s.DeleteAgent(ctx, agentID); err != nil {
		t.Fatalf("DeleteAgent: %v", err)
	}
	if _, err := s.GetAgent(ctx, agentID); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetAgent after delete = %v, want ErrNotFound", err)
	}

	gotOwned, err := s.GetSyncJob(ctx, owned.ID)
	if err != nil {
		t.Fatalf("GetSyncJob(owned): %v", err)
	}
	if gotOwned.AgentID != nil {
		t.Errorf("owned job AgentID = %v, want nil after agent delete", *gotOwned.AgentID)
	}

	gotForeign, err := s.GetSyncJob(ctx, foreign.ID)
	if err != nil {
		t.Fatalf("GetSyncJob(foreign): %v", err)
	}
	if gotForeign.AgentID == nil || *gotForeign.AgentID != otherAgent {
		t.Errorf("other agent's job AgentID = %v, want %s", gotForeign.AgentID, otherAgent)
	}

	gotUnowned, err := s.GetSyncJob(ctx, unowned.ID)
	if err != nil {
		t.Fatalf("GetSyncJob(unowned): %v", err)
	}
	if gotUnowned.AgentID != nil {
		t.Errorf("unowned job AgentID = %v, want nil", *gotUnowned.AgentID)
	}
}
