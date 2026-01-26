package validation

import (
	"errors"
	"testing"
)

func TestValidateCIDR(t *testing.T) {
	tests := []struct {
		name    string
		cidr    string
		wantErr error
	}{
		// Valid CIDRs
		{name: "valid /16", cidr: "10.0.0.0/16", wantErr: nil},
		{name: "valid /24", cidr: "192.168.1.0/24", wantErr: nil},
		{name: "valid /8", cidr: "10.0.0.0/8", wantErr: nil},
		{name: "valid /32", cidr: "10.0.0.1/32", wantErr: nil},
		{name: "valid with whitespace", cidr: "  10.0.0.0/16  ", wantErr: nil},
		{name: "valid private range", cidr: "172.16.0.0/12", wantErr: nil},

		// Empty/invalid format
		{name: "empty string", cidr: "", wantErr: ErrEmptyValue},
		{name: "whitespace only", cidr: "   ", wantErr: ErrEmptyValue},
		{name: "no prefix", cidr: "10.0.0.0", wantErr: ErrInvalidFormat},
		{name: "invalid ip", cidr: "256.0.0.0/16", wantErr: ErrInvalidFormat},
		{name: "invalid prefix", cidr: "10.0.0.0/33", wantErr: ErrInvalidFormat},
		{name: "garbage", cidr: "not-a-cidr", wantErr: ErrInvalidFormat},

		// IPv6 not supported
		{name: "ipv6", cidr: "2001:db8::/32", wantErr: ErrIPv6NotSupported},
		{name: "ipv6 loopback", cidr: "::1/128", wantErr: ErrIPv6NotSupported},

		// Reserved ranges
		{name: "loopback /8", cidr: "127.0.0.0/8", wantErr: ErrReservedRange},
		{name: "loopback subset", cidr: "127.0.0.1/32", wantErr: ErrReservedRange},
		{name: "this network", cidr: "0.0.0.0/8", wantErr: ErrReservedRange},
		{name: "link-local", cidr: "169.254.0.0/16", wantErr: ErrReservedRange},
		{name: "link-local subset", cidr: "169.254.1.0/24", wantErr: ErrReservedRange},
		{name: "multicast", cidr: "224.0.0.0/4", wantErr: ErrReservedRange},
		{name: "multicast subset", cidr: "239.255.255.0/24", wantErr: ErrReservedRange},
		{name: "reserved future", cidr: "240.0.0.0/4", wantErr: ErrReservedRange},
		{name: "reserved future subset", cidr: "250.0.0.0/8", wantErr: ErrReservedRange},
		{name: "broadcast", cidr: "255.255.255.255/32", wantErr: ErrReservedRange},

		// Prefix length bounds
		{name: "prefix too small /7", cidr: "8.0.0.0/7", wantErr: ErrInvalidPrefix},
		{name: "prefix too small /1 overlaps reserved", cidr: "0.0.0.0/1", wantErr: ErrReservedRange},
		{name: "prefix too small /4", cidr: "64.0.0.0/4", wantErr: ErrInvalidPrefix},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateCIDR(tt.cidr)
			if tt.wantErr == nil {
				if err != nil {
					t.Errorf("ValidateCIDR(%q) = %v, want nil", tt.cidr, err)
				}
				return
			}
			if err == nil {
				t.Errorf("ValidateCIDR(%q) = nil, want error containing %v", tt.cidr, tt.wantErr)
				return
			}
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("ValidateCIDR(%q) = %v, want error containing %v", tt.cidr, err, tt.wantErr)
			}
		})
	}
}

func TestValidateAccountKey(t *testing.T) {
	tests := []struct {
		name    string
		key     string
		wantErr error
	}{
		// Valid AWS keys
		{name: "valid aws 12 digits", key: "aws:123456789012", wantErr: nil},
		{name: "valid aws all zeros", key: "aws:000000000000", wantErr: nil},
		{name: "valid aws all nines", key: "aws:999999999999", wantErr: nil},
		{name: "valid aws with whitespace", key: "  aws:123456789012  ", wantErr: nil},

		// Valid GCP keys
		{name: "valid gcp simple", key: "gcp:my-project", wantErr: nil},
		{name: "valid gcp with numbers", key: "gcp:project-123", wantErr: nil},
		{name: "valid gcp min length", key: "gcp:a12345", wantErr: nil},
		{name: "valid gcp max length", key: "gcp:a23456789012345678901234567890", wantErr: nil},

		// Empty/whitespace
		{name: "empty string", key: "", wantErr: ErrEmptyValue},
		{name: "whitespace only", key: "   ", wantErr: ErrEmptyValue},

		// Invalid AWS format
		{name: "aws too few digits", key: "aws:12345678901", wantErr: ErrInvalidFormat},
		{name: "aws too many digits", key: "aws:1234567890123", wantErr: ErrInvalidFormat},
		{name: "aws with letters", key: "aws:12345678901a", wantErr: ErrInvalidFormat},
		{name: "aws missing colon", key: "aws123456789012", wantErr: ErrInvalidFormat},
		{name: "aws uppercase", key: "AWS:123456789012", wantErr: ErrInvalidFormat},

		// Invalid GCP format
		{name: "gcp starts with number", key: "gcp:1project", wantErr: ErrInvalidFormat},
		{name: "gcp starts with hyphen", key: "gcp:-project", wantErr: ErrInvalidFormat},
		{name: "gcp ends with hyphen", key: "gcp:project-", wantErr: ErrInvalidFormat},
		{name: "gcp too short", key: "gcp:ab12", wantErr: ErrInvalidFormat},
		{name: "gcp uppercase", key: "gcp:My-Project", wantErr: ErrInvalidFormat},
		{name: "gcp underscore", key: "gcp:my_project", wantErr: ErrInvalidFormat},
		{name: "gcp missing colon", key: "gcpmy-project", wantErr: ErrInvalidFormat},
		{name: "GCP uppercase prefix", key: "GCP:my-project", wantErr: ErrInvalidFormat},

		// Invalid provider
		{name: "unknown provider", key: "azure:subscription-id", wantErr: ErrInvalidFormat},
		{name: "no provider", key: "123456789012", wantErr: ErrInvalidFormat},
		{name: "just colon", key: ":", wantErr: ErrInvalidFormat},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateAccountKey(tt.key)
			if tt.wantErr == nil {
				if err != nil {
					t.Errorf("ValidateAccountKey(%q) = %v, want nil", tt.key, err)
				}
				return
			}
			if err == nil {
				t.Errorf("ValidateAccountKey(%q) = nil, want error containing %v", tt.key, tt.wantErr)
				return
			}
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("ValidateAccountKey(%q) = %v, want error containing %v", tt.key, err, tt.wantErr)
			}
		})
	}
}

func TestValidateAccountKey_TooLong(t *testing.T) {
	// Create a key that exceeds MaxAccountKeyLength
	longKey := "aws:" + string(make([]byte, MaxAccountKeyLength+1))
	for i := range longKey[4:] {
		longKey = longKey[:4+i] + "0" + longKey[5+i:]
	}
	// Actually make it too long with valid prefix
	longKey = "aws:1234567890120000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000"

	err := ValidateAccountKey(longKey)
	if err == nil {
		t.Error("ValidateAccountKey(long key) = nil, want ErrTooLong")
		return
	}
	if !errors.Is(err, ErrTooLong) {
		t.Errorf("ValidateAccountKey(long key) = %v, want ErrTooLong", err)
	}
}

func TestValidateName(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr error
	}{
		// Valid names
		{name: "simple name", input: "Production VPC", wantErr: nil},
		{name: "short name", input: "A", wantErr: nil},
		{name: "name with numbers", input: "VPC-123", wantErr: nil},
		{name: "name with special chars", input: "Prod_VPC-East (Primary)", wantErr: nil},
		{name: "name with unicode", input: "生产环境", wantErr: nil},
		{name: "name with whitespace trimmed", input: "  My VPC  ", wantErr: nil},

		// Empty/whitespace
		{name: "empty string", input: "", wantErr: ErrEmptyValue},
		{name: "whitespace only", input: "   ", wantErr: ErrEmptyValue},
		{name: "tabs only", input: "\t\t", wantErr: ErrEmptyValue},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateName(tt.input)
			if tt.wantErr == nil {
				if err != nil {
					t.Errorf("ValidateName(%q) = %v, want nil", tt.input, err)
				}
				return
			}
			if err == nil {
				t.Errorf("ValidateName(%q) = nil, want error containing %v", tt.input, tt.wantErr)
				return
			}
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("ValidateName(%q) = %v, want error containing %v", tt.input, err, tt.wantErr)
			}
		})
	}
}

func TestValidateName_TooLong(t *testing.T) {
	// Create a name that exceeds MaxNameLength
	longName := make([]byte, MaxNameLength+1)
	for i := range longName {
		longName[i] = 'a'
	}

	err := ValidateName(string(longName))
	if err == nil {
		t.Error("ValidateName(long name) = nil, want ErrTooLong")
		return
	}
	if !errors.Is(err, ErrTooLong) {
		t.Errorf("ValidateName(long name) = %v, want ErrTooLong", err)
	}
}

func TestCIDRError(t *testing.T) {
	err := &CIDRError{CIDR: "127.0.0.0/8", Reason: "loopback range", Err: ErrReservedRange}
	msg := err.Error()
	if msg == "" {
		t.Error("CIDRError.Error() returned empty string")
	}
	if !errors.Is(err, ErrReservedRange) {
		t.Error("CIDRError should unwrap to ErrReservedRange")
	}
}

func TestAccountKeyError(t *testing.T) {
	err := &AccountKeyError{Key: "invalid", Reason: "bad format", Err: ErrInvalidFormat}
	msg := err.Error()
	if msg == "" {
		t.Error("AccountKeyError.Error() returned empty string")
	}
	if !errors.Is(err, ErrInvalidFormat) {
		t.Error("AccountKeyError should unwrap to ErrInvalidFormat")
	}
}

func TestNameError(t *testing.T) {
	err := &NameError{Name: "test", Reason: "too short", Err: ErrEmptyValue}
	msg := err.Error()
	if msg == "" {
		t.Error("NameError.Error() returned empty string")
	}
	if !errors.Is(err, ErrEmptyValue) {
		t.Error("NameError should unwrap to ErrEmptyValue")
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		input  string
		maxLen int
		want   string
	}{
		{"short", 10, "short"},
		{"exactly10!", 10, "exactly10!"},
		{"this is a long string", 10, "this is..."},
		{"", 5, ""},
	}

	for _, tt := range tests {
		got := truncate(tt.input, tt.maxLen)
		if got != tt.want {
			t.Errorf("truncate(%q, %d) = %q, want %q", tt.input, tt.maxLen, got, tt.want)
		}
	}
}
