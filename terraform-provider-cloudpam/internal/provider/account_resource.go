package provider

import (
	"context"
	"fmt"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/listdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/BadgerOps/cloudpam/terraform-provider-cloudpam/internal/client"
)

var (
	_ resource.Resource                = (*accountResource)(nil)
	_ resource.ResourceWithConfigure   = (*accountResource)(nil)
	_ resource.ResourceWithImportState = (*accountResource)(nil)
)

// NewAccountResource returns the cloudpam_account resource implementation.
func NewAccountResource() resource.Resource { return &accountResource{} }

type accountResource struct {
	client *client.Client
}

type accountResourceModel struct {
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
	ForceDestroy  types.Bool   `tfsdk:"force_destroy"`
}

func (r *accountResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_account"
}

func (r *accountResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a CloudPAM account: the cloud account, project or subscription that pools are assigned to.",
		Attributes: map[string]schema.Attribute{
			"id": schema.Int64Attribute{
				Computed:            true,
				MarkdownDescription: "Numeric CloudPAM account identifier.",
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.UseStateForUnknown(),
				},
			},
			"key": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Unique account key, e.g. `aws:123456789012` or `gcp:my-project`. Immutable: changing it replaces the account.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "Human readable account name.",
			},
			// Named cloud_provider rather than provider: `provider` is a
			// reserved meta-argument name in Terraform resource blocks. It maps
			// to the API's `provider` field.
			"cloud_provider": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				Default:             stringdefault.StaticString(""),
				MarkdownDescription: "Cloud provider identifier, e.g. `aws`, `gcp`, `azure`. Maps to the API's `provider` field (renamed because `provider` is a reserved Terraform meta-argument).",
			},
			"external_id": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				Default:             stringdefault.StaticString(""),
				MarkdownDescription: "Provider-native account identifier, e.g. an AWS account ID.",
			},
			"description": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				Default:             stringdefault.StaticString(""),
				MarkdownDescription: "Free-form description.",
			},
			"platform": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				Default:             stringdefault.StaticString(""),
				MarkdownDescription: "Platform label, e.g. `kubernetes`, `ec2`.",
			},
			"tier": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				Default:             stringdefault.StaticString(""),
				MarkdownDescription: "Tier label, e.g. `prod`, `nonprod`.",
			},
			"environment": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				Default:             stringdefault.StaticString(""),
				MarkdownDescription: "Environment label, e.g. `production`, `staging`.",
			},
			"regions": schema.ListAttribute{
				Optional:            true,
				Computed:            true,
				ElementType:         types.StringType,
				Default:             listdefault.StaticValue(types.ListValueMust(types.StringType, nil)),
				MarkdownDescription: "Regions associated with the account.",
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
				MarkdownDescription: "When `true`, destroying this account cascades to the pools assigned to it (`?force=true`). Otherwise CloudPAM refuses to delete an account still in use.",
			},
		},
	}
}

func (r *accountResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	c, ok := configureClient(req.ProviderData, &resp.Diagnostics)
	if !ok {
		return
	}
	r.client = c
}

func (r *accountResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan accountResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	regions, diags := listToStringSlice(ctx, plan.Regions)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	in := client.AccountCreate{
		Key:         plan.Key.ValueString(),
		Name:        plan.Name.ValueString(),
		Provider:    stringOrEmpty(plan.CloudProvider),
		ExternalID:  stringOrEmpty(plan.ExternalID),
		Description: stringOrEmpty(plan.Description),
		Platform:    stringOrEmpty(plan.Platform),
		Tier:        stringOrEmpty(plan.Tier),
		Environment: stringOrEmpty(plan.Environment),
		Regions:     regions,
	}

	account, err := r.client.CreateAccount(ctx, in)
	if err != nil {
		resp.Diagnostics.AddError("Unable to create CloudPAM account", err.Error())
		return
	}

	state := plan
	resp.Diagnostics.Append(applyAccountToModel(account, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *accountResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state accountResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	account, err := r.client.GetAccount(ctx, state.ID.ValueInt64())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Unable to read CloudPAM account", err.Error())
		return
	}

	resp.Diagnostics.Append(applyAccountToModel(account, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *accountResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan accountResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	var state accountResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	regions, diags := listToStringSlice(ctx, plan.Regions)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Every optional field is sent on each update: the API replaces them
	// wholesale, so omitting one would leave a stale server-side value behind.
	update := client.AccountUpdate{
		Name:        plan.Name.ValueString(),
		Provider:    stringOrEmpty(plan.CloudProvider),
		ExternalID:  stringOrEmpty(plan.ExternalID),
		Description: stringOrEmpty(plan.Description),
		Platform:    stringOrEmpty(plan.Platform),
		Tier:        stringOrEmpty(plan.Tier),
		Environment: stringOrEmpty(plan.Environment),
		Regions:     regions,
	}

	account, err := r.client.UpdateAccount(ctx, state.ID.ValueInt64(), update)
	if err != nil {
		if client.IsNotFound(err) {
			resp.Diagnostics.AddError(
				"CloudPAM account no longer exists",
				fmt.Sprintf("Account %d was deleted outside of Terraform. Run `terraform apply -refresh-only` and re-plan.", state.ID.ValueInt64()),
			)
			return
		}
		resp.Diagnostics.AddError("Unable to update CloudPAM account", err.Error())
		return
	}

	newState := plan
	newState.ID = state.ID
	resp.Diagnostics.Append(applyAccountToModel(account, &newState)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &newState)...)
}

func (r *accountResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state accountResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeleteAccount(ctx, state.ID.ValueInt64(), state.ForceDestroy.ValueBool())
	if err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.AddError("Unable to delete CloudPAM account", err.Error())
	}
}

func (r *accountResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	id, err := strconv.ParseInt(req.ID, 10, 64)
	if err != nil {
		resp.Diagnostics.AddError(
			"Invalid import ID",
			fmt.Sprintf("Expected a numeric CloudPAM account ID, got %q.", req.ID),
		)
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), id)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("force_destroy"), false)...)
}

func applyAccountToModel(a *client.Account, m *accountResourceModel) diag.Diagnostics {
	var diags diag.Diagnostics

	regions, d := stringSliceToListValue(a.Regions)
	diags.Append(d...)
	if diags.HasError() {
		return diags
	}

	m.ID = types.Int64Value(a.ID)
	m.Key = types.StringValue(a.Key)
	m.Name = types.StringValue(a.Name)
	m.CloudProvider = types.StringValue(a.Provider)
	m.ExternalID = types.StringValue(a.ExternalID)
	m.Description = types.StringValue(a.Description)
	m.Platform = types.StringValue(a.Platform)
	m.Tier = types.StringValue(a.Tier)
	m.Environment = types.StringValue(a.Environment)
	m.Regions = regions
	m.CreatedAt = types.StringValue(a.CreatedAt)
	m.UpdatedAt = types.StringValue(a.UpdatedAt)
	if m.ForceDestroy.IsNull() || m.ForceDestroy.IsUnknown() {
		m.ForceDestroy = types.BoolValue(false)
	}
	return diags
}
