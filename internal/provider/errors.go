package provider

import (
	"fmt"

	"github.com/WebbPulse/terraform-provider-webbpulse/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/diag"
)

// apiDiagnostic renders one client error as a diagnostic, surfacing the API's
// own message and error code rather than flattening everything to one string.
func apiDiagnostic(summary string, err error) diag.Diagnostic {
	return diag.NewErrorDiagnostic(summary, err.Error())
}

// configureClient pulls the shared API client out of whatever provider data the
// framework handed a resource, data source or action. Nil provider data is not
// an error: the framework calls Configure with nil during validation, before
// the provider itself has been configured.
func configureClient(providerData any, target **client.Client, diags *diag.Diagnostics) {
	if providerData == nil {
		return
	}
	apiClient, ok := providerData.(*client.Client)
	if !ok {
		diags.AddError(
			"Unexpected provider data",
			fmt.Sprintf("Expected *client.Client, got %T. This is a bug in the provider.", providerData),
		)
		return
	}
	*target = apiClient
}
