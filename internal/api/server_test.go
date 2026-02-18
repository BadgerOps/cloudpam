package api

import (
	"testing"
)

func TestComputeSubnetsIPv4Window_Basics(t *testing.T) {
	blocks, hosts, total, err := computeSubnetsIPv4Window("10.0.0.0/16", 24, 0, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if total != 256 {
		t.Fatalf("expected total=256, got %d", total)
	}
	if len(blocks) != 256 {
		t.Fatalf("expected 256 blocks, got %d", len(blocks))
	}
	if hosts != 254 { // /24 usable hosts
		t.Fatalf("expected hosts=254, got %d", hosts)
	}
	// spot check first and last
	if blocks[0] != "10.0.0.0/24" {
		t.Fatalf("first block mismatch: %s", blocks[0])
	}
	if blocks[len(blocks)-1] != "10.0.255.0/24" {
		t.Fatalf("last block mismatch: %s", blocks[len(blocks)-1])
	}
}

func TestComputeSubnetsIPv4Window_Paged(t *testing.T) {
	blocks, hosts, total, err := computeSubnetsIPv4Window("192.168.0.0/16", 20, 4, 3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if total != 16 { // 2^(20-16)
		t.Fatalf("expected total=16, got %d", total)
	}
	if len(blocks) != 3 {
		t.Fatalf("expected 3 blocks page, got %d", len(blocks))
	}
	if hosts != 4094 { // /20 usable hosts (4096-2)
		t.Fatalf("expected hosts=4094, got %d", hosts)
	}
	// page starting at 4th index (0-based)
	if blocks[0] != "192.168.64.0/20" {
		t.Fatalf("unexpected first paged block: %s", blocks[0])
	}
}

func TestValidateChildCIDR(t *testing.T) {
	if err := validateChildCIDR("10.0.0.0/16", "10.0.1.0/24"); err != nil {
		t.Fatalf("expected valid child, got %v", err)
	}
	if err := validateChildCIDR("10.0.0.0/16", "10.1.0.0/24"); err == nil {
		t.Fatalf("expected error for child outside parent")
	}
}
