// Package provider holds the Terraform provider for the WebbPulse Terraform
// control plane: its schema, its resources and its data sources.
package provider

import (
	"context"
	"os"

	"github.com/WebbPulse/terraform-provider-webbpulse/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// EnvHost is the environment variable the host falls back to.
const EnvHost = "WEBBPULSE_TF_HOST"

// EnvToken is the environment variable the token falls back to.
const EnvToken = "WEBBPULSE_TF_TOKEN"

var _ provider.Provider = (*webbpulseProvider)(nil)

type webbpulseProvider struct {
	version string
}

// New returns the provider constructor the plugin server is handed.
func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &webbpulseProvider{version: version}
	}
}

type providerModel struct {
	Host  types.String `tfsdk:"host"`
	Token types.String `tfsdk:"token"`
}

// Metadata sets the provider type name and version.
func (p *webbpulseProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "webbpulse"
	resp.Version = p.version
}

// Schema defines the schema of the provider.
func (p *webbpulseProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages workspaces and variables in the WebbPulse Terraform control plane.",
		Attributes: map[string]schema.Attribute{
			"host": schema.StringAttribute{
				Optional: true,
				MarkdownDescription: "Base URL of the control plane API, such as " +
					"`https://api.staging.terraform.webbpulse.com`. The `/api/v1` suffix is added when absent. " +
					"Falls back to the `" + EnvHost + "` environment variable. There is no default, so a " +
					"configuration always names the environment it manages.",
			},
			"token": schema.StringAttribute{
				Optional:  true,
				Sensitive: true,
				MarkdownDescription: "A bearer token: an agent API key, which carries a `wpk_` prefix, or a " +
					"user JWT. Falls back to the `" + EnvToken + "` environment variable.",
			},
		},
	}
}

// Configure builds the API client from configuration and environment and hands it to every resource and data source.
func (p *webbpulseProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var config providerModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if config.Host.IsUnknown() {
		resp.Diagnostics.AddAttributeError(
			path.Root("host"),
			"Host is not known at configure time",
			"The provider cannot be configured against an unknown host. Set it to a literal value, or "+
				"supply it through the "+EnvHost+" environment variable.",
		)
	}
	if config.Token.IsUnknown() {
		resp.Diagnostics.AddAttributeError(
			path.Root("token"),
			"Token is not known at configure time",
			"The provider cannot be configured against an unknown token. Set it to a literal value, or "+
				"supply it through the "+EnvToken+" environment variable.",
		)
	}
	if resp.Diagnostics.HasError() {
		return
	}

	host := os.Getenv(EnvHost)
	if !config.Host.IsNull() {
		host = config.Host.ValueString()
	}
	token := os.Getenv(EnvToken)
	if !config.Token.IsNull() {
		token = config.Token.ValueString()
	}

	if host == "" {
		resp.Diagnostics.AddAttributeError(
			path.Root("host"),
			"Missing API host",
			"Set the provider's host attribute or the "+EnvHost+" environment variable to the control plane URL.",
		)
	}
	if token == "" {
		resp.Diagnostics.AddAttributeError(
			path.Root("token"),
			"Missing API token",
			"Set the provider's token attribute or the "+EnvToken+" environment variable to an agent API key or a user JWT.",
		)
	}
	if resp.Diagnostics.HasError() {
		return
	}

	apiClient, err := client.New(host, token, client.WithUserAgent("terraform-provider-webbpulse/"+p.version))
	if err != nil {
		resp.Diagnostics.AddError("Cannot build the API client", err.Error())
		return
	}

	resp.DataSourceData = apiClient
	resp.ResourceData = apiClient
}

// Resources lists the resources the provider serves.
func (p *webbpulseProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewWorkspaceResource,
		NewVariableResource,
	}
}

// DataSources lists the data sources the provider serves.
func (p *webbpulseProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		NewWorkspaceDataSource,
		NewWorkspacesDataSource,
		NewRunRoleCheckDataSource,
	}
}
