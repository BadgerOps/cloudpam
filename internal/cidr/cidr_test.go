package cidr

import (
	"net/netip"
	"testing"
)

func TestPrefixContains(t *testing.T) {
	tests := []struct {
		name  string
		outer string
		inner string
		want  bool
	}{
		{"parent contains child", "10.0.0.0/8", "10.1.0.0/16", true},
		{"parent contains exact match", "10.0.0.0/24", "10.0.0.0/24", true},
		{"parent contains deep child", "10.0.0.0/8", "10.1.2.0/24", true},
		{"child exceeds parent", "10.0.0.0/24", "10.0.1.0/24", false},
		{"disjoint prefixes", "10.0.0.0/8", "192.168.0.0/16", false},
		{"child wider than parent", "10.0.0.0/16", "10.0.0.0/8", false},
		{"single host in parent", "10.0.0.0/24", "10.0.0.5/32", true},
		{"single host outside parent", "10.0.0.0/24", "10.0.1.5/32", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			outer := netip.MustParsePrefix(tt.outer)
			inner := netip.MustParsePrefix(tt.inner)
			if got := PrefixContains(outer, inner); got != tt.want {
				t.Errorf("PrefixContains(%s, %s) = %v, want %v", tt.outer, tt.inner, got, tt.want)
			}
		})
	}
}

func TestPrefixContains_Invalid(t *testing.T) {
	valid := netip.MustParsePrefix("10.0.0.0/8")
	if PrefixContains(netip.Prefix{}, valid) {
		t.Error("expected false for invalid outer")
	}
	if PrefixContains(valid, netip.Prefix{}) {
		t.Error("expected false for invalid inner")
	}
}

func TestPrefixContainsAddr(t *testing.T) {
	tests := []struct {
		name   string
		prefix string
		addr   string
		want   bool
	}{
		{"addr in prefix", "10.0.0.0/24", "10.0.0.100", true},
		{"addr at start", "10.0.0.0/24", "10.0.0.0", true},
		{"addr at end", "10.0.0.0/24", "10.0.0.255", true},
		{"addr outside", "10.0.0.0/24", "10.0.1.0", false},
		{"addr in /32", "10.0.0.5/32", "10.0.0.5", true},
		{"addr not in /32", "10.0.0.5/32", "10.0.0.6", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := netip.MustParsePrefix(tt.prefix)
			a := netip.MustParseAddr(tt.addr)
			if got := PrefixContainsAddr(p, a); got != tt.want {
				t.Errorf("PrefixContainsAddr(%s, %s) = %v, want %v", tt.prefix, tt.addr, got, tt.want)
			}
		})
	}
}

func TestPrefixContainsAddr_Invalid(t *testing.T) {
	if PrefixContainsAddr(netip.Prefix{}, netip.MustParseAddr("10.0.0.1")) {
		t.Error("expected false for invalid prefix")
	}
	if PrefixContainsAddr(netip.MustParsePrefix("10.0.0.0/8"), netip.Addr{}) {
		t.Error("expected false for invalid addr")
	}
}

func TestParseCIDROrIP(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{"cidr prefix", "10.0.0.0/8", "10.0.0.0/8", false},
		{"cidr with host bits", "10.1.2.3/8", "10.0.0.0/8", false},
		{"bare ip becomes /32", "10.1.2.5", "10.1.2.5/32", false},
		{"bare ip loopback", "127.0.0.1", "127.0.0.1/32", false},
		{"invalid input", "notanip", "", true},
		{"empty string", "", "", true},
		{"ipv6 cidr rejected", "::1/128", "", true},
		{"ipv6 addr rejected", "::1", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseCIDROrIP(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("ParseCIDROrIP(%q) expected error, got %v", tt.input, got)
				}
				return
			}
			if err != nil {
				t.Errorf("ParseCIDROrIP(%q) unexpected error: %v", tt.input, err)
				return
			}
			if got.String() != tt.want {
				t.Errorf("ParseCIDROrIP(%q) = %s, want %s", tt.input, got, tt.want)
			}
		})
	}
}

func TestLastAddr(t *testing.T) {
	tests := []struct {
		prefix string
		want   string
	}{
		{"10.0.0.0/24", "10.0.0.255"},
		{"10.0.0.0/8", "10.255.255.255"},
		{"192.168.1.0/32", "192.168.1.0"},
		{"0.0.0.0/0", "255.255.255.255"},
	}
	for _, tt := range tests {
		t.Run(tt.prefix, func(t *testing.T) {
			p := netip.MustParsePrefix(tt.prefix)
			got := lastAddr(p)
			if got.String() != tt.want {
				t.Errorf("lastAddr(%s) = %s, want %s", tt.prefix, got, tt.want)
			}
		})
	}
}
