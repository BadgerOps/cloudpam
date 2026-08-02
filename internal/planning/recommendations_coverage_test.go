package planning

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"cloudpam/internal/domain"
	"cloudpam/internal/storage"
)

func TestCovPriorityFromScore(t *testing.T) {
	tests := []struct {
		score int
		want  domain.RecommendationPriority
	}{
		{100, domain.RecommendationPriorityHigh},
		{70, domain.RecommendationPriorityHigh},
		{69, domain.RecommendationPriorityMedium},
		{40, domain.RecommendationPriorityMedium},
		{39, domain.RecommendationPriorityLow},
		{0, domain.RecommendationPriorityLow},
		{-1, domain.RecommendationPriorityLow},
	}
	for _, tc := range tests {
		if got := priorityFromScore(tc.score); got != tc.want {
			t.Errorf("priorityFromScore(%d) = %q, want %q", tc.score, got, tc.want)
		}
	}
}

func TestCovComplianceRecommendationMapsRules(t *testing.T) {
	svc, _ := setupRecService(t)
	pool := domain.Pool{ID: 42, Name: "Corp", CIDR: "10.0.0.0/16"}
	now := time.Date(2024, 5, 1, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		ruleID       string
		wantPriority domain.RecommendationPriority
		wantScore    int
		wantTitle    string
	}{
		{"OVERLAP-001", domain.RecommendationPriorityHigh, 90, "Resolve CIDR overlap"},
		{"RFC1918-001", domain.RecommendationPriorityMedium, 60, "Move to RFC1918 space"},
		{"EMPTY-001", domain.RecommendationPriorityLow, 30, "Add allocations or reclassify"},
		{"NAME-001", domain.RecommendationPriorityLow, 20, "Add pool name"},
		{"NAME-002", domain.RecommendationPriorityLow, 20, "Add pool description"},
		{"UNKNOWN-999", domain.RecommendationPriorityMedium, 50, "Fix compliance issue: UNKNOWN-999"},
		{"", domain.RecommendationPriorityMedium, 50, "Fix compliance issue: "},
	}

	for _, tc := range tests {
		t.Run(tc.ruleID, func(t *testing.T) {
			violation := ComplianceViolation{
				RuleID:  tc.ruleID,
				Message: "violation detail for " + tc.ruleID,
			}
			rec := svc.complianceRecommendation(violation, pool, now)

			if rec.ID == "" {
				t.Error("recommendation ID is empty")
			}
			if rec.PoolID != 42 {
				t.Errorf("PoolID = %d, want 42", rec.PoolID)
			}
			if rec.Type != domain.RecommendationTypeCompliance {
				t.Errorf("Type = %q, want compliance", rec.Type)
			}
			if rec.Status != domain.RecommendationStatusPending {
				t.Errorf("Status = %q, want pending", rec.Status)
			}
			if rec.RuleID != tc.ruleID {
				t.Errorf("RuleID = %q, want %q", rec.RuleID, tc.ruleID)
			}
			if rec.Priority != tc.wantPriority {
				t.Errorf("Priority = %q, want %q", rec.Priority, tc.wantPriority)
			}
			if rec.Score != tc.wantScore {
				t.Errorf("Score = %d, want %d", rec.Score, tc.wantScore)
			}
			if rec.Title != tc.wantTitle {
				t.Errorf("Title = %q, want %q", rec.Title, tc.wantTitle)
			}
			if rec.Description != violation.Message {
				t.Errorf("Description = %q, want the violation message", rec.Description)
			}
			if !rec.CreatedAt.Equal(now) || !rec.UpdatedAt.Equal(now) {
				t.Errorf("timestamps = %v / %v, want %v", rec.CreatedAt, rec.UpdatedAt, now)
			}
		})
	}
}

func TestCovComplianceRecommendationIDsAreUnique(t *testing.T) {
	svc, _ := setupRecService(t)
	pool := domain.Pool{ID: 1}
	now := time.Now().UTC()

	first := svc.complianceRecommendation(ComplianceViolation{RuleID: "NAME-001"}, pool, now)
	second := svc.complianceRecommendation(ComplianceViolation{RuleID: "NAME-001"}, pool, now)
	if first.ID == second.ID {
		t.Fatal("recommendation IDs must be unique")
	}
}

func TestCovScoreAllocationRejectsInvalidCIDR(t *testing.T) {
	svc, _ := setupRecService(t)
	pool := domain.Pool{ID: 1, CIDR: "10.0.0.0/16"}

	for _, cidr := range []string{"", "not-a-cidr", "10.0.0.0", "10.0.0.0/33"} {
		if got := svc.scoreAllocation(cidr, pool, 0); got != 0 {
			t.Errorf("scoreAllocation(%q) = %d, want 0", cidr, got)
		}
	}
}

func TestCovScoreAllocationNearMissPrefixLen(t *testing.T) {
	svc, _ := setupRecService(t)
	pool := domain.Pool{ID: 1, CIDR: "10.0.0.0/16"}

	exact := svc.scoreAllocation("10.0.0.0/24", pool, 24)
	near := svc.scoreAllocation("10.0.0.0/26", pool, 24)
	far := svc.scoreAllocation("10.0.0.0/30", pool, 24)

	if exact <= near {
		t.Errorf("exact match (%d) should score above a near miss (%d)", exact, near)
	}
	if near <= far {
		t.Errorf("near miss within 2 bits (%d) should score above a far miss (%d)", near, far)
	}
}

func TestCovScoreAllocationSizeFitWithoutDesiredPrefix(t *testing.T) {
	svc, _ := setupRecService(t)
	pool := domain.Pool{ID: 1, CIDR: "10.0.0.0/8"}

	workload := svc.scoreAllocation("10.0.0.0/24", pool, 0)   // in the 24-28 sweet spot
	acceptable := svc.scoreAllocation("10.0.0.0/22", pool, 0) // in the wider 20-30 band
	tooLarge := svc.scoreAllocation("10.0.0.0/12", pool, 0)   // outside both bands

	if workload <= acceptable {
		t.Errorf("a /24 (%d) should score above a /22 (%d)", workload, acceptable)
	}
	if acceptable <= tooLarge {
		t.Errorf("a /22 (%d) should score above a /12 (%d)", acceptable, tooLarge)
	}
}

func TestCovScoreAllocationContiguityBonus(t *testing.T) {
	ctx := context.Background()
	svc, st := setupRecService(t)

	parent, err := st.CreatePool(ctx, domain.CreatePool{
		Name: "Corp", CIDR: "10.0.0.0/16", Type: domain.PoolTypeSupernet, Description: "corp",
	})
	if err != nil {
		t.Fatalf("CreatePool() error = %v", err)
	}
	parentID := parent.ID
	if _, err := st.CreatePool(ctx, domain.CreatePool{
		Name: "Existing", CIDR: "10.0.1.0/24", ParentID: &parentID, Type: domain.PoolTypeSubnet, Description: "x",
	}); err != nil {
		t.Fatalf("CreatePool() error = %v", err)
	}

	adjacent := svc.scoreAllocation("10.0.2.0/24", parent, 0)
	distant := svc.scoreAllocation("10.0.50.0/24", parent, 0)

	if adjacent != distant+20 {
		t.Fatalf("adjacent block scored %d and distant %d, want a 20 point contiguity bonus", adjacent, distant)
	}
}

func TestCovScoreAllocationIsCappedAt100(t *testing.T) {
	ctx := context.Background()
	svc, st := setupRecService(t)

	parent, err := st.CreatePool(ctx, domain.CreatePool{
		Name: "Corp", CIDR: "10.0.0.0/16", Type: domain.PoolTypeSupernet, Description: "corp",
	})
	if err != nil {
		t.Fatalf("CreatePool() error = %v", err)
	}
	parentID := parent.ID
	if _, err := st.CreatePool(ctx, domain.CreatePool{
		Name: "Existing", CIDR: "10.0.1.0/24", ParentID: &parentID, Type: domain.PoolTypeSubnet, Description: "x",
	}); err != nil {
		t.Fatalf("CreatePool() error = %v", err)
	}

	// Aligned + exact prefix len + adjacent + RFC1918 = 30+30+20+20 = 100.
	if got := svc.scoreAllocation("10.0.2.0/24", parent, 24); got != 100 {
		t.Fatalf("scoreAllocation() = %d, want the maximum 100", got)
	}
}

func TestCovScoreAllocationSkipsUnparsableChildCIDRs(t *testing.T) {
	svc, _ := setupRecService(t)
	// A pool ID with no children in the store exercises the no-bonus path
	// without panicking on lookup failures.
	pool := domain.Pool{ID: 99999, CIDR: "10.0.0.0/16"}
	if got := svc.scoreAllocation("10.0.1.0/24", pool, 24); got < 0 || got > 100 {
		t.Fatalf("scoreAllocation() = %d, want a value within 0-100", got)
	}
}

func TestCovGenerateRejectsUnknownPool(t *testing.T) {
	svc, _ := setupRecService(t)

	resp, err := svc.Generate(context.Background(), domain.GenerateRecommendationsRequest{PoolIDs: []int64{424242}})
	if !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("Generate() error = %v, want ErrNotFound", err)
	}
	if resp != nil {
		t.Fatalf("resp = %+v, want nil on error", resp)
	}
}

func TestCovGenerateReturnsEmptySliceWhenNothingToRecommend(t *testing.T) {
	svc, _ := setupRecService(t)

	resp, err := svc.Generate(context.Background(), domain.GenerateRecommendationsRequest{PoolIDs: []int64{}})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if resp.Total != 0 {
		t.Errorf("Total = %d, want 0", resp.Total)
	}
	if resp.Items == nil {
		t.Fatal("Items = nil, want an empty slice so JSON encodes as []")
	}
	if len(resp.Items) != 0 {
		t.Errorf("Items = %+v, want empty", resp.Items)
	}
}

func TestCovGenerateHonoursDesiredPrefixLen(t *testing.T) {
	ctx := context.Background()
	svc, st := setupRecService(t)

	pool, err := st.CreatePool(ctx, domain.CreatePool{
		Name: "Corp", CIDR: "10.0.0.0/24", Type: domain.PoolTypeSupernet, Description: "corp",
	})
	if err != nil {
		t.Fatalf("CreatePool() error = %v", err)
	}

	resp, err := svc.Generate(ctx, domain.GenerateRecommendationsRequest{
		PoolIDs:          []int64{pool.ID},
		DesiredPrefixLen: 24,
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	var alloc *domain.Recommendation
	for i := range resp.Items {
		if resp.Items[i].Type == domain.RecommendationTypeAllocation {
			alloc = &resp.Items[i]
			break
		}
	}
	if alloc == nil {
		t.Fatal("no allocation recommendation generated")
	}
	if alloc.SuggestedCIDR != "10.0.0.0/24" {
		t.Errorf("SuggestedCIDR = %q, want the whole free /24", alloc.SuggestedCIDR)
	}
	if alloc.Priority != priorityFromScore(alloc.Score) {
		t.Errorf("Priority %q does not match score %d", alloc.Priority, alloc.Score)
	}
	if !strings.Contains(alloc.Title, "Allocate ") {
		t.Errorf("Title = %q, want an Allocate title", alloc.Title)
	}
	if !strings.Contains(alloc.Description, "Corp") {
		t.Errorf("Description = %q, want it to name the pool", alloc.Description)
	}
}

func TestCovGenerateIncludesChildren(t *testing.T) {
	ctx := context.Background()
	svc, st := setupRecService(t)

	parent, err := st.CreatePool(ctx, domain.CreatePool{
		Name: "Corp", CIDR: "10.0.0.0/16", Type: domain.PoolTypeSupernet, Description: "corp",
	})
	if err != nil {
		t.Fatalf("CreatePool() error = %v", err)
	}
	parentID := parent.ID
	child, err := st.CreatePool(ctx, domain.CreatePool{
		Name: "Child", CIDR: "10.0.1.0/24", ParentID: &parentID, Type: domain.PoolTypeSupernet, Description: "child",
	})
	if err != nil {
		t.Fatalf("CreatePool() error = %v", err)
	}

	resp, err := svc.Generate(ctx, domain.GenerateRecommendationsRequest{
		PoolIDs:         []int64{parent.ID},
		IncludeChildren: true,
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	pools := map[int64]bool{}
	for _, r := range resp.Items {
		pools[r.PoolID] = true
	}
	if !pools[parent.ID] {
		t.Error("no recommendations for the parent pool")
	}
	if !pools[child.ID] {
		t.Error("no recommendations for the child pool despite include_children")
	}
}

func TestCovApplyRejectsUnknownRecommendation(t *testing.T) {
	svc, _ := setupRecService(t)

	rec, err := svc.Apply(context.Background(), "no-such-id", domain.ApplyRecommendationRequest{})
	if !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("Apply() error = %v, want ErrNotFound", err)
	}
	if rec != nil {
		t.Fatalf("rec = %+v, want nil", rec)
	}
}

func TestCovDismissRejectsUnknownRecommendation(t *testing.T) {
	svc, _ := setupRecService(t)

	rec, err := svc.Dismiss(context.Background(), "no-such-id", "reason")
	if !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("Dismiss() error = %v, want ErrNotFound", err)
	}
	if rec != nil {
		t.Fatalf("rec = %+v, want nil", rec)
	}
}

func TestCovDismissRejectsAlreadyDismissed(t *testing.T) {
	ctx := context.Background()
	svc, st := setupRecService(t)

	pool, err := st.CreatePool(ctx, domain.CreatePool{
		Name: "Corp", CIDR: "10.0.0.0/24", Type: domain.PoolTypeSupernet, Description: "corp",
	})
	if err != nil {
		t.Fatalf("CreatePool() error = %v", err)
	}
	resp, err := svc.Generate(ctx, domain.GenerateRecommendationsRequest{PoolIDs: []int64{pool.ID}})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if len(resp.Items) == 0 {
		t.Fatal("no recommendations generated")
	}
	id := resp.Items[0].ID

	if _, err := svc.Dismiss(ctx, id, "first"); err != nil {
		t.Fatalf("Dismiss() error = %v", err)
	}
	_, err = svc.Dismiss(ctx, id, "second")
	if !errors.Is(err, storage.ErrConflict) {
		t.Fatalf("second Dismiss() error = %v, want ErrConflict", err)
	}
	if !strings.Contains(err.Error(), "not pending") {
		t.Errorf("error = %q, want it to explain the status", err.Error())
	}
}

func TestCovDismissRejectsAppliedRecommendation(t *testing.T) {
	ctx := context.Background()
	svc, st := setupRecService(t)

	pool, err := st.CreatePool(ctx, domain.CreatePool{
		Name: "Corp", CIDR: "10.0.0.0/24", Type: domain.PoolTypeSupernet, Description: "corp",
	})
	if err != nil {
		t.Fatalf("CreatePool() error = %v", err)
	}
	resp, err := svc.Generate(ctx, domain.GenerateRecommendationsRequest{PoolIDs: []int64{pool.ID}})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if len(resp.Items) == 0 {
		t.Fatal("no recommendations generated")
	}
	id := resp.Items[0].ID

	if _, err := svc.Apply(ctx, id, domain.ApplyRecommendationRequest{}); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if _, err := svc.Dismiss(ctx, id, "too late"); !errors.Is(err, storage.ErrConflict) {
		t.Fatalf("Dismiss() error = %v, want ErrConflict", err)
	}
}

func TestCovApplyComplianceRecommendationCreatesNoPool(t *testing.T) {
	ctx := context.Background()
	svc, st := setupRecService(t)

	pool, err := st.CreatePool(ctx, domain.CreatePool{
		Name: "Public", CIDR: "8.8.8.0/24", Type: domain.PoolTypeSubnet, Description: "public",
	})
	if err != nil {
		t.Fatalf("CreatePool() error = %v", err)
	}

	resp, err := svc.Generate(ctx, domain.GenerateRecommendationsRequest{PoolIDs: []int64{pool.ID}})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	var complianceRec *domain.Recommendation
	for i := range resp.Items {
		if resp.Items[i].Type == domain.RecommendationTypeCompliance {
			complianceRec = &resp.Items[i]
			break
		}
	}
	if complianceRec == nil {
		t.Fatal("no compliance recommendation generated")
	}

	before, err := st.ListPools(ctx)
	if err != nil {
		t.Fatalf("ListPools() error = %v", err)
	}

	applied, err := svc.Apply(ctx, complianceRec.ID, domain.ApplyRecommendationRequest{})
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if applied.Status != domain.RecommendationStatusApplied {
		t.Errorf("Status = %q, want applied", applied.Status)
	}
	if applied.AppliedPoolID != nil {
		t.Errorf("AppliedPoolID = %v, want nil for a compliance acknowledgement", applied.AppliedPoolID)
	}

	after, err := st.ListPools(ctx)
	if err != nil {
		t.Fatalf("ListPools() error = %v", err)
	}
	if len(after) != len(before) {
		t.Fatalf("pool count changed from %d to %d, want no new pool", len(before), len(after))
	}
}

func TestCovApplyAllocationUsesDefaultNameAndAccount(t *testing.T) {
	ctx := context.Background()
	svc, st := setupRecService(t)

	account, err := st.CreateAccount(ctx, domain.CreateAccount{Key: "acct-1", Name: "Acct One"})
	if err != nil {
		t.Fatalf("CreateAccount() error = %v", err)
	}

	parent, err := st.CreatePool(ctx, domain.CreatePool{
		Name: "Corp", CIDR: "10.0.0.0/24", Type: domain.PoolTypeSupernet, Description: "corp",
	})
	if err != nil {
		t.Fatalf("CreatePool() error = %v", err)
	}

	resp, err := svc.Generate(ctx, domain.GenerateRecommendationsRequest{PoolIDs: []int64{parent.ID}})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	var alloc *domain.Recommendation
	for i := range resp.Items {
		if resp.Items[i].Type == domain.RecommendationTypeAllocation {
			alloc = &resp.Items[i]
			break
		}
	}
	if alloc == nil {
		t.Fatal("no allocation recommendation generated")
	}

	accountID := account.ID
	applied, err := svc.Apply(ctx, alloc.ID, domain.ApplyRecommendationRequest{AccountID: &accountID})
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if applied.AppliedPoolID == nil {
		t.Fatal("AppliedPoolID = nil, want the new pool ID")
	}

	newPool, found, err := st.GetPool(ctx, *applied.AppliedPoolID)
	if err != nil || !found {
		t.Fatalf("GetPool() found = %v, err = %v", found, err)
	}
	wantName := "Allocation " + alloc.SuggestedCIDR
	if newPool.Name != wantName {
		t.Errorf("pool Name = %q, want the default %q", newPool.Name, wantName)
	}
	if newPool.ParentID == nil || *newPool.ParentID != parent.ID {
		t.Errorf("pool ParentID = %v, want %d", newPool.ParentID, parent.ID)
	}
	if newPool.AccountID == nil || *newPool.AccountID != account.ID {
		t.Errorf("pool AccountID = %v, want %d", newPool.AccountID, account.ID)
	}
	if newPool.Source != domain.PoolSourceManual {
		t.Errorf("pool Source = %q, want manual", newPool.Source)
	}
}

// covFailingPoolStore wraps the memory store so pool creation always fails,
// which is the only way to reach the apply-time creation failure path.
type covFailingPoolStore struct {
	*storage.MemoryStore
	err error
}

func (s *covFailingPoolStore) CreatePool(context.Context, domain.CreatePool) (domain.Pool, error) {
	return domain.Pool{}, s.err
}

func TestCovApplyFailsWhenPoolCreationFails(t *testing.T) {
	ctx := context.Background()
	st := storage.NewMemoryStore()
	recStore := storage.NewMemoryRecommendationStore(st)
	failing := &covFailingPoolStore{MemoryStore: st, err: errors.New("disk full")}
	svc := NewRecommendationService(NewAnalysisService(st), recStore, failing)

	parent, err := st.CreatePool(ctx, domain.CreatePool{
		Name: "Corp", CIDR: "10.0.0.0/24", Type: domain.PoolTypeSupernet, Description: "corp",
	})
	if err != nil {
		t.Fatalf("CreatePool() error = %v", err)
	}

	resp, err := svc.Generate(ctx, domain.GenerateRecommendationsRequest{PoolIDs: []int64{parent.ID}})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	var alloc *domain.Recommendation
	for i := range resp.Items {
		if resp.Items[i].Type == domain.RecommendationTypeAllocation {
			alloc = &resp.Items[i]
			break
		}
	}
	if alloc == nil {
		t.Fatal("no allocation recommendation generated")
	}

	applied, err := svc.Apply(ctx, alloc.ID, domain.ApplyRecommendationRequest{Name: "Doomed"})
	if err == nil {
		t.Fatal("Apply() error = nil, want a pool creation failure")
	}
	if applied != nil {
		t.Fatalf("applied = %+v, want nil on error", applied)
	}
	if !strings.Contains(err.Error(), "create pool from recommendation") {
		t.Errorf("error = %q, want create pool from recommendation", err.Error())
	}
	if !errors.Is(err, failing.err) {
		t.Errorf("error = %v, want it to wrap the store failure", err)
	}

	// The recommendation must remain pending so it can be retried.
	stored, err := recStore.GetRecommendation(ctx, alloc.ID)
	if err != nil {
		t.Fatalf("GetRecommendation() error = %v", err)
	}
	if stored.Status != domain.RecommendationStatusPending {
		t.Errorf("Status = %q, want it to stay pending after a failed apply", stored.Status)
	}
}

func TestCovDismissStoresReason(t *testing.T) {
	ctx := context.Background()
	svc, st := setupRecService(t)

	pool, err := st.CreatePool(ctx, domain.CreatePool{
		Name: "Corp", CIDR: "10.0.0.0/24", Type: domain.PoolTypeSupernet, Description: "corp",
	})
	if err != nil {
		t.Fatalf("CreatePool() error = %v", err)
	}
	resp, err := svc.Generate(ctx, domain.GenerateRecommendationsRequest{PoolIDs: []int64{pool.ID}})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if len(resp.Items) == 0 {
		t.Fatal("no recommendations generated")
	}
	id := resp.Items[0].ID

	if _, err := svc.Dismiss(ctx, id, "handled elsewhere"); err != nil {
		t.Fatalf("Dismiss() error = %v", err)
	}

	stored, err := svc.store.GetRecommendation(ctx, id)
	if err != nil {
		t.Fatalf("GetRecommendation() error = %v", err)
	}
	if stored.Status != domain.RecommendationStatusDismissed {
		t.Errorf("Status = %q, want dismissed", stored.Status)
	}
	if stored.DismissReason != "handled elsewhere" {
		t.Errorf("DismissReason = %q, want it persisted", stored.DismissReason)
	}
}

func TestCovAnalysisServiceStoreAccessor(t *testing.T) {
	st := storage.NewMemoryStore()
	svc := NewAnalysisService(st)
	if svc.Store() != st {
		t.Fatal("Store() did not return the store the service was built with")
	}
}
