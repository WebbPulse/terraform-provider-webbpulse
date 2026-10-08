package provider

import (
	"context"

	"github.com/WebbPulse/terraform-provider-webbpulse/internal/client"
	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/setdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

const remoteStateConsumersMax = 100

var (
	_ resource.Resource                = (*remoteStateSharingResource)(nil)
	_ resource.ResourceWithConfigure   = (*remoteStateSharingResource)(nil)
	_ resource.ResourceWithImportState = (*remoteStateSharingResource)(nil)
)

type remoteStateSharingResource struct {
	client *client.Client
}

// NewRemoteStateSharingResource returns the webbpulse_workspace_remote_state_sharing resource.
func NewRemoteStateSharingResource() resource.Resource { return &remoteStateSharingResource{} }

type remoteStateSharingModel struct {
	WorkspaceID            types.String `tfsdk:"workspace_id"`
	GlobalRemoteState      types.Bool   `tfsdk:"global_remote_state"`
	RemoteStateConsumerIDs types.Set    `tfsdk:"remote_state_consumer_ids"`
}

// Metadata sets the type name of the remote state sharing resource.
func (r *remoteStateSharingResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_workspace_remote_state_sharing"
}

// Schema defines the schema of the remote state sharing resource.
func (r *remoteStateSharingResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Which workspaces' runs may read this workspace's non-sensitive outputs through " +
			"`webbpulse_workspace_outputs`, the way remote state sharing works on HCP Terraform. A workspace " +
			"always shares with itself, and an admin key reads any workspace. Keep one of these per workspace, " +
			"and do not set the same fields elsewhere. Destroying it stops sharing.",
		Attributes: map[string]schema.Attribute{
			"workspace_id": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "The workspace whose outputs are shared. Changing it replaces the resource.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
				Validators:          []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"global_remote_state": schema.BoolAttribute{
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(false),
				MarkdownDescription: "Share with the runs of every workspace. Defaults to `false`.",
			},
			"remote_state_consumer_ids": schema.SetAttribute{
				Optional:            true,
				Computed:            true,
				ElementType:         types.StringType,
				Default:             setdefault.StaticValue(emptyStringSet()),
				MarkdownDescription: "The workspaces whose runs may read the outputs, at most 100. Never this workspace itself, which always shares with itself.",
				Validators:          []validator.Set{setvalidator.SizeAtMost(remoteStateConsumersMax)},
			},
		},
	}
}

// Configure stores the shared API client on the remote state sharing resource.
func (r *remoteStateSharingResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	configureClient(req.ProviderData, &r.client, &resp.Diagnostics)
}

// Create sets the workspace's sharing.
func (r *remoteStateSharingResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan remoteStateSharingModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(r.write(ctx, plan, &resp.State)...)
}

// Read refreshes the workspace's sharing, dropping the resource when the workspace is gone.
func (r *remoteStateSharingResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state remoteStateSharingModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	found, err := r.client.GetRemoteStateSharing(ctx, state.WorkspaceID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.Append(apiDiagnostic("Cannot read the remote state sharing", err))
		return
	}
	resp.Diagnostics.Append(applyRemoteStateSharing(ctx, found, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update replaces the workspace's sharing.
func (r *remoteStateSharingResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan remoteStateSharingModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(r.write(ctx, plan, &resp.State)...)
}

// Delete stops sharing. A workspace that is already gone counts as done.
func (r *remoteStateSharingResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state remoteStateSharingModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	_, err := r.client.SetRemoteStateSharing(ctx, state.WorkspaceID.ValueString(), client.RemoteStateSharingUpdate{})
	if err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.Append(apiDiagnostic("Cannot stop the remote state sharing", err))
	}
}

// ImportState imports the sharing of one workspace by its workspace id.
func (r *remoteStateSharingResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("workspace_id"), req, resp)
}

// write sends the planned sharing and stores what the API kept.
func (r *remoteStateSharingResource) write(ctx context.Context, plan remoteStateSharingModel, into *tfsdk.State) diag.Diagnostics {
	var diags diag.Diagnostics
	consumers := []string{}
	diags.Append(plan.RemoteStateConsumerIDs.ElementsAs(ctx, &consumers, false)...)
	if diags.HasError() {
		return diags
	}
	stored, err := r.client.SetRemoteStateSharing(ctx, plan.WorkspaceID.ValueString(), client.RemoteStateSharingUpdate{
		GlobalRemoteState:      plan.GlobalRemoteState.ValueBool(),
		RemoteStateConsumerIDs: consumers,
	})
	if err != nil {
		diags.Append(apiDiagnostic("Cannot set the remote state sharing", err))
		return diags
	}
	diags.Append(applyRemoteStateSharing(ctx, stored, &plan)...)
	if diags.HasError() {
		return diags
	}
	diags.Append(into.Set(ctx, &plan)...)
	return diags
}

// applyRemoteStateSharing copies the API's sharing into a model.
func applyRemoteStateSharing(ctx context.Context, from *client.RemoteStateSharing, into *remoteStateSharingModel) diag.Diagnostics {
	consumers := from.RemoteStateConsumerIDs
	if consumers == nil {
		consumers = []string{}
	}
	set, diags := types.SetValueFrom(ctx, types.StringType, consumers)
	if diags.HasError() {
		return diags
	}
	into.WorkspaceID = types.StringValue(from.WorkspaceID)
	into.GlobalRemoteState = types.BoolValue(from.GlobalRemoteState)
	into.RemoteStateConsumerIDs = set
	return diags
}
