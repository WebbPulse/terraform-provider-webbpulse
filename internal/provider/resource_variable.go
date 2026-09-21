package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/WebbPulse/terraform-provider-webbpulse/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                = (*variableResource)(nil)
	_ resource.ResourceWithConfigure   = (*variableResource)(nil)
	_ resource.ResourceWithImportState = (*variableResource)(nil)
)

type variableResource struct {
	client *client.Client
}

// NewVariableResource returns the webbpulse_variable resource.
func NewVariableResource() resource.Resource { return &variableResource{} }

type variableModel struct {
	WorkspaceID types.String `tfsdk:"workspace_id"`
	Key         types.String `tfsdk:"key"`
	Value       types.String `tfsdk:"value"`
	Category    types.String `tfsdk:"category"`
	Sensitive   types.Bool   `tfsdk:"sensitive"`
	Description types.String `tfsdk:"description"`
	CreatedAt   types.String `tfsdk:"created_at"`
	UpdatedAt   types.String `tfsdk:"updated_at"`
}

func (r *variableResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_variable"
}

func (r *variableResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "One variable on one workspace. The API never returns a sensitive value, so the " +
			"provider keeps the configured value in state and cannot detect a sensitive value changed outside " +
			"Terraform.",
		Attributes: map[string]schema.Attribute{
			"workspace_id": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "The workspace this variable belongs to. Changing it replaces the variable.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"key": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "The variable name. Changing it replaces the variable.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"value": schema.StringAttribute{
				Required:  true,
				Sensitive: true,
				MarkdownDescription: "The variable value. Marked sensitive at the schema level so it is never " +
					"printed, whatever `sensitive` is set to.",
			},
			"category": schema.StringAttribute{
				Optional: true,
				Computed: true,
				Default:  stringdefault.StaticString(client.CategoryTerraform),
				MarkdownDescription: "`terraform` for a `-var` on the command line, `env` for a process " +
					"environment variable on the task.",
			},
			"sensitive": schema.BoolAttribute{
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(false),
				MarkdownDescription: "Whether the value is sealed at rest and withheld from every read. " +
					"A sensitive value cannot be imported, since the API will not return it.",
			},
			"description": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				Default:             stringdefault.StaticString(""),
				MarkdownDescription: "A description for this variable.",
			},
			"created_at": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "When the variable was created.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"updated_at": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "When the variable was last set.",
			},
		},
	}
}

func (r *variableResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	configureClient(req.ProviderData, &r.client, &resp.Diagnostics)
}

func (r *variableResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan variableModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	r.write(ctx, plan, &resp.Diagnostics, func(state variableModel) {
		resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
	})
}

func (r *variableResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state variableModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	found, err := r.client.GetVariable(ctx, state.WorkspaceID.ValueString(), state.Key.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.Append(apiDiagnostic("Cannot read the variable", err))
		return
	}

	applyVariable(found, &state)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *variableResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan variableModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	r.write(ctx, plan, &resp.Diagnostics, func(state variableModel) {
		resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
	})
}

func (r *variableResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state variableModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeleteVariable(ctx, state.WorkspaceID.ValueString(), state.Key.ValueString())
	if err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.Append(apiDiagnostic("Cannot delete the variable", err))
	}
}

func (r *variableResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	workspaceID, key, ok := strings.Cut(req.ID, "/")
	if !ok || workspaceID == "" || key == "" {
		resp.Diagnostics.AddError(
			"Unexpected import id",
			fmt.Sprintf("Expected <workspace_id>/<key>, got %q.", req.ID),
		)
		return
	}

	found, err := r.client.GetVariable(ctx, workspaceID, key)
	if err != nil {
		resp.Diagnostics.Append(apiDiagnostic("Cannot import the variable", err))
		return
	}
	if found.Sensitive {
		resp.Diagnostics.AddError(
			"Cannot import a sensitive variable",
			fmt.Sprintf(
				"Variable %q on workspace %q is sensitive, and the API never returns a sensitive value. "+
					"Importing it would leave the value empty in state and rewrite it on the next apply. "+
					"Recreate it through Terraform instead.",
				key, workspaceID,
			),
		)
		return
	}

	state := variableModel{}
	applyVariable(found, &state)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// write sends one variable to the PUT route, which upserts, and hands the
// resulting state to onSuccess.
func (r *variableResource) write(
	ctx context.Context,
	plan variableModel,
	diags *diag.Diagnostics,
	onSuccess func(variableModel),
) {
	body := client.VariableWrite{
		Value:       plan.Value.ValueString(),
		Category:    plan.Category.ValueString(),
		Sensitive:   plan.Sensitive.ValueBool(),
		Description: plan.Description.ValueString(),
	}

	stored, err := r.client.PutVariable(ctx, plan.WorkspaceID.ValueString(), plan.Key.ValueString(), body)
	if err != nil {
		diags.Append(apiDiagnostic("Cannot set the variable", err))
		return
	}

	state := plan
	applyVariable(stored, &state)
	onSuccess(state)
}

// applyVariable copies one API variable onto a state model. The value is taken
// from the response only when the API returned one: a sensitive value always
// comes back null, and overwriting the configured value with null would show a
// permanent diff on every plan.
func applyVariable(from *client.Variable, into *variableModel) {
	into.WorkspaceID = types.StringValue(from.WorkspaceID)
	into.Key = types.StringValue(from.Key)
	if !from.Sensitive && from.Value != nil {
		into.Value = types.StringValue(*from.Value)
	}
	into.Category = types.StringValue(from.Category)
	into.Sensitive = types.BoolValue(from.Sensitive)
	into.Description = types.StringValue(from.Description)
	into.CreatedAt = types.StringValue(from.CreatedAt)
	into.UpdatedAt = optionalString(from.UpdatedAt)
}
