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
	_ datasource.DataSource              = (*poolDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*poolDataSource)(nil)
)

// NewPoolDataSource returns the cloudpam_pool data source implementation.
func NewPoolDataSource() datasource.DataSource { return &poolDataSource{} }

type poolDataSource struct {
	client *client.Client
}

type poolDataSourceModel struct {
	ID          types.Int64  `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	CIDR        types.String `tfsdk:"cidr"`
	ParentID    types.Int64  `tfsdk:"parent_id"`
	AccountID   types.Int64  `tfsdk:"account_id"`
	Type        types.String `tfsdk:"type"`
	Status      types.String `tfsdk:"status"`
	Source      types.String `tfsdk:"source"`
	Description types.String `tfsdk:"description"`
	Tags        types.Map    `tfsdk:"tags"`
	CreatedAt   types.String `tfsdk:"created_at"`
	UpdatedAt   types.String `tfsdk:"updated_at"`
}

func (d *poolDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_pool"
}

func (d *poolDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Looks up a single CloudPAM pool by `id` or by `cidr`. Exactly one of the two must be set.",
		Attributes: map[string]schema.Attribute{
			"id": schema.Int64Attribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Numeric pool identifier. Set this or `cidr`.",
			},
			"cidr": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "CIDR of the pool to look up. Set this or `id`.",
			},
			"name":        schema.StringAttribute{Computed: true, MarkdownDescription: "Pool name."},
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
	}
}

func (d *poolDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	c, ok := configureClient(req.ProviderData, &resp.Diagnostics)
	if !ok {
		return
	}
	d.client = c
}

func (d *poolDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var cfg poolDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}

	hasID := !cfg.ID.IsNull() && !cfg.ID.IsUnknown()
	hasCIDR := !cfg.CIDR.IsNull() && !cfg.CIDR.IsUnknown() && cfg.CIDR.ValueString() != ""

	switch {
	case hasID && hasCIDR:
		resp.Diagnostics.AddError("Ambiguous pool lookup", "Set either `id` or `cidr`, not both.")
		return
	case !hasID && !hasCIDR:
		resp.Diagnostics.AddError("Missing pool lookup key", "Set either `id` or `cidr`.")
		return
	}

	var pool *client.Pool
	if hasID {
		p, err := d.client.GetPool(ctx, cfg.ID.ValueInt64())
		if err != nil {
			if client.IsNotFound(err) {
				resp.Diagnostics.AddError("CloudPAM pool not found", fmt.Sprintf("No pool with id %d.", cfg.ID.ValueInt64()))
				return
			}
			resp.Diagnostics.AddError("Unable to read CloudPAM pool", err.Error())
			return
		}
		pool = p
	} else {
		pools, err := d.client.ListPools(ctx)
		if err != nil {
			resp.Diagnostics.AddError("Unable to list CloudPAM pools", err.Error())
			return
		}
		want := strings.TrimSpace(cfg.CIDR.ValueString())
		for i := range pools {
			if strings.EqualFold(strings.TrimSpace(pools[i].CIDR), want) {
				if pool != nil {
					resp.Diagnostics.AddError(
						"Ambiguous pool lookup",
						fmt.Sprintf("More than one pool matches cidr %q. Look it up by `id` instead.", want),
					)
					return
				}
				pool = &pools[i]
			}
		}
		if pool == nil {
			resp.Diagnostics.AddError("CloudPAM pool not found", fmt.Sprintf("No pool with cidr %q.", want))
			return
		}
	}

	state, diags := poolToDataSourceModel(pool)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func poolToDataSourceModel(p *client.Pool) (poolDataSourceModel, diag.Diagnostics) {
	tags, diags := stringMapToMapValue(p.Tags)
	return poolDataSourceModel{
		ID:          types.Int64Value(p.ID),
		Name:        types.StringValue(p.Name),
		CIDR:        types.StringValue(p.CIDR),
		ParentID:    types.Int64PointerValue(p.ParentID),
		AccountID:   types.Int64PointerValue(p.AccountID),
		Type:        types.StringValue(p.Type),
		Status:      types.StringValue(p.Status),
		Source:      types.StringValue(p.Source),
		Description: types.StringValue(p.Description),
		Tags:        tags,
		CreatedAt:   types.StringValue(p.CreatedAt),
		UpdatedAt:   types.StringValue(p.UpdatedAt),
	}, diags
}
