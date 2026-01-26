// Package validation provides input validation for CloudPAM API requests.
package validation

import (
	"errors"
	"fmt"
	"net/netip"
	"regexp"
	"strings"
)

// Validation error types for specific error handling.
var (
	ErrEmptyValue       = errors.New("value cannot be empty")
	ErrTooLong          = errors.New("value exceeds maximum length")
	ErrInvalidFormat    = errors.New("invalid format")
	ErrReservedRange    = errors.New("cidr uses reserved address range")
	ErrInvalidPrefix    = errors.New("invalid prefix length")
	ErrIPv6NotSupported = errors.New("ipv6 not supported")
)

// Constraints for validation.
const (
	MaxNameLength       = 255
	MaxAccountKeyLength = 128
	MinPrefixLength     = 8
	MaxPrefixLength     = 32
)

// Reserved IPv4 ranges that should not be used for allocation.
// These are based on IANA special-purpose registries.
var reservedIPv4Ranges = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),       // "This network" (RFC 791)
	netip.MustParsePrefix("127.0.0.0/8"),     // Loopback (RFC 1122)
	netip.MustParsePrefix("169.254.0.0/16"),  // Link-local (RFC 3927)
	netip.MustParsePrefix("224.0.0.0/4"),     // Multicast (RFC 5771)
	netip.MustParsePrefix("240.0.0.0/4"),     // Reserved for future use (RFC 1112)
	netip.MustParsePrefix("255.255.255.255/32"), // Broadcast
}

// accountKeyPattern matches valid account key formats:
// - AWS: "aws:" followed by 12 digits
// - GCP: "gcp:" followed by alphanumeric project ID (lowercase letters, digits, hyphens)
var accountKeyPattern = regexp.MustCompile(`^(aws:\d{12}|gcp:[a-z][a-z0-9-]{4,28}[a-z0-9])$`)

// CIDRError provides detailed CIDR validation error information.
type CIDRError struct {
	CIDR   string
	Reason string
	Err    error
}

func (e *CIDRError) Error() string {
	if e.Reason != "" {
		return fmt.Sprintf("invalid cidr %q: %s", e.CIDR, e.Reason)
	}
	return fmt.Sprintf("invalid cidr %q: %v", e.CIDR, e.Err)
}

func (e *CIDRError) Unwrap() error {
	return e.Err
}

// AccountKeyError provides detailed account key validation error information.
type AccountKeyError struct {
	Key    string
	Reason string
	Err    error
}

func (e *AccountKeyError) Error() string {
	if e.Reason != "" {
		return fmt.Sprintf("invalid account key %q: %s", e.Key, e.Reason)
	}
	return fmt.Sprintf("invalid account key %q: %v", e.Key, e.Err)
}

func (e *AccountKeyError) Unwrap() error {
	return e.Err
}

// NameError provides detailed name validation error information.
type NameError struct {
	Name   string
	Reason string
	Err    error
}

func (e *NameError) Error() string {
	if e.Reason != "" {
		return fmt.Sprintf("invalid name %q: %s", truncate(e.Name, 50), e.Reason)
	}
	return fmt.Sprintf("invalid name %q: %v", truncate(e.Name, 50), e.Err)
}

func (e *NameError) Unwrap() error {
	return e.Err
}

// ValidateCIDR validates a CIDR string for use in CloudPAM.
// It checks for:
// - Valid CIDR format (a.b.c.d/x)
// - IPv4 only (IPv6 not supported)
// - Not in reserved ranges (loopback, multicast, etc.)
// - Reasonable prefix length (between MinPrefixLength and MaxPrefixLength)
func ValidateCIDR(cidr string) error {
	cidr = strings.TrimSpace(cidr)
	if cidr == "" {
		return &CIDRError{CIDR: cidr, Reason: "cannot be empty", Err: ErrEmptyValue}
	}

	// Check for prefix notation
	if !strings.Contains(cidr, "/") {
		return &CIDRError{CIDR: cidr, Reason: "must be in a.b.c.d/x form", Err: ErrInvalidFormat}
	}

	// Parse the prefix
	pfx, err := netip.ParsePrefix(cidr)
	if err != nil {
		return &CIDRError{CIDR: cidr, Reason: "invalid cidr notation", Err: ErrInvalidFormat}
	}

	// IPv4 only
	if !pfx.Addr().Is4() {
		return &CIDRError{CIDR: cidr, Reason: "only ipv4 is supported", Err: ErrIPv6NotSupported}
	}

	// Check for reserved ranges first (security concern takes priority)
	for _, reserved := range reservedIPv4Ranges {
		if prefixOverlaps(pfx, reserved) {
			return &CIDRError{
				CIDR:   cidr,
				Reason: fmt.Sprintf("overlaps with reserved range %s", reserved),
				Err:    ErrReservedRange,
			}
		}
	}

	// Check prefix length bounds
	bits := pfx.Bits()
	if bits < MinPrefixLength {
		return &CIDRError{
			CIDR:   cidr,
			Reason: fmt.Sprintf("prefix length %d is too small (minimum is /%d)", bits, MinPrefixLength),
			Err:    ErrInvalidPrefix,
		}
	}
	if bits > MaxPrefixLength {
		return &CIDRError{
			CIDR:   cidr,
			Reason: fmt.Sprintf("prefix length %d is too large (maximum is /%d)", bits, MaxPrefixLength),
			Err:    ErrInvalidPrefix,
		}
	}

	return nil
}

// ValidateAccountKey validates an account key format.
// Valid formats:
// - AWS: "aws:123456789012" (aws: followed by 12-digit account ID)
// - GCP: "gcp:my-project-id" (gcp: followed by valid GCP project ID)
func ValidateAccountKey(key string) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return &AccountKeyError{Key: key, Reason: "cannot be empty", Err: ErrEmptyValue}
	}

	if len(key) > MaxAccountKeyLength {
		return &AccountKeyError{
			Key:    key,
			Reason: fmt.Sprintf("exceeds maximum length of %d characters", MaxAccountKeyLength),
			Err:    ErrTooLong,
		}
	}

	if !accountKeyPattern.MatchString(key) {
		return &AccountKeyError{
			Key:    key,
			Reason: "must be in format 'aws:123456789012' or 'gcp:project-id'",
			Err:    ErrInvalidFormat,
		}
	}

	return nil
}

// ValidateName validates a name field (pool name, account name, etc.).
// It checks for:
// - Non-empty (after trimming whitespace)
// - Not exceeding maximum length
func ValidateName(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return &NameError{Name: name, Reason: "cannot be empty", Err: ErrEmptyValue}
	}

	if len(name) > MaxNameLength {
		return &NameError{
			Name:   name,
			Reason: fmt.Sprintf("exceeds maximum length of %d characters", MaxNameLength),
			Err:    ErrTooLong,
		}
	}

	return nil
}

// prefixOverlaps checks if two prefixes overlap (either contains the other or are equal).
func prefixOverlaps(a, b netip.Prefix) bool {
	// Check if a contains b's first address or b contains a's first address
	return a.Contains(b.Addr()) || b.Contains(a.Addr())
}

// truncate shortens a string for display in error messages.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
