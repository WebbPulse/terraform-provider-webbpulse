// Package provider holds the Terraform provider for the WebbPulse Terraform
// control plane: its schema, its resources and its data sources.
package provider

import (
	"context"
	"fmt"
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

// EnvOriginVerify is the environment variable the origin_verify value falls back to.
const EnvOriginVerify = "WEBBPULSE_TF_ORIGIN_VERIFY"

var _ provider.Provider = (*webbpulseProvider)(nil)

type webbpulseProvider struct {
	version       string
	clientOptions []client.Option
	readSSM       ssmParameterReader
}

// New returns the provider constructor the plugin server is handed.
func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &webbpulseProvider{version: version}
	}
}

type providerModel struct {
	Host         types.String `tfsdk:"host"`
	Token        types.String `tfsdk:"token"`
	OriginVerify types.String `tfsdk:"origin_verify"`

	OriginVerifySSMParameter types.String `tfsdk:"origin_verify_ssm_parameter"`
}

// Metadata sets the provider type name and version.
func (p *webbpulseProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "webbpulse"
	resp.Version = p.version
}

// Schema defines the schema of the provider.
func (p *webbpulseProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages workspaces, variables and private registry modules and providers in the WebbPulse " +
			"Terraform control plane.",
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
			"origin_verify": schema.StringAttribute{
				Optional:  true,
				Sensitive: true,
				MarkdownDescription: "The edge access gate value, sent as the `" + client.OriginVerifyHeader + "` " +
					"header on every API request. Needed when the control plane sits behind the access gate, which " +
					"otherwise answers 403. Falls back to the `" + EnvOriginVerify + "` environment variable. Left " +
					"unset, the value is read from `origin_verify_ssm_parameter` when that is set, and otherwise no " +
					"header is sent.",
			},
			"origin_verify_ssm_parameter": schema.StringAttribute{
				Optional: true,
				MarkdownDescription: "The name or ARN of the SSM parameter holding the edge access gate value, such as " +
					"`/webbpulse-terraform-prod/access-gate/origin-verify`. The provider reads it with decryption " +
					"using the ambient AWS credentials and region (an ARN is read in its own region) and keeps the " +
					"value in memory only, never in state or logs. Used only when no direct value is given: " +
					"`origin_verify` or `" + EnvOriginVerify + "` wins when set. Falls back to the `" +
					EnvOriginVerifySSMParameter + "` environment variable. The credentials need `ssm:GetParameter` " +
					"on the parameter and `kms:Decrypt` on its key.",
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
	if config.OriginVerify.IsUnknown() {
		resp.Diagnostics.AddAttributeError(
			path.Root("origin_verify"),
			"Origin verify value is not known at configure time",
			"The provider cannot be configured with an unknown access gate value. Set it to a literal value, or "+
				"supply it through the "+EnvOriginVerify+" environment variable.",
		)
	}
	if config.OriginVerifySSMParameter.IsUnknown() {
		resp.Diagnostics.AddAttributeError(
			path.Root("origin_verify_ssm_parameter"),
			"Origin verify SSM parameter is not known at configure time",
			"The provider cannot read the access gate value from an unknown parameter. Set it to a literal value, "+
				"or supply it through the "+EnvOriginVerifySSMParameter+" environment variable.",
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
	originVerify := os.Getenv(EnvOriginVerify)
	if !config.OriginVerify.IsNull() {
		originVerify = config.OriginVerify.ValueString()
	}
	originVerifyParameter := os.Getenv(EnvOriginVerifySSMParameter)
	if !config.OriginVerifySSMParameter.IsNull() {
		originVerifyParameter = config.OriginVerifySSMParameter.ValueString()
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

	if originVerify == "" && originVerifyParameter != "" {
		readSSM := p.readSSM
		if readSSM == nil {
			readSSM = readSSMParameter
		}
		value, err := readSSM(ctx, originVerifyParameter)
		if err != nil {
			resp.Diagnostics.AddAttributeError(
				path.Root("origin_verify_ssm_parameter"),
				"Cannot read the access gate value from SSM",
				fmt.Sprintf("Reading SSM parameter %q with decryption failed: %v", originVerifyParameter, err),
			)
			return
		}
		originVerify = value
	}

	options := []client.Option{client.WithUserAgent("terraform-provider-webbpulse/" + p.version)}
	if originVerify != "" {
		options = append(options, client.WithOriginVerify(originVerify))
	}
	options = append(options, p.clientOptions...)
	apiClient, err := client.New(host, token, options...)
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
		NewRegistryModuleResource,
		NewRegistryProviderResource,
	}
}

// DataSources lists the data sources the provider serves.
func (p *webbpulseProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		NewWorkspaceDataSource,
		NewWorkspacesDataSource,
		NewRunRoleCheckDataSource,
		NewRegistryModuleDataSource,
		NewRegistryProviderDataSource,
	}
}
