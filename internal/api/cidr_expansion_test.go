package api

import (
	"net/http"
	"strings"
	"testing"
)

// TestComputeSubnetsRejectsUnboundedExpansion covers the case where a caller
// omits page_size (or asks for "all") on a parent wide enough that full
// expansion would allocate millions of strings per request.
func TestComputeSubnetsRejectsUnboundedExpansion(t *testing.T) {
	// 10.0.0.0/8 into /30s is 2^22 = 4,194,304 blocks.
	_, _, _, err := computeSubnetsIPv4Window("10.0.0.0/8", 30, 0, 0)
	if err == nil {
		t.Fatal("expected an error for an unpaginated expansion above the limit")
	}
	if !strings.Contains(err.Error(), "page_size") {
		t.Errorf("error should tell the caller to paginate, got: %v", err)
	}
}

// TestComputeSubnetsAllowsUnboundedExpansionAtOrBelowLimit keeps the existing
// "page_size=all" behaviour working for reasonably sized pools.
func TestComputeSubnetsAllowsUnboundedExpansionAtOrBelowLimit(t *testing.T) {
	// 10.0.0.0/16 into /32s is exactly 65536 blocks, right at the limit.
	blocks, _, total, err := computeSubnetsIPv4Window("10.0.0.0/16", 32, 0, 0)
	if err != nil {
		t.Fatalf("expansion at the limit should be allowed: %v", err)
	}
	if len(blocks) != MaxSubnetExpansion || total != MaxSubnetExpansion {
		t.Errorf("len(blocks) = %d, total = %d, want %d", len(blocks), total, MaxSubnetExpansion)
	}
}

// TestComputeSubnetsClampsOversizedPageSize checks an explicit page_size cannot
// be used to bypass the cap.
func TestComputeSubnetsClampsOversizedPageSize(t *testing.T) {
	blocks, _, total, err := computeSubnetsIPv4Window("10.0.0.0/8", 30, 0, 1_000_000)
	if err != nil {
		t.Fatalf("paginated request should succeed: %v", err)
	}
	if len(blocks) != MaxSubnetExpansion {
		t.Errorf("len(blocks) = %d, want it clamped to %d", len(blocks), MaxSubnetExpansion)
	}
	// The reported total still describes the whole space, so clients can page.
	if total != 1<<22 {
		t.Errorf("total = %d, want %d", total, 1<<22)
	}
}

// TestBlocksEndpointRejectsUnboundedExpansion exercises the same guard through
// the HTTP handler.
func TestBlocksEndpointRejectsUnboundedExpansion(t *testing.T) {
	srv, _ := setupTestServer()

	body := `{"name":"wide","cidr":"10.0.0.0/8"}`
	rr := doReqCov(t, srv.mux, http.MethodPost, "/api/v1/pools", body)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create pool: status %d body %s", rr.Code, rr.Body.String())
	}

	rr = doReqCov(t, srv.mux, http.MethodGet, "/api/v1/pools/1/blocks?new_prefix_len=30", "")
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body: %s", rr.Code, rr.Body.String())
	}

	// A paginated request for the same pool still works.
	rr = doReqCov(t, srv.mux, http.MethodGet, "/api/v1/pools/1/blocks?new_prefix_len=30&page_size=50&page=1", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("paginated status = %d, want 200; body: %s", rr.Code, rr.Body.String())
	}
}
