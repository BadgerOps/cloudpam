package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/BadgerOps/cloudpam/terraform-provider-cloudpam/internal/client"
)

// configureClient extracts the *client.Client stashed by the provider's
// Configure method. providerData is nil during the framework's initial
// configuration pass, which is not an error.
func configureClient(providerData any, diags *diag.Diagnostics) (*client.Client, bool) {
	if providerData == nil {
		return nil, false
	}
	c, ok := providerData.(*client.Client)
	if !ok {
		diags.AddError(
			"Unexpected provider data type",
			fmt.Sprintf("Expected *client.Client, got %T. This is a bug in the CloudPAM provider.", providerData),
		)
		return nil, false
	}
	return c, true
}

// stringOrEmpty returns the underlying string, or "" for null/unknown values.
func stringOrEmpty(v types.String) string {
	if v.IsNull() || v.IsUnknown() {
		return ""
	}
	return v.ValueString()
}

// mapToStringMap converts a types.Map of strings into a Go map. Null and
// unknown maps become an empty (non-nil) map so that clearing tags in
// configuration actually clears them server-side.
func mapToStringMap(ctx context.Context, v types.Map) (map[string]string, diag.Diagnostics) {
	out := map[string]string{}
	if v.IsNull() || v.IsUnknown() {
		return out, nil
	}
	diags := v.ElementsAs(ctx, &out, false)
	if out == nil {
		out = map[string]string{}
	}
	return out, diags
}

// stringMapToMapValue converts an API map into a types.Map, normalising nil to
// an empty map so it matches the schema default.
func stringMapToMapValue(in map[string]string) (types.Map, diag.Diagnostics) {
	elems := make(map[string]attr.Value, len(in))
	for k, val := range in {
		elems[k] = types.StringValue(val)
	}
	return types.MapValue(types.StringType, elems)
}

// listToStringSlice converts a types.List of strings into a Go slice. Null and
// unknown lists become an empty (non-nil) slice.
func listToStringSlice(ctx context.Context, v types.List) ([]string, diag.Diagnostics) {
	out := []string{}
	if v.IsNull() || v.IsUnknown() {
		return out, nil
	}
	diags := v.ElementsAs(ctx, &out, false)
	if out == nil {
		out = []string{}
	}
	return out, diags
}

// stringSliceToListValue converts an API slice into a types.List, normalising
// nil to an empty list so it matches the schema default.
func stringSliceToListValue(in []string) (types.List, diag.Diagnostics) {
	elems := make([]attr.Value, 0, len(in))
	for _, v := range in {
		elems = append(elems, types.StringValue(v))
	}
	return types.ListValue(types.StringType, elems)
}
