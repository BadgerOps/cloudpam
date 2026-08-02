package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/BadgerOps/cloudpam/terraform-provider-cloudpam/internal/client"
)

func TestAccountResourceMetadata(t *testing.T) {
	resp := &resource.MetadataResponse{}
	NewAccountResource().Metadata(context.Background(), resource.MetadataRequest{ProviderTypeName: "cloudpam"}, resp)
	if resp.TypeName != "cloudpam_account" {
		t.Fatalf("TypeName = %q, want cloudpam_account", resp.TypeName)
	}
}

// TestAccountResourceSchemaAvoidsReservedProviderName pins the rename of the
// API's `provider` field: `provider` is a reserved meta-argument inside
// Terraform resource blocks and cannot be used as an attribute name.
func TestAccountResourceSchemaAvoidsReservedProviderName(t *testing.T) {
	resp := &resource.SchemaResponse{}
	NewAccountResource().Schema(context.Background(), resource.SchemaRequest{}, resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("schema diagnostics: %v", resp.Diagnostics)
	}
	if _, ok := resp.Schema.Attributes["provider"]; ok {
		t.Fatal("`provider` is a reserved Terraform meta-argument and must not be an attribute")
	}
	if _, ok := resp.Schema.Attributes["cloud_provider"]; !ok {
		t.Fatal("missing cloud_provider attribute")
	}

	// key is immutable server-side (UpdateAccount ignores it).
	key, ok := resp.Schema.Attributes["key"]
	if !ok {
		t.Fatal("missing key attribute")
	}
	if !key.IsRequired() {
		t.Fatal("key must be required")
	}
}

func TestApplyAccountToModel(t *testing.T) {
	a := client.Account{
		ID: 4, Key: "aws:123456789012", Name: "prod", Provider: "aws",
		ExternalID: "123456789012", Description: "prod account",
		Platform: "ec2", Tier: "prod", Environment: "production",
		Regions: []string{"us-east-1", "us-west-2"},
		// created_at/updated_at intentionally empty to exercise zero values.
	}

	var m accountResourceModel
	if diags := applyAccountToModel(&a, &m); diags.HasError() {
		t.Fatalf("diagnostics: %v", diags)
	}

	if !m.CloudProvider.Equal(types.StringValue("aws")) {
		t.Errorf("CloudProvider = %v, want aws", m.CloudProvider)
	}
	if !m.Key.Equal(types.StringValue("aws:123456789012")) {
		t.Errorf("Key = %v", m.Key)
	}
	if len(m.Regions.Elements()) != 2 {
		t.Errorf("Regions = %v, want 2 elements", m.Regions)
	}
	if m.ID.ValueInt64() != 4 {
		t.Errorf("ID = %v, want 4", m.ID)
	}
}

// TestApplyAccountToModelNormalisesEmptyValues checks that fields the API omits
// come back as known empty values so they match the schema defaults instead of
// producing a perpetual diff.
func TestApplyAccountToModelNormalisesEmptyValues(t *testing.T) {
	var m accountResourceModel
	if diags := applyAccountToModel(&client.Account{ID: 1, Key: "gcp:x", Name: "x"}, &m); diags.HasError() {
		t.Fatalf("diagnostics: %v", diags)
	}

	for name, v := range map[string]types.String{
		"cloud_provider": m.CloudProvider,
		"external_id":    m.ExternalID,
		"description":    m.Description,
		"platform":       m.Platform,
		"tier":           m.Tier,
		"environment":    m.Environment,
	} {
		if v.IsNull() || v.IsUnknown() {
			t.Errorf("%s = %v, want a known empty string", name, v)
		}
		if v.ValueString() != "" {
			t.Errorf("%s = %v, want empty", name, v)
		}
	}

	if m.Regions.IsNull() || m.Regions.IsUnknown() {
		t.Errorf("Regions = %v, want a known empty list", m.Regions)
	}
	if len(m.Regions.Elements()) != 0 {
		t.Errorf("Regions = %v, want empty", m.Regions)
	}
}

func TestApplyAccountToModelPreservesForceDestroy(t *testing.T) {
	m := accountResourceModel{ForceDestroy: types.BoolValue(true)}
	if diags := applyAccountToModel(&client.Account{ID: 1}, &m); diags.HasError() {
		t.Fatalf("diagnostics: %v", diags)
	}
	if !m.ForceDestroy.ValueBool() {
		t.Fatal("force_destroy is provider-only and must survive a refresh")
	}
}
