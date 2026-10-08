package client

import (
	"context"
	"encoding/json"
	"net/http"
)

// RemoteStateNotSharedCode is the stable code a 403 on an outputs read carries
// when a run's token asks for a workspace that does not share its outputs with
// the run's workspace.
const RemoteStateNotSharedCode = "REMOTE_STATE_NOT_SHARED"

// RemoteStateConsumerNotFoundCode is the stable code a 422 on a workspace update
// carries when remote_state_consumer_ids names a workspace that does not exist.
const RemoteStateConsumerNotFoundCode = "REMOTE_STATE_CONSUMER_NOT_FOUND"

// WorkspaceOutput is one non-sensitive output of a workspace's current state.
// Value stays raw JSON so the provider can map any output type.
type WorkspaceOutput struct {
	Name         string          `json:"name"`
	Type         string          `json:"type"`
	DetailedType json.RawMessage `json:"detailed_type"`
	Value        json.RawMessage `json:"value"`
}

// WorkspaceOutputs is the outputs read: the non-sensitive outputs with values and
// the sensitive ones by name only.
type WorkspaceOutputs struct {
	WorkspaceID          string            `json:"workspace_id"`
	StateVersionID       *string           `json:"state_version_id"`
	Outputs              []WorkspaceOutput `json:"outputs"`
	SensitiveOutputNames []string          `json:"sensitive_output_names"`
}

// RemoteStateSharing is a workspace's remote state sharing, as the workspace
// read renders it.
type RemoteStateSharing struct {
	WorkspaceID            string   `json:"workspace_id"`
	GlobalRemoteState      bool     `json:"global_remote_state"`
	RemoteStateConsumerIDs []string `json:"remote_state_consumer_ids"`
}

// RemoteStateSharingUpdate is the workspace PATCH body that sets only the
// sharing fields. An empty consumer list clears the list.
type RemoteStateSharingUpdate struct {
	GlobalRemoteState      bool     `json:"global_remote_state"`
	RemoteStateConsumerIDs []string `json:"remote_state_consumer_ids"`
}

// GetWorkspaceOutputs reads the non-sensitive outputs of a workspace's current
// state. A workspace with no state answers an empty list.
func (c *Client) GetWorkspaceOutputs(ctx context.Context, workspaceID string) (*WorkspaceOutputs, error) {
	var out WorkspaceOutputs
	if err := c.do(ctx, http.MethodGet, workspacePath(workspaceID)+"/outputs", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetRemoteStateSharing reads a workspace's remote state sharing.
func (c *Client) GetRemoteStateSharing(ctx context.Context, workspaceID string) (*RemoteStateSharing, error) {
	var out RemoteStateSharing
	if err := c.do(ctx, http.MethodGet, workspacePath(workspaceID), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// SetRemoteStateSharing replaces a workspace's remote state sharing and returns
// what the API stored.
func (c *Client) SetRemoteStateSharing(ctx context.Context, workspaceID string, body RemoteStateSharingUpdate) (*RemoteStateSharing, error) {
	if body.RemoteStateConsumerIDs == nil {
		body.RemoteStateConsumerIDs = []string{}
	}
	var out RemoteStateSharing
	if err := c.do(ctx, http.MethodPatch, workspacePath(workspaceID), body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
