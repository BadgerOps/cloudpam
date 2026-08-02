package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/BadgerOps/cloudpam/terraform-provider-cloudpam/internal/client"
)

var (
	_ datasource.DataSource              = (*accountsDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*accountsDataSource)(nil)
)

// NewAccountsDataSource returns the cloudpam_accounts data source implementation.
func NewAccountsDataSource() datasource.DataSource { return &accountsDataSource{} }

type accountsDataSource struct {
	client *client.Client
}

type accountsDataSourceModel struct {
	CloudProvider types.String             `tfsdk:"cloud_provider"`
	Environment   types.String             `tfsdk:"environment"`
	Accounts      []accountDataSourceModel `tfsdk:"accounts"`
}

func (d *accountsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_accounts"
}

func (d *accountsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Lists CloudPAM accounts, optionally narrowed by cloud provider or environment.",
		Attributes: map[string]schema.Attribute{
			"cloud_provider": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Only return accounts with this provider (the API's `provider` field).",
			},
			"environment": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Only return accounts with this environment label.",
			},
			"accounts": schema.ListNestedAttribute{
				Computed:            true,
				MarkdownDescription: "Matching accounts.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id":             schema.Int64Attribute{Computed: true, MarkdownDescription: "Numeric account identifier."},
						"key":            schema.StringAttribute{Computed: true, MarkdownDescription: "Unique account key."},
						"name":           schema.StringAttribute{Computed: true, MarkdownDescription: "Account name."},
						"cloud_provider": schema.StringAttribute{Computed: true, MarkdownDescription: "Cloud provider identifier (the API's `provider` field)."},
						"external_id":    schema.StringAttribute{Computed: true, MarkdownDescription: "Provider-native account identifier."},
						"description":    schema.StringAttribute{Computed: true, MarkdownDescription: "Account description."},
						"platform":       schema.StringAttribute{Computed: true, MarkdownDescription: "Platform label."},
						"tier":           schema.StringAttribute{Computed: true, MarkdownDescription: "Tier label."},
						"environment":    schema.StringAttribute{Computed: true, MarkdownDescription: "Environment label."},
						"regions": schema.ListAttribute{
							Computed:            true,
							ElementType:         types.StringType,
							MarkdownDescription: "Regions associated with the account.",
						},
						"created_at": schema.StringAttribute{Computed: true, MarkdownDescription: "RFC 3339 creation timestamp."},
						"updated_at": schema.StringAttribute{Computed: true, MarkdownDescription: "RFC 3339 update timestamp."},
					},
				},
			},
		},
	}
}

func (d *accountsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	c, ok := configureClient(req.ProviderData, &resp.Diagnostics)
	if !ok {
		return
	}
	d.client = c
}

func (d *accountsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg accountsDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}

	accounts, err := d.client.ListAccounts(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Unable to list CloudPAM accounts", err.Error())
		return
	}

	out := make([]accountDataSourceModel, 0, len(accounts))
	for i := range accounts {
		a := accounts[i]
		if !cfg.CloudProvider.IsNull() && cfg.CloudProvider.ValueString() != a.Provider {
			continue
		}
		if !cfg.Environment.IsNull() && cfg.Environment.ValueString() != a.Environment {
			continue
		}
		m, diags := accountToDataSourceModel(&a)
		resp.Diagnostics.Append(diags...)
		if resp.Diagnostics.HasError() {
			return
		}
		out = append(out, m)
	}

	cfg.Accounts = out
	resp.Diagnostics.Append(resp.State.Set(ctx, &cfg)...)
}
