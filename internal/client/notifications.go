package client

import (
	"context"
	"net/http"
	"net/url"
)

// Notification destination types.
const (
	DestinationSlack   = "slack"
	DestinationDiscord = "discord"
	DestinationGeneric = "generic"
)

// NotificationTriggers lists every run event a notification configuration can
// subscribe to, spelled as HCP Terraform spells them.
var NotificationTriggers = []string{
	"run:created",
	"run:planning",
	"run:needs_attention",
	"run:applying",
	"run:completed",
	"run:errored",
}

// NotificationConfiguration is a stored configuration as the API renders it.
// The URL and the token are never returned: URLMasked shows the scheme and host
// only, and HasToken reports whether a token is set.
type NotificationConfiguration struct {
	ID              string   `json:"id"`
	WorkspaceID     string   `json:"workspace_id"`
	Name            string   `json:"name"`
	DestinationType string   `json:"destination_type"`
	Enabled         bool     `json:"enabled"`
	Triggers        []string `json:"triggers"`
	URLMasked       string   `json:"url_masked"`
	HasToken        bool     `json:"has_token"`
	CreatedAt       string   `json:"created_at"`
	UpdatedAt       string   `json:"updated_at"`
}

// NotificationConfigurationCreate is the body of a configuration create.
type NotificationConfigurationCreate struct {
	Name            string   `json:"name"`
	DestinationType string   `json:"destination_type"`
	URL             string   `json:"url"`
	Token           *string  `json:"token,omitempty"`
	Enabled         bool     `json:"enabled"`
	Triggers        []string `json:"triggers"`
}

// NotificationConfigurationUpdate is a partial edit that omits nil fields. A
// pointer to a nil token encodes as an explicit null, which clears the token.
type NotificationConfigurationUpdate struct {
	Name            *string   `json:"name,omitempty"`
	DestinationType *string   `json:"destination_type,omitempty"`
	URL             *string   `json:"url,omitempty"`
	Token           **string  `json:"token,omitempty"`
	Enabled         *bool     `json:"enabled,omitempty"`
	Triggers        *[]string `json:"triggers,omitempty"`
}

// CreateNotificationConfiguration adds a notification configuration to a workspace.
func (c *Client) CreateNotificationConfiguration(ctx context.Context, workspaceID string, body NotificationConfigurationCreate) (*NotificationConfiguration, error) {
	var out NotificationConfiguration
	if err := c.do(ctx, http.MethodPost, notificationsPath(workspaceID), body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetNotificationConfiguration reads one notification configuration.
func (c *Client) GetNotificationConfiguration(ctx context.Context, workspaceID, notificationID string) (*NotificationConfiguration, error) {
	var out NotificationConfiguration
	if err := c.do(ctx, http.MethodGet, notificationPath(workspaceID, notificationID), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateNotificationConfiguration applies a partial edit to one notification configuration.
func (c *Client) UpdateNotificationConfiguration(ctx context.Context, workspaceID, notificationID string, body NotificationConfigurationUpdate) (*NotificationConfiguration, error) {
	var out NotificationConfiguration
	if err := c.do(ctx, http.MethodPatch, notificationPath(workspaceID, notificationID), body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteNotificationConfiguration deletes one notification configuration.
func (c *Client) DeleteNotificationConfiguration(ctx context.Context, workspaceID, notificationID string) error {
	return c.do(ctx, http.MethodDelete, notificationPath(workspaceID, notificationID), nil, nil)
}

func notificationsPath(workspaceID string) string {
	return workspacePath(workspaceID) + "/notification-configurations"
}

func notificationPath(workspaceID, notificationID string) string {
	return notificationsPath(workspaceID) + "/" + url.PathEscape(notificationID)
}
