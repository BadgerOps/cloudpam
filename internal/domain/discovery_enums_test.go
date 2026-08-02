package domain

import "testing"

func TestIsValidCloudResourceTypeTable(t *testing.T) {
	tests := []struct {
		resourceType CloudResourceType
		valid        bool
	}{
		{ResourceTypeVPC, true},
		{ResourceTypeSubnet, true},
		{ResourceTypeNetworkInterface, true},
		{ResourceTypeElasticIP, true},
		{"", false},
		{"instance", false},
		{"VPC", false},  // case sensitive
		{"vpc ", false}, // no trimming
		{"vpc,subnet", false},
	}

	for _, tt := range tests {
		t.Run(string(tt.resourceType), func(t *testing.T) {
			if got := IsValidCloudResourceType(tt.resourceType); got != tt.valid {
				t.Errorf("IsValidCloudResourceType(%q) = %v, want %v", tt.resourceType, got, tt.valid)
			}
		})
	}
}

func TestValidCloudResourceTypesAreAllAccepted(t *testing.T) {
	expected := map[CloudResourceType]bool{
		ResourceTypeVPC:              true,
		ResourceTypeSubnet:           true,
		ResourceTypeNetworkInterface: true,
		ResourceTypeElasticIP:        true,
	}

	if len(ValidCloudResourceTypes) != len(expected) {
		t.Errorf("expected %d resource types, got %d", len(expected), len(ValidCloudResourceTypes))
	}
	for _, rt := range ValidCloudResourceTypes {
		if !expected[rt] {
			t.Errorf("unexpected resource type: %s", rt)
		}
		if !IsValidCloudResourceType(rt) {
			t.Errorf("listed resource type %q is rejected by IsValidCloudResourceType", rt)
		}
	}
}

func TestIsValidDiscoveryStatusTable(t *testing.T) {
	tests := []struct {
		status DiscoveryStatus
		valid  bool
	}{
		{DiscoveryStatusActive, true},
		{DiscoveryStatusStale, true},
		{DiscoveryStatusDeleted, true},
		{"", false},
		{"pending", false},
		{"ACTIVE", false}, // case sensitive
		{"active ", false},
	}

	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			if got := IsValidDiscoveryStatus(tt.status); got != tt.valid {
				t.Errorf("IsValidDiscoveryStatus(%q) = %v, want %v", tt.status, got, tt.valid)
			}
		})
	}
}

func TestValidDiscoveryStatusesAreAllAccepted(t *testing.T) {
	expected := map[DiscoveryStatus]bool{
		DiscoveryStatusActive:  true,
		DiscoveryStatusStale:   true,
		DiscoveryStatusDeleted: true,
	}

	if len(ValidDiscoveryStatuses) != len(expected) {
		t.Errorf("expected %d statuses, got %d", len(expected), len(ValidDiscoveryStatuses))
	}
	for _, s := range ValidDiscoveryStatuses {
		if !expected[s] {
			t.Errorf("unexpected discovery status: %s", s)
		}
		if !IsValidDiscoveryStatus(s) {
			t.Errorf("listed status %q is rejected by IsValidDiscoveryStatus", s)
		}
	}
}
