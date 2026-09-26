package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	providerschema "github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	resourceschema "github.com/hashicorp/terraform-plugin-framework/resource/schema"
)

// TestProviderSchemaMarksTheTokenSensitive checks the token is sensitive and both settings are optional.
func TestProviderSchemaMarksTheTokenSensitive(t *testing.T) {
	t.Parallel()

	resp := &provider.SchemaResponse{}
	New("test")().Schema(context.Background(), provider.SchemaRequest{}, resp)

	if resp.Diagnostics.HasError() {
		t.Fatalf("provider schema returned diagnostics: %v", resp.Diagnostics)
	}

	token, ok := resp.Schema.Attributes["token"].(providerschema.StringAttribute)
	if !ok {
		t.Fatal("token is not a string attribute")
	}
	if !token.Sensitive {
		t.Error("token is not marked sensitive")
	}
	if token.Required {
		t.Error("token is required, but it has an environment variable fallback")
	}

	host, ok := resp.Schema.Attributes["host"].(providerschema.StringAttribute)
	if !ok {
		t.Fatal("host is not a string attribute")
	}
	if host.Required {
		t.Error("host is required, but it has an environment variable fallback")
	}
}

// TestProviderSchemaValidates checks the provider schema is a valid implementation.
func TestProviderSchemaValidates(t *testing.T) {
	t.Parallel()

	resp := &provider.SchemaResponse{}
	New("test")().Schema(context.Background(), provider.SchemaRequest{}, resp)

	if diags := resp.Schema.ValidateImplementation(context.Background()); diags.HasError() {
		t.Errorf("provider schema is not a valid implementation: %v", diags)
	}
}

// TestResourceSchemasValidate checks every resource schema is a valid implementation.
func TestResourceSchemasValidate(t *testing.T) {
	t.Parallel()

	for _, newResource := range New("test")().(interface {
		Resources(context.Context) []func() resource.Resource
	}).Resources(context.Background()) {
		r := newResource()

		metadataResp := &resource.MetadataResponse{}
		r.Metadata(context.Background(), resource.MetadataRequest{ProviderTypeName: "webbpulse"}, metadataResp)

		schemaResp := &resource.SchemaResponse{}
		r.Schema(context.Background(), resource.SchemaRequest{}, schemaResp)

		if schemaResp.Diagnostics.HasError() {
			t.Errorf("%s schema returned diagnostics: %v", metadataResp.TypeName, schemaResp.Diagnostics)
			continue
		}
		if diags := schemaResp.Schema.ValidateImplementation(context.Background()); diags.HasError() {
			t.Errorf("%s is not a valid implementation: %v", metadataResp.TypeName, diags)
		}
	}
}

// TestDataSourceSchemasValidate checks every data source schema is a valid implementation.
func TestDataSourceSchemasValidate(t *testing.T) {
	t.Parallel()

	for _, newDataSource := range New("test")().(interface {
		DataSources(context.Context) []func() datasource.DataSource
	}).DataSources(context.Background()) {
		d := newDataSource()

		metadataResp := &datasource.MetadataResponse{}
		d.Metadata(context.Background(), datasource.MetadataRequest{ProviderTypeName: "webbpulse"}, metadataResp)

		schemaResp := &datasource.SchemaResponse{}
		d.Schema(context.Background(), datasource.SchemaRequest{}, schemaResp)

		if schemaResp.Diagnostics.HasError() {
			t.Errorf("%s schema returned diagnostics: %v", metadataResp.TypeName, schemaResp.Diagnostics)
			continue
		}
		if diags := schemaResp.Schema.ValidateImplementation(context.Background()); diags.HasError() {
			t.Errorf("%s is not a valid implementation: %v", metadataResp.TypeName, diags)
		}
	}
}

// TestResourceTypeNames checks the provider serves exactly the expected resources.
func TestResourceTypeNames(t *testing.T) {
	t.Parallel()

	want := map[string]bool{
		"webbpulse_workspace": false,
		"webbpulse_variable":  false,
	}

	for _, newResource := range New("test")().(interface {
		Resources(context.Context) []func() resource.Resource
	}).Resources(context.Background()) {
		resp := &resource.MetadataResponse{}
		newResource().Metadata(context.Background(), resource.MetadataRequest{ProviderTypeName: "webbpulse"}, resp)
		if _, ok := want[resp.TypeName]; !ok {
			t.Errorf("unexpected resource %q", resp.TypeName)
			continue
		}
		want[resp.TypeName] = true
	}

	for name, seen := range want {
		if !seen {
			t.Errorf("resource %q is missing", name)
		}
	}
}

// TestVariableValueIsSensitive checks the variable value is always marked sensitive.
func TestVariableValueIsSensitive(t *testing.T) {
	t.Parallel()

	resp := &resource.SchemaResponse{}
	NewVariableResource().Schema(context.Background(), resource.SchemaRequest{}, resp)

	value, ok := resp.Schema.Attributes["value"].(resourceschema.StringAttribute)
	if !ok {
		t.Fatal("value is not a string attribute")
	}
	if !value.Sensitive {
		t.Error("the variable value is not marked sensitive")
	}
}

// TestWorkspaceComputedAttributes checks which workspace attributes are computed and required.
func TestWorkspaceComputedAttributes(t *testing.T) {
	t.Parallel()

	resp := &resource.SchemaResponse{}
	NewWorkspaceResource().Schema(context.Background(), resource.SchemaRequest{}, resp)

	for _, name := range []string{"workspace_id", "created_at", "run_role_setup", "run_role_checked_at", "run_role_account_id"} {
		attribute, ok := resp.Schema.Attributes[name]
		if !ok {
			t.Errorf("the workspace has no %q attribute", name)
			continue
		}
		if !attribute.IsComputed() {
			t.Errorf("%q is not computed", name)
		}
	}

	if !resp.Schema.Attributes["name"].IsRequired() {
		t.Error("name is not required")
	}
	if !resp.Schema.Attributes["engine_version"].IsRequired() {
		t.Error("engine_version is not required")
	}
}

// TestRunRoleCheckDataSourceReturnsItsOutcome checks the run role check exposes its outcome attributes.
func TestRunRoleCheckDataSourceReturnsItsOutcome(t *testing.T) {
	t.Parallel()

	resp := &datasource.SchemaResponse{}
	NewRunRoleCheckDataSource().Schema(context.Background(), datasource.SchemaRequest{}, resp)

	for _, name := range []string{"connected", "account_id", "error"} {
		attribute, ok := resp.Schema.Attributes[name]
		if !ok {
			t.Errorf("the run role check has no %q attribute", name)
			continue
		}
		if !attribute.IsComputed() {
			t.Errorf("%q is not computed", name)
		}
	}
	if !resp.Schema.Attributes["workspace_id"].IsRequired() {
		t.Error("workspace_id is not required")
	}
}
