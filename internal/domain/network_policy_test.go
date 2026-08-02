package domain

import "testing"

func TestDefaultNetworkSchemaPolicyIsAccountLevel(t *testing.T) {
	got := DefaultNetworkSchemaPolicy()
	want := NetworkSchemaPolicy{
		Name:              "account_level",
		OwnershipStrategy: "account",
		DuplicateScope:    "account",
		HierarchyScope:    "account",
	}
	if got != want {
		t.Fatalf("DefaultNetworkSchemaPolicy() = %+v, want %+v", got, want)
	}
	if got.ManualRelationships {
		t.Error("default policy must not enable manual relationships")
	}
}

func TestNormalizeNetworkSchemaPolicyDerivesFields(t *testing.T) {
	tests := []struct {
		name  string
		input *NetworkSchemaPolicy
		want  NetworkSchemaPolicy
	}{
		{
			name:  "nil falls back to default",
			input: nil,
			want:  DefaultNetworkSchemaPolicy(),
		},
		{
			name:  "empty name falls back to default",
			input: &NetworkSchemaPolicy{},
			want:  DefaultNetworkSchemaPolicy(),
		},
		{
			name:  "unknown name falls back to default",
			input: &NetworkSchemaPolicy{Name: "planet_level"},
			want:  DefaultNetworkSchemaPolicy(),
		},
		{
			name:  "account_level is normalized to defaults",
			input: &NetworkSchemaPolicy{Name: "  Account_Level  "},
			want:  DefaultNetworkSchemaPolicy(),
		},
		{
			name:  "region_level derives region scopes",
			input: &NetworkSchemaPolicy{Name: "REGION_LEVEL"},
			want: NetworkSchemaPolicy{
				Name:              "region_level",
				OwnershipStrategy: "region",
				DuplicateScope:    "region",
				HierarchyScope:    "region",
			},
		},
		{
			name:  "global derives global scopes",
			input: &NetworkSchemaPolicy{Name: " global "},
			want: NetworkSchemaPolicy{
				Name:              "global",
				OwnershipStrategy: "global",
				DuplicateScope:    "global",
				HierarchyScope:    "global",
			},
		},
		{
			name:  "custom enables manual relationships",
			input: &NetworkSchemaPolicy{Name: "custom"},
			want: NetworkSchemaPolicy{
				Name:                "custom",
				OwnershipStrategy:   "manual",
				DuplicateScope:      "manual",
				HierarchyScope:      "manual",
				ManualRelationships: true,
			},
		},
		{
			name:  "manual enables manual relationships",
			input: &NetworkSchemaPolicy{Name: "Manual"},
			want: NetworkSchemaPolicy{
				Name:                "manual",
				OwnershipStrategy:   "manual",
				DuplicateScope:      "manual",
				HierarchyScope:      "manual",
				ManualRelationships: true,
			},
		},
		{
			name:  "manual honours a global duplicate scope override",
			input: &NetworkSchemaPolicy{Name: "manual", DuplicateScope: " GLOBAL "},
			want: NetworkSchemaPolicy{
				Name:                "manual",
				OwnershipStrategy:   "manual",
				DuplicateScope:      "global",
				HierarchyScope:      "manual",
				ManualRelationships: true,
			},
		},
		{
			name:  "manual ignores an unsupported duplicate scope override",
			input: &NetworkSchemaPolicy{Name: "manual", DuplicateScope: "region"},
			want: NetworkSchemaPolicy{
				Name:                "manual",
				OwnershipStrategy:   "manual",
				DuplicateScope:      "manual",
				HierarchyScope:      "manual",
				ManualRelationships: true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeNetworkSchemaPolicy(tt.input)
			if got == nil {
				t.Fatal("NormalizeNetworkSchemaPolicy returned nil")
			}
			if *got != tt.want {
				t.Errorf("NormalizeNetworkSchemaPolicy() = %+v, want %+v", *got, tt.want)
			}
		})
	}
}

func TestNormalizeNetworkSchemaPolicyDoesNotMutateCaller(t *testing.T) {
	input := &NetworkSchemaPolicy{Name: "region_level", OwnershipStrategy: "account"}
	original := *input

	got := NormalizeNetworkSchemaPolicy(input)
	if got == input {
		t.Fatal("normalize must return a distinct policy value, not the caller's pointer")
	}
	if *input != original {
		t.Errorf("caller policy was mutated: got %+v, want %+v", *input, original)
	}
	if got.OwnershipStrategy != "region" {
		t.Errorf("OwnershipStrategy = %q, want %q", got.OwnershipStrategy, "region")
	}
}

func TestValidateNetworkSchemaPolicy(t *testing.T) {
	tests := []struct {
		name   string
		policy *NetworkSchemaPolicy
		want   string
	}{
		{
			name:   "nil policy rejected",
			policy: nil,
			want:   "policy is required",
		},
		{
			name:   "unknown name rejected",
			policy: &NetworkSchemaPolicy{Name: "org_level"},
			want:   "schema policy must be account_level, region_level, global, manual, or custom",
		},
		{
			name:   "empty name rejected",
			policy: &NetworkSchemaPolicy{},
			want:   "schema policy must be account_level, region_level, global, manual, or custom",
		},
		{
			name:   "account_level with no derived fields accepted",
			policy: &NetworkSchemaPolicy{Name: "account_level"},
			want:   "",
		},
		{
			name: "account_level with matching derived fields accepted",
			policy: &NetworkSchemaPolicy{
				Name:              "Account_Level",
				OwnershipStrategy: "ACCOUNT",
				DuplicateScope:    " account ",
				HierarchyScope:    "account",
			},
			want: "",
		},
		{
			name:   "mismatched ownership strategy rejected",
			policy: &NetworkSchemaPolicy{Name: "account_level", OwnershipStrategy: "global"},
			want:   "ownership_strategy does not match schema policy",
		},
		{
			name:   "mismatched duplicate scope rejected",
			policy: &NetworkSchemaPolicy{Name: "region_level", DuplicateScope: "account"},
			want:   "duplicate_scope does not match schema policy",
		},
		{
			name:   "mismatched hierarchy scope rejected",
			policy: &NetworkSchemaPolicy{Name: "global", HierarchyScope: "region"},
			want:   "hierarchy_scope does not match schema policy",
		},
		{
			name:   "manual with no derived fields accepted",
			policy: &NetworkSchemaPolicy{Name: "manual"},
			want:   "",
		},
		{
			name:   "custom with global duplicate scope accepted",
			policy: &NetworkSchemaPolicy{Name: "custom", OwnershipStrategy: "manual", DuplicateScope: "global", HierarchyScope: "manual"},
			want:   "",
		},
		{
			name:   "manual with non-manual ownership rejected",
			policy: &NetworkSchemaPolicy{Name: "manual", OwnershipStrategy: "account"},
			want:   "ownership_strategy does not match schema policy",
		},
		{
			name:   "manual with unsupported duplicate scope rejected",
			policy: &NetworkSchemaPolicy{Name: "manual", DuplicateScope: "region"},
			want:   "duplicate_scope for manual policy must be manual or global",
		},
		{
			name:   "manual with non-manual hierarchy rejected",
			policy: &NetworkSchemaPolicy{Name: "custom", HierarchyScope: "global"},
			want:   "hierarchy_scope does not match schema policy",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ValidateNetworkSchemaPolicy(tt.policy)
			if got != tt.want {
				t.Errorf("ValidateNetworkSchemaPolicy() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestValidateNetworkSchemaPolicyAcceptsEveryNormalizedPolicy(t *testing.T) {
	for _, name := range []string{"account_level", "region_level", "global", "manual", "custom"} {
		t.Run(name, func(t *testing.T) {
			normalized := NormalizeNetworkSchemaPolicy(&NetworkSchemaPolicy{Name: name})
			if got := ValidateNetworkSchemaPolicy(normalized); got != "" {
				t.Errorf("normalized policy %+v rejected: %q", *normalized, got)
			}
		})
	}
}
