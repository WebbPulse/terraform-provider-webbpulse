package client

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// DefaultProjectID is the virtual project every workspace without a project
// belongs to. It is never stored, so it can be neither renamed nor deleted.
const DefaultProjectID = "prj-default"

// ProjectNameTakenCode is the stable code a 409 on a project create or rename
// carries when another project holds the name, ignoring case.
const ProjectNameTakenCode = "PROJECT_NAME_TAKEN"

// ProjectNotEmptyCode is the stable code a 409 on a project delete carries
// while the project still holds workspaces.
const ProjectNotEmptyCode = "PROJECT_NOT_EMPTY"

// DefaultProjectReadOnlyCode is the stable code a 409 carries when an edit or
// a delete targets the default project.
const DefaultProjectReadOnlyCode = "DEFAULT_PROJECT_READ_ONLY"

// ProjectNotFoundCode is the stable code a 422 on a workspace create or update
// carries when project_id names no project.
const ProjectNotFoundCode = "PROJECT_NOT_FOUND"

// Project is a project as the API renders it. CreatedAt and UpdatedAt are nil
// on the default project, which is never stored.
type Project struct {
	ProjectID      string  `json:"project_id"`
	Name           string  `json:"name"`
	Description    string  `json:"description"`
	IsDefault      bool    `json:"is_default"`
	WorkspaceCount int64   `json:"workspace_count"`
	CreatedAt      *string `json:"created_at"`
	UpdatedAt      *string `json:"updated_at"`
}

// ProjectCreate is the body of a project create.
type ProjectCreate struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// ProjectUpdate is a partial project edit that omits nil fields.
type ProjectUpdate struct {
	Name        *string `json:"name,omitempty"`
	Description *string `json:"description,omitempty"`
}

// ProjectList is the envelope the project listing returns.
type ProjectList struct {
	Items []Project `json:"items"`
}

// CreateProject creates a project and returns it.
func (c *Client) CreateProject(ctx context.Context, body ProjectCreate) (*Project, error) {
	var out Project
	if err := c.do(ctx, http.MethodPost, "/projects", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetProject reads one project by id. The default project answers to DefaultProjectID.
func (c *Client) GetProject(ctx context.Context, projectID string) (*Project, error) {
	var out Project
	if err := c.do(ctx, http.MethodGet, projectPath(projectID), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListProjects reads every project, the default first.
func (c *Client) ListProjects(ctx context.Context) ([]Project, error) {
	var out ProjectList
	if err := c.do(ctx, http.MethodGet, "/projects", nil, &out); err != nil {
		return nil, err
	}
	return out.Items, nil
}

// GetProjectByName reads every project and returns the one with this name,
// ignoring case as the API does when it keeps names unique.
func (c *Client) GetProjectByName(ctx context.Context, name string) (*Project, error) {
	items, err := c.ListProjects(ctx)
	if err != nil {
		return nil, err
	}
	for i := range items {
		if items[i].Name == name {
			return &items[i], nil
		}
	}
	for i := range items {
		if strings.EqualFold(items[i].Name, name) {
			return &items[i], nil
		}
	}
	return nil, &Error{
		StatusCode: http.StatusNotFound,
		Message:    fmt.Sprintf("No project named %q.", name),
		ErrorCode:  "NOT_FOUND",
	}
}

// UpdateProject renames a project or changes its description.
func (c *Client) UpdateProject(ctx context.Context, projectID string, body ProjectUpdate) (*Project, error) {
	var out Project
	if err := c.do(ctx, http.MethodPatch, projectPath(projectID), body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteProject deletes an empty project.
func (c *Client) DeleteProject(ctx context.Context, projectID string) error {
	return c.do(ctx, http.MethodDelete, projectPath(projectID), nil, nil)
}

func projectPath(projectID string) string {
	return "/projects/" + url.PathEscape(projectID)
}
