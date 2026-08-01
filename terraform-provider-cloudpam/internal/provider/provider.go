// Package provider implements the CloudPAM Terraform provider on top of
// terraform-plugin-framework.
package provider

import (
	"context"
	"os"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/BadgerOps/cloudpam/terraform-provider-cloudpam/internal/client"
)

// Environment variables consulted when the corresponding provider attribute is
// not set in configuration.
const (
	EnvEndpoint = "CLOUDPAM_ENDPOINT"
	EnvAPIKey   = "CLOUDPAM_API_KEY"
)

var (
	_ provider.Provider = (*cloudpamProvider)(nil)
)

type cloudpamProvider struct {
	version string
}

// New returns a provider factory for the given build version.
func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &cloudpamProvider{version: version}
	}
}

type providerModel struct {
	Endpoint types.String `tfsdk:"endpoint"`
	APIKey   types.String `tfsdk:"api_key"`
}

func (p *cloudpamProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "cloudpam"
	resp.Version = p.version
}

func (p *cloudpamProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages IP address pools and cloud accounts in a [CloudPAM](https://github.com/BadgerOps/cloudpam) server.",
		Attributes: map[string]schema.Attribute{
			"endpoint": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Base URL of the CloudPAM server, e.g. `https://cloudpam.example.com`. Falls back to the `" + EnvEndpoint + "` environment variable.",
			},
			"api_key": schema.StringAttribute{
				Optional:            true,
				Sensitive:           true,
				MarkdownDescription: "CloudPAM API key (the `cpam_...` token shown once at creation time). Falls back to the `" + EnvAPIKey + "` environment variable.",
			},
		},
	}
}

func (p *cloudpamProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var cfg providerModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if cfg.Endpoint.IsUnknown() {
		resp.Diagnostics.AddAttributeError(
			path.Root("endpoint"),
			"Unknown CloudPAM endpoint",
			"The provider cannot be configured because the endpoint is not known until apply. "+
				"Set it to a static value or export "+EnvEndpoint+".",
		)
	}
	if cfg.APIKey.IsUnknown() {
		resp.Diagnostics.AddAttributeError(
			path.Root("api_key"),
			"Unknown CloudPAM API key",
			"The provider cannot be configured because the API key is not known until apply. "+
				"Set it to a static value or export "+EnvAPIKey+".",
		)
	}
	if resp.Diagnostics.HasError() {
		return
	}

	endpoint := strings.TrimSpace(os.Getenv(EnvEndpoint))
	if !cfg.Endpoint.IsNull() && cfg.Endpoint.ValueString() != "" {
		endpoint = strings.TrimSpace(cfg.Endpoint.ValueString())
	}
	apiKey := strings.TrimSpace(os.Getenv(EnvAPIKey))
	if !cfg.APIKey.IsNull() && cfg.APIKey.ValueString() != "" {
		apiKey = strings.TrimSpace(cfg.APIKey.ValueString())
	}

	if endpoint == "" {
		resp.Diagnostics.AddAttributeError(
			path.Root("endpoint"),
			"Missing CloudPAM endpoint",
			"Set the `endpoint` provider attribute or export "+EnvEndpoint+".",
		)
	}
	if apiKey == "" {
		resp.Diagnostics.AddAttributeError(
			path.Root("api_key"),
			"Missing CloudPAM API key",
			"Set the `api_key` provider attribute or export "+EnvAPIKey+".",
		)
	}
	if resp.Diagnostics.HasError() {
		return
	}

	if !strings.HasPrefix(apiKey, client.APIKeyPrefix) {
		resp.Diagnostics.AddAttributeWarning(
			path.Root("api_key"),
			"Unexpected CloudPAM API key format",
			"CloudPAM API keys start with \""+client.APIKeyPrefix+"\". The server rejects bearer tokens without that prefix, "+
				"so authentication is likely to fail.",
		)
	}

	c, err := client.New(endpoint, apiKey, client.WithUserAgent("terraform-provider-cloudpam/"+p.version))
	if err != nil {
		resp.Diagnostics.AddError("Unable to create CloudPAM API client", err.Error())
		return
	}

	resp.DataSourceData = c
	resp.ResourceData = c
}

func (p *cloudpamProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewPoolResource,
		NewAccountResource,
	}
}

func (p *cloudpamProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		NewPoolDataSource,
		NewPoolsDataSource,
		NewAccountDataSource,
		NewAccountsDataSource,
	}
}
