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
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                = (*registryModuleResource)(nil)
	_ resource.ResourceWithConfigure   = (*registryModuleResource)(nil)
	_ resource.ResourceWithImportState = (*registryModuleResource)(nil)
)

type registryModuleResource struct {
	client *client.Client
}

// NewRegistryModuleResource returns the webbpulse_registry_module resource.
func NewRegistryModuleResource() resource.Resource { return &registryModuleResource{} }

type registryModuleModel struct {
	ID             types.String `tfsdk:"id"`
	Namespace      types.String `tfsdk:"namespace"`
	Name           types.String `tfsdk:"name"`
	ModuleProvider types.String `tfsdk:"module_provider"`
	Source         types.String `tfsdk:"source"`
	CreatedAt      types.String `tfsdk:"created_at"`
	ImportTags     types.Bool   `tfsdk:"import_tags"`
	ResyncTriggers types.Map    `tfsdk:"resync_triggers"`
	VCSRepo        types.Object `tfsdk:"vcs_repo"`
}

// Metadata sets the type name of the registry module resource.
func (r *registryModuleResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_registry_module"
}

// Schema defines the schema of the registry module resource.
func (r *registryModuleResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	computedString := func(description string) schema.StringAttribute {
		return schema.StringAttribute{
			Computed:            true,
			MarkdownDescription: description,
			PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
		}
	}
	addressPart := func(description string, pattern *regexp.Regexp, message string) schema.StringAttribute {
		return schema.StringAttribute{
			Optional:            true,
			Computed:            true,
			MarkdownDescription: description,
			Validators:          []validator.String{stringvalidator.RegexMatches(pattern, message)},
			PlanModifiers: []planmodifier.String{
				stringplanmodifier.UseStateForUnknown(),
				stringplanmodifier.RequiresReplaceIfConfigured(),
			},
		}
	}
	resp.Schema = schema.Schema{
		MarkdownDescription: "A private module in the registry, connected to a GitHub repository like " +
			"`tfe_registry_module` with `vcs_repo`. Each `vX.Y.Z` or `X.Y.Z` tag pushed to the repository " +
			"publishes that version. Versions publish in the background, so read them with the " +
			"`webbpulse_registry_module` data source.",
		Attributes: map[string]schema.Attribute{
			"id":        computedString("The module address, `namespace/name/provider`."),
			"namespace": computedString("The namespace, which is the repository owner."),
			"name": addressPart(
				"The module name. Defaults to `<name>` from a repository named `terraform-<provider>-<name>`, "+
					"and is required for any other repository. Changing it replaces the module.",
				regexp.MustCompile(`^[0-9A-Za-z](?:[0-9A-Za-z_-]{0,62}[0-9A-Za-z])?$`),
				"must be 1 to 64 letters, digits, underscores or hyphens, starting and ending with a letter or digit",
			),
			"module_provider": addressPart(
				"The module's provider, the third part of its address. Defaults to `<provider>` from a "+
					"repository named `terraform-<provider>-<name>`, and is required for any other repository. "+
					"Changing it replaces the module.",
				regexp.MustCompile(`^[0-9a-z]{1,64}$`),
				"must be 1 to 64 lowercase letters or digits",
			),
			"source": computedString("The address a module block's `source` names after the registry host, " +
				"such as `WebbPulse/network/aws`."),
			"created_at": computedString("When the module was connected."),
			"import_tags": schema.BoolAttribute{
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(true),
				MarkdownDescription: "Whether connecting imports the repository's existing semantic version tags. " +
					"Read only on create, so changing it later does nothing. Defaults to `true`.",
			},
			"resync_triggers": schema.MapAttribute{
				Optional:    true,
				ElementType: types.StringType,
				MarkdownDescription: "Arbitrary values whose change queues an import of every semantic version tag " +
					"in the repository, in place. Picks up tags GitHub sent no push event for. Published " +
					"versions are left alone.",
			},
		},
		Blocks: map[string]schema.Block{
			"vcs_repo": registryVCSRepoBlock("The GitHub repository the module publishes from, through the " +
				"environment's GitHub App. Required."),
		},
	}
}

// Configure stores the shared API client on the registry module resource.
func (r *registryModuleResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	configureClient(req.ProviderData, &r.client, &resp.Diagnostics)
}

// Create connects the module and records the result in state.
func (r *registryModuleResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan registryModuleModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	repo, diags := registryVCSRepoIdentifier(ctx, plan.VCSRepo)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	body := client.ModuleCreate{VCSRepo: repo, ImportTags: plan.ImportTags.ValueBoolPointer()}
	if !plan.Name.IsUnknown() {
		body.Name = plan.Name.ValueString()
	}
	if !plan.ModuleProvider.IsUnknown() {
		body.Provider = plan.ModuleProvider.ValueString()
	}
	created, err := r.client.CreateModule(ctx, body)
	if err != nil {
		resp.Diagnostics.Append(registryWriteDiagnostic("Cannot connect the module", err))
		return
	}

	resp.Diagnostics.Append(applyRegistryModule(created, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Read refreshes the module, dropping it from state on a 404.
func (r *registryModuleResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state registryModuleModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	found, err := r.client.GetModule(ctx, state.Namespace.ValueString(), state.Name.ValueString(), state.ModuleProvider.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.Append(apiDiagnostic("Cannot read the module", err))
		return
	}

	resp.Diagnostics.Append(applyRegistryModule(found, &state)...)
	if state.ImportTags.IsNull() {
		state.ImportTags = types.BoolValue(true)
	}
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update queues a resync when resync_triggers changed. Everything else is
// either create only or replaces the module.
func (r *registryModuleResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state registryModuleModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resyncOnTriggerChange(plan.ResyncTriggers, state.ResyncTriggers, func() error {
		_, err := r.client.ResyncModule(ctx, state.Namespace.ValueString(), state.Name.ValueString(), state.ModuleProvider.ValueString())
		return err
	})...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Delete removes the module with every version, treating a 404 as already gone.
func (r *registryModuleResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state registryModuleModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.DeleteModule(ctx, state.Namespace.ValueString(), state.Name.ValueString(), state.ModuleProvider.ValueString())
	if err == nil || client.IsNotFound(err) {
		return
	}
	resp.Diagnostics.Append(registryWriteDiagnostic("Cannot delete the module", err))
}

// ImportState imports a module by its `namespace/name/provider` address.
func (r *registryModuleResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts, ok := splitImportID(req.ID, 3)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected import id",
			fmt.Sprintf("Expected namespace/name/provider, such as WebbPulse/network/aws, got %q.", req.ID),
		)
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("namespace"), parts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), parts[1])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("module_provider"), parts[2])...)
}

// applyRegistryModule copies an API module onto a model, keeping the
// configured repository spelling when only its case differs.
func applyRegistryModule(module *client.Module, model *registryModuleModel) diag.Diagnostics {
	model.ID = types.StringValue(module.Namespace + "/" + module.Name + "/" + module.Provider)
	model.Namespace = types.StringValue(module.Namespace)
	model.Name = types.StringValue(module.Name)
	model.ModuleProvider = types.StringValue(module.Provider)
	model.Source = types.StringValue(module.Source)
	model.CreatedAt = types.StringPointerValue(module.CreatedAt)
	if model.VCSRepo.IsUnknown() || len(model.VCSRepo.AttributeTypes(context.Background())) == 0 {
		model.VCSRepo = types.ObjectNull(registryVCSRepoAttrTypes())
	}
	if model.ResyncTriggers.ElementType(context.Background()) == nil {
		model.ResyncTriggers = types.MapNull(types.StringType)
	}
	var diags diag.Diagnostics
	model.VCSRepo, diags = registryVCSRepoValue(model.VCSRepo, module.VCSRepo)
	return diags
}
