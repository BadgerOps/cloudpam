// Package validation provides input validation for CloudPAM API requests.
package validation

import (
	"errors"
	"fmt"
	"net/netip"
	"regexp"
	"strings"
	"unicode"
)

// Validation error types for specific error handling.
var (
	ErrEmptyValue       = errors.New("value cannot be empty")
	ErrTooLong          = errors.New("value exceeds maximum length")
	ErrInvalidFormat    = errors.New("invalid format")
	ErrReservedRange    = errors.New("cidr uses reserved address range")
	ErrInvalidPrefix    = errors.New("invalid prefix length")
	ErrIPv6NotSupported = errors.New("ipv6 not supported")
	ErrNotCanonical     = errors.New("cidr is not in canonical form")
	ErrControlCharacter = errors.New("value contains control characters")
	ErrInvalidProvider  = errors.New("invalid provider")
	ErrInvalidAccountID = errors.New("invalid account id format")
)

// Constraints for validation.
const (
	MaxNameLength       = 255
	MaxAccountKeyLength = 100 // Updated to 100 as per requirements
	MinPrefixLength     = 8
	MaxPrefixLength     = 30 // Reduced to /30 for typical use cases
)

// Reserved IPv4 ranges that should not be used for allocation.
// These are based on IANA special-purpose registries.
var reservedIPv4Ranges = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),          // "This network" (RFC 791)
	netip.MustParsePrefix("127.0.0.0/8"),        // Loopback (RFC 1122)
	netip.MustParsePrefix("169.254.0.0/16"),     // Link-local (RFC 3927)
	netip.MustParsePrefix("224.0.0.0/4"),        // Multicast (RFC 5771)
	netip.MustParsePrefix("240.0.0.0/4"),        // Reserved for future use (RFC 1112)
	netip.MustParsePrefix("255.255.255.255/32"), // Broadcast
}

// Account key patterns for each provider.
var (
	// AWS: 12 digits
	awsAccountPattern = regexp.MustCompile(`^\d{12}$`)
	// GCP: 6-30 chars, starts with lowercase letter, contains lowercase letters, digits, hyphens
	gcpProjectPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{4,28}[a-z0-9]$`)
	// Azure: UUID format (8-4-4-4-12 hex)
	azureSubscriptionPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	// On-prem: alphanumeric with hyphens/underscores, max 64 chars
	onpremIDPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,62}[a-zA-Z0-9]$|^[a-zA-Z0-9]$`)
)

// ValidProviders lists the supported cloud/on-prem providers.
var ValidProviders = []string{"aws", "gcp", "azure", "onprem"}

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

// CIDROptions provides options for CIDR validation.
type CIDROptions struct {
	// AllowReserved permits reserved IP ranges (loopback, multicast, etc.)
	AllowReserved bool
	// AllowNonCanonical permits CIDRs that are not in canonical form (e.g., 10.0.0.1/24)
	AllowNonCanonical bool
	// MinPrefix overrides the minimum prefix length (default: MinPrefixLength)
	MinPrefix int
	// MaxPrefix overrides the maximum prefix length (default: MaxPrefixLength)
	MaxPrefix int
}

// ValidateCIDR validates a CIDR string for use in CloudPAM.
// It checks for:
// - Valid CIDR format (a.b.c.d/x)
// - IPv4 only (IPv6 not supported)
// - Not in reserved ranges (loopback, multicast, etc.) unless allowed
// - Canonical form (network address matches CIDR) unless allowed
// - Reasonable prefix length (between MinPrefixLength and MaxPrefixLength)
func ValidateCIDR(cidr string) error {
	return ValidateCIDRWithOptions(cidr, CIDROptions{})
}

// ValidateCIDRWithOptions validates a CIDR string with custom options.
func ValidateCIDRWithOptions(cidr string, opts CIDROptions) error {
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

	// Check canonical form: the address should be the network address
	// (e.g., 10.0.0.1/24 should be 10.0.0.0/24)
	if !opts.AllowNonCanonical {
		canonical := pfx.Masked()
		if pfx.Addr() != canonical.Addr() {
			return &CIDRError{
				CIDR:   cidr,
				Reason: fmt.Sprintf("not in canonical form, should be %s", canonical),
				Err:    ErrNotCanonical,
			}
		}
	}

	// Check for reserved ranges (security concern takes priority)
	if !opts.AllowReserved {
		for _, reserved := range reservedIPv4Ranges {
			if prefixOverlaps(pfx, reserved) {
				return &CIDRError{
					CIDR:   cidr,
					Reason: fmt.Sprintf("overlaps with reserved range %s", reserved),
					Err:    ErrReservedRange,
				}
			}
		}
	}

	// Determine prefix bounds
	minPrefix := MinPrefixLength
	if opts.MinPrefix > 0 {
		minPrefix = opts.MinPrefix
	}
	maxPrefix := MaxPrefixLength
	if opts.MaxPrefix > 0 {
		maxPrefix = opts.MaxPrefix
	}

	// Check prefix length bounds
	bits := pfx.Bits()
	if bits < minPrefix {
		return &CIDRError{
			CIDR:   cidr,
			Reason: fmt.Sprintf("prefix length %d is too small (minimum is /%d)", bits, minPrefix),
			Err:    ErrInvalidPrefix,
		}
	}
	if bits > maxPrefix {
		return &CIDRError{
			CIDR:   cidr,
			Reason: fmt.Sprintf("prefix length %d is too large (maximum is /%d)", bits, maxPrefix),
			Err:    ErrInvalidPrefix,
		}
	}

	return nil
}

// ValidateAccountKey validates an account key format.
// Valid formats:
// - AWS: "aws:123456789012" (aws: followed by 12-digit account ID)
// - GCP: "gcp:my-project-id" (gcp: followed by valid GCP project ID, 6-30 chars)
// - Azure: "azure:xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx" (azure: followed by subscription UUID)
// - On-prem: "onprem:datacenter-id" (onprem: followed by alphanumeric ID, max 64 chars)
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

	// Split into provider:id
	parts := strings.SplitN(key, ":", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return &AccountKeyError{
			Key:    key,
			Reason: "must be in format 'provider:account_id' (e.g., 'aws:123456789012')",
			Err:    ErrInvalidFormat,
		}
	}

	provider := parts[0]
	accountID := parts[1]

	// Provider must be lowercase
	if provider != strings.ToLower(provider) {
		return &AccountKeyError{
			Key:    key,
			Reason: "provider must be lowercase",
			Err:    ErrInvalidProvider,
		}
	}

	// Validate provider
	validProvider := false
	for _, p := range ValidProviders {
		if provider == p {
			validProvider = true
			break
		}
	}
	if !validProvider {
		return &AccountKeyError{
			Key:    key,
			Reason: fmt.Sprintf("unknown provider %q, must be one of: %s", provider, strings.Join(ValidProviders, ", ")),
			Err:    ErrInvalidProvider,
		}
	}

	// Validate account ID based on provider
	switch provider {
	case "aws":
		if !awsAccountPattern.MatchString(accountID) {
			return &AccountKeyError{
				Key:    key,
				Reason: "aws account id must be exactly 12 digits",
				Err:    ErrInvalidAccountID,
			}
		}
	case "gcp":
		if !gcpProjectPattern.MatchString(accountID) {
			return &AccountKeyError{
				Key:    key,
				Reason: "gcp project id must be 6-30 lowercase alphanumeric chars starting with a letter",
				Err:    ErrInvalidAccountID,
			}
		}
	case "azure":
		if !azureSubscriptionPattern.MatchString(strings.ToLower(accountID)) {
			return &AccountKeyError{
				Key:    key,
				Reason: "azure subscription id must be a valid uuid",
				Err:    ErrInvalidAccountID,
			}
		}
	case "onprem":
		if len(accountID) > 64 {
			return &AccountKeyError{
				Key:    key,
				Reason: "onprem id exceeds maximum length of 64 characters",
				Err:    ErrTooLong,
			}
		}
		if !onpremIDPattern.MatchString(accountID) {
			return &AccountKeyError{
				Key:    key,
				Reason: "onprem id must be alphanumeric (hyphens and underscores allowed)",
				Err:    ErrInvalidAccountID,
			}
		}
	}

	return nil
}

// NameOptions provides options for name validation.
type NameOptions struct {
	// MaxLength overrides the maximum length (default: MaxNameLength)
	MaxLength int
	// MinLength sets a minimum length (default: 1)
	MinLength int
	// AllowControlChars permits control characters (default: false)
	AllowControlChars bool
}

// ValidateName validates a name field (pool name, account name, etc.).
// It checks for:
// - Non-empty (after trimming whitespace)
// - Not exceeding maximum length
// - No control characters
func ValidateName(name string) error {
	return ValidateNameWithOptions(name, NameOptions{})
}

// ValidateNameWithOptions validates a name field with custom options.
func ValidateNameWithOptions(name string, opts NameOptions) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return &NameError{Name: name, Reason: "cannot be empty", Err: ErrEmptyValue}
	}

	maxLen := MaxNameLength
	if opts.MaxLength > 0 {
		maxLen = opts.MaxLength
	}

	minLen := 1
	if opts.MinLength > 0 {
		minLen = opts.MinLength
	}

	if len(name) > maxLen {
		return &NameError{
			Name:   name,
			Reason: fmt.Sprintf("exceeds maximum length of %d characters", maxLen),
			Err:    ErrTooLong,
		}
	}

	if len(name) < minLen {
		return &NameError{
			Name:   name,
			Reason: fmt.Sprintf("must be at least %d characters", minLen),
			Err:    ErrEmptyValue,
		}
	}

	// Check for control characters (security concern)
	if !opts.AllowControlChars {
		for _, r := range name {
			// Allow common whitespace (space, but not other control chars)
			// unicode.IsControl includes \t, \n, \r, etc.
			if unicode.IsControl(r) {
				return &NameError{
					Name:   name,
					Reason: "contains control characters",
					Err:    ErrControlCharacter,
				}
			}
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
	// Ensure we have room for "..."
	if maxLen < 4 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}
