package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/BadgerOps/cloudpam/terraform-provider-cloudpam/internal/client"
)

func requireClient(t *testing.T, resp *provider.ConfigureResponse) *client.Client {
	t.Helper()
	c, ok := resp.ResourceData.(*client.Client)
	if !ok {
		t.Fatalf("ResourceData = %T, want *client.Client", resp.ResourceData)
	}
	return c
}

func TestConfigureClient(t *testing.T) {
	t.Run("nil provider data is not an error", func(t *testing.T) {
		var diags diag.Diagnostics
		c, ok := configureClient(nil, &diags)
		if ok || c != nil {
			t.Fatalf("configureClient(nil) = %v, %v", c, ok)
		}
		if diags.HasError() {
			t.Fatalf("unexpected diagnostics: %v", diags)
		}
	})

	t.Run("wrong type is an error", func(t *testing.T) {
		var diags diag.Diagnostics
		if _, ok := configureClient("nonsense", &diags); ok {
			t.Fatal("expected failure")
		}
		if !diags.HasError() {
			t.Fatal("expected an error diagnostic")
		}
	})

	t.Run("correct type passes through", func(t *testing.T) {
		var diags diag.Diagnostics
		want, err := client.New("https://x.example.com", "cpam_x")
		if err != nil {
			t.Fatalf("client.New() error = %v", err)
		}
		got, ok := configureClient(want, &diags)
		if !ok || got != want {
			t.Fatalf("configureClient() = %v, %v", got, ok)
		}
	})
}

func TestStringOrEmpty(t *testing.T) {
	tests := []struct {
		name string
		in   types.String
		want string
	}{
		{name: "null", in: types.StringNull(), want: ""},
		{name: "unknown", in: types.StringUnknown(), want: ""},
		{name: "empty", in: types.StringValue(""), want: ""},
		{name: "value", in: types.StringValue("prod"), want: "prod"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := stringOrEmpty(tc.in); got != tc.want {
				t.Fatalf("stringOrEmpty() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestMapToStringMap(t *testing.T) {
	ctx := context.Background()

	for _, tc := range []struct {
		name string
		in   types.Map
		want map[string]string
	}{
		{name: "null becomes empty non-nil map", in: types.MapNull(types.StringType), want: map[string]string{}},
		{name: "unknown becomes empty non-nil map", in: types.MapUnknown(types.StringType), want: map[string]string{}},
		{
			name: "values are preserved",
			in: types.MapValueMust(types.StringType, map[string]attr.Value{
				"env":  types.StringValue("prod"),
				"team": types.StringValue("net"),
			}),
			want: map[string]string{"env": "prod", "team": "net"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, diags := mapToStringMap(ctx, tc.in)
			if diags.HasError() {
				t.Fatalf("diagnostics: %v", diags)
			}
			if got == nil {
				t.Fatal("result must never be nil: a nil map would be sent as JSON null")
			}
			if len(got) != len(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			for k, v := range tc.want {
				if got[k] != v {
					t.Fatalf("got[%q] = %q, want %q", k, got[k], v)
				}
			}
		})
	}
}

func TestStringMapToMapValue(t *testing.T) {
	t.Run("nil map becomes an empty map value, not null", func(t *testing.T) {
		got, diags := stringMapToMapValue(nil)
		if diags.HasError() {
			t.Fatalf("diagnostics: %v", diags)
		}
		if got.IsNull() || got.IsUnknown() {
			t.Fatalf("got %v, want a known empty map", got)
		}
		if len(got.Elements()) != 0 {
			t.Fatalf("got %v, want empty", got)
		}
	})

	t.Run("values round-trip", func(t *testing.T) {
		got, diags := stringMapToMapValue(map[string]string{"a": "1"})
		if diags.HasError() {
			t.Fatalf("diagnostics: %v", diags)
		}
		if len(got.Elements()) != 1 {
			t.Fatalf("got %v", got)
		}
	})
}

func TestListToStringSlice(t *testing.T) {
	ctx := context.Background()

	for _, tc := range []struct {
		name string
		in   types.List
		want []string
	}{
		{name: "null becomes empty non-nil slice", in: types.ListNull(types.StringType), want: []string{}},
		{name: "unknown becomes empty non-nil slice", in: types.ListUnknown(types.StringType), want: []string{}},
		{
			name: "values preserve order",
			in: types.ListValueMust(types.StringType, []attr.Value{
				types.StringValue("us-east-1"),
				types.StringValue("eu-west-1"),
			}),
			want: []string{"us-east-1", "eu-west-1"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, diags := listToStringSlice(ctx, tc.in)
			if diags.HasError() {
				t.Fatalf("diagnostics: %v", diags)
			}
			if got == nil {
				t.Fatal("result must never be nil: a nil slice would be sent as JSON null")
			}
			if len(got) != len(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Fatalf("got %v, want %v", got, tc.want)
				}
			}
		})
	}
}

func TestStringSliceToListValue(t *testing.T) {
	got, diags := stringSliceToListValue(nil)
	if diags.HasError() {
		t.Fatalf("diagnostics: %v", diags)
	}
	if got.IsNull() || got.IsUnknown() {
		t.Fatalf("got %v, want a known empty list", got)
	}
	if len(got.Elements()) != 0 {
		t.Fatalf("got %v, want empty", got)
	}
}
