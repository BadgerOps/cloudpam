package provider

import (
	"context"
	"fmt"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/mapdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/BadgerOps/cloudpam/terraform-provider-cloudpam/internal/client"
)

var (
	_ resource.Resource                = (*poolResource)(nil)
	_ resource.ResourceWithConfigure   = (*poolResource)(nil)
	_ resource.ResourceWithImportState = (*poolResource)(nil)
)

// NewPoolResource returns the cloudpam_pool resource implementation.
func NewPoolResource() resource.Resource { return &poolResource{} }

type poolResource struct {
	client *client.Client
}

type poolResourceModel struct {
	ID           types.Int64  `tfsdk:"id"`
	Name         types.String `tfsdk:"name"`
	CIDR         types.String `tfsdk:"cidr"`
	ParentID     types.Int64  `tfsdk:"parent_id"`
	AccountID    types.Int64  `tfsdk:"account_id"`
	Type         types.String `tfsdk:"type"`
	Status       types.String `tfsdk:"status"`
	Source       types.String `tfsdk:"source"`
	Description  types.String `tfsdk:"description"`
	Tags         types.Map    `tfsdk:"tags"`
	CreatedAt    types.String `tfsdk:"created_at"`
	UpdatedAt    types.String `tfsdk:"updated_at"`
	ForceDestroy types.Bool   `tfsdk:"force_destroy"`
}

func (r *poolResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_pool"
}

func (r *poolResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a CloudPAM IP address pool. Pools nest via `parent_id`, so the same resource models supernets, regional blocks and leaf subnets.",
		Attributes: map[string]schema.Attribute{
			"id": schema.Int64Attribute{
				Computed:            true,
				MarkdownDescription: "Numeric CloudPAM pool identifier.",
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Human readable pool name.",
			},
			"cidr": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "IPv4 CIDR managed by this pool, e.g. `10.0.0.0/16`. Immutable: changing it replaces the pool.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"parent_id": schema.Int64Attribute{
				Optional:            true,
				MarkdownDescription: "ID of the parent pool. Omit for a top-level pool. Immutable: changing it replaces the pool.",
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.RequiresReplace(),
				},
			},
			"account_id": schema.Int64Attribute{
				Optional:            true,
				MarkdownDescription: "ID of the account this pool is assigned to. Removing the attribute clears the assignment in place (no replacement).",
			},
			"type": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Pool type: `supernet`, `region`, `environment`, `vpc` or `subnet`. Defaults to `subnet` server-side.",
			},
			"status": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "Lifecycle status: `planned`, `active` or `deprecated`. Defaults to `active` server-side.",
			},
			"source": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				MarkdownDescription: "How the pool was created: `manual`, `discovered` or `imported`. Defaults to `manual` server-side. The API does not allow changing it, so a change replaces the pool.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplaceIfConfigured(),
				},
			},
			"description": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				Default:             stringdefault.StaticString(""),
				MarkdownDescription: "Free-form description.",
			},
			"tags": schema.MapAttribute{
				Optional:            true,
				Computed:            true,
				ElementType:         types.StringType,
				Default:             mapdefault.StaticValue(types.MapValueMust(types.StringType, map[string]attr.Value{})),
				MarkdownDescription: "Key/value tags stored with the pool.",
			},
			"created_at": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "RFC 3339 creation timestamp.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"updated_at": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "RFC 3339 timestamp of the last server-side update.",
			},
			"force_destroy": schema.BoolAttribute{
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(false),
				MarkdownDescription: "When `true`, destroying this pool cascades to its child pools (`?force=true`). Otherwise CloudPAM refuses to delete a pool that still has children.",
			},
		},
	}
}

func (r *poolResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	c, ok := configureClient(req.ProviderData, &resp.Diagnostics)
	if !ok {
		return
	}
	r.client = c
}

func (r *poolResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan poolResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tags, diags := mapToStringMap(ctx, plan.Tags)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	in := client.PoolCreate{
		Name:        plan.Name.ValueString(),
		CIDR:        plan.CIDR.ValueString(),
		ParentID:    plan.ParentID.ValueInt64Pointer(),
		AccountID:   plan.AccountID.ValueInt64Pointer(),
		Type:        stringOrEmpty(plan.Type),
		Status:      stringOrEmpty(plan.Status),
		Source:      stringOrEmpty(plan.Source),
		Description: stringOrEmpty(plan.Description),
		Tags:        tags,
	}

	pool, err := r.client.CreatePool(ctx, in)
	if err != nil {
		resp.Diagnostics.AddError("Unable to create CloudPAM pool", err.Error())
		return
	}

	state := plan
	resp.Diagnostics.Append(applyPoolToModel(pool, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *poolResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state poolResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	pool, err := r.client.GetPool(ctx, state.ID.ValueInt64())
	if err != nil {
		if client.IsNotFound(err) {
			// The pool is gone server-side: drop it from state so Terraform
			// plans a re-create instead of failing.
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Unable to read CloudPAM pool", err.Error())
		return
	}

	resp.Diagnostics.Append(applyPoolToModel(pool, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *poolResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan poolResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var state poolResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tags, diags := mapToStringMap(ctx, plan.Tags)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	name := plan.Name.ValueString()
	description := stringOrEmpty(plan.Description)

	update := client.PoolUpdate{
		Name:        &name,
		Description: &description,
		Tags:        &tags,
		// account_id is always sent explicitly. When the attribute is null the
		// client emits a JSON null, which is the only way to clear an existing
		// assignment: the server keeps the current value when the key is absent.
		SetAccountID: true,
		AccountID:    plan.AccountID.ValueInt64Pointer(),
	}
	if v := stringOrEmpty(plan.Type); v != "" {
		update.Type = &v
	}
	if v := stringOrEmpty(plan.Status); v != "" {
		update.Status = &v
	}

	pool, err := r.client.UpdatePool(ctx, state.ID.ValueInt64(), update)
	if err != nil {
		if client.IsNotFound(err) {
			resp.Diagnostics.AddError(
				"CloudPAM pool no longer exists",
				fmt.Sprintf("Pool %d was deleted outside of Terraform. Run `terraform apply -refresh-only` and re-plan.", state.ID.ValueInt64()),
			)
			return
		}
		resp.Diagnostics.AddError("Unable to update CloudPAM pool", err.Error())
		return
	}

	newState := plan
	newState.ID = state.ID
	resp.Diagnostics.Append(applyPoolToModel(pool, &newState)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &newState)...)
}

func (r *poolResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state poolResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeletePool(ctx, state.ID.ValueInt64(), state.ForceDestroy.ValueBool())
	if err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Unable to delete CloudPAM pool", err.Error())
	}
}

func (r *poolResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	id, err := strconv.ParseInt(req.ID, 10, 64)
	if err != nil {
		resp.Diagnostics.AddError(
			"Invalid import ID",
			fmt.Sprintf("Expected a numeric CloudPAM pool ID, got %q.", req.ID),
		)
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), id)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("force_destroy"), false)...)
}

// applyPoolToModel copies API values onto the Terraform model, leaving
// provider-only attributes (force_destroy) untouched.
func applyPoolToModel(p *client.Pool, m *poolResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics

	tags, d := stringMapToMapValue(p.Tags)
	diags.Append(d...)
	if diags.HasError() {
		return diags
	}

	m.ID = types.Int64Value(p.ID)
	m.Name = types.StringValue(p.Name)
	m.CIDR = types.StringValue(p.CIDR)
	m.ParentID = types.Int64PointerValue(p.ParentID)
	m.AccountID = types.Int64PointerValue(p.AccountID)
	m.Type = types.StringValue(p.Type)
	m.Status = types.StringValue(p.Status)
	m.Source = types.StringValue(p.Source)
	m.Description = types.StringValue(p.Description)
	m.Tags = tags
	m.CreatedAt = types.StringValue(p.CreatedAt)
	m.UpdatedAt = types.StringValue(p.UpdatedAt)
	if m.ForceDestroy.IsNull() || m.ForceDestroy.IsUnknown() {
		m.ForceDestroy = types.BoolValue(false)
	}
	return diags
}
