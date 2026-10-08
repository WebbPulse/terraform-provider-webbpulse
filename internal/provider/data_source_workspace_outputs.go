package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"sort"

	"github.com/WebbPulse/terraform-provider-webbpulse/internal/client"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ datasource.DataSource              = (*workspaceOutputsDataSource)(nil)
	_ datasource.DataSourceWithConfigure = (*workspaceOutputsDataSource)(nil)
)

type workspaceOutputsDataSource struct {
	client *client.Client
}

// NewWorkspaceOutputsDataSource returns the webbpulse_workspace_outputs data source.
func NewWorkspaceOutputsDataSource() datasource.DataSource { return &workspaceOutputsDataSource{} }

type workspaceOutputsModel struct {
	WorkspaceID          types.String  `tfsdk:"workspace_id"`
	StateVersionID       types.String  `tfsdk:"state_version_id"`
	Values               types.Dynamic `tfsdk:"values"`
	SensitiveOutputNames types.List    `tfsdk:"sensitive_output_names"`
}

// Metadata sets the type name of the workspace outputs data source.
func (d *workspaceOutputsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_workspace_outputs"
}

// Schema defines the schema of the workspace outputs data source.
func (d *workspaceOutputsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "The non-sensitive outputs of another workspace's current state, the way " +
			"`tfe_outputs` reads them on HCP Terraform. Inside a run, the run's token reads a workspace only " +
			"when that workspace shares its outputs with the run's workspace, through " +
			"`webbpulse_workspace_remote_state_sharing`. Sensitive outputs are never returned: they are listed " +
			"by name only.",
		Attributes: map[string]schema.Attribute{
			"workspace_id": schema.StringAttribute{
				Required:            true,
				MarkdownDescription: "The workspace whose outputs to read.",
				Validators:          []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"state_version_id": schema.StringAttribute{
				Computed:            true,
				MarkdownDescription: "The state version the outputs came from, or null when the workspace has no state yet.",
			},
			"values": schema.DynamicAttribute{
				Computed: true,
				MarkdownDescription: "An object with one attribute per non-sensitive output, keeping each output's " +
					"type. Empty when the workspace has no state yet.",
			},
			"sensitive_output_names": schema.ListAttribute{
				Computed:            true,
				ElementType:         types.StringType,
				MarkdownDescription: "The names of the outputs left out because they are sensitive.",
			},
		},
	}
}

// Configure stores the shared API client on the workspace outputs data source.
func (d *workspaceOutputsDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	configureClient(req.ProviderData, &d.client, &resp.Diagnostics)
}

// Read fetches the workspace's non-sensitive outputs.
func (d *workspaceOutputsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config workspaceOutputsModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	found, err := d.client.GetWorkspaceOutputs(ctx, config.WorkspaceID.ValueString())
	if err != nil {
		resp.Diagnostics.Append(apiDiagnostic("Cannot read the workspace outputs", err))
		return
	}

	values, err := outputsObject(found.Outputs)
	if err != nil {
		resp.Diagnostics.AddError("Cannot map the workspace outputs", err.Error())
		return
	}
	names := append([]string{}, found.SensitiveOutputNames...)
	sort.Strings(names)
	sensitive, diags := types.ListValueFrom(ctx, types.StringType, names)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	state := workspaceOutputsModel{
		WorkspaceID:          config.WorkspaceID,
		StateVersionID:       optionalString(found.StateVersionID),
		Values:               types.DynamicValue(values),
		SensitiveOutputNames: sensitive,
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// outputsObject builds one object value whose attributes are the outputs.
func outputsObject(outputs []client.WorkspaceOutput) (attr.Value, error) {
	attributeTypes := make(map[string]attr.Type, len(outputs))
	values := make(map[string]attr.Value, len(outputs))
	for _, output := range outputs {
		value, err := jsonValue(output.Value)
		if err != nil {
			return nil, fmt.Errorf("output %q: %w", output.Name, err)
		}
		attributeTypes[output.Name] = value.Type(context.Background())
		values[output.Name] = value
	}
	object, diags := types.ObjectValue(attributeTypes, values)
	if diags.HasError() {
		return nil, fmt.Errorf("%v", diags)
	}
	return object, nil
}

// jsonValue maps one JSON value to a framework value of matching type: objects
// become objects, arrays become tuples, and null becomes a null string.
func jsonValue(raw json.RawMessage) (attr.Value, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return types.StringNull(), nil
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var decoded any
	if err := decoder.Decode(&decoded); err != nil {
		return nil, err
	}
	return anyValue(decoded)
}

// anyValue maps one decoded JSON value to a framework value.
func anyValue(decoded any) (attr.Value, error) {
	switch v := decoded.(type) {
	case nil:
		return types.StringNull(), nil
	case string:
		return types.StringValue(v), nil
	case bool:
		return types.BoolValue(v), nil
	case json.Number:
		number, ok := new(big.Float).SetString(v.String())
		if !ok {
			return nil, fmt.Errorf("cannot read %q as a number", v.String())
		}
		return types.NumberValue(number), nil
	case []any:
		elementTypes := make([]attr.Type, len(v))
		elements := make([]attr.Value, len(v))
		for i, item := range v {
			value, err := anyValue(item)
			if err != nil {
				return nil, err
			}
			elementTypes[i] = value.Type(context.Background())
			elements[i] = value
		}
		tuple, diags := types.TupleValue(elementTypes, elements)
		if diags.HasError() {
			return nil, fmt.Errorf("%v", diags)
		}
		return tuple, nil
	case map[string]any:
		attributeTypes := make(map[string]attr.Type, len(v))
		attributes := make(map[string]attr.Value, len(v))
		for key, item := range v {
			value, err := anyValue(item)
			if err != nil {
				return nil, err
			}
			attributeTypes[key] = value.Type(context.Background())
			attributes[key] = value
		}
		object, diags := types.ObjectValue(attributeTypes, attributes)
		if diags.HasError() {
			return nil, fmt.Errorf("%v", diags)
		}
		return object, nil
	}
	return nil, fmt.Errorf("unexpected JSON value of type %T", decoded)
}
