// Package cidr provides reusable CIDR math utilities for containment checks,
// overlap detection, and address parsing.
package cidr

import (
	"fmt"
	"net/netip"
)

// PrefixContains reports whether outer fully contains inner.
// Both prefixes must be valid IPv4 prefixes; returns false otherwise.
func PrefixContains(outer, inner netip.Prefix) bool {
	if !outer.IsValid() || !inner.IsValid() {
		return false
	}
	if !outer.Addr().Is4() || !inner.Addr().Is4() {
		return false
	}
	// inner must have equal or longer prefix length
	if inner.Bits() < outer.Bits() {
		return false
	}
	// both first and last address of inner must be within outer
	return outer.Contains(inner.Masked().Addr()) && outer.Contains(lastAddr(inner))
}

// PrefixContainsAddr reports whether prefix contains addr.
func PrefixContainsAddr(prefix netip.Prefix, addr netip.Addr) bool {
	if !prefix.IsValid() || !addr.IsValid() {
		return false
	}
	return prefix.Contains(addr)
}

// ParseCIDROrIP parses a string as either a CIDR prefix ("10.0.0.0/8")
// or a bare IP address ("10.1.2.5" → 10.1.2.5/32).
func ParseCIDROrIP(s string) (netip.Prefix, error) {
	// Try CIDR first
	if p, err := netip.ParsePrefix(s); err == nil {
		if !p.Addr().Is4() {
			return netip.Prefix{}, fmt.Errorf("only IPv4 supported: %s", s)
		}
		return p.Masked(), nil
	}
	// Try bare IP
	if a, err := netip.ParseAddr(s); err == nil {
		if !a.Is4() {
			return netip.Prefix{}, fmt.Errorf("only IPv4 supported: %s", s)
		}
		return netip.PrefixFrom(a, 32), nil
	}
	return netip.Prefix{}, fmt.Errorf("invalid CIDR or IP: %q", s)
}

// lastAddr returns the last (broadcast) address in a prefix.
func lastAddr(p netip.Prefix) netip.Addr {
	a4 := p.Masked().Addr().As4()
	bits := p.Bits()
	// set all host bits to 1
	hostBits := 32 - bits
	for i := 0; i < hostBits; i++ {
		byteIdx := 3 - i/8
		bitIdx := i % 8
		a4[byteIdx] |= 1 << bitIdx
	}
	return netip.AddrFrom4(a4)
}
