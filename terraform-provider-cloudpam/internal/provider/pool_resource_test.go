package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/BadgerOps/cloudpam/terraform-provider-cloudpam/internal/client"
)

func TestPoolResourceMetadata(t *testing.T) {
	resp := &resource.MetadataResponse{}
	NewPoolResource().Metadata(context.Background(), resource.MetadataRequest{ProviderTypeName: "cloudpam"}, resp)
	if resp.TypeName != "cloudpam_pool" {
		t.Fatalf("TypeName = %q, want cloudpam_pool", resp.TypeName)
	}
}

// TestPoolResourceSchemaImmutableAttributes documents which attributes the
// CloudPAM API refuses to change in place, and therefore must trigger a
// replacement rather than an update.
func TestPoolResourceSchemaImmutableAttributes(t *testing.T) {
	resp := &resource.SchemaResponse{}
	NewPoolResource().Schema(context.Background(), resource.SchemaRequest{}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("schema diagnostics: %v", resp.Diagnostics)
	}

	for _, name := range []string{"cidr", "parent_id", "source"} {
		attribute, ok := resp.Schema.Attributes[name]
		if !ok {
			t.Fatalf("missing attribute %q", name)
		}
		if !attribute.IsRequired() && !attribute.IsOptional() {
			t.Fatalf("attribute %q should be settable", name)
		}
	}

	// account_id is updatable in place, so it must NOT require replacement.
	accountID, ok := resp.Schema.Attributes["account_id"]
	if !ok {
		t.Fatal("missing attribute account_id")
	}
	if !accountID.IsOptional() {
		t.Fatal("account_id must be optional so it can be cleared")
	}
	if accountID.IsComputed() {
		t.Fatal("account_id must not be computed: a null config value has to mean 'unassign'")
	}
}

// TestApplyPoolToModelNullableFields is the provider-side half of the
// null-versus-omitted contract: a missing parent_id/account_id in the API
// response must land in state as null, never as 0.
func TestApplyPoolToModelNullableFields(t *testing.T) {
	tests := []struct {
		name          string
		pool          client.Pool
		wantParentID  types.Int64
		wantAccountID types.Int64
	}{
		{
			name:          "top level unassigned pool",
			pool:          client.Pool{ID: 1, Name: "root", CIDR: "10.0.0.0/8"},
			wantParentID:  types.Int64Null(),
			wantAccountID: types.Int64Null(),
		},
		{
			name:          "child assigned pool",
			pool:          client.Pool{ID: 2, Name: "child", CIDR: "10.1.0.0/16", ParentID: ptrInt64(1), AccountID: ptrInt64(7)},
			wantParentID:  types.Int64Value(1),
			wantAccountID: types.Int64Value(7),
		},
		{
			name:          "explicit zero is preserved as a value, not null",
			pool:          client.Pool{ID: 3, ParentID: ptrInt64(0), AccountID: ptrInt64(0)},
			wantParentID:  types.Int64Value(0),
			wantAccountID: types.Int64Value(0),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var m poolResourceModel
			diags := applyPoolToModel(&tc.pool, &m)
			if diags.HasError() {
				t.Fatalf("diagnostics: %v", diags)
			}
			if !m.ParentID.Equal(tc.wantParentID) {
				t.Errorf("ParentID = %v, want %v", m.ParentID, tc.wantParentID)
			}
			if !m.AccountID.Equal(tc.wantAccountID) {
				t.Errorf("AccountID = %v, want %v", m.AccountID, tc.wantAccountID)
			}
			if m.ID.ValueInt64() != tc.pool.ID {
				t.Errorf("ID = %v, want %d", m.ID, tc.pool.ID)
			}
		})
	}
}

func TestApplyPoolToModelNormalisesCollections(t *testing.T) {
	var m poolResourceModel
	diags := applyPoolToModel(&client.Pool{ID: 1, Tags: nil, Description: ""}, &m)
	if diags.HasError() {
		t.Fatalf("diagnostics: %v", diags)
	}
	if m.Tags.IsNull() || m.Tags.IsUnknown() {
		t.Fatalf("Tags = %v, want a known empty map to match the schema default", m.Tags)
	}
	if len(m.Tags.Elements()) != 0 {
		t.Fatalf("Tags = %v, want empty", m.Tags)
	}
	if !m.Description.Equal(types.StringValue("")) {
		t.Fatalf("Description = %v, want an empty string to match the schema default", m.Description)
	}
}

func TestApplyPoolToModelPreservesForceDestroy(t *testing.T) {
	m := poolResourceModel{ForceDestroy: types.BoolValue(true)}
	if diags := applyPoolToModel(&client.Pool{ID: 1}, &m); diags.HasError() {
		t.Fatalf("diagnostics: %v", diags)
	}
	if !m.ForceDestroy.ValueBool() {
		t.Fatal("force_destroy is provider-only and must survive a refresh")
	}

	// A null force_destroy (for example straight after import) defaults to false.
	m = poolResourceModel{ForceDestroy: types.BoolNull()}
	if diags := applyPoolToModel(&client.Pool{ID: 1}, &m); diags.HasError() {
		t.Fatalf("diagnostics: %v", diags)
	}
	if m.ForceDestroy.IsNull() || m.ForceDestroy.ValueBool() {
		t.Fatalf("ForceDestroy = %v, want a known false", m.ForceDestroy)
	}
}

func TestMatchesInt64Filter(t *testing.T) {
	tests := []struct {
		name   string
		filter types.Int64
		value  *int64
		want   bool
	}{
		{name: "unset filter matches null value", filter: types.Int64Null(), value: nil, want: true},
		{name: "unset filter matches any value", filter: types.Int64Null(), value: ptrInt64(3), want: true},
		{name: "set filter rejects null value", filter: types.Int64Value(3), value: nil, want: false},
		{name: "set filter matches equal value", filter: types.Int64Value(3), value: ptrInt64(3), want: true},
		{name: "set filter rejects different value", filter: types.Int64Value(3), value: ptrInt64(4), want: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := matchesInt64Filter(tc.filter, tc.value); got != tc.want {
				t.Fatalf("matchesInt64Filter() = %v, want %v", got, tc.want)
			}
		})
	}
}

func ptrInt64(v int64) *int64 { return &v }
