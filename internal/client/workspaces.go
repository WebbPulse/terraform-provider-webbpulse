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

// UpdateWorkspace edits one workspace. The API takes a PATCH, and every nil
// field on the body is left untouched.
func (c *Client) UpdateWorkspace(ctx context.Context, workspaceID string, body WorkspaceUpdate) (*Workspace, error) {
	var out Workspace
	if err := c.do(ctx, http.MethodPatch, workspacePath(workspaceID), body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteWorkspace deletes one workspace and its variables.
func (c *Client) DeleteWorkspace(ctx context.Context, workspaceID string) error {
	return c.do(ctx, http.MethodDelete, workspacePath(workspaceID), nil, nil)
}

// CheckRunRole assumes the workspace's run role and reports whether it
// answered. A configured role that does not answer is still a 200 with
// Connected false; a workspace with no role at all is a 400 carrying
// RunRoleMissingCode.
func (c *Client) CheckRunRole(ctx context.Context, workspaceID string) (*RunRoleCheck, error) {
	var out RunRoleCheck
	path := workspacePath(workspaceID) + "/run-role/check"
	if err := c.do(ctx, http.MethodPost, path, nil, &out); err != nil {
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

func variablePath(workspaceID, key string) string {
	return workspacePath(workspaceID) + "/variables/" + url.PathEscape(key)
}
