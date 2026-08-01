package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/BadgerOps/cloudpam/terraform-provider-cloudpam/internal/client"
)

var (
	_ datasource.DataSource              = (*poolsDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*poolsDataSource)(nil)
)

// NewPoolsDataSource returns the cloudpam_pools data source implementation.
func NewPoolsDataSource() datasource.DataSource { return &poolsDataSource{} }

type poolsDataSource struct {
	client *client.Client
}

type poolsDataSourceModel struct {
	ParentID  types.Int64           `tfsdk:"parent_id"`
	AccountID types.Int64           `tfsdk:"account_id"`
	Type      types.String          `tfsdk:"type"`
	Status    types.String          `tfsdk:"status"`
	Pools     []poolDataSourceModel `tfsdk:"pools"`
}

func (d *poolsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_pools"
}

func (d *poolsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Lists CloudPAM pools, optionally narrowed by parent, account, type or status.",
		Attributes: map[string]schema.Attribute{
			"parent_id": schema.Int64Attribute{
				Optional:            true,
				MarkdownDescription: "Only return direct children of this pool.",
			},
			"account_id": schema.Int64Attribute{
				Optional:            true,
				MarkdownDescription: "Only return pools assigned to this account.",
			},
			"type": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Only return pools of this type.",
			},
			"status": schema.StringAttribute{
				Optional:            true,
				MarkdownDescription: "Only return pools with this lifecycle status.",
			},
			"pools": schema.ListNestedAttribute{
				Computed:            true,
				MarkdownDescription: "Matching pools.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id":          schema.Int64Attribute{Computed: true, MarkdownDescription: "Numeric pool identifier."},
						"name":        schema.StringAttribute{Computed: true, MarkdownDescription: "Pool name."},
						"cidr":        schema.StringAttribute{Computed: true, MarkdownDescription: "Pool CIDR."},
						"parent_id":   schema.Int64Attribute{Computed: true, MarkdownDescription: "Parent pool ID, null for top-level pools."},
						"account_id":  schema.Int64Attribute{Computed: true, MarkdownDescription: "Assigned account ID, null when unassigned."},
						"type":        schema.StringAttribute{Computed: true, MarkdownDescription: "Pool type."},
						"status":      schema.StringAttribute{Computed: true, MarkdownDescription: "Pool lifecycle status."},
						"source":      schema.StringAttribute{Computed: true, MarkdownDescription: "How the pool was created."},
						"description": schema.StringAttribute{Computed: true, MarkdownDescription: "Pool description."},
						"tags": schema.MapAttribute{
							Computed:            true,
							ElementType:         types.StringType,
							MarkdownDescription: "Pool tags.",
						},
						"created_at": schema.StringAttribute{Computed: true, MarkdownDescription: "RFC 3339 creation timestamp."},
						"updated_at": schema.StringAttribute{Computed: true, MarkdownDescription: "RFC 3339 update timestamp."},
					},
				},
			},
		},
	}
}

func (d *poolsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	c, ok := configureClient(req.ProviderData, &resp.Diagnostics)
	if !ok {
		return
	}
	d.client = c
}

func (d *poolsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg poolsDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}

	pools, err := d.client.ListPools(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Unable to list CloudPAM pools", err.Error())
		return
	}

	// The list endpoint takes no filters, so narrow the result client-side.
	out := make([]poolDataSourceModel, 0, len(pools))
	for i := range pools {
		p := pools[i]
		if !matchesInt64Filter(cfg.ParentID, p.ParentID) {
			continue
		}
		if !matchesInt64Filter(cfg.AccountID, p.AccountID) {
			continue
		}
		if !cfg.Type.IsNull() && cfg.Type.ValueString() != p.Type {
			continue
		}
		if !cfg.Status.IsNull() && cfg.Status.ValueString() != p.Status {
			continue
		}
		m, diags := poolToDataSourceModel(&p)
		resp.Diagnostics.Append(diags...)
		if resp.Diagnostics.HasError() {
			return
		}
		out = append(out, m)
	}

	cfg.Pools = out
	resp.Diagnostics.Append(resp.State.Set(ctx, &cfg)...)
}

// matchesInt64Filter reports whether a nullable API value satisfies an optional
// filter. An unset filter matches everything.
func matchesInt64Filter(filter types.Int64, value *int64) bool {
	if filter.IsNull() || filter.IsUnknown() {
		return true
	}
	return value != nil && *value == filter.ValueInt64()
}
