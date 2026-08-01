package planning

import (
	"context"
	"errors"
	"strings"
	"testing"

	"cloudpam/internal/domain"
	"cloudpam/internal/storage"
)

// covErrStore wraps the memory store and injects failures into the specific
// reads the analysis engine performs.
type covErrStore struct {
	*storage.MemoryStore
	listErr     error
	getErr      error
	childrenErr error
}

func (s *covErrStore) ListPools(ctx context.Context) ([]domain.Pool, error) {
	if s.listErr != nil {
		return nil, s.listErr
	}
	return s.MemoryStore.ListPools(ctx)
}

func (s *covErrStore) GetPool(ctx context.Context, id int64) (domain.Pool, bool, error) {
	if s.getErr != nil {
		return domain.Pool{}, false, s.getErr
	}
	return s.MemoryStore.GetPool(ctx, id)
}

func (s *covErrStore) GetPoolChildren(ctx context.Context, id int64) ([]domain.Pool, error) {
	if s.childrenErr != nil {
		return nil, s.childrenErr
	}
	return s.MemoryStore.GetPoolChildren(ctx, id)
}

func covNewErrService(t *testing.T) (*AnalysisService, *covErrStore, *storage.MemoryStore) {
	t.Helper()
	mem := storage.NewMemoryStore()
	wrapper := &covErrStore{MemoryStore: mem}
	return NewAnalysisService(wrapper), wrapper, mem
}

func TestCovResolvePoolsPropagatesListError(t *testing.T) {
	svc, wrapper, _ := covNewErrService(t)
	wrapper.listErr = errors.New("db down")

	_, err := svc.resolvePools(context.Background(), nil, false)
	if err == nil || !strings.Contains(err.Error(), "list pools") {
		t.Fatalf("resolvePools() error = %v, want list pools failure", err)
	}
	if !errors.Is(err, wrapper.listErr) {
		t.Errorf("error = %v, want it to wrap the store failure", err)
	}
}

func TestCovResolvePoolsPropagatesGetError(t *testing.T) {
	svc, wrapper, _ := covNewErrService(t)
	wrapper.getErr = errors.New("db down")

	_, err := svc.resolvePools(context.Background(), []int64{1}, false)
	if err == nil || !strings.Contains(err.Error(), "get pool 1") {
		t.Fatalf("resolvePools() error = %v, want get pool failure", err)
	}
}

func TestCovResolvePoolsSkipsChildrenLookupFailures(t *testing.T) {
	ctx := context.Background()
	svc, wrapper, mem := covNewErrService(t)

	pool, err := mem.CreatePool(ctx, domain.CreatePool{Name: "Root", CIDR: "10.0.0.0/16"})
	if err != nil {
		t.Fatalf("CreatePool() error = %v", err)
	}
	wrapper.childrenErr = errors.New("children unavailable")

	pools, err := svc.resolvePools(ctx, []int64{pool.ID}, true)
	if err != nil {
		t.Fatalf("resolvePools() error = %v, want children lookup failures to be tolerated", err)
	}
	if len(pools) != 1 || pools[0].ID != pool.ID {
		t.Fatalf("pools = %+v, want just the requested pool", pools)
	}
}

func TestCovResolvePoolsIsCycleSafe(t *testing.T) {
	ctx := context.Background()
	st := storage.NewMemoryStore()
	svc := NewAnalysisService(st)

	parent, err := st.CreatePool(ctx, domain.CreatePool{Name: "Root", CIDR: "10.0.0.0/16"})
	if err != nil {
		t.Fatalf("CreatePool() error = %v", err)
	}
	parentID := parent.ID
	child, err := st.CreatePool(ctx, domain.CreatePool{Name: "Child", CIDR: "10.0.1.0/24", ParentID: &parentID})
	if err != nil {
		t.Fatalf("CreatePool() error = %v", err)
	}
	childID := child.ID
	if _, err := st.CreatePool(ctx, domain.CreatePool{Name: "Grandchild", CIDR: "10.0.1.0/26", ParentID: &childID}); err != nil {
		t.Fatalf("CreatePool() error = %v", err)
	}

	pools, err := svc.resolvePools(ctx, []int64{parent.ID}, true)
	if err != nil {
		t.Fatalf("resolvePools() error = %v", err)
	}
	if len(pools) != 3 {
		t.Fatalf("pools = %d, want the full 3-level descent", len(pools))
	}
	seen := map[int64]int{}
	for _, p := range pools {
		seen[p.ID]++
	}
	for id, count := range seen {
		if count != 1 {
			t.Errorf("pool %d appears %d times, want exactly once", id, count)
		}
	}
}

func TestCovAnalyzeGapsPropagatesStoreErrors(t *testing.T) {
	ctx := context.Background()

	t.Run("get pool error", func(t *testing.T) {
		svc, wrapper, _ := covNewErrService(t)
		wrapper.getErr = errors.New("db down")
		if _, err := svc.AnalyzeGaps(ctx, 1); err == nil || !strings.Contains(err.Error(), "get pool 1") {
			t.Fatalf("AnalyzeGaps() error = %v, want get pool failure", err)
		}
	})

	t.Run("children error", func(t *testing.T) {
		svc, wrapper, mem := covNewErrService(t)
		pool, err := mem.CreatePool(ctx, domain.CreatePool{Name: "Root", CIDR: "10.0.0.0/16"})
		if err != nil {
			t.Fatalf("CreatePool() error = %v", err)
		}
		wrapper.childrenErr = errors.New("children unavailable")
		if _, err := svc.AnalyzeGaps(ctx, pool.ID); err == nil || !strings.Contains(err.Error(), "get children for pool") {
			t.Fatalf("AnalyzeGaps() error = %v, want children failure", err)
		}
	})

	t.Run("pool not found", func(t *testing.T) {
		st := storage.NewMemoryStore()
		svc := NewAnalysisService(st)
		if _, err := svc.AnalyzeGaps(ctx, 999); !errors.Is(err, storage.ErrNotFound) {
			t.Fatalf("AnalyzeGaps() error = %v, want ErrNotFound", err)
		}
	})
}

func TestCovAnalyzeGapsRejectsUnparsablePoolCIDR(t *testing.T) {
	ctx := context.Background()
	st := storage.NewMemoryStore()
	svc := NewAnalysisService(st)

	pool, err := st.CreatePool(ctx, domain.CreatePool{Name: "Broken", CIDR: "not-a-cidr"})
	if err != nil {
		t.Fatalf("CreatePool() error = %v", err)
	}

	_, err = svc.AnalyzeGaps(ctx, pool.ID)
	if err == nil || !strings.Contains(err.Error(), "parse pool CIDR") {
		t.Fatalf("AnalyzeGaps() error = %v, want a CIDR parse failure", err)
	}
}

func TestCovAnalyzeGapsIgnoresUnparsableChildCIDR(t *testing.T) {
	ctx := context.Background()
	st := storage.NewMemoryStore()
	svc := NewAnalysisService(st)

	parent, err := st.CreatePool(ctx, domain.CreatePool{Name: "Root", CIDR: "10.0.0.0/24"})
	if err != nil {
		t.Fatalf("CreatePool() error = %v", err)
	}
	parentID := parent.ID
	if _, err := st.CreatePool(ctx, domain.CreatePool{Name: "Bad", CIDR: "garbage", ParentID: &parentID}); err != nil {
		t.Fatalf("CreatePool() error = %v", err)
	}
	if _, err := st.CreatePool(ctx, domain.CreatePool{Name: "Good", CIDR: "10.0.0.0/26", ParentID: &parentID}); err != nil {
		t.Fatalf("CreatePool() error = %v", err)
	}

	gap, err := svc.AnalyzeGaps(ctx, parent.ID)
	if err != nil {
		t.Fatalf("AnalyzeGaps() error = %v", err)
	}
	if len(gap.AllocatedBlocks) != 1 || gap.AllocatedBlocks[0].CIDR != "10.0.0.0/26" {
		t.Fatalf("allocated blocks = %+v, want only the parsable child", gap.AllocatedBlocks)
	}
	// The unparsable child must not consume address space.
	if gap.UsedAddresses != 64 {
		t.Errorf("UsedAddresses = %d, want 64 from the single valid /26", gap.UsedAddresses)
	}
}

func TestCovFindFreeRangesMergesAndClamps(t *testing.T) {
	tests := []struct {
		name        string
		start, end  uint32
		children    []interval
		wantStarts  []uint32
		wantEndsLen int
	}{
		{
			name:        "no children yields the whole range",
			start:       0,
			end:         255,
			children:    nil,
			wantStarts:  []uint32{0},
			wantEndsLen: 1,
		},
		{
			name:        "adjacent children merge into one block",
			start:       0,
			end:         255,
			children:    []interval{{start: 0, end: 63}, {start: 64, end: 127}},
			wantStarts:  []uint32{128},
			wantEndsLen: 1,
		},
		{
			name:        "overlapping children merge",
			start:       0,
			end:         255,
			children:    []interval{{start: 0, end: 100}, {start: 50, end: 63}},
			wantStarts:  []uint32{101},
			wantEndsLen: 1,
		},
		{
			name:        "unsorted children are handled",
			start:       0,
			end:         255,
			children:    []interval{{start: 192, end: 255}, {start: 0, end: 63}},
			wantStarts:  []uint32{64},
			wantEndsLen: 1,
		},
		{
			name:        "children fully outside the parent are skipped",
			start:       100,
			end:         200,
			children:    []interval{{start: 0, end: 50}, {start: 300, end: 400}},
			wantStarts:  []uint32{100},
			wantEndsLen: 1,
		},
		{
			name:        "children straddling the parent bounds are clamped",
			start:       100,
			end:         200,
			children:    []interval{{start: 50, end: 120}},
			wantStarts:  []uint32{121},
			wantEndsLen: 1,
		},
		{
			name:        "child covering the whole parent leaves no gaps",
			start:       100,
			end:         200,
			children:    []interval{{start: 0, end: 500}},
			wantStarts:  nil,
			wantEndsLen: 0,
		},
		{
			name:        "gaps on both sides",
			start:       0,
			end:         255,
			children:    []interval{{start: 64, end: 127}},
			wantStarts:  []uint32{0, 128},
			wantEndsLen: 2,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := findFreeRanges(tc.start, tc.end, tc.children)
			if len(got) != tc.wantEndsLen {
				t.Fatalf("gaps = %+v, want %d", got, tc.wantEndsLen)
			}
			for i, want := range tc.wantStarts {
				if got[i].start != want {
					t.Errorf("gap %d start = %d, want %d", i, got[i].start, want)
				}
			}
			for _, g := range got {
				if g.start < tc.start || g.end > tc.end {
					t.Errorf("gap %+v escapes the parent range %d-%d", g, tc.start, tc.end)
				}
			}
		})
	}
}

func TestCovFindFreeRangesHandlesFullIPv4Range(t *testing.T) {
	// A child ending at the maximum uint32 makes the cursor overflow to 0.
	got := findFreeRanges(0, ^uint32(0), []interval{{start: 1, end: ^uint32(0)}})
	if len(got) != 1 {
		t.Fatalf("gaps = %+v, want a single leading gap", got)
	}
	if got[0].start != 0 || got[0].end != 0 {
		t.Fatalf("gap = %+v, want just address 0", got[0])
	}
}

func TestCovCheckNamingFlagsMissingNameAndDescription(t *testing.T) {
	svc := NewAnalysisService(storage.NewMemoryStore())

	report := &ComplianceReport{}
	svc.checkNaming(domain.Pool{ID: 1, CIDR: "10.0.0.0/16"}, report)

	rules := map[string]ComplianceViolation{}
	for _, v := range report.Violations {
		rules[v.RuleID] = v
	}
	if _, ok := rules["NAME-001"]; !ok {
		t.Errorf("violations = %+v, want NAME-001 for a nameless pool", report.Violations)
	}
	if _, ok := rules["NAME-002"]; !ok {
		t.Errorf("violations = %+v, want NAME-002 for a pool with no description", report.Violations)
	}
	if report.TotalChecks != 2 {
		t.Errorf("TotalChecks = %d, want 2", report.TotalChecks)
	}
	if report.Warnings != 1 {
		t.Errorf("Warnings = %d, want 1 (only the missing name counts as a warning)", report.Warnings)
	}

	full := &ComplianceReport{}
	svc.checkNaming(domain.Pool{ID: 2, Name: "Named", Description: "described", CIDR: "10.0.0.0/16"}, full)
	if len(full.Violations) != 0 {
		t.Errorf("violations = %+v, want none for a fully described pool", full.Violations)
	}
}

func TestCovCheckRFC1918IgnoresUnparsableCIDR(t *testing.T) {
	svc := NewAnalysisService(storage.NewMemoryStore())

	report := &ComplianceReport{}
	svc.checkRFC1918(domain.Pool{ID: 1, Name: "Broken", CIDR: "nonsense"}, report)

	if len(report.Violations) != 0 {
		t.Fatalf("violations = %+v, want none for an unparsable CIDR", report.Violations)
	}
	if report.TotalChecks != 1 {
		t.Errorf("TotalChecks = %d, want the check to still be counted", report.TotalChecks)
	}
}

func TestCovCheckOverlapIgnoresUnparsableSiblings(t *testing.T) {
	svc := NewAnalysisService(storage.NewMemoryStore())
	parentID := int64(1)

	pool := domain.Pool{ID: 2, Name: "A", CIDR: "10.0.0.0/24", ParentID: &parentID}
	siblings := []domain.Pool{
		pool,
		{ID: 3, Name: "Broken", CIDR: "garbage", ParentID: &parentID},
	}

	report := &ComplianceReport{}
	svc.checkOverlap(context.Background(), pool, siblings, report)
	if len(report.Violations) != 0 {
		t.Fatalf("violations = %+v, want none when the sibling CIDR cannot be parsed", report.Violations)
	}
}

func TestCovCheckOverlapSkipsRootPools(t *testing.T) {
	svc := NewAnalysisService(storage.NewMemoryStore())

	root := domain.Pool{ID: 1, Name: "Root", CIDR: "10.0.0.0/16"}
	report := &ComplianceReport{}
	svc.checkOverlap(context.Background(), root, []domain.Pool{root}, report)

	if len(report.Violations) != 0 {
		t.Fatalf("violations = %+v, want none for a pool with no parent", report.Violations)
	}
	if report.TotalChecks != 1 {
		t.Errorf("TotalChecks = %d, want 1", report.TotalChecks)
	}
}

func TestCovCheckOverlapDetectsSiblingCollision(t *testing.T) {
	svc := NewAnalysisService(storage.NewMemoryStore())
	parentID := int64(1)

	pool := domain.Pool{ID: 2, Name: "A", CIDR: "10.0.0.0/24", ParentID: &parentID}
	sibling := domain.Pool{ID: 3, Name: "B", CIDR: "10.0.0.128/25", ParentID: &parentID}

	report := &ComplianceReport{}
	svc.checkOverlap(context.Background(), pool, []domain.Pool{pool, sibling}, report)

	if len(report.Violations) != 1 {
		t.Fatalf("violations = %+v, want exactly one overlap violation", report.Violations)
	}
	v := report.Violations[0]
	if v.RuleID != "OVERLAP-001" || v.Severity != "error" || v.PoolID != pool.ID {
		t.Fatalf("violation = %+v", v)
	}
	if report.Failed != 1 {
		t.Errorf("Failed = %d, want 1", report.Failed)
	}
	if !strings.Contains(v.Message, "10.0.0.128/25") {
		t.Errorf("message = %q, want it to name the overlapping sibling", v.Message)
	}
}

func TestCovCheckEmptyIgnoresChildrenLookupFailure(t *testing.T) {
	svc, wrapper, _ := covNewErrService(t)
	wrapper.childrenErr = errors.New("children unavailable")

	report := &ComplianceReport{}
	svc.checkEmpty(context.Background(), domain.Pool{ID: 1, Name: "Root", CIDR: "10.0.0.0/16", Type: domain.PoolTypeSupernet}, report)

	if len(report.Violations) != 0 {
		t.Fatalf("violations = %+v, want none when children cannot be read", report.Violations)
	}
	if report.TotalChecks != 1 {
		t.Errorf("TotalChecks = %d, want the check to still be counted", report.TotalChecks)
	}
}

func TestCovCheckComplianceSurfacesResolveError(t *testing.T) {
	svc, wrapper, _ := covNewErrService(t)
	wrapper.listErr = errors.New("db down")

	if _, err := svc.CheckCompliance(context.Background(), nil, false); err == nil {
		t.Fatal("CheckCompliance() error = nil, want the resolve failure")
	}
}

func TestCovAnalyzeFragmentationErrors(t *testing.T) {
	ctx := context.Background()

	t.Run("get pool error", func(t *testing.T) {
		svc, wrapper, _ := covNewErrService(t)
		wrapper.getErr = errors.New("db down")
		if _, err := svc.AnalyzeFragmentation(ctx, 1); err == nil || !strings.Contains(err.Error(), "get pool 1") {
			t.Fatalf("AnalyzeFragmentation() error = %v, want get pool failure", err)
		}
	})

	t.Run("pool not found", func(t *testing.T) {
		svc := NewAnalysisService(storage.NewMemoryStore())
		if _, err := svc.AnalyzeFragmentation(ctx, 999); !errors.Is(err, storage.ErrNotFound) {
			t.Fatalf("AnalyzeFragmentation() error = %v, want ErrNotFound", err)
		}
	})

	t.Run("children error", func(t *testing.T) {
		svc, wrapper, mem := covNewErrService(t)
		pool, err := mem.CreatePool(ctx, domain.CreatePool{Name: "Root", CIDR: "10.0.0.0/16"})
		if err != nil {
			t.Fatalf("CreatePool() error = %v", err)
		}
		wrapper.childrenErr = errors.New("children unavailable")
		if _, err := svc.AnalyzeFragmentation(ctx, pool.ID); err == nil ||
			!strings.Contains(err.Error(), "get children for pool") {
			t.Fatalf("AnalyzeFragmentation() error = %v, want children failure", err)
		}
	})
}

func TestCovAnalyzeFragmentationFailsWhenGapAnalysisFails(t *testing.T) {
	ctx := context.Background()
	st := storage.NewMemoryStore()
	svc := NewAnalysisService(st)

	// An unparsable parent CIDR makes the nested gap analysis fail, but only
	// once the pool has a child (childless pools short-circuit earlier).
	parent, err := st.CreatePool(ctx, domain.CreatePool{Name: "Broken", CIDR: "not-a-cidr"})
	if err != nil {
		t.Fatalf("CreatePool() error = %v", err)
	}
	parentID := parent.ID
	if _, err := st.CreatePool(ctx, domain.CreatePool{Name: "Child", CIDR: "10.0.0.0/24", ParentID: &parentID}); err != nil {
		t.Fatalf("CreatePool() error = %v", err)
	}

	if _, err := svc.AnalyzeFragmentation(ctx, parent.ID); err == nil ||
		!strings.Contains(err.Error(), "gap analysis for fragmentation") {
		t.Fatalf("AnalyzeFragmentation() error = %v, want a gap analysis failure", err)
	}
}

func TestCovAnalyzeFragmentationChildlessPoolIsUnfragmented(t *testing.T) {
	ctx := context.Background()
	st := storage.NewMemoryStore()
	svc := NewAnalysisService(st)

	pool, err := st.CreatePool(ctx, domain.CreatePool{Name: "Leaf", CIDR: "10.0.0.0/24"})
	if err != nil {
		t.Fatalf("CreatePool() error = %v", err)
	}

	result, err := svc.AnalyzeFragmentation(ctx, pool.ID)
	if err != nil {
		t.Fatalf("AnalyzeFragmentation() error = %v", err)
	}
	if result.Score != 0 {
		t.Errorf("Score = %d, want 0 for a pool with no children", result.Score)
	}
	if len(result.Issues) != 0 {
		t.Errorf("Issues = %+v, want none", result.Issues)
	}
	if result.PoolName != "Leaf" {
		t.Errorf("PoolName = %q, want Leaf", result.PoolName)
	}
}

func TestCovAnalyzeFragmentationFlagsMisalignedChildren(t *testing.T) {
	ctx := context.Background()
	st := storage.NewMemoryStore()
	svc := NewAnalysisService(st)

	parent, err := st.CreatePool(ctx, domain.CreatePool{Name: "Root", CIDR: "10.0.0.0/16", Type: domain.PoolTypeSupernet})
	if err != nil {
		t.Fatalf("CreatePool() error = %v", err)
	}
	parentID := parent.ID
	for _, c := range []struct{ name, cidr string }{
		{"a", "10.0.0.0/24"},
		{"b", "10.0.1.0/25"},
		{"c", "10.0.2.0/26"},
		{"bad", "unparsable"},
	} {
		if _, err := st.CreatePool(ctx, domain.CreatePool{Name: c.name, CIDR: c.cidr, ParentID: &parentID}); err != nil {
			t.Fatalf("CreatePool(%s) error = %v", c.name, err)
		}
	}

	result, err := svc.AnalyzeFragmentation(ctx, parent.ID)
	if err != nil {
		t.Fatalf("AnalyzeFragmentation() error = %v", err)
	}

	var misaligned *FragmentationIssue
	for i := range result.Issues {
		if result.Issues[i].Type == FragmentMisaligned {
			misaligned = &result.Issues[i]
			break
		}
	}
	if misaligned == nil {
		t.Fatalf("issues = %+v, want a misalignment issue for 3 distinct prefix lengths", result.Issues)
	}
	if !strings.Contains(misaligned.Description, "3 different prefix lengths") {
		t.Errorf("description = %q, want the unparsable child excluded from the count", misaligned.Description)
	}

	var standardise bool
	for _, r := range result.Recommendations {
		if strings.Contains(r, "Standardize subnet sizes") {
			standardise = true
		}
	}
	if !standardise {
		t.Errorf("recommendations = %v, want a standardisation recommendation", result.Recommendations)
	}
}

func TestCovSeverityForScore(t *testing.T) {
	tests := []struct {
		score float64
		want  string
	}{
		{1.0, "error"},
		{0.7, "error"},
		{0.69, "warning"},
		{0.4, "warning"},
		{0.39, "info"},
		{0.0, "info"},
	}
	for _, tc := range tests {
		if got := severityForScore(tc.score); got != tc.want {
			t.Errorf("severityForScore(%v) = %q, want %q", tc.score, got, tc.want)
		}
	}
}

func TestCovAnalyzeEmptyStoreIsPerfectlyHealthy(t *testing.T) {
	svc := NewAnalysisService(storage.NewMemoryStore())

	report, err := svc.Analyze(context.Background(), AnalysisRequest{})
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	if report.Summary.HealthScore != 100 {
		t.Errorf("HealthScore = %d, want 100 for an empty store", report.Summary.HealthScore)
	}
	if report.GeneratedAt.IsZero() {
		t.Error("GeneratedAt not set")
	}
	if len(report.GapAnalyses) != 0 {
		t.Errorf("GapAnalyses = %+v, want none", report.GapAnalyses)
	}
}

func TestCovAnalyzeDerivesRootPoolsWhenNoIDsGiven(t *testing.T) {
	ctx := context.Background()
	st := storage.NewMemoryStore()
	svc := NewAnalysisService(st)

	rootA, err := st.CreatePool(ctx, domain.CreatePool{
		Name: "RootA", CIDR: "10.0.0.0/16", Type: domain.PoolTypeSupernet, Description: "a",
	})
	if err != nil {
		t.Fatalf("CreatePool() error = %v", err)
	}
	rootAID := rootA.ID
	if _, err := st.CreatePool(ctx, domain.CreatePool{
		Name: "ChildA", CIDR: "10.0.1.0/24", ParentID: &rootAID, Type: domain.PoolTypeSubnet, Description: "ca",
	}); err != nil {
		t.Fatalf("CreatePool() error = %v", err)
	}
	if _, err := st.CreatePool(ctx, domain.CreatePool{
		Name: "RootB", CIDR: "172.16.0.0/16", Type: domain.PoolTypeSupernet, Description: "b",
	}); err != nil {
		t.Fatalf("CreatePool() error = %v", err)
	}

	report, err := svc.Analyze(ctx, AnalysisRequest{})
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	if len(report.GapAnalyses) != 2 {
		t.Fatalf("GapAnalyses = %d, want one per root pool", len(report.GapAnalyses))
	}
	if report.Fragmentation == nil {
		t.Error("Fragmentation = nil, want an analysis for the first root pool")
	}
	if report.Compliance == nil {
		t.Fatal("Compliance = nil")
	}
	if report.Summary.HealthScore < 0 || report.Summary.HealthScore > 100 {
		t.Errorf("HealthScore = %d, want 0-100", report.Summary.HealthScore)
	}
}

func TestCovAnalyzeFailsWhenGapAnalysisFails(t *testing.T) {
	ctx := context.Background()
	st := storage.NewMemoryStore()
	svc := NewAnalysisService(st)

	pool, err := st.CreatePool(ctx, domain.CreatePool{Name: "Broken", CIDR: "not-a-cidr"})
	if err != nil {
		t.Fatalf("CreatePool() error = %v", err)
	}

	_, err = svc.Analyze(ctx, AnalysisRequest{PoolIDs: []int64{pool.ID}})
	if err == nil || !strings.Contains(err.Error(), "gap analysis for pool") {
		t.Fatalf("Analyze() error = %v, want a gap analysis failure", err)
	}
}

func TestCovAnalyzeSurfacesResolveError(t *testing.T) {
	svc, wrapper, _ := covNewErrService(t)
	wrapper.listErr = errors.New("db down")

	if _, err := svc.Analyze(context.Background(), AnalysisRequest{}); err == nil {
		t.Fatal("Analyze() error = nil, want the resolve failure")
	}
}

func TestCovAnalyzeHealthScoreFloorsAtZero(t *testing.T) {
	ctx := context.Background()
	st := storage.NewMemoryStore()
	svc := NewAnalysisService(st)

	// Many non-RFC1918, undescribed, empty supernets drive deductions past 100.
	var ids []int64
	for i := range 30 {
		p, err := st.CreatePool(ctx, domain.CreatePool{
			Name: "P",
			CIDR: "8.8." + itoaByte(i) + ".0/24",
			Type: domain.PoolTypeSupernet,
		})
		if err != nil {
			t.Fatalf("CreatePool() error = %v", err)
		}
		ids = append(ids, p.ID)
	}

	report, err := svc.Analyze(ctx, AnalysisRequest{PoolIDs: ids})
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	if report.Summary.HealthScore != 0 {
		t.Fatalf("HealthScore = %d, want it floored at 0", report.Summary.HealthScore)
	}
}

func itoaByte(i int) string {
	if i == 0 {
		return "0"
	}
	var digits []byte
	for i > 0 {
		digits = append([]byte{byte('0' + i%10)}, digits...)
		i /= 10
	}
	return string(digits)
}
