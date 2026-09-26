package client

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
)

// RunRoleMissingCode is the stable code a 400 carries when a workspace has no
// run role ARN yet.
const RunRoleMissingCode = "RUN_ROLE_MISSING"

// WorkspaceManagesResourcesCode is the stable code a 409 on delete carries when
// the workspace state still tracks resources. A forced delete skips it.
const WorkspaceManagesResourcesCode = "WORKSPACE_MANAGES_RESOURCES"

// WorkspaceHasActiveRunCode is the stable code a 409 on delete carries while a
// run on the workspace has not finished. A forced delete does not skip it.
const WorkspaceHasActiveRunCode = "WORKSPACE_HAS_ACTIVE_RUN"

// CreateWorkspace creates a workspace and returns it. The run role is optional
// here because the role's trust policy names the workspace id as its external
// id, so the role cannot exist until the workspace does.
func (c *Client) CreateWorkspace(ctx context.Context, body WorkspaceCreate) (*Workspace, error) {
	var out Workspace
	if err := c.do(ctx, http.MethodPost, "/workspaces", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetWorkspace reads one workspace by id.
func (c *Client) GetWorkspace(ctx context.Context, workspaceID string) (*Workspace, error) {
	var out Workspace
	if err := c.do(ctx, http.MethodGet, workspacePath(workspaceID), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListWorkspaces reads every workspace in the environment.
func (c *Client) ListWorkspaces(ctx context.Context) ([]Workspace, error) {
	var out WorkspaceList
	if err := c.do(ctx, http.MethodGet, "/workspaces", nil, &out); err != nil {
		return nil, err
	}
	return out.Items, nil
}

// GetWorkspaceByName reads every workspace and returns the one with this name.
// The API has no name lookup route, so the filtering happens here.
func (c *Client) GetWorkspaceByName(ctx context.Context, name string) (*Workspace, error) {
	items, err := c.ListWorkspaces(ctx)
	if err != nil {
		return nil, err
	}
	for i := range items {
		if items[i].Name == name {
			return &items[i], nil
		}
	}
	return nil, &Error{
		StatusCode: http.StatusNotFound,
		Message:    fmt.Sprintf("No workspace named %q.", name),
		ErrorCode:  "NOT_FOUND",
	}
}

// UpdateWorkspace edits one workspace with a JSON Merge Patch: a nil field is
// omitted and left untouched, and a pointer to a nil string sends an explicit
// null, which clears one of the clearable fields.
func (c *Client) UpdateWorkspace(ctx context.Context, workspaceID string, body WorkspaceUpdate) (*Workspace, error) {
	var out Workspace
	if err := c.do(ctx, http.MethodPatch, workspacePath(workspaceID), body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteWorkspace deletes one workspace with its finished runs, state and
// variables. A workspace whose state still tracks resources is refused with
// WorkspaceManagesResourcesCode unless force is set, and one with an active run
// is refused with WorkspaceHasActiveRunCode either way.
func (c *Client) DeleteWorkspace(ctx context.Context, workspaceID string, force bool) error {
	path := workspacePath(workspaceID)
	if force {
		path += "?force=true"
	}
	return c.do(ctx, http.MethodDelete, path, nil, nil)
}

// ReadRunRoleCheck reports whether the runner has assumed the workspace's run
// role, from the runner's own record, without writing anything. The API never
// calls STS and records nothing for a GET, so this is safe to call on every plan
// and refresh. A configured role is always a 200, with Status unverified until a
// run has tried it; a workspace with no role at all is a 400 carrying
// RunRoleMissingCode.
func (c *Client) ReadRunRoleCheck(ctx context.Context, workspaceID string) (*RunRoleCheck, error) {
	var out RunRoleCheck
	if err := c.do(ctx, http.MethodGet, runRoleCheckPath(workspaceID), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListVariables reads every variable on one workspace. A sensitive value is
// never returned.
func (c *Client) ListVariables(ctx context.Context, workspaceID string) ([]Variable, error) {
	var out VariableList
	if err := c.do(ctx, http.MethodGet, workspacePath(workspaceID)+"/variables", nil, &out); err != nil {
		return nil, err
	}
	return out.Items, nil
}

// GetVariable reads one variable by key. A sensitive value is never returned.
func (c *Client) GetVariable(ctx context.Context, workspaceID, key string) (*Variable, error) {
	var out Variable
	if err := c.do(ctx, http.MethodGet, variablePath(workspaceID, key), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// PutVariable sets one variable. The route is an upsert, so it serves both a
// create and an update.
func (c *Client) PutVariable(ctx context.Context, workspaceID, key string, body VariableWrite) (*Variable, error) {
	var out Variable
	if err := c.do(ctx, http.MethodPut, variablePath(workspaceID, key), body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteVariable deletes one variable.
func (c *Client) DeleteVariable(ctx context.Context, workspaceID, key string) error {
	return c.do(ctx, http.MethodDelete, variablePath(workspaceID, key), nil, nil)
}

func workspacePath(workspaceID string) string {
	return "/workspaces/" + url.PathEscape(workspaceID)
}

func runRoleCheckPath(workspaceID string) string {
	return workspacePath(workspaceID) + "/run-role/check"
}

func variablePath(workspaceID, key string) string {
	return workspacePath(workspaceID) + "/variables/" + url.PathEscape(key)
}
