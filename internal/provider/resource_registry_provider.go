package provider

import (
	"context"
	"fmt"

	"github.com/WebbPulse/terraform-provider-webbpulse/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                = (*registryProviderResource)(nil)
	_ resource.ResourceWithConfigure   = (*registryProviderResource)(nil)
	_ resource.ResourceWithImportState = (*registryProviderResource)(nil)
)

type registryProviderResource struct {
	client *client.Client
}

// NewRegistryProviderResource returns the webbpulse_registry_provider resource.
func NewRegistryProviderResource() resource.Resource { return &registryProviderResource{} }

type registryProviderModel struct {
	ID             types.String `tfsdk:"id"`
	Namespace      types.String `tfsdk:"namespace"`
	Type           types.String `tfsdk:"type"`
	Source         types.String `tfsdk:"source"`
	CreatedAt      types.String `tfsdk:"created_at"`
	ImportReleases types.Bool   `tfsdk:"import_releases"`
	ResyncTriggers types.Map    `tfsdk:"resync_triggers"`
	VCSRepo        types.Object `tfsdk:"vcs_repo"`
}

// Metadata sets the type name of the registry provider resource.
func (r *registryProviderResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_registry_provider"
}

// Schema defines the schema of the registry provider resource.
func (r *registryProviderResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	computedString := func(description string) schema.StringAttribute {
		return schema.StringAttribute{
			Computed:            true,
			MarkdownDescription: description,
			PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
		}
	}
	resp.Schema = schema.Schema{
		MarkdownDescription: "A private provider in the registry, connected to a GitHub repository named " +
			"`terraform-provider-<type>`. Each published GitHub release carrying signed GoReleaser " +
			"artifacts publishes that version. Versions publish in the background, so read them with the " +
			"`webbpulse_registry_provider` data source.",
		Attributes: map[string]schema.Attribute{
			"id":         computedString("The provider address, `namespace/type`."),
			"namespace":  computedString("The namespace, which is the repository owner."),
			"type":       computedString("The provider type, from the repository name `terraform-provider-<type>`."),
			"source":     computedString("The address a `required_providers` source names after the registry host."),
			"created_at": computedString("When the provider was connected."),
			"import_releases": schema.BoolAttribute{
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(true),
				MarkdownDescription: "Whether connecting imports the repository's existing releases. Read only on " +
					"create, so changing it later does nothing. Defaults to `true`.",
			},
			"resync_triggers": schema.MapAttribute{
				Optional:    true,
				ElementType: types.StringType,
				MarkdownDescription: "Arbitrary values whose change queues an import of every release in the " +
					"repository, in place. Published versions are left alone.",
			},
		},
		Blocks: map[string]schema.Block{
			"vcs_repo": registryVCSRepoBlock("The GitHub repository the provider publishes from, through the " +
				"environment's GitHub App. It has to be named `terraform-provider-<type>`. Required."),
		},
	}
}

// Configure stores the shared API client on the registry provider resource.
func (r *registryProviderResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	configureClient(req.ProviderData, &r.client, &resp.Diagnostics)
}

// Create connects the provider and records the result in state.
func (r *registryProviderResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan registryProviderModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	repo, diags := registryVCSRepoIdentifier(ctx, plan.VCSRepo)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	created, err := r.client.CreateRegistryProvider(ctx, client.ProviderCreate{
		VCSRepo:        repo,
		ImportReleases: plan.ImportReleases.ValueBoolPointer(),
	})
	if err != nil {
		resp.Diagnostics.Append(registryWriteDiagnostic("Cannot connect the provider", err))
		return
	}

	resp.Diagnostics.Append(applyRegistryProvider(created, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Read refreshes the provider, dropping it from state on a 404.
func (r *registryProviderResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state registryProviderModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	found, err := r.client.GetRegistryProvider(ctx, state.Namespace.ValueString(), state.Type.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.Append(apiDiagnostic("Cannot read the provider", err))
		return
	}

	resp.Diagnostics.Append(applyRegistryProvider(found, &state)...)
	if state.ImportReleases.IsNull() {
		state.ImportReleases = types.BoolValue(true)
	}
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update queues a resync when resync_triggers changed. Everything else is
// either create only or replaces the provider.
func (r *registryProviderResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state registryProviderModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resyncOnTriggerChange(plan.ResyncTriggers, state.ResyncTriggers, func() error {
		_, err := r.client.ResyncRegistryProvider(ctx, state.Namespace.ValueString(), state.Type.ValueString())
		return err
	})...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Delete removes the provider with every version, treating a 404 as already gone.
func (r *registryProviderResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state registryProviderModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.DeleteRegistryProvider(ctx, state.Namespace.ValueString(), state.Type.ValueString())
	if err == nil || client.IsNotFound(err) {
		return
	}
	resp.Diagnostics.Append(registryWriteDiagnostic("Cannot delete the provider", err))
}

// ImportState imports a provider by its `namespace/type` address.
func (r *registryProviderResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts, ok := splitImportID(req.ID, 2)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected import id",
			fmt.Sprintf("Expected namespace/type, such as WebbPulse/webbpulse, got %q.", req.ID),
		)
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("namespace"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("type"), parts[1])...)
}

// applyRegistryProvider copies an API provider onto a model, keeping the
// configured repository spelling when only its case differs.
func applyRegistryProvider(provider *client.RegistryProvider, model *registryProviderModel) diag.Diagnostics {
	model.ID = types.StringValue(provider.Namespace + "/" + provider.Type)
	model.Namespace = types.StringValue(provider.Namespace)
	model.Type = types.StringValue(provider.Type)
	model.Source = types.StringValue(provider.Source)
	model.CreatedAt = types.StringPointerValue(provider.CreatedAt)
	if model.VCSRepo.IsUnknown() || len(model.VCSRepo.AttributeTypes(context.Background())) == 0 {
		model.VCSRepo = types.ObjectNull(registryVCSRepoAttrTypes())
	}
	if model.ResyncTriggers.ElementType(context.Background()) == nil {
		model.ResyncTriggers = types.MapNull(types.StringType)
	}
	repo := provider.VCSRepo
	var diags diag.Diagnostics
	model.VCSRepo, diags = registryVCSRepoValue(model.VCSRepo, &repo)
	return diags
}
