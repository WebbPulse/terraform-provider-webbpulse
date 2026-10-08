package provider

import (
	"context"

	"github.com/WebbPulse/terraform-provider-webbpulse/internal/client"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ datasource.DataSource              = (*projectDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*projectDataSource)(nil)
)

type projectDataSource struct {
	client *client.Client
}

// NewProjectDataSource returns the webbpulse_project data source.
func NewProjectDataSource() datasource.DataSource { return &projectDataSource{} }

type projectDataModel struct {
	ID             types.String `tfsdk:"id"`
	Name           types.String `tfsdk:"name"`
	Description    types.String `tfsdk:"description"`
	IsDefault      types.Bool   `tfsdk:"is_default"`
	WorkspaceCount types.Int64  `tfsdk:"workspace_count"`
	CreatedAt      types.String `tfsdk:"created_at"`
	UpdatedAt      types.String `tfsdk:"updated_at"`
}

// Metadata sets the type name of the project data source.
func (d *projectDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_project"
}

// Schema defines the schema of the project data source.
func (d *projectDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "One project, looked up by name ignoring case, the default project included as " +
			"`Default Project`. The API has no name lookup route, so this lists every project and filters in " +
			"the provider.",
		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "The project name to look up.",
				Validators:          []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"id":          schema.StringAttribute{Computed: true, MarkdownDescription: "The project id, `prj-default` for the default project."},
			"description": schema.StringAttribute{Computed: true, MarkdownDescription: "The project description."},
			"is_default": schema.BoolAttribute{
				Computed:            true,
				MarkdownDescription: "Whether this is the default project, which holds every workspace not moved elsewhere.",
			},
			"workspace_count": schema.Int64Attribute{Computed: true, MarkdownDescription: "How many workspaces the project holds."},
			"created_at":      schema.StringAttribute{Computed: true, MarkdownDescription: "When the project was created. Null for the default project."},
			"updated_at":      schema.StringAttribute{Computed: true, MarkdownDescription: "When the project was last changed. Null for the default project."},
		},
	}
}

// Configure stores the shared API client on the project data source.
func (d *projectDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	configureClient(req.ProviderData, &d.client, &resp.Diagnostics)
}

// Read looks one project up by name.
func (d *projectDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config projectDataModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	found, err := d.client.GetProjectByName(ctx, config.Name.ValueString())
	if err != nil {
		resp.Diagnostics.Append(apiDiagnostic("Cannot read the project", err))
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &projectDataModel{
		ID:             types.StringValue(found.ProjectID),
		Name:           types.StringValue(found.Name),
		Description:    types.StringValue(found.Description),
		IsDefault:      types.BoolValue(found.IsDefault),
		WorkspaceCount: types.Int64Value(found.WorkspaceCount),
		CreatedAt:      optionalString(found.CreatedAt),
		UpdatedAt:      optionalString(found.UpdatedAt),
	})...)
}
