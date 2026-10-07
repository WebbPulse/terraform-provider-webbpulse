package provider

import (
	"context"
	"fmt"
	"strings"

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
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                   = (*notificationConfigurationResource)(nil)
	_ resource.ResourceWithConfigure      = (*notificationConfigurationResource)(nil)
	_ resource.ResourceWithImportState    = (*notificationConfigurationResource)(nil)
	_ resource.ResourceWithValidateConfig = (*notificationConfigurationResource)(nil)
)

type notificationConfigurationResource struct {
	client *client.Client
}

// NewNotificationConfigurationResource returns the webbpulse_notification_configuration resource.
func NewNotificationConfigurationResource() resource.Resource {
	return &notificationConfigurationResource{}
}

type notificationConfigurationModel struct {
	ID              types.String `tfsdk:"id"`
	WorkspaceID     types.String `tfsdk:"workspace_id"`
	Name            types.String `tfsdk:"name"`
	DestinationType types.String `tfsdk:"destination_type"`
	URL             types.String `tfsdk:"url"`
	Token           types.String `tfsdk:"token"`
	Enabled         types.Bool   `tfsdk:"enabled"`
	Triggers        types.Set    `tfsdk:"triggers"`
	URLMasked       types.String `tfsdk:"url_masked"`
	HasToken        types.Bool   `tfsdk:"has_token"`
	CreatedAt       types.String `tfsdk:"created_at"`
	UpdatedAt       types.String `tfsdk:"updated_at"`
}

// Metadata sets the type name of the notification configuration resource.
func (r *notificationConfigurationResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_notification_configuration"
}

// Schema defines the schema of the notification configuration resource.
func (r *notificationConfigurationResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "One run notification configuration on one workspace, sending run events to a Slack " +
			"incoming webhook, a Discord channel webhook or any HTTPS endpoint. The API never returns the URL or " +
			"the token, so the provider keeps the configured values in state and cannot detect either changed " +
			"outside Terraform.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The configuration id, such as `nc-01JABCDEF0123456789ABCDEFG`.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"workspace_id": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "The workspace this configuration belongs to. Changing it replaces the configuration.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"name": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "A name for this configuration.",
				Validators:          []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"destination_type": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "`slack` for an incoming webhook on `hooks.slack.com`, `discord` for a channel " +
					"webhook on `discord.com`, or `generic` for any HTTPS endpoint, which receives HCP Terraform's " +
					"version 1 payload. Changing it updates the configuration in place and resends `url`.",
				Validators: []validator.String{stringvalidator.OneOf(
					client.DestinationSlack, client.DestinationDiscord, client.DestinationGeneric,
				)},
			},
			"url": schema.StringAttribute{
				Required:  true,
				Sensitive: true,
				MarkdownDescription: "The webhook URL. Write only: the API never returns it, so a URL changed " +
					"outside Terraform is not detected. `url_masked` shows its scheme and host.",
			},
			"token": schema.StringAttribute{
				Optional:  true,
				Sensitive: true,
				MarkdownDescription: "A `generic` destination's signing token, which HMAC-SHA512 signs each body " +
					"into `X-TFE-Notification-Signature`. Write only, like `url`. Removing it clears the token.",
			},
			"enabled": schema.BoolAttribute{
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(true),
				MarkdownDescription: "Whether run events are delivered. Defaults to `true`.",
			},
			"triggers": schema.SetAttribute{
				Optional:    true,
				Computed:    true,
				ElementType: types.StringType,
				Default:     setdefault.StaticValue(emptyStringSet()),
				MarkdownDescription: "The run events to deliver: `run:created`, `run:planning`, " +
					"`run:needs_attention`, `run:applying`, `run:completed` and `run:errored`. Defaults to `[]`, " +
					"which delivers nothing but a test.",
				Validators: []validator.Set{
					setvalidator.ValueStringsAre(stringvalidator.OneOf(client.NotificationTriggers...)),
				},
			},
			"url_masked": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The URL's scheme and host with the path hidden, such as `https://hooks.slack.com/****`.",
			},
			"has_token": schema.BoolAttribute{
				Computed:            true,
				MarkdownDescription: "Whether the API holds a token for this configuration.",
			},
			"created_at": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "When the configuration was created.",
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"updated_at": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "When the configuration was last changed.",
			},
		},
	}
}

// Configure stores the shared API client on the notification configuration resource.
func (r *notificationConfigurationResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	configureClient(req.ProviderData, &r.client, &resp.Diagnostics)
}

// ValidateConfig refuses a token on a destination other than generic, which the API would refuse with a 422.
func (r *notificationConfigurationResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var config notificationConfigurationModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if config.DestinationType.IsUnknown() || config.DestinationType.IsNull() || config.Token.IsUnknown() || config.Token.IsNull() {
		return
	}
	if config.DestinationType.ValueString() != client.DestinationGeneric && config.Token.ValueString() != "" {
		resp.Diagnostics.AddAttributeError(
			path.Root("token"),
			"A token needs a generic destination",
			fmt.Sprintf("Only a %q destination signs its deliveries, so %q takes no token.",
				client.DestinationGeneric, config.DestinationType.ValueString()),
		)
	}
}

// Create adds the notification configuration.
func (r *notificationConfigurationResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan notificationConfigurationModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	triggers, diags := setStrings(ctx, plan.Triggers)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	if triggers == nil {
		triggers = []string{}
	}
	body := client.NotificationConfigurationCreate{
		Name:            plan.Name.ValueString(),
		DestinationType: plan.DestinationType.ValueString(),
		URL:             plan.URL.ValueString(),
		Enabled:         plan.Enabled.ValueBool(),
		Triggers:        triggers,
	}
	if token := plan.Token.ValueString(); token != "" {
		body.Token = &token
	}

	created, err := r.client.CreateNotificationConfiguration(ctx, plan.WorkspaceID.ValueString(), body)
	if err != nil {
		resp.Diagnostics.Append(apiDiagnostic("Cannot create the notification configuration", err))
		return
	}

	state := plan
	resp.Diagnostics.Append(applyNotificationConfiguration(ctx, created, &state)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Read refreshes the configuration, keeping the URL and token from state and dropping the resource on a 404.
func (r *notificationConfigurationResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state notificationConfigurationModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	found, err := r.client.GetNotificationConfiguration(ctx, state.WorkspaceID.ValueString(), state.ID.ValueString())
	if err != nil {
		if client.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.Append(apiDiagnostic("Cannot read the notification configuration", err))
		return
	}

	resp.Diagnostics.Append(applyNotificationConfiguration(ctx, found, &state)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update sends a partial edit carrying only the changed attributes.
func (r *notificationConfigurationResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state notificationConfigurationModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	body, diags := notificationConfigurationPatch(ctx, plan, state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	updated, err := r.client.UpdateNotificationConfiguration(ctx, state.WorkspaceID.ValueString(), state.ID.ValueString(), body)
	if err != nil {
		resp.Diagnostics.Append(apiDiagnostic("Cannot update the notification configuration", err))
		return
	}

	next := plan
	next.ID = state.ID
	resp.Diagnostics.Append(applyNotificationConfiguration(ctx, updated, &next)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &next)...)
}

// Delete deletes the notification configuration, treating a 404 as already gone.
func (r *notificationConfigurationResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state notificationConfigurationModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeleteNotificationConfiguration(ctx, state.WorkspaceID.ValueString(), state.ID.ValueString())
	if err != nil && !client.IsNotFound(err) {
		resp.Diagnostics.Append(apiDiagnostic("Cannot delete the notification configuration", err))
	}
}

// ImportState imports a configuration by <workspace_id>/<notification_id>. The
// URL and token stay null, so the next apply writes the configured values.
func (r *notificationConfigurationResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	workspaceID, notificationID, ok := strings.Cut(req.ID, "/")
	if !ok || workspaceID == "" || notificationID == "" {
		resp.Diagnostics.AddError(
			"Unexpected import id",
			fmt.Sprintf("Expected <workspace_id>/<notification_id>, got %q.", req.ID),
		)
		return
	}

	found, err := r.client.GetNotificationConfiguration(ctx, workspaceID, notificationID)
	if err != nil {
		resp.Diagnostics.Append(apiDiagnostic("Cannot import the notification configuration", err))
		return
	}

	state := notificationConfigurationModel{URL: types.StringNull(), Token: types.StringNull()}
	resp.Diagnostics.Append(applyNotificationConfiguration(ctx, found, &state)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// notificationConfigurationPatch builds the partial edit from prior state to
// plan. The URL is resent whenever the destination type changes, since the API
// needs one with it, and a removed or empty token is sent as an explicit null.
func notificationConfigurationPatch(ctx context.Context, plan, state notificationConfigurationModel) (client.NotificationConfigurationUpdate, diag.Diagnostics) {
	var body client.NotificationConfigurationUpdate
	var diags diag.Diagnostics
	if !plan.Name.Equal(state.Name) {
		body.Name = plan.Name.ValueStringPointer()
	}
	destinationChanged := !plan.DestinationType.Equal(state.DestinationType)
	if destinationChanged {
		body.DestinationType = plan.DestinationType.ValueStringPointer()
	}
	if destinationChanged || !plan.URL.Equal(state.URL) {
		body.URL = plan.URL.ValueStringPointer()
	}
	if !plan.Token.Equal(state.Token) {
		var token *string
		if value := plan.Token.ValueString(); value != "" {
			token = &value
		}
		body.Token = &token
	}
	if !plan.Enabled.Equal(state.Enabled) {
		body.Enabled = plan.Enabled.ValueBoolPointer()
	}
	if !plan.Triggers.IsUnknown() && !plan.Triggers.Equal(state.Triggers) {
		triggers, triggerDiags := setStrings(ctx, plan.Triggers)
		diags.Append(triggerDiags...)
		if triggers == nil {
			triggers = []string{}
		}
		body.Triggers = &triggers
	}
	return body, diags
}

// applyNotificationConfiguration copies one API configuration onto a state
// model. The URL and token are never returned, so the model keeps whatever it
// already holds for them.
func applyNotificationConfiguration(ctx context.Context, from *client.NotificationConfiguration, into *notificationConfigurationModel) diag.Diagnostics {
	into.ID = types.StringValue(from.ID)
	into.WorkspaceID = types.StringValue(from.WorkspaceID)
	into.Name = types.StringValue(from.Name)
	into.DestinationType = types.StringValue(from.DestinationType)
	into.Enabled = types.BoolValue(from.Enabled)
	into.URLMasked = types.StringValue(from.URLMasked)
	into.HasToken = types.BoolValue(from.HasToken)
	into.CreatedAt = types.StringValue(from.CreatedAt)
	into.UpdatedAt = types.StringValue(from.UpdatedAt)
	triggers := from.Triggers
	if triggers == nil {
		triggers = []string{}
	}
	set, diags := types.SetValueFrom(ctx, types.StringType, triggers)
	into.Triggers = set
	return diags
}
