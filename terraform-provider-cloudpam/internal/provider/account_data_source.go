package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/BadgerOps/cloudpam/terraform-provider-cloudpam/internal/client"
)

var (
	_ datasource.DataSource              = (*accountDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*accountDataSource)(nil)
)

// NewAccountDataSource returns the cloudpam_account data source implementation.
func NewAccountDataSource() datasource.DataSource { return &accountDataSource{} }

type accountDataSource struct {
	client *client.Client
}

type accountDataSourceModel struct {
	ID            types.Int64  `tfsdk:"id"`
	Key           types.String `tfsdk:"key"`
	Name          types.String `tfsdk:"name"`
	CloudProvider types.String `tfsdk:"cloud_provider"`
	ExternalID    types.String `tfsdk:"external_id"`
	Description   types.String `tfsdk:"description"`
	Platform      types.String `tfsdk:"platform"`
	Tier          types.String `tfsdk:"tier"`
	Environment   types.String `tfsdk:"environment"`
	Regions       types.List   `tfsdk:"regions"`
	CreatedAt     types.String `tfsdk:"created_at"`
	UpdatedAt     types.String `tfsdk:"updated_at"`
}

func (d *accountDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_account"
}

func (d *accountDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Looks up a single CloudPAM account by `id` or by `key`. Exactly one of the two must be set.",
		Attributes: map[string]schema.Attribute{
			"id": schema.Int64Attribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Numeric account identifier. Set this or `key`.",
			},
			"key": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Unique account key, e.g. `aws:123456789012`. Set this or `id`.",
			},
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
	}
}

func (d *accountDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	c, ok := configureClient(req.ProviderData, &resp.Diagnostics)
	if !ok {
		return
	}
	d.client = c
}

func (d *accountDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg accountDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}

	hasID := !cfg.ID.IsNull() && !cfg.ID.IsUnknown()
	hasKey := !cfg.Key.IsNull() && !cfg.Key.IsUnknown() && cfg.Key.ValueString() != ""

	switch {
	case hasID && hasKey:
		resp.Diagnostics.AddError("Ambiguous account lookup", "Set either `id` or `key`, not both.")
		return
	case !hasID && !hasKey:
		resp.Diagnostics.AddError("Missing account lookup key", "Set either `id` or `key`.")
		return
	}

	var account *client.Account
	if hasID {
		a, err := d.client.GetAccount(ctx, cfg.ID.ValueInt64())
		if err != nil {
			if client.IsNotFound(err) {
				resp.Diagnostics.AddError("CloudPAM account not found", fmt.Sprintf("No account with id %d.", cfg.ID.ValueInt64()))
				return
			}
			resp.Diagnostics.AddError("Unable to read CloudPAM account", err.Error())
			return
		}
		account = a
	} else {
		accounts, err := d.client.ListAccounts(ctx)
		if err != nil {
			resp.Diagnostics.AddError("Unable to list CloudPAM accounts", err.Error())
			return
		}
		want := strings.TrimSpace(cfg.Key.ValueString())
		for i := range accounts {
			if accounts[i].Key == want {
				account = &accounts[i]
				break
			}
		}
		if account == nil {
			resp.Diagnostics.AddError("CloudPAM account not found", fmt.Sprintf("No account with key %q.", want))
			return
		}
	}

	state, diags := accountToDataSourceModel(account)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func accountToDataSourceModel(a *client.Account) (accountDataSourceModel, diag.Diagnostics) {
	regions, diags := stringSliceToListValue(a.Regions)
	return accountDataSourceModel{
		ID:            types.Int64Value(a.ID),
		Key:           types.StringValue(a.Key),
		Name:          types.StringValue(a.Name),
		CloudProvider: types.StringValue(a.Provider),
		ExternalID:    types.StringValue(a.ExternalID),
		Description:   types.StringValue(a.Description),
		Platform:      types.StringValue(a.Platform),
		Tier:          types.StringValue(a.Tier),
		Environment:   types.StringValue(a.Environment),
		Regions:       regions,
		CreatedAt:     types.StringValue(a.CreatedAt),
		UpdatedAt:     types.StringValue(a.UpdatedAt),
	}, diags
}
