package planning

import (
	"context"
	"net/netip"
	"reflect"
	"sync/atomic"
	"testing"

	"cloudpam/internal/domain"
	"cloudpam/internal/storage"
	"cloudpam/internal/validation"
)

func createPool(t *testing.T, st storage.Store, name, cidr string, parentID *int64) domain.Pool {
	t.Helper()
	pool, err := st.CreatePool(context.Background(), domain.CreatePool{
		Name: name, CIDR: cidr, ParentID: parentID, Type: domain.PoolTypeSubnet, Description: name,
	})
	if err != nil {
		t.Fatalf("CreatePool(%s) error = %v", cidr, err)
	}
	return pool
}

func generateAllocations(t *testing.T, svc *RecommendationService, poolID int64, desiredPrefixLen int) []domain.Recommendation {
	t.Helper()
	resp, err := svc.Generate(context.Background(), domain.GenerateRecommendationsRequest{
		PoolIDs: []int64{poolID}, DesiredPrefixLen: desiredPrefixLen,
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	var out []domain.Recommendation
	for _, r := range resp.Items {
		if r.Type == domain.RecommendationTypeAllocation {
			out = append(out, r)
		}
	}
	return out
}

func TestGenerate_DesiredPrefixLenSplitsEmptyPool(t *testing.T) {
	svc, st := setupRecService(t)
	pool := createPool(t, st, "Empty", "10.0.0.0/24", nil)

	recs := generateAllocations(t, svc, pool.ID, 26)

	want := []string{"10.0.0.0/26", "10.0.0.64/26", "10.0.0.128/26", "10.0.0.192/26"}
	if got := allocationCIDRs(recs); !reflect.DeepEqual(got, want) {
		t.Fatalf("allocation CIDRs = %v, want %v", got, want)
	}
	for _, r := range recs {
		if r.Priority != domain.RecommendationPriorityHigh {
			t.Errorf("%s priority = %s (score %d), want high for an exact size fit", r.SuggestedCIDR, r.Priority, r.Score)
		}
	}
}

func TestGenerate_DesiredPrefixLenSplitsPartialGap(t *testing.T) {
	svc, st := setupRecService(t)
	parent := createPool(t, st, "Corp", "10.0.0.0/24", nil)
	createPool(t, st, "Used", "10.0.0.0/25", &parent.ID)

	got := allocationCIDRs(generateAllocations(t, svc, parent.ID, 27))

	want := []string{"10.0.0.128/27", "10.0.0.160/27", "10.0.0.192/27", "10.0.0.224/27"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("allocation CIDRs = %v, want %v", got, want)
	}
}

func TestGenerate_DesiredPrefixLenKeepsSmallerFreeBlocksWhole(t *testing.T) {
	svc, st := setupRecService(t)
	parent := createPool(t, st, "Corp", "10.0.0.0/24", nil)
	for _, cidr := range []string{"10.0.0.0/25", "10.0.0.128/26", "10.0.0.192/27", "10.0.0.224/28"} {
		createPool(t, st, cidr, cidr, &parent.ID)
	}

	got := allocationCIDRs(generateAllocations(t, svc, parent.ID, 26))

	if want := []string{"10.0.0.240/28"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("allocation CIDRs = %v, want the free /28 offered whole %v", got, want)
	}
}

func TestGenerate_DesiredPrefixLenIsBoundedByMaxPrefixLength(t *testing.T) {
	svc, st := setupRecService(t)
	pool := createPool(t, st, "Small", "10.0.0.0/28", nil)

	got := allocationCIDRs(generateAllocations(t, svc, pool.ID, 32))

	if len(got) != 4 {
		t.Fatalf("got %d blocks %v, want the four /%ds of a /28", len(got), got, validation.MaxPrefixLength)
	}
	for _, c := range got {
		if bits := netip.MustParsePrefix(c).Bits(); bits != validation.MaxPrefixLength {
			t.Errorf("%s has prefix length %d, want the /%d bound", c, bits, validation.MaxPrefixLength)
		}
	}
}

func TestGenerate_DesiredPrefixLenNotLongerThanPoolFallsBackToHalves(t *testing.T) {
	svc, st := setupRecService(t)
	pool := createPool(t, st, "Empty", "10.0.0.0/24", nil)

	for _, desired := range []int{24, 20} {
		got := allocationCIDRs(generateAllocations(t, svc, pool.ID, desired))
		if want := []string{"10.0.0.0/25", "10.0.0.128/25"}; !reflect.DeepEqual(got, want) {
			t.Errorf("desired /%d: allocation CIDRs = %v, want the halves %v", desired, got, want)
		}
	}
}

func TestGenerate_DesiredPrefixLenCapsCandidatesPerPool(t *testing.T) {
	svc, st := setupRecService(t)
	pool := createPool(t, st, "Big", "10.0.0.0/16", nil)

	got := allocationCIDRs(generateAllocations(t, svc, pool.ID, 30))

	if len(got) != maxAllocationCandidates {
		t.Fatalf("got %d candidates, want the cap of %d", len(got), maxAllocationCandidates)
	}
	if first := netip.MustParsePrefix(got[0]); first.Addr() != netip.MustParseAddr("10.0.0.0") {
		t.Errorf("lowest candidate = %s, want the pool's first /30", got[0])
	}
	for _, c := range got {
		if netip.MustParsePrefix(c).Bits() != 30 {
			t.Errorf("%s is not a /30", c)
		}
	}
}

func TestGenerate_DesiredPrefixLenContiguityPrefersNeighbourOfChild(t *testing.T) {
	svc, st := setupRecService(t)
	parent := createPool(t, st, "Corp", "10.0.0.0/24", nil)
	createPool(t, st, "Used", "10.0.0.0/26", &parent.ID)

	scores := map[string]int{}
	for _, r := range generateAllocations(t, svc, parent.ID, 26) {
		scores[r.SuggestedCIDR] = r.Score
	}
	adjacent, distant := scores["10.0.0.64/26"], scores["10.0.0.192/26"]
	if adjacent != distant+20 {
		t.Fatalf("adjacent block scored %d and distant %d, want a 20 point contiguity bonus (scores %v)", adjacent, distant, scores)
	}
}

type childrenCountingStore struct {
	storage.Store
	calls atomic.Int64
}

func (s *childrenCountingStore) GetPoolChildren(ctx context.Context, parentID int64) ([]domain.Pool, error) {
	s.calls.Add(1)
	return s.Store.GetPoolChildren(ctx, parentID)
}

func TestGenerate_ChildFetchesDoNotScaleWithCandidates(t *testing.T) {
	mem := storage.NewMemoryStore()
	st := &childrenCountingStore{Store: mem}
	svc := NewRecommendationService(NewAnalysisService(st), storage.NewMemoryRecommendationStore(mem), st, storePlacer{mem})
	pool := createPool(t, st, "Big", "10.0.0.0/16", nil)

	halves := generateAllocations(t, svc, pool.ID, 0)
	callsForHalves := st.calls.Swap(0)
	many := generateAllocations(t, svc, pool.ID, 24)
	callsForMany := st.calls.Load()

	if len(many) <= len(halves) {
		t.Fatalf("expected more candidates for a /24 request (%d) than for halves (%d)", len(many), len(halves))
	}
	if callsForMany != callsForHalves {
		t.Fatalf("GetPoolChildren called %d times for %d candidates and %d times for %d, want the same count", callsForMany, len(many), callsForHalves, len(halves))
	}
}
