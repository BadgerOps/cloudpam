package planning

import (
	"encoding/binary"
	"math/bits"
	"net/netip"
)

// ipv4ToUint32 converts an IPv4 address to a uint32 in network byte order.
func ipv4ToUint32(a netip.Addr) uint32 {
	b := a.As4()
	return binary.BigEndian.Uint32(b[:])
}

// uint32ToIPv4 converts a uint32 to an IPv4 netip.Addr.
func uint32ToIPv4(u uint32) netip.Addr {
	var b [4]byte
	binary.BigEndian.PutUint32(b[:], u)
	return netip.AddrFrom4(b)
}

// prefixesOverlap reports whether two IPv4 prefixes share any addresses.
func prefixesOverlap(a, b netip.Prefix) bool {
	ai := prefixToInterval(a.Masked())
	bi := prefixToInterval(b.Masked())
	return ai.end >= bi.start && bi.end >= ai.start
}

// rangeToCIDRs decomposes an inclusive uint32 range [start, end] into the
// minimal set of CIDR-aligned prefixes that exactly cover the range.
func rangeToCIDRs(start, end uint32) []netip.Prefix {
	var result []netip.Prefix
	for start <= end {
		// Largest power-of-two block starting at 'start' that fits alignment.
		// Alignment: the number of trailing zeros in start determines max block size.
		maxBits := 32
		if start != 0 {
			maxBits = bits.TrailingZeros32(start)
		}
		// Don't exceed remaining range.
		remaining := uint64(end) - uint64(start) + 1
		fitBits := 63 - bits.LeadingZeros64(remaining) // floor(log2(remaining))
		if fitBits > maxBits {
			fitBits = maxBits
		}
		prefixLen := 32 - fitBits
		result = append(result, netip.PrefixFrom(uint32ToIPv4(start), prefixLen))
		blockSize := uint32(1) << fitBits
		start += blockSize
		if start == 0 { // overflow
			break
		}
	}
	return result
}

// isRFC1918 reports whether the prefix falls entirely within RFC 1918 private space.
func isRFC1918(p netip.Prefix) bool {
	rfc1918 := []netip.Prefix{
		netip.MustParsePrefix("10.0.0.0/8"),
		netip.MustParsePrefix("172.16.0.0/12"),
		netip.MustParsePrefix("192.168.0.0/16"),
	}
	pi := prefixToInterval(p.Masked())
	for _, r := range rfc1918 {
		ri := prefixToInterval(r)
		if pi.start >= ri.start && pi.end <= ri.end {
			return true
		}
	}
	return false
}

// prefixAddressCount returns the total number of addresses in a prefix.
func prefixAddressCount(p netip.Prefix) uint64 {
	return 1 << (32 - p.Bits())
}
