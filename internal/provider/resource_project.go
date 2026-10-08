package provider

import (
	"context"
	"fmt"
	"regexp"

	"github.com/WebbPulse/terraform-provider-webbpulse/internal/client"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// projectIDPattern matches a stored project's id or the default project's.
const projectIDPattern = `^prj-(default|[0-9A-HJKMNP-TV-Z]{26})$`

// projectNamePattern mirrors the API's name rule: letters, digits, spaces,
// hyphens and underscores, starting with a letter or digit. It also refuses a
// trailing space or two spaces in a row, which the API would collapse and so
// leave state disagreeing with the configuration.
const projectNamePattern = `^[A-Za-z0-9](?:[A-Za-z0-9_-]| [A-Za-z0-9_-])*$`

var (
	_ resource.Resource                = (*projectResource)(nil)
	_ resource.ResourceWithConfigure   = (*projectResource)(nil)
	_ resource.ResourceWithImportState = (*projectResource)(nil)
)

type projectResource struct {
	client *client.Client
}

// NewProjectResource returns the webbpulse_project resource.
func NewProjectResource() resource.Resource { return &projectResource{} }

type projectModel struct {
	ID             types.String `tfsdk:"id"`
	Name           types.String `tfsdk:"name"`
	Description    types.String `tfsdk:"description"`
	WorkspaceCount types.Int64  `tfsdk:"workspace_count"`
	CreatedAt      types.String `tfsdk:"created_at"`
	UpdatedAt      types.String `tfsdk:"updated_at"`
}

// Metadata sets the type name of the project resource.
func (r *projectResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_project"
}

// Schema defines the schema of the project resource.
func (r *projectResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "One project, a group of workspaces like an HCP Terraform project. Place a workspace " +
			"in it with `project_id` on `webbpulse_workspace`. The default project is built in and cannot be " +
			"managed here; read it with the `webbpulse_project` data source instead.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The project id, such as `prj-01JABCDEF0123456789ABCDEFG`.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "The project name, up to 40 letters, digits, single spaces, hyphens and " +
					"underscores, starting with a letter or digit. Unique across the environment ignoring case, " +
					"so a name another project holds is refused with `PROJECT_NAME_TAKEN`. Renames in place.",
				Validators: []validator.String{
					stringvalidator.LengthBetween(1, 40),
					stringvalidator.RegexMatches(
						regexp.MustCompile(projectNamePattern),
						"must start with a letter or digit and hold only letters, digits, single spaces, hyphens and underscores",
					),
				},
			},
			"description": schema.StringAttribute{
				Optional:            true,
				Computed:            true,
				Default:             stringdefault.StaticString(""),
				MarkdownDescription: "A description for this project, up to 256 characters. Removing it clears it.",
				Validators:          []validator.String{stringvalidator.LengthAtMost(256)},
			},
			"workspace_count": schema.Int64Attribute{
				Computed:            true,
				MarkdownDescription: "How many workspaces the project held when last read.",
			},
			"created_at": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "When the project was created.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"updated_at": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "When the project was last changed.",
			},
		},
	}
}

// Configure stores the shared API client on the project resource.
func (r *projectResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	configureClient(req.ProviderData, &r.client, &resp.Diagnostics)
}

// Create creates the project.
func (r *projectResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan projectModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	created, err := r.client.CreateProject(ctx, client.ProjectCreate{
		Name:        plan.Name.ValueString(),
		Description: plan.Description.ValueString(),
	})
	if err != nil {
		resp.Diagnostics.Append(projectDiagnostic("Cannot create the project", err))
		return
	}

	var state projectModel
	applyProject(created, &state)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Read refreshes the project, dropping it from state on a 404.
func (r *projectResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state projectModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	found, err := r.client.GetProject(ctx, state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.Append(apiDiagnostic("Cannot read the project", err))
		return
	}

	applyProject(found, &state)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update sends a partial edit carrying only the changed attributes.
func (r *projectResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state projectModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := client.ProjectUpdate{
		Name:        changedString(plan.Name, state.Name),
		Description: changedString(plan.Description, state.Description),
	}
	updated, err := r.client.UpdateProject(ctx, state.ID.ValueString(), body)
	if err != nil {
		resp.Diagnostics.Append(projectDiagnostic("Cannot update the project", err))
		return
	}

	var next projectModel
	applyProject(updated, &next)
	resp.Diagnostics.Append(resp.State.Set(ctx, &next)...)
}

// Delete deletes the project, treating a 404 as already gone.
func (r *projectResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state projectModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeleteProject(ctx, state.ID.ValueString())
	if err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.Append(projectDiagnostic("Cannot delete the project", err))
	}
}

// ImportState imports a project by its id. The default project is refused,
// since it can be neither changed nor destroyed.
func (r *projectResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	if req.ID == client.DefaultProjectID {
		resp.Diagnostics.AddError(
			"The default project cannot be managed",
			fmt.Sprintf("%s is built in and can be neither renamed nor deleted, so it cannot be imported. "+
				"Read it with the webbpulse_project data source, or leave project_id unset on a workspace "+
				"to place it there.", client.DefaultProjectID),
		)
		return
	}
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// projectDiagnostic turns a refused project write into a diagnostic that
// names the API's error code and what to do about it.
func projectDiagnostic(summary string, err error) diag.Diagnostic {
	switch client.ErrorCode(err) {
	case client.ProjectNameTakenCode:
		return diag.NewAttributeErrorDiagnostic(
			path.Root("name"),
			"The project name is taken",
			client.ProjectNameTakenCode+": another project already holds this name, ignoring case, and "+
				"\"Default Project\" is reserved. Choose another name, or import the existing project with "+
				"terraform import. "+err.Error(),
		)
	case client.ProjectNotEmptyCode:
		return diag.NewErrorDiagnostic(
			"The project still holds workspaces",
			client.ProjectNotEmptyCode+": a project is deleted only once it is empty, and nothing is moved "+
				"for you. Move its workspaces to another project, or to the default by removing their "+
				"project_id, then destroy again. "+err.Error(),
		)
	case client.DefaultProjectReadOnlyCode:
		return diag.NewErrorDiagnostic(
			"The default project cannot be changed",
			client.DefaultProjectReadOnlyCode+": the default project is built in and can be neither renamed "+
				"nor deleted. Remove it from state with terraform state rm and read it with the "+
				"webbpulse_project data source instead. "+err.Error(),
		)
	}
	return apiDiagnostic(summary, err)
}

// applyProject copies one API project onto a state model.
func applyProject(from *client.Project, into *projectModel) {
	into.ID = types.StringValue(from.ProjectID)
	into.Name = types.StringValue(from.Name)
	into.Description = types.StringValue(from.Description)
	into.WorkspaceCount = types.Int64Value(from.WorkspaceCount)
	into.CreatedAt = optionalString(from.CreatedAt)
	into.UpdatedAt = optionalString(from.UpdatedAt)
}
