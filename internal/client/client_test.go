package client

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestNormalizeHost checks host normalization adds a scheme and the API prefix.
func TestNormalizeHost(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		in   string
		want string
	}{
		"bare host":           {"api.staging.terraform.webbpulse.com", "https://api.staging.terraform.webbpulse.com/api/v1"},
		"with scheme":         {"https://example.test", "https://example.test/api/v1"},
		"with api suffix":     {"https://example.test/api/v1", "https://example.test/api/v1"},
		"with trailing slash": {"https://example.test/", "https://example.test/api/v1"},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, err := normalizeHost(tc.in)
			if err != nil {
				t.Fatalf("normalizeHost(%q) returned %v", tc.in, err)
			}
			if got != tc.want {
				t.Fatalf("normalizeHost(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestNewRequiresToken checks a client cannot be built without a token.
func TestNewRequiresToken(t *testing.T) {
	t.Parallel()

	if _, err := New("https://example.test", ""); err == nil {
		t.Fatal("New with an empty token returned no error")
	}
}

func newTestClient(t *testing.T, handler http.Handler) *Client {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	c, err := New(server.URL, "wpk_test", WithHTTPClient(server.Client()))
	if err != nil {
		t.Fatalf("New returned %v", err)
	}
	return c
}

// TestCreateWorkspaceSendsBodyAndAuth checks a create sends its body and the bearer token.
func TestCreateWorkspaceSendsBodyAndAuth(t *testing.T) {
	t.Parallel()

	var gotAuth, gotPath, gotMethod string
	var gotBody WorkspaceCreate

	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		gotMethod = r.Method
		_ = json.NewDecoder(r.Body).Decode(&gotBody)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(Workspace{
			WorkspaceID:   "ws-01J",
			Name:          "prod",
			Engine:        EngineTerraform,
			EngineVersion: "1.9.8",
			CreatedAt:     "2026-09-20T00:00:00Z",
			RunRoleSetup: RunRoleSetup{
				PrincipalARN:  "arn:aws:iam::111122223333:role/runner-plan",
				PrincipalARNs: []string{"arn:aws:iam::111122223333:role/runner-plan"},
				ExternalID:    "ws-01J",
				RoleName:      "wp-tf-run-01J",
			},
		})
	}))

	got, err := c.CreateWorkspace(context.Background(), WorkspaceCreate{Name: "prod", EngineVersion: "1.9.8"})
	if err != nil {
		t.Fatalf("CreateWorkspace returned %v", err)
	}

	if gotAuth != "Bearer wpk_test" {
		t.Errorf("Authorization = %q, want the bearer token", gotAuth)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotPath != "/api/v1/workspaces" {
		t.Errorf("path = %q, want /api/v1/workspaces", gotPath)
	}
	if gotBody.Name != "prod" || gotBody.EngineVersion != "1.9.8" {
		t.Errorf("body = %+v, want the configured name and engine version", gotBody)
	}
	if got.WorkspaceID != "ws-01J" || got.RunRoleSetup.ExternalID != "ws-01J" {
		t.Errorf("workspace = %+v, want the decoded response", got)
	}
}

// TestUpdateWorkspaceUsesPatchAndOmitsNilFields checks an update is a PATCH that omits nil fields.
func TestUpdateWorkspaceUsesPatchAndOmitsNilFields(t *testing.T) {
	t.Parallel()

	var gotMethod string
	var gotBody map[string]any

	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(Workspace{WorkspaceID: "ws-01J", Name: "prod"})
	}))

	description := "edited"
	descriptionPointer := &description
	if _, err := c.UpdateWorkspace(context.Background(), "ws-01J", WorkspaceUpdate{Description: &descriptionPointer}); err != nil {
		t.Fatalf("UpdateWorkspace returned %v", err)
	}

	if gotMethod != http.MethodPatch {
		t.Errorf("method = %q, want PATCH", gotMethod)
	}
	if len(gotBody) != 1 {
		t.Errorf("body = %+v, want only the set field", gotBody)
	}
	if gotBody["description"] != "edited" {
		t.Errorf("body description = %v, want edited", gotBody["description"])
	}
}

// TestNotFoundIsRecognised checks a 404 is reported as not found.
func TestNotFoundIsRecognised(t *testing.T) {
	t.Parallel()

	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Request-ID", "req-1")
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status":     404,
			"message":    "No such workspace.",
			"error_code": "NOT_FOUND",
			"request_id": "req-1",
			"success":    false,
		})
	}))

	_, err := c.GetWorkspace(context.Background(), "ws-missing")
	if err == nil {
		t.Fatal("GetWorkspace on a missing workspace returned no error")
	}
	if !IsNotFound(err) {
		t.Errorf("IsNotFound = false for %v, want true", err)
	}
	if ErrorCode(err) != "NOT_FOUND" {
		t.Errorf("ErrorCode = %q, want NOT_FOUND", ErrorCode(err))
	}

	var apiErr *Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("error is not an *Error: %v", err)
	}
	if apiErr.Message != "No such workspace." {
		t.Errorf("message = %q, want the API message", apiErr.Message)
	}
	if apiErr.RequestID != "req-1" {
		t.Errorf("request id = %q, want req-1", apiErr.RequestID)
	}
}

// TestReadRunRoleCheckUsesTheReadOnlyRoute checks the run role check reads through GET, never POST.
func TestReadRunRoleCheckUsesTheReadOnlyRoute(t *testing.T) {
	t.Parallel()

	var methods []string
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method)
		if r.URL.Path != "/api/v1/workspaces/ws-01J/run-role/check" {
			t.Errorf("path = %q, want the run role check route", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"connected":  true,
			"status":     RunRoleStatusConnected,
			"account_id": "870550636948",
			"error":      nil,
			"run_id":     "run-01J",
			"checked_at": "2026-09-25T12:00:00Z",
		})
	}))

	for range 2 {
		got, err := c.ReadRunRoleCheck(context.Background(), "ws-01J")
		if err != nil {
			t.Fatalf("ReadRunRoleCheck returned %v", err)
		}
		if !got.Connected {
			t.Error("connected = false, want true")
		}
		if got.AccountID == nil || *got.AccountID != "870550636948" {
			t.Errorf("account id = %v, want the account", got.AccountID)
		}
		if got.Error != nil {
			t.Errorf("error = %v, want nil", *got.Error)
		}
		if got.Status != RunRoleStatusConnected {
			t.Errorf("status = %q, want connected", got.Status)
		}
		if got.RunID == nil || *got.RunID != "run-01J" || got.CheckedAt == nil || *got.CheckedAt != "2026-09-25T12:00:00Z" {
			t.Errorf("run id %v and checked at %v, want the proving run", got.RunID, got.CheckedAt)
		}
	}

	for _, method := range methods {
		if method != http.MethodGet {
			t.Errorf("method = %q, want GET so the read writes nothing", method)
		}
	}
	if len(methods) != 2 {
		t.Errorf("requests = %d, want 2", len(methods))
	}
}

// TestReadRunRoleCheckDecodesAnUnconnectedOutcome checks an unconnected outcome decodes as data.
func TestReadRunRoleCheckDecodesAnUnconnectedOutcome(t *testing.T) {
	t.Parallel()

	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %q, want GET", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"connected":  false,
			"status":     RunRoleStatusFailed,
			"account_id": nil,
			"error":      "The role does not trust the runner or the external id does not match",
			"run_id":     "run-01J",
			"checked_at": "2026-09-25T12:00:00Z",
		})
	}))

	got, err := c.ReadRunRoleCheck(context.Background(), "ws-01J")
	if err != nil {
		t.Fatalf("ReadRunRoleCheck returned %v", err)
	}
	if got.Connected {
		t.Error("connected = true, want false")
	}
	if got.AccountID != nil {
		t.Errorf("account id = %v, want nil", got.AccountID)
	}
	if got.Error == nil {
		t.Fatal("error = nil, want the reason")
	}
	if got.Status != RunRoleStatusFailed || got.RunID == nil {
		t.Errorf("status %q and run id %v, want failed with the run", got.Status, got.RunID)
	}
}

// TestReadRunRoleCheckDecodesAnUnverifiedOutcome checks a role no run has tried decodes with null run fields.
func TestReadRunRoleCheckDecodesAnUnverifiedOutcome(t *testing.T) {
	t.Parallel()

	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"connected":false,"status":"unverified","account_id":null,"error":null,"run_id":null,"checked_at":null}`))
	}))

	got, err := c.ReadRunRoleCheck(context.Background(), "ws-01J")
	if err != nil {
		t.Fatalf("ReadRunRoleCheck returned %v", err)
	}
	if got.Connected || got.Status != RunRoleStatusUnverified {
		t.Errorf("connected %v and status %q, want an unverified outcome", got.Connected, got.Status)
	}
	if got.RunID != nil || got.CheckedAt != nil || got.Error != nil {
		t.Error("an unverified outcome carried run fields")
	}
}

// TestReadRunRoleCheckDecodesAMissingRunRole checks a missing run role surfaces its error code.
func TestReadRunRoleCheckDecodesAMissingRunRole(t *testing.T) {
	t.Parallel()

	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"detail": map[string]any{
				"message":    "This workspace has no run role ARN yet.",
				"error_code": RunRoleMissingCode,
			},
		})
	}))

	_, err := c.ReadRunRoleCheck(context.Background(), "ws-01J")
	if err == nil {
		t.Fatal("ReadRunRoleCheck returned no error")
	}
	if ErrorCode(err) != RunRoleMissingCode {
		t.Errorf("ErrorCode = %q, want %s", ErrorCode(err), RunRoleMissingCode)
	}
}

// TestGetVariableWithholdsASensitiveValue checks a sensitive variable decodes with a nil value.
func TestGetVariableWithholdsASensitiveValue(t *testing.T) {
	t.Parallel()

	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"workspace_id": "ws-01J",
			"key":          "token",
			"value":        nil,
			"category":     CategoryEnv,
			"sensitive":    true,
			"created_at":   "2026-09-20T00:00:00Z",
		})
	}))

	got, err := c.GetVariable(context.Background(), "ws-01J", "token")
	if err != nil {
		t.Fatalf("GetVariable returned %v", err)
	}
	if got.Value != nil {
		t.Errorf("value = %v, want nil for a sensitive variable", got.Value)
	}
	if !got.Sensitive || got.Category != CategoryEnv {
		t.Errorf("variable = %+v, want a sensitive env variable", got)
	}
}

// TestPutVariableEscapesTheKeyInThePath checks a variable key is path escaped.
func TestPutVariableEscapesTheKeyInThePath(t *testing.T) {
	t.Parallel()

	var gotPath string
	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.EscapedPath()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(Variable{WorkspaceID: "ws-01J", Key: "a.b-c"})
	}))

	if _, err := c.PutVariable(context.Background(), "ws-01J", "a.b-c", VariableWrite{Value: "x"}); err != nil {
		t.Fatalf("PutVariable returned %v", err)
	}
	if gotPath != "/api/v1/workspaces/ws-01J/variables/a.b-c" {
		t.Errorf("path = %q, want the variable route", gotPath)
	}
}

// TestDeleteWorkspaceAcceptsNoContent checks a 204 delete succeeds and sends
// force=true only for a forced delete.
func TestDeleteWorkspaceAcceptsNoContent(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		force     bool
		wantQuery string
	}{
		{false, ""},
		{true, "force=true"},
	} {
		c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodDelete || r.URL.Path != "/api/v1/workspaces/ws-01J" {
				t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			}
			if r.URL.RawQuery != tc.wantQuery {
				t.Errorf("force %v: query = %q, want %q", tc.force, r.URL.RawQuery, tc.wantQuery)
			}
			w.WriteHeader(http.StatusNoContent)
		}))

		if err := c.DeleteWorkspace(context.Background(), "ws-01J", tc.force); err != nil {
			t.Fatalf("DeleteWorkspace returned %v", err)
		}
	}
}

// TestDeleteWorkspaceSurfacesConflictCodes checks both delete refusals keep their error codes.
func TestDeleteWorkspaceSurfacesConflictCodes(t *testing.T) {
	t.Parallel()

	for _, code := range []string{WorkspaceManagesResourcesCode, WorkspaceHasActiveRunCode} {
		c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status":     http.StatusConflict,
				"message":    "The workspace cannot be deleted yet.",
				"error_code": code,
			})
		}))

		err := c.DeleteWorkspace(context.Background(), "ws-01J", false)
		if ErrorCode(err) != code {
			t.Errorf("ErrorCode = %q, want %s", ErrorCode(err), code)
		}
		if !strings.Contains(err.Error(), "409") {
			t.Errorf("error %q lost its status", err)
		}
	}
}

// TestGetWorkspaceByNameFiltersTheListing checks a name lookup filters the listing.
func TestGetWorkspaceByNameFiltersTheListing(t *testing.T) {
	t.Parallel()

	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(WorkspaceList{Items: []Workspace{
			{WorkspaceID: "ws-01A", Name: "staging"},
			{WorkspaceID: "ws-01B", Name: "prod"},
		}})
	}))

	got, err := c.GetWorkspaceByName(context.Background(), "prod")
	if err != nil {
		t.Fatalf("GetWorkspaceByName returned %v", err)
	}
	if got.WorkspaceID != "ws-01B" {
		t.Errorf("workspace id = %q, want ws-01B", got.WorkspaceID)
	}

	if _, err := c.GetWorkspaceByName(context.Background(), "absent"); !IsNotFound(err) {
		t.Errorf("a missing name returned %v, want a not found error", err)
	}
}

// TestContextCancellationSurfaces checks a cancelled context fails the request.
func TestContextCancellationSurfaces(t *testing.T) {
	t.Parallel()

	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(WorkspaceList{})
	}))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := c.ListWorkspaces(ctx); err == nil {
		t.Fatal("ListWorkspaces on a cancelled context returned no error")
	}
}

// TestNonJSONErrorBodyStillSurfaces checks a non JSON error body still yields a status error.
func TestNonJSONErrorBodyStillSurfaces(t *testing.T) {
	t.Parallel()

	c := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("upstream is down"))
	}))

	_, err := c.GetWorkspace(context.Background(), "ws-01J")
	if err == nil {
		t.Fatal("GetWorkspace returned no error")
	}
	var apiErr *Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("error is not an *Error: %v", err)
	}
	if apiErr.StatusCode != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", apiErr.StatusCode)
	}
	if apiErr.Message != "Bad Gateway" {
		t.Errorf("message = %q, want the HTTP status without the raw body", apiErr.Message)
	}
}
