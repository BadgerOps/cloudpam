package domain

import "testing"

func TestIsValidPoolType(t *testing.T) {
	tests := []struct {
		poolType PoolType
		valid    bool
	}{
		{PoolTypeSupernet, true},
		{PoolTypeRegion, true},
		{PoolTypeEnvironment, true},
		{PoolTypeVPC, true},
		{PoolTypeSubnet, true},
		{"invalid", false},
		{"", false},
		{"SUPERNET", false}, // case sensitive
	}

	for _, tt := range tests {
		t.Run(string(tt.poolType), func(t *testing.T) {
			if got := IsValidPoolType(tt.poolType); got != tt.valid {
				t.Errorf("IsValidPoolType(%q) = %v, want %v", tt.poolType, got, tt.valid)
			}
		})
	}
}

func TestIsValidPoolStatus(t *testing.T) {
	tests := []struct {
		status PoolStatus
		valid  bool
	}{
		{PoolStatusPlanned, true},
		{PoolStatusActive, true},
		{PoolStatusDeprecated, true},
		{"invalid", false},
		{"", false},
		{"ACTIVE", false}, // case sensitive
	}

	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			if got := IsValidPoolStatus(tt.status); got != tt.valid {
				t.Errorf("IsValidPoolStatus(%q) = %v, want %v", tt.status, got, tt.valid)
			}
		})
	}
}

func TestIsValidPoolSource(t *testing.T) {
	tests := []struct {
		source PoolSource
		valid  bool
	}{
		{PoolSourceManual, true},
		{PoolSourceDiscovered, true},
		{PoolSourceImported, true},
		{"invalid", false},
		{"", false},
		{"MANUAL", false}, // case sensitive
	}

	for _, tt := range tests {
		t.Run(string(tt.source), func(t *testing.T) {
			if got := IsValidPoolSource(tt.source); got != tt.valid {
				t.Errorf("IsValidPoolSource(%q) = %v, want %v", tt.source, got, tt.valid)
			}
		})
	}
}

func TestValidPoolTypesContainsExpectedValues(t *testing.T) {
	expected := map[PoolType]bool{
		PoolTypeSupernet:    true,
		PoolTypeRegion:      true,
		PoolTypeEnvironment: true,
		PoolTypeVPC:         true,
		PoolTypeSubnet:      true,
	}

	if len(ValidPoolTypes) != len(expected) {
		t.Errorf("expected %d pool types, got %d", len(expected), len(ValidPoolTypes))
	}

	for _, pt := range ValidPoolTypes {
		if !expected[pt] {
			t.Errorf("unexpected pool type: %s", pt)
		}
	}
}

func TestValidPoolStatusesContainsExpectedValues(t *testing.T) {
	expected := map[PoolStatus]bool{
		PoolStatusPlanned:    true,
		PoolStatusActive:     true,
		PoolStatusDeprecated: true,
	}

	if len(ValidPoolStatuses) != len(expected) {
		t.Errorf("expected %d pool statuses, got %d", len(expected), len(ValidPoolStatuses))
	}

	for _, ps := range ValidPoolStatuses {
		if !expected[ps] {
			t.Errorf("unexpected pool status: %s", ps)
		}
	}
}

func TestValidPoolSourcesContainsExpectedValues(t *testing.T) {
	expected := map[PoolSource]bool{
		PoolSourceManual:     true,
		PoolSourceDiscovered: true,
		PoolSourceImported:   true,
	}

	if len(ValidPoolSources) != len(expected) {
		t.Errorf("expected %d pool sources, got %d", len(expected), len(ValidPoolSources))
	}

	for _, ps := range ValidPoolSources {
		if !expected[ps] {
			t.Errorf("unexpected pool source: %s", ps)
		}
	}
}
