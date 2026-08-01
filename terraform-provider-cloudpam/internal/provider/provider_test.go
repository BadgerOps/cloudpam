package provider

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// TestProviderSchemaIsValid runs the framework's own schema validation for the
// provider, every resource and every data source. It catches reserved attribute
// names (notably `provider`, which is why the account attribute is called
// `cloud_provider`), bad Optional/Computed/Default combinations and duplicate
// type names.
func TestProviderSchemaIsValid(t *testing.T) {
	ctx := context.Background()
	server := providerserver.NewProtocol6(New("test")())()

	resp, err := server.GetProviderSchema(ctx, &tfprotov6.GetProviderSchemaRequest{})
	if err != nil {
		t.Fatalf("GetProviderSchema() error = %v", err)
	}
	for _, d := range resp.Diagnostics {
		if d.Severity == tfprotov6.DiagnosticSeverityError {
			t.Errorf("schema diagnostic: %s: %s", d.Summary, d.Detail)
		}
	}

	for _, want := range []string{"cloudpam_pool", "cloudpam_account"} {
		if _, ok := resp.ResourceSchemas[want]; !ok {
			t.Errorf("missing resource %q (have %v)", want, keys(resp.ResourceSchemas))
		}
	}
	for _, want := range []string{"cloudpam_pool", "cloudpam_pools", "cloudpam_account", "cloudpam_accounts"} {
		if _, ok := resp.DataSourceSchemas[want]; !ok {
			t.Errorf("missing data source %q (have %v)", want, keys(resp.DataSourceSchemas))
		}
	}
}

func keys[T any](m map[string]T) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestProviderMetadata(t *testing.T) {
	resp := &provider.MetadataResponse{}
	New("1.2.3")().Metadata(context.Background(), provider.MetadataRequest{}, resp)
	if resp.TypeName != "cloudpam" {
		t.Errorf("TypeName = %q, want cloudpam", resp.TypeName)
	}
	if resp.Version != "1.2.3" {
		t.Errorf("Version = %q, want 1.2.3", resp.Version)
	}
}

// configure runs the provider's Configure with the given (possibly null)
// endpoint / api_key configuration values.
func configure(t *testing.T, endpoint, apiKey *string) *provider.ConfigureResponse {
	t.Helper()
	ctx := context.Background()
	p := New("test")()

	schemaResp := &provider.SchemaResponse{}
	p.Schema(ctx, provider.SchemaRequest{}, schemaResp)
	if schemaResp.Diagnostics.HasError() {
		t.Fatalf("provider schema diagnostics: %v", schemaResp.Diagnostics)
	}

	objType := schemaResp.Schema.Type().TerraformType(ctx).(tftypes.Object)
	value := func(v *string) tftypes.Value {
		if v == nil {
			return tftypes.NewValue(tftypes.String, nil)
		}
		return tftypes.NewValue(tftypes.String, *v)
	}
	raw := tftypes.NewValue(objType, map[string]tftypes.Value{
		"endpoint": value(endpoint),
		"api_key":  value(apiKey),
	})

	resp := &provider.ConfigureResponse{}
	p.Configure(ctx, provider.ConfigureRequest{
		Config: tfsdk.Config{Schema: schemaResp.Schema, Raw: raw},
	}, resp)
	return resp
}

func ptr(s string) *string { return &s }

func TestConfigureUsesExplicitValues(t *testing.T) {
	t.Setenv(EnvEndpoint, "https://env.example.com")
	t.Setenv(EnvAPIKey, "cpam_fromenv")

	resp := configure(t, ptr("https://config.example.com"), ptr("cpam_fromconfig"))
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}
	c := requireClient(t, resp)
	if got := c.BaseURL(); got != "https://config.example.com" {
		t.Fatalf("BaseURL() = %q, want the configured endpoint", got)
	}
}

func TestConfigureFallsBackToEnvironment(t *testing.T) {
	t.Setenv(EnvEndpoint, "https://env.example.com/api/v1")
	t.Setenv(EnvAPIKey, "cpam_fromenv")

	resp := configure(t, nil, nil)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}
	c := requireClient(t, resp)
	// The trailing /api/v1 is normalised away by the client.
	if got := c.BaseURL(); got != "https://env.example.com" {
		t.Fatalf("BaseURL() = %q, want https://env.example.com", got)
	}
}

func TestConfigureMissingValuesProduceDiagnostics(t *testing.T) {
	t.Setenv(EnvEndpoint, "")
	t.Setenv(EnvAPIKey, "")

	tests := []struct {
		name     string
		endpoint *string
		apiKey   *string
		want     string
	}{
		{name: "missing endpoint", endpoint: nil, apiKey: ptr("cpam_x"), want: "Missing CloudPAM endpoint"},
		{name: "missing api key", endpoint: ptr("https://x.example.com"), apiKey: nil, want: "Missing CloudPAM API key"},
		{name: "missing both", endpoint: nil, apiKey: nil, want: "Missing CloudPAM endpoint"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp := configure(t, tc.endpoint, tc.apiKey)
			if !resp.Diagnostics.HasError() {
				t.Fatal("expected an error diagnostic")
			}
			if !diagnosticsContain(resp, tc.want) {
				t.Fatalf("diagnostics %v do not mention %q", resp.Diagnostics, tc.want)
			}
			if resp.ResourceData != nil {
				t.Fatal("ResourceData must stay nil when configuration fails")
			}
		})
	}
}

func TestConfigureInvalidEndpointProducesDiagnostic(t *testing.T) {
	t.Setenv(EnvEndpoint, "")
	t.Setenv(EnvAPIKey, "")

	resp := configure(t, ptr("not-a-url"), ptr("cpam_x"))
	if !resp.Diagnostics.HasError() {
		t.Fatal("expected an error diagnostic")
	}
	if !diagnosticsContain(resp, "Unable to create CloudPAM API client") {
		t.Fatalf("diagnostics = %v", resp.Diagnostics)
	}
}

// TestConfigureWarnsOnBadAPIKeyPrefix documents the server contract: the auth
// middleware only accepts bearer tokens starting with cpam_.
func TestConfigureWarnsOnBadAPIKeyPrefix(t *testing.T) {
	t.Setenv(EnvEndpoint, "")
	t.Setenv(EnvAPIKey, "")

	resp := configure(t, ptr("https://x.example.com"), ptr("not-a-cloudpam-key"))
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected error diagnostics: %v", resp.Diagnostics)
	}
	if resp.Diagnostics.WarningsCount() != 1 {
		t.Fatalf("WarningsCount() = %d, want 1", resp.Diagnostics.WarningsCount())
	}
	if !diagnosticsContain(resp, "Unexpected CloudPAM API key format") {
		t.Fatalf("diagnostics = %v", resp.Diagnostics)
	}
	// A bad prefix is a warning, not a hard failure: the client is still built.
	requireClient(t, resp)
}

func TestConfigureSharesClientWithResourcesAndDataSources(t *testing.T) {
	resp := configure(t, ptr("https://x.example.com"), ptr("cpam_x"))
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", resp.Diagnostics)
	}
	if resp.ResourceData == nil || resp.DataSourceData == nil {
		t.Fatal("both ResourceData and DataSourceData must be populated")
	}
	if resp.ResourceData != resp.DataSourceData {
		t.Fatal("resources and data sources should share one client")
	}
}

func diagnosticsContain(resp *provider.ConfigureResponse, want string) bool {
	for _, d := range resp.Diagnostics {
		if strings.Contains(d.Summary(), want) || strings.Contains(d.Detail(), want) {
			return true
		}
	}
	return false
}
