package planning

import (
	"context"
	"net/netip"
	"testing"

	"cloudpam/internal/domain"
	"cloudpam/internal/storage"
)

func TestRangeToCIDRs(t *testing.T) {
	tests := []struct {
		name  string
		start uint32
		end   uint32
		want  []string
	}{
		{
			name:  "single /24",
			start: ipv4ToUint32(netip.MustParseAddr("10.0.0.0")),
			end:   ipv4ToUint32(netip.MustParseAddr("10.0.0.255")),
			want:  []string{"10.0.0.0/24"},
		},
		{
			name:  "single /32",
			start: ipv4ToUint32(netip.MustParseAddr("10.0.0.1")),
			end:   ipv4ToUint32(netip.MustParseAddr("10.0.0.1")),
			want:  []string{"10.0.0.1/32"},
		},
		{
			name:  "two /25s make a /24",
			start: ipv4ToUint32(netip.MustParseAddr("10.0.0.0")),
			end:   ipv4ToUint32(netip.MustParseAddr("10.0.0.255")),
			want:  []string{"10.0.0.0/24"},
		},
		{
			name:  "non-aligned range",
			start: ipv4ToUint32(netip.MustParseAddr("10.0.0.128")),
			end:   ipv4ToUint32(netip.MustParseAddr("10.0.1.127")),
			want:  []string{"10.0.0.128/25", "10.0.1.0/25"},
		},
		{
			name:  "small gap: 3 addresses",
			start: ipv4ToUint32(netip.MustParseAddr("10.0.0.1")),
			end:   ipv4ToUint32(netip.MustParseAddr("10.0.0.3")),
			want:  []string{"10.0.0.1/32", "10.0.0.2/31"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := rangeToCIDRs(tt.start, tt.end)
			if len(got) != len(tt.want) {
				t.Fatalf("got %d CIDRs, want %d: %v", len(got), len(tt.want), got)
			}
			for i, g := range got {
				if g.String() != tt.want[i] {
					t.Errorf("CIDR[%d] = %s, want %s", i, g.String(), tt.want[i])
				}
			}
		})
	}
}

func TestFindFreeRanges(t *testing.T) {
	parseIP := func(s string) uint32 { return ipv4ToUint32(netip.MustParseAddr(s)) }

	tests := []struct {
		name     string
		pStart   uint32
		pEnd     uint32
		children []interval
		wantGaps int
	}{
		{
			name:     "no children = entire range free",
			pStart:   parseIP("10.0.0.0"),
			pEnd:     parseIP("10.0.0.255"),
			children: nil,
			wantGaps: 1,
		},
		{
			name:   "one child at start, gap at end",
			pStart: parseIP("10.0.0.0"),
			pEnd:   parseIP("10.0.0.255"),
			children: []interval{
				{start: parseIP("10.0.0.0"), end: parseIP("10.0.0.127")},
			},
			wantGaps: 1,
		},
		{
			name:   "one child in middle, two gaps",
			pStart: parseIP("10.0.0.0"),
			pEnd:   parseIP("10.0.0.255"),
			children: []interval{
				{start: parseIP("10.0.0.64"), end: parseIP("10.0.0.127")},
			},
			wantGaps: 2,
		},
		{
			name:   "child covers entire parent",
			pStart: parseIP("10.0.0.0"),
			pEnd:   parseIP("10.0.0.255"),
			children: []interval{
				{start: parseIP("10.0.0.0"), end: parseIP("10.0.0.255")},
			},
			wantGaps: 0,
		},
		{
			name:   "overlapping children are merged",
			pStart: parseIP("10.0.0.0"),
			pEnd:   parseIP("10.0.0.255"),
			children: []interval{
				{start: parseIP("10.0.0.0"), end: parseIP("10.0.0.127")},
				{start: parseIP("10.0.0.64"), end: parseIP("10.0.0.191")},
			},
			wantGaps: 1, // gap at 192-255
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gaps := findFreeRanges(tt.pStart, tt.pEnd, tt.children)
			if len(gaps) != tt.wantGaps {
				t.Errorf("got %d gaps, want %d: %v", len(gaps), tt.wantGaps, gaps)
			}
		})
	}
}

func TestAnalyzeGaps(t *testing.T) {
	ctx := context.Background()
	store := storage.NewMemoryStore()

	// Create parent /16
	parent, err := store.CreatePool(ctx, domain.CreatePool{
		Name: "Corp Network",
		CIDR: "10.0.0.0/16",
		Type: domain.PoolTypeSupernet,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Create two /24 children
	parentID := parent.ID
	_, err = store.CreatePool(ctx, domain.CreatePool{
		Name:     "Subnet A",
		CIDR:     "10.0.0.0/24",
		ParentID: &parentID,
		Type:     domain.PoolTypeSubnet,
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = store.CreatePool(ctx, domain.CreatePool{
		Name:     "Subnet B",
		CIDR:     "10.0.1.0/24",
		ParentID: &parentID,
		Type:     domain.PoolTypeSubnet,
	})
	if err != nil {
		t.Fatal(err)
	}

	svc := NewAnalysisService(store)
	result, err := svc.AnalyzeGaps(ctx, parent.ID)
	if err != nil {
		t.Fatal(err)
	}

	if result.PoolID != parent.ID {
		t.Errorf("pool_id = %d, want %d", result.PoolID, parent.ID)
	}
	if result.TotalAddresses != 65536 { // /16 = 2^16
		t.Errorf("total_addresses = %d, want 65536", result.TotalAddresses)
	}
	if result.UsedAddresses != 512 { // 2 * /24 = 2*256
		t.Errorf("used_addresses = %d, want 512", result.UsedAddresses)
	}
	if result.FreeAddresses != 65024 {
		t.Errorf("free_addresses = %d, want 65024", result.FreeAddresses)
	}
	if len(result.AllocatedBlocks) != 2 {
		t.Errorf("allocated_blocks count = %d, want 2", len(result.AllocatedBlocks))
	}
	if len(result.AvailableBlocks) == 0 {
		t.Error("expected available blocks, got none")
	}
}

func TestAnalyzeGaps_NotFound(t *testing.T) {
	ctx := context.Background()
	store := storage.NewMemoryStore()
	svc := NewAnalysisService(store)

	_, err := svc.AnalyzeGaps(ctx, 999)
	if err == nil {
		t.Error("expected error for non-existent pool")
	}
}

func TestAnalyzeGaps_NoChildren(t *testing.T) {
	ctx := context.Background()
	store := storage.NewMemoryStore()

	parent, _ := store.CreatePool(ctx, domain.CreatePool{
		Name: "Empty Parent",
		CIDR: "192.168.0.0/24",
		Type: domain.PoolTypeSupernet,
	})

	svc := NewAnalysisService(store)
	result, err := svc.AnalyzeGaps(ctx, parent.ID)
	if err != nil {
		t.Fatal(err)
	}

	if result.UsedAddresses != 0 {
		t.Errorf("used_addresses = %d, want 0", result.UsedAddresses)
	}
	if result.FreeAddresses != 256 {
		t.Errorf("free_addresses = %d, want 256", result.FreeAddresses)
	}
	if len(result.AvailableBlocks) != 1 {
		t.Errorf("available_blocks = %d, want 1 (the whole /24)", len(result.AvailableBlocks))
	}
}
