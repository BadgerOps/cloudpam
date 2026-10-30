package api

import (
	"context"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	stdhttp "net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"cloudpam/internal/domain"
	"cloudpam/internal/planning"
	"cloudpam/internal/storage"
)

type poolImportResult struct {
	Created int      `json:"created"`
	Skipped int      `json:"skipped"`
	Errors  []string `json:"errors"`
}

func importPoolsCSV(t *testing.T, srv *Server, body string) poolImportResult {
	t.Helper()
	rr := doJSON(t, srv.mux, stdhttp.MethodPost, "/api/v1/import/pools", body, stdhttp.StatusOK)
	var res poolImportResult
	if err := json.Unmarshal(rr.Body.Bytes(), &res); err != nil {
		t.Fatalf("unmarshal import result: %v", err)
	}
	return res
}

func storedCIDRs(t *testing.T, st storage.Store) map[string]bool {
	t.Helper()
	pools, err := st.ListPools(context.Background())
	if err != nil {
		t.Fatalf("list pools: %v", err)
	}
	out := make(map[string]bool, len(pools))
	for _, p := range pools {
		out[p.CIDR] = true
	}
	return out
}

func hasErrorContaining(errs []string, want string) bool {
	for _, e := range errs {
		if strings.Contains(e, want) {
			return true
		}
	}
	return false
}

func createChildPool(t *testing.T, srv *Server, parentID int64, cidr string) poolDTO {
	t.Helper()
	body := fmt.Sprintf(`{"name":"child-%s","cidr":%q,"parent_id":%d}`, strings.ReplaceAll(cidr, "/", "-"), cidr, parentID)
	rr := doJSON(t, srv.mux, stdhttp.MethodPost, "/api/v1/pools", body, stdhttp.StatusCreated)
	var p poolDTO
	if err := json.Unmarshal(rr.Body.Bytes(), &p); err != nil {
		t.Fatalf("unmarshal child: %v", err)
	}
	return p
}

func TestImportPools_ChildOutsideParentIsRowError(t *testing.T) {
	srv, st := setupTestServer()
	res := importPoolsCSV(t, srv, "id,name,cidr,parent_id\n1,root,10.0.0.0/16,\n2,stray,192.168.0.0/24,1\n")

	if res.Created != 1 || len(res.Errors) != 1 || !hasErrorContaining(res.Errors, "invalid sub-pool cidr") {
		t.Fatalf("want root created and one invalid sub-pool cidr row error, got %+v", res)
	}
	if storedCIDRs(t, st)["192.168.0.0/24"] {
		t.Fatal("a child outside its parent was stored")
	}
}

func TestImportPools_OverlappingSiblingIsRowError(t *testing.T) {
	srv, st := setupTestServer()
	root := createRootPool(t, srv, "10.0.0.0/8")
	createChildPool(t, srv, root.ID, "10.1.0.0/16")

	res := importPoolsCSV(t, srv, fmt.Sprintf("name,cidr,parent_id\nover,10.1.0.0/17,%d\nfree,10.2.0.0/16,%d\nroot-again,10.0.0.0/8,\n", root.ID, root.ID))

	if res.Created != 1 || res.Skipped != 1 || len(res.Errors) != 1 || !hasErrorContaining(res.Errors, "overlaps") {
		t.Fatalf("want free created, the duplicate root skipped and one overlap row error, got %+v", res)
	}
	if storedCIDRs(t, st)["10.1.0.0/17"] {
		t.Fatal("an overlapping sibling was stored")
	}
}

func TestApplyPlan_ChildOutsideParentIsRefused(t *testing.T) {
	ai, st, _ := setupAITestServerCov(t, &stubLLMProviderCov{available: true})
	body := `{"plan":{"pools":[
		{"ref":"root","name":"Root","cidr":"10.0.0.0/16","type":"supernet"},
		{"ref":"stray","name":"Stray","cidr":"192.168.0.0/24","type":"subnet","parent_ref":"root"}
	]}}`
	rr := assertStatusCov(t, doReqCov(t, ai.srv.mux, stdhttp.MethodPost, "/api/v1/ai/sessions/s/apply-plan", body), stdhttp.StatusBadRequest)
	if !strings.Contains(rr.Body.String(), "invalid sub-pool cidr") {
		t.Fatalf("want invalid sub-pool cidr, got %s", rr.Body.String())
	}
	if n := len(storedCIDRs(t, st)); n != 0 {
		t.Fatalf("refused plan stored %d pools", n)
	}
}

func TestApplyPlan_OverlapWithExistingPool(t *testing.T) {
	ai, st, _ := setupAITestServerCov(t, &stubLLMProviderCov{available: true})
	if _, err := st.CreatePool(t.Context(), domain.CreatePool{Name: "corp", CIDR: "10.0.0.0/8"}); err != nil {
		t.Fatal(err)
	}
	body := func(skip bool) string {
		return fmt.Sprintf(`{"plan":{"pools":[{"ref":"x","name":"X","cidr":"10.1.0.0/16","type":"region"}]},"skip_conflicts":%t}`, skip)
	}

	assertStatusCov(t, doReqCov(t, ai.srv.mux, stdhttp.MethodPost, "/api/v1/ai/sessions/s/apply-plan", body(false)), stdhttp.StatusConflict)
	if storedCIDRs(t, st)["10.1.0.0/16"] {
		t.Fatal("skip_conflicts:false stored an overlapping pool")
	}

	assertStatusCov(t, doReqCov(t, ai.srv.mux, stdhttp.MethodPost, "/api/v1/ai/sessions/s/apply-plan", body(true)), stdhttp.StatusOK)
	if !storedCIDRs(t, st)["10.1.0.0/16"] {
		t.Fatal("skip_conflicts:true did not store the pool")
	}
}

func discoveredForPlacementTest(accountID int64, typ domain.CloudResourceType, providerID, cidr string, parent *string) domain.DiscoveredResource {
	now := time.Now().UTC()
	return domain.DiscoveredResource{
		ID:               uuid.New(),
		AccountID:        accountID,
		Provider:         "aws",
		Region:           "us-east-1",
		ResourceType:     typ,
		ResourceID:       providerID,
		Name:             providerID,
		CIDR:             cidr,
		ParentResourceID: parent,
		Status:           domain.DiscoveryStatusActive,
		DiscoveredAt:     now,
		LastSeenAt:       now,
	}
}

func applyDiscoveryImportForTest(t *testing.T, d *DiscoveryServer, accountID int64, ids ...uuid.UUID) domain.DiscoveryImportApplyResponse {
	t.Helper()
	quoted := make([]string, len(ids))
	for i, id := range ids {
		quoted[i] = fmt.Sprintf("%q", id.String())
	}
	body := fmt.Sprintf(`{"account_id":%d,"resource_ids":[%s]}`, accountID, strings.Join(quoted, ","))
	rr := doJSON(t, d.srv.mux, stdhttp.MethodPost, "/api/v1/discovery/import/apply", body, stdhttp.StatusOK)
	var resp domain.DiscoveryImportApplyResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal apply: %v", err)
	}
	return resp
}

func TestDiscoveryImportApply_ChildOutsideParentIsItemError(t *testing.T) {
	d, st, ds, _ := setupDiscoveryTestServer()
	account, err := st.CreateAccount(t.Context(), domain.CreateAccount{Key: "aws:1", Name: "prod", Provider: "aws"})
	if err != nil {
		t.Fatal(err)
	}
	vpcID := "vpc-1"
	vpc := discoveredForPlacementTest(account.ID, domain.ResourceTypeVPC, vpcID, "10.0.0.0/16", nil)
	subnet := discoveredForPlacementTest(account.ID, domain.ResourceTypeSubnet, "subnet-1", "192.168.1.0/24", &vpcID)
	upsertDiscoveredForImportTest(t, ds, vpc)
	upsertDiscoveredForImportTest(t, ds, subnet)

	resp := applyDiscoveryImportForTest(t, d, account.ID, vpc.ID, subnet.ID)

	if resp.PoolsCreated != 1 || !hasErrorContaining(resp.Errors, "invalid sub-pool cidr") {
		t.Fatalf("want the vpc created and an invalid sub-pool cidr error for the subnet, got %+v", resp)
	}
	if storedCIDRs(t, st)["192.168.1.0/24"] {
		t.Fatal("a subnet outside its vpc pool was stored")
	}
}

func TestDiscoveryImportApply_OverlappingSiblingIsItemError(t *testing.T) {
	d, st, ds, _ := setupDiscoveryTestServer()
	root := createRootPool(t, d.srv, "10.0.0.0/8")
	createChildPool(t, d.srv, root.ID, "10.1.0.0/16")
	account, err := st.CreateAccount(t.Context(), domain.CreateAccount{Key: "aws:1", Name: "prod", Provider: "aws"})
	if err != nil {
		t.Fatal(err)
	}
	vpc := discoveredForPlacementTest(account.ID, domain.ResourceTypeVPC, "vpc-1", "10.1.0.0/17", nil)
	upsertDiscoveredForImportTest(t, ds, vpc)

	resp := applyDiscoveryImportForTest(t, d, account.ID, vpc.ID)

	if resp.PoolsCreated != 0 || !hasErrorContaining(resp.Errors, "overlaps") {
		t.Fatalf("want no pool created and an overlap error, got %+v", resp)
	}
	if storedCIDRs(t, st)["10.1.0.0/17"] {
		t.Fatal("an overlapping sibling was stored")
	}
}

func importDiscoveredSchemaForTest(t *testing.T, d *DiscoveryServer, accountID int64) discoveryImportResponse {
	t.Helper()
	rr := doJSON(t, d.srv.mux, stdhttp.MethodPost, "/api/v1/discovery/import", fmt.Sprintf(`{"account_id":%d}`, accountID), stdhttp.StatusOK)
	var resp discoveryImportResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal import: %v", err)
	}
	return resp
}

func TestImportDiscoveredSchema_ChildOutsideParentIsError(t *testing.T) {
	d, st, ds, _ := setupDiscoveryTestServer()
	account, err := st.CreateAccount(t.Context(), domain.CreateAccount{Key: "aws:1", Name: "prod", Provider: "aws"})
	if err != nil {
		t.Fatal(err)
	}
	vpcID := "vpc-1"
	upsertDiscoveredForImportTest(t, ds, discoveredForPlacementTest(account.ID, domain.ResourceTypeVPC, vpcID, "10.0.0.0/16", nil))
	upsertDiscoveredForImportTest(t, ds, discoveredForPlacementTest(account.ID, domain.ResourceTypeSubnet, "subnet-1", "192.168.1.0/24", &vpcID))

	resp := importDiscoveredSchemaForTest(t, d, account.ID)

	if resp.PoolsCreated != 1 || !hasErrorContaining(resp.Errors, "invalid sub-pool cidr") {
		t.Fatalf("want the vpc created and an invalid sub-pool cidr error for the subnet, got %+v", resp)
	}
	if storedCIDRs(t, st)["192.168.1.0/24"] {
		t.Fatal("a subnet outside its vpc pool was stored")
	}
}

func TestImportDiscoveredSchema_OverlappingSiblingIsError(t *testing.T) {
	d, st, ds, _ := setupDiscoveryTestServer()
	createRootPool(t, d.srv, "10.0.0.0/8")
	account, err := st.CreateAccount(t.Context(), domain.CreateAccount{Key: "aws:1", Name: "prod", Provider: "aws"})
	if err != nil {
		t.Fatal(err)
	}
	upsertDiscoveredForImportTest(t, ds, discoveredForPlacementTest(account.ID, domain.ResourceTypeVPC, "vpc-1", "10.1.0.0/16", nil))

	resp := importDiscoveredSchemaForTest(t, d, account.ID)

	if resp.PoolsCreated != 0 || !hasErrorContaining(resp.Errors, "overlaps") {
		t.Fatalf("want no pool created and an overlap error, got %+v", resp)
	}
	if storedCIDRs(t, st)["10.1.0.0/16"] {
		t.Fatal("an overlapping top-level pool was stored")
	}
}

func setupRecommendationApplyServer(t *testing.T) (*Server, *storage.MemoryStore, storage.RecommendationStore) {
	t.Helper()
	srv, st := setupTestServer()
	recStore := storage.NewMemoryRecommendationStore(st)
	recSvc := planning.NewRecommendationService(planning.NewAnalysisService(st), recStore, st, srv)
	NewRecommendationServer(srv, recSvc, recStore).RegisterRecommendationRoutes()
	return srv, st, recStore
}

func pendingAllocation(t *testing.T, recStore storage.RecommendationStore, poolID int64, cidr string) string {
	t.Helper()
	now := time.Now().UTC()
	rec := domain.Recommendation{
		ID:            uuid.New().String(),
		PoolID:        poolID,
		Type:          domain.RecommendationTypeAllocation,
		Status:        domain.RecommendationStatusPending,
		Priority:      domain.RecommendationPriorityMedium,
		Title:         "Allocate " + cidr,
		SuggestedCIDR: cidr,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := recStore.CreateRecommendation(context.Background(), rec); err != nil {
		t.Fatalf("create recommendation: %v", err)
	}
	return rec.ID
}

func requirePending(t *testing.T, recStore storage.RecommendationStore, id string) {
	t.Helper()
	rec, err := recStore.GetRecommendation(context.Background(), id)
	if err != nil {
		t.Fatalf("get recommendation: %v", err)
	}
	if rec.Status != domain.RecommendationStatusPending || rec.AppliedPoolID != nil {
		t.Fatalf("refused apply changed the recommendation: %+v", rec)
	}
}

func TestRecommendationApply_ChildOutsideParentIs400(t *testing.T) {
	srv, st, recStore := setupRecommendationApplyServer(t)
	root := createRootPool(t, srv, "10.0.0.0/24")
	id := pendingAllocation(t, recStore, root.ID, "192.168.0.0/28")

	rr := doJSON(t, srv.mux, stdhttp.MethodPost, "/api/v1/recommendations/"+id+"/apply", `{}`, stdhttp.StatusBadRequest)
	if !strings.Contains(rr.Body.String(), "invalid sub-pool cidr") {
		t.Fatalf("want invalid sub-pool cidr, got %s", rr.Body.String())
	}
	if storedCIDRs(t, st)["192.168.0.0/28"] {
		t.Fatal("a child outside its parent was stored")
	}
	requirePending(t, recStore, id)
}

func TestRecommendationApply_StaleSuggestionOverlappingSiblingIs409(t *testing.T) {
	srv, st, recStore := setupRecommendationApplyServer(t)
	root := createRootPool(t, srv, "10.0.0.0/24")
	id := pendingAllocation(t, recStore, root.ID, "10.0.0.128/25")
	createChildPool(t, srv, root.ID, "10.0.0.128/26")

	doJSON(t, srv.mux, stdhttp.MethodPost, "/api/v1/recommendations/"+id+"/apply", `{}`, stdhttp.StatusConflict)
	if storedCIDRs(t, st)["10.0.0.128/25"] {
		t.Fatal("a stale suggestion overlapping a sibling was stored")
	}
	requirePending(t, recStore, id)
}

func TestConcurrentPoolsAndCSVImportStoreOne(t *testing.T) {
	srv, st := setupSlowListServer()
	var posts []post
	for i := range 6 {
		posts = append(posts,
			post{"/api/v1/pools", fmt.Sprintf(`{"name":"p%d","cidr":"10.1.0.0/%d"}`, i, 16+i)},
			post{"/api/v1/import/pools", fmt.Sprintf("name,cidr\ns%d,10.1.0.0/%d\n", i, 22+i)},
		)
	}
	postTogether(srv, posts)
	pools, err := st.ListPools(t.Context())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(pools) != 1 {
		t.Fatalf("want 1 pool stored, got %d: %+v", len(pools), pools)
	}
}

func TestCreatePool_MissingParentIs400(t *testing.T) {
	srv, st := setupTestServer()
	rr := doJSON(t, srv.mux, stdhttp.MethodPost, "/api/v1/pools", `{"name":"orphan","cidr":"10.1.0.0/16","parent_id":99}`, stdhttp.StatusBadRequest)
	var got apiError
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Error != "parent not found" {
		t.Fatalf("want parent not found, got %+v", got)
	}
	if n := len(storedCIDRs(t, st)); n != 0 {
		t.Fatalf("stored %d pools", n)
	}
}

func TestImportPools_ReimportOfExportSkipsEveryRow(t *testing.T) {
	srv, _ := setupTestServer()
	root := createRootPool(t, srv, "10.0.0.0/8")
	child := createChildPool(t, srv, root.ID, "10.1.0.0/16")
	grand := createChildPool(t, srv, child.ID, "10.1.1.0/24")
	csv := fmt.Sprintf("id,name,cidr,parent_id\n%d,root,10.0.0.0/8,\n%d,child,10.1.0.0/16,%d\n%d,grand,10.1.1.0/24,%d\n",
		root.ID, child.ID, root.ID, grand.ID, child.ID)

	res := importPoolsCSV(t, srv, csv)

	if res.Created != 0 || res.Skipped != 3 || len(res.Errors) != 0 {
		t.Fatalf("want every row skipped as a duplicate, got %+v", res)
	}
}

func TestImportPools_BudgetSpentIs503(t *testing.T) {
	old := placementBudget
	placementBudget = 50 * time.Millisecond
	t.Cleanup(func() { placementBudget = old })
	st := storage.NewMemoryStore()
	gate := make(chan struct{})
	srv := NewServerWithSlog(stdhttp.NewServeMux(), gatedListStore{st, gate}, nil)
	srv.registerUnprotectedTestRoutes()

	held := make(chan struct{})
	go func() {
		defer close(held)
		req := httptest.NewRequest(stdhttp.MethodPost, "/api/v1/pools", strings.NewReader(`{"name":"held","cidr":"10.1.0.0/16"}`))
		srv.mux.ServeHTTP(httptest.NewRecorder(), req)
	}()
	for !srv.placeGate.held() {
		time.Sleep(5 * time.Millisecond)
	}

	rr := httptest.NewRecorder()
	srv.mux.ServeHTTP(rr, httptest.NewRequest(stdhttp.MethodPost, "/api/v1/import/pools", strings.NewReader("name,cidr\nqueued,10.2.0.0/16\n")))
	close(gate)
	<-held

	if rr.Code != stdhttp.StatusServiceUnavailable || rr.Header().Get("Retry-After") == "" {
		t.Fatalf("want 503 with Retry-After, got %d %s", rr.Code, rr.Body.String())
	}
	if storedCIDRs(t, st)["10.2.0.0/16"] {
		t.Fatal("an import past its budget stored a pool")
	}
}

func TestDiscoveryImportApply_SubnetGoesUnderItsVPC(t *testing.T) {
	d, st, ds, _ := setupDiscoveryTestServer()
	createRootPool(t, d.srv, "10.0.0.0/8")
	account, err := st.CreateAccount(t.Context(), domain.CreateAccount{Key: "aws:1", Name: "prod", Provider: "aws"})
	if err != nil {
		t.Fatal(err)
	}
	vpcID := "vpc-1"
	vpc := discoveredForPlacementTest(account.ID, domain.ResourceTypeVPC, vpcID, "10.1.0.0/16", nil)
	subnet := discoveredForPlacementTest(account.ID, domain.ResourceTypeSubnet, "subnet-1", "10.1.1.0/24", &vpcID)
	upsertDiscoveredForImportTest(t, ds, vpc)
	upsertDiscoveredForImportTest(t, ds, subnet)

	resp := applyDiscoveryImportForTest(t, d, account.ID, vpc.ID, subnet.ID)

	if resp.PoolsCreated != 2 || len(resp.Errors) != 0 {
		t.Fatalf("want the vpc and its subnet created, got %+v", resp)
	}
	pools, err := st.ListPools(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	byCIDR := map[string]domain.Pool{}
	for _, p := range pools {
		byCIDR[p.CIDR] = p
	}
	if got := byCIDR["10.1.1.0/24"].ParentID; got == nil || *got != byCIDR["10.1.0.0/16"].ID {
		t.Fatalf("subnet parent = %v, want the vpc pool %d", got, byCIDR["10.1.0.0/16"].ID)
	}
}

func TestImportDiscoveredSchema_SameShapeInTwoRegionsLinksToOnePool(t *testing.T) {
	d, st, ds, _ := setupDiscoveryTestServer()
	account, err := st.CreateAccount(t.Context(), domain.CreateAccount{Key: "aws:1", Name: "prod", Provider: "aws"})
	if err != nil {
		t.Fatal(err)
	}
	east, west := "vpc-east", "vpc-west"
	upsertDiscoveredForImportTest(t, ds, discoveredForPlacementTest(account.ID, domain.ResourceTypeVPC, east, "172.31.0.0/16", nil))
	upsertDiscoveredForImportTest(t, ds, discoveredForPlacementTest(account.ID, domain.ResourceTypeVPC, west, "172.31.0.0/16", nil))
	upsertDiscoveredForImportTest(t, ds, discoveredForPlacementTest(account.ID, domain.ResourceTypeSubnet, "subnet-east", "172.31.0.0/20", &east))
	upsertDiscoveredForImportTest(t, ds, discoveredForPlacementTest(account.ID, domain.ResourceTypeSubnet, "subnet-west", "172.31.0.0/20", &west))

	resp := importDiscoveredSchemaForTest(t, d, account.ID)

	if resp.PoolsCreated != 2 || resp.ResourcesLinked != 4 || len(resp.Errors) != 0 {
		t.Fatalf("want one vpc pool and one subnet pool shared by both regions, got %+v", resp)
	}
	if n := len(storedCIDRs(t, st)); n != 2 {
		t.Fatalf("want 2 pools stored, got %d", n)
	}
}

func TestPoolsAreCreatedOnlyByPlacement(t *testing.T) {
	root := filepath.Join("..", "..")
	allowed := filepath.Join("internal", "api", "pool_placement.go")
	fset := token.NewFileSet()
	for _, dir := range []string{"internal", "cmd"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(root, path)
			if entry.IsDir() && rel == filepath.Join("internal", "storage") {
				return filepath.SkipDir
			}
			if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") || rel == allowed {
				return nil
			}
			file, err := parser.ParseFile(fset, path, nil, 0)
			if err != nil {
				return err
			}
			ast.Inspect(file, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "CreatePool" {
					t.Errorf("%s calls CreatePool directly; create pools through placePools or Server.PlacePool so they get the containment, overlap and lock checks", fset.Position(call.Pos()))
				}
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}
