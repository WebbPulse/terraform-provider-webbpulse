package provider

import (
	"context"

	"github.com/WebbPulse/terraform-provider-webbpulse/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ datasource.DataSource              = (*registryModuleDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*registryModuleDataSource)(nil)
	_ datasource.DataSource              = (*registryProviderDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*registryProviderDataSource)(nil)
)

type registryModuleDataSource struct {
	client *client.Client
}

type registryProviderDataSource struct {
	client *client.Client
}

// NewRegistryModuleDataSource returns the webbpulse_registry_module data source.
func NewRegistryModuleDataSource() datasource.DataSource { return &registryModuleDataSource{} }

// NewRegistryProviderDataSource returns the webbpulse_registry_provider data source.
func NewRegistryProviderDataSource() datasource.DataSource { return &registryProviderDataSource{} }

type registryModuleVersionModel struct {
	Version     types.String `tfsdk:"version"`
	Status      types.String `tfsdk:"status"`
	Error       types.String `tfsdk:"error"`
	Tag         types.String `tfsdk:"tag"`
	SHA         types.String `tfsdk:"sha"`
	PublishedAt types.String `tfsdk:"published_at"`
}

type registryModuleDataModel struct {
	ID                types.String                 `tfsdk:"id"`
	Namespace         types.String                 `tfsdk:"namespace"`
	Name              types.String                 `tfsdk:"name"`
	ModuleProvider    types.String                 `tfsdk:"module_provider"`
	Source            types.String                 `tfsdk:"source"`
	VCSRepo           types.String                 `tfsdk:"vcs_repo"`
	CreatedAt         types.String                 `tfsdk:"created_at"`
	PublishedVersions []types.String               `tfsdk:"published_versions"`
	Versions          []registryModuleVersionModel `tfsdk:"versions"`
}

type registryPlatformModel struct {
	OS       types.String `tfsdk:"os"`
	Arch     types.String `tfsdk:"arch"`
	Filename types.String `tfsdk:"filename"`
	Shasum   types.String `tfsdk:"shasum"`
}

type registryProviderVersionModel struct {
	Version     types.String            `tfsdk:"version"`
	Status      types.String            `tfsdk:"status"`
	Error       types.String            `tfsdk:"error"`
	Tag         types.String            `tfsdk:"tag"`
	Protocols   []types.String          `tfsdk:"protocols"`
	KeyID       types.String            `tfsdk:"key_id"`
	PublishedAt types.String            `tfsdk:"published_at"`
	Platforms   []registryPlatformModel `tfsdk:"platforms"`
}

type registryProviderDataModel struct {
	ID                types.String                   `tfsdk:"id"`
	Namespace         types.String                   `tfsdk:"namespace"`
	Type              types.String                   `tfsdk:"type"`
	Source            types.String                   `tfsdk:"source"`
	VCSRepo           types.String                   `tfsdk:"vcs_repo"`
	CreatedAt         types.String                   `tfsdk:"created_at"`
	PublishedVersions []types.String                 `tfsdk:"published_versions"`
	Versions          []registryProviderVersionModel `tfsdk:"versions"`
}

func computedDataString(description string) schema.StringAttribute {
	return schema.StringAttribute{Computed: true, MarkdownDescription: description}
}

func versionAttributes() map[string]schema.Attribute {
	return map[string]schema.Attribute{
		"version": computedDataString("The semantic version."),
		"status":  computedDataString("`pending`, `published` or `failed`."),
		"error":   computedDataString("Why publishing failed, or null."),
		"tag":     computedDataString("The git tag the version came from."),
		"published_at": computedDataString(
			"When the version published, or null while it is pending or after it failed."),
	}
}

// Metadata sets the type name of the registry module data source.
func (d *registryModuleDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_registry_module"
}

// Schema defines the schema of the registry module data source.
func (d *registryModuleDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	versionAttrs := versionAttributes()
	versionAttrs["sha"] = computedDataString("The commit the tag pointed at.")
	resp.Schema = schema.Schema{
		MarkdownDescription: "One private module in the registry with every version published or attempted, " +
			"newest first.",
		Attributes: map[string]schema.Attribute{
			"id":              computedDataString("The module address, `namespace/name/provider`."),
			"namespace":       schema.StringAttribute{Required: true, MarkdownDescription: "The namespace, which is the repository owner."},
			"name":            schema.StringAttribute{Required: true, MarkdownDescription: "The module name."},
			"module_provider": schema.StringAttribute{Required: true, MarkdownDescription: "The module's provider."},
			"source":          computedDataString("The address a module block's `source` names after the registry host."),
			"vcs_repo":        computedDataString("The connected repository as `owner/name`, or null."),
			"created_at":      computedDataString("When the module was connected, or null."),
			"published_versions": schema.ListAttribute{
				Computed:            true,
				ElementType:         types.StringType,
				MarkdownDescription: "Every published version, newest first.",
			},
			"versions": schema.ListNestedAttribute{
				Computed:            true,
				MarkdownDescription: "Every version published or attempted, newest first.",
				NestedObject:        schema.NestedAttributeObject{Attributes: versionAttrs},
			},
		},
	}
}

// Configure stores the shared API client on the registry module data source.
func (d *registryModuleDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	configureClient(req.ProviderData, &d.client, &resp.Diagnostics)
}

// Read reads one module and every version.
func (d *registryModuleDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config registryModuleDataModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}
	found, err := d.client.GetModule(ctx, config.Namespace.ValueString(), config.Name.ValueString(), config.ModuleProvider.ValueString())
	if err != nil {
		resp.Diagnostics.Append(apiDiagnostic("Cannot read the module", err))
		return
	}

	state := registryModuleDataModel{
		ID:                types.StringValue(found.Namespace + "/" + found.Name + "/" + found.Provider),
		Namespace:         types.StringValue(found.Namespace),
		Name:              types.StringValue(found.Name),
		ModuleProvider:    types.StringValue(found.Provider),
		Source:            types.StringValue(found.Source),
		VCSRepo:           types.StringPointerValue(found.VCSRepo),
		CreatedAt:         types.StringPointerValue(found.CreatedAt),
		PublishedVersions: []types.String{},
		Versions:          []registryModuleVersionModel{},
	}
	for _, version := range found.Versions {
		if version.Status == "published" {
			state.PublishedVersions = append(state.PublishedVersions, types.StringValue(version.Version))
		}
		state.Versions = append(state.Versions, registryModuleVersionModel{
			Version:     types.StringValue(version.Version),
			Status:      types.StringValue(version.Status),
			Error:       types.StringPointerValue(version.Error),
			Tag:         types.StringPointerValue(version.Tag),
			SHA:         types.StringValue(version.SHA),
			PublishedAt: types.StringPointerValue(version.PublishedAt),
		})
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Metadata sets the type name of the registry provider data source.
func (d *registryProviderDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_registry_provider"
}

// Schema defines the schema of the registry provider data source.
func (d *registryProviderDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	versionAttrs := versionAttributes()
	versionAttrs["protocols"] = schema.ListAttribute{
		Computed:            true,
		ElementType:         types.StringType,
		MarkdownDescription: "The plugin protocol versions the version speaks, such as `6.0`.",
	}
	versionAttrs["key_id"] = computedDataString("The GPG key id the checksums are signed with, or null.")
	versionAttrs["platforms"] = schema.ListNestedAttribute{
		Computed:            true,
		MarkdownDescription: "Every os and architecture the version ships for.",
		NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
			"os":       computedDataString("The operating system, such as `linux`."),
			"arch":     computedDataString("The architecture, such as `amd64`."),
			"filename": computedDataString("The zip archive's file name."),
			"shasum":   computedDataString("The zip archive's SHA-256."),
		}},
	}
	resp.Schema = schema.Schema{
		MarkdownDescription: "One private provider in the registry with every version published or attempted, " +
			"newest first.",
		Attributes: map[string]schema.Attribute{
			"id":         computedDataString("The provider address, `namespace/type`."),
			"namespace":  schema.StringAttribute{Required: true, MarkdownDescription: "The namespace, which is the repository owner."},
			"type":       schema.StringAttribute{Required: true, MarkdownDescription: "The provider type."},
			"source":     computedDataString("The address a `required_providers` source names after the registry host."),
			"vcs_repo":   computedDataString("The connected repository as `owner/name`."),
			"created_at": computedDataString("When the provider was connected, or null."),
			"published_versions": schema.ListAttribute{
				Computed:            true,
				ElementType:         types.StringType,
				MarkdownDescription: "Every published version, newest first.",
			},
			"versions": schema.ListNestedAttribute{
				Computed:            true,
				MarkdownDescription: "Every version published or attempted, newest first.",
				NestedObject:        schema.NestedAttributeObject{Attributes: versionAttrs},
			},
		},
	}
}

// Configure stores the shared API client on the registry provider data source.
func (d *registryProviderDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	configureClient(req.ProviderData, &d.client, &resp.Diagnostics)
}

// Read reads one provider and every version.
func (d *registryProviderDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config registryProviderDataModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}
	found, err := d.client.GetRegistryProvider(ctx, config.Namespace.ValueString(), config.Type.ValueString())
	if err != nil {
		resp.Diagnostics.Append(apiDiagnostic("Cannot read the provider", err))
		return
	}

	state := registryProviderDataModel{
		ID:                types.StringValue(found.Namespace + "/" + found.Type),
		Namespace:         types.StringValue(found.Namespace),
		Type:              types.StringValue(found.Type),
		Source:            types.StringValue(found.Source),
		VCSRepo:           types.StringValue(found.VCSRepo),
		CreatedAt:         types.StringPointerValue(found.CreatedAt),
		PublishedVersions: []types.String{},
		Versions:          []registryProviderVersionModel{},
	}
	for _, version := range found.Versions {
		if version.Status == "published" {
			state.PublishedVersions = append(state.PublishedVersions, types.StringValue(version.Version))
		}
		item := registryProviderVersionModel{
			Version:     types.StringValue(version.Version),
			Status:      types.StringValue(version.Status),
			Error:       types.StringPointerValue(version.Error),
			Tag:         types.StringValue(version.Tag),
			Protocols:   []types.String{},
			KeyID:       types.StringPointerValue(version.KeyID),
			PublishedAt: types.StringPointerValue(version.PublishedAt),
			Platforms:   []registryPlatformModel{},
		}
		for _, protocol := range version.Protocols {
			item.Protocols = append(item.Protocols, types.StringValue(protocol))
		}
		for _, platform := range version.Platforms {
			item.Platforms = append(item.Platforms, registryPlatformModel{
				OS:       types.StringValue(platform.OS),
				Arch:     types.StringValue(platform.Arch),
				Filename: types.StringValue(platform.Filename),
				Shasum:   types.StringValue(platform.Shasum),
			})
		}
		state.Versions = append(state.Versions, item)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
