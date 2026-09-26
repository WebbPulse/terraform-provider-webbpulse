package provider

import (
	"context"

	"github.com/WebbPulse/terraform-provider-webbpulse/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ datasource.DataSource              = (*workspacesDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*workspacesDataSource)(nil)
)

type workspacesDataSource struct {
	client *client.Client
}

// NewWorkspacesDataSource returns the webbpulse_workspaces data source.
func NewWorkspacesDataSource() datasource.DataSource { return &workspacesDataSource{} }

type workspacesModel struct {
	IDs   types.List `tfsdk:"ids"`
	Names types.List `tfsdk:"names"`
}

// Metadata sets the type name of the workspaces data source.
func (d *workspacesDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_workspaces"
}

// Schema defines the schema of the workspaces data source.
func (d *workspacesDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Every workspace in the environment. The API takes no filters on its listing " +
			"route, so this returns all of them and a configuration narrows the result itself.",
		Attributes: map[string]schema.Attribute{
			"ids": schema.ListAttribute{
				Computed:            true,
				ElementType:         types.StringType,
				MarkdownDescription: "Every workspace id, in the order the API returned them.",
			},
			"names": schema.ListAttribute{
				Computed:            true,
				ElementType:         types.StringType,
				MarkdownDescription: "Every workspace name, positionally matching `ids`.",
			},
		},
	}
}

// Configure stores the shared API client on the workspaces data source.
func (d *workspacesDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	configureClient(req.ProviderData, &d.client, &resp.Diagnostics)
}

// Read lists every workspace id and name.
func (d *workspacesDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	items, err := d.client.ListWorkspaces(ctx)
	if err != nil {
		resp.Diagnostics.Append(apiDiagnostic("Cannot list workspaces", err))
		return
	}

	ids := make([]string, 0, len(items))
	names := make([]string, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.WorkspaceID)
		names = append(names, item.Name)
	}

	idList, idDiags := types.ListValueFrom(ctx, types.StringType, ids)
	resp.Diagnostics.Append(idDiags...)
	nameList, nameDiags := types.ListValueFrom(ctx, types.StringType, names)
	resp.Diagnostics.Append(nameDiags...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &workspacesModel{IDs: idList, Names: nameList})...)
}
