package client

import (
	"context"
	"net/http"
	"net/url"
	"strings"
)

// RegistryNotFoundCode is the stable code a 404 from a registry route carries.
const RegistryNotFoundCode = "REGISTRY_NOT_FOUND"

// RegistryModuleExistsCode is the stable code a 409 carries when a module
// already sits at the address a create resolves to.
const RegistryModuleExistsCode = "REGISTRY_MODULE_EXISTS"

// RegistryProviderExistsCode is the stable code a 409 carries when a provider
// already sits at the address a create resolves to.
const RegistryProviderExistsCode = "REGISTRY_PROVIDER_EXISTS"

// RegistryInvalidModuleNameCode is the stable code a 422 carries when the
// repository and the given name and provider do not make a module address.
const RegistryInvalidModuleNameCode = "REGISTRY_INVALID_MODULE_NAME"

// RegistryInvalidProviderNameCode is the stable code a 422 carries when the
// repository is not named terraform-provider-<type>.
const RegistryInvalidProviderNameCode = "REGISTRY_INVALID_PROVIDER_NAME"

// RegistrySyncUnavailableCode is the stable code a 503 carries when a resync
// could not be queued.
const RegistrySyncUnavailableCode = "REGISTRY_SYNC_UNAVAILABLE"

// ModuleCreate connects a module to a repository the GitHub App is installed on.
type ModuleCreate struct {
	VCSRepo    string `json:"vcs_repo"`
	Name       string `json:"name,omitempty"`
	Provider   string `json:"provider,omitempty"`
	ImportTags *bool  `json:"import_tags,omitempty"`
}

// ModuleVersion is one version of a module and where its publishing stands.
type ModuleVersion struct {
	Version     string  `json:"version"`
	Status      string  `json:"status"`
	Error       *string `json:"error"`
	Repository  string  `json:"repository"`
	Tag         *string `json:"tag"`
	SHA         string  `json:"sha"`
	Actor       string  `json:"actor"`
	CreatedAt   string  `json:"created_at"`
	PublishedAt *string `json:"published_at"`
	SizeBytes   *int64  `json:"size_bytes"`
}

// Module is one registry module with every version, newest first.
type Module struct {
	Namespace string          `json:"namespace"`
	Name      string          `json:"name"`
	Provider  string          `json:"provider"`
	Source    string          `json:"source"`
	VCSRepo   *string         `json:"vcs_repo"`
	CreatedAt *string         `json:"created_at"`
	Versions  []ModuleVersion `json:"versions"`
}

// ModuleList is the body of the module listing.
type ModuleList struct {
	Modules []Module `json:"modules"`
}

// ProviderCreate connects a private provider to a repository named
// terraform-provider-<type>.
type ProviderCreate struct {
	VCSRepo        string `json:"vcs_repo"`
	ImportReleases *bool  `json:"import_releases,omitempty"`
}

// ProviderPlatform is one os and architecture a provider version ships for.
type ProviderPlatform struct {
	OS       string `json:"os"`
	Arch     string `json:"arch"`
	Filename string `json:"filename"`
	Shasum   string `json:"shasum"`
}

// ProviderVersion is one version of a private provider.
type ProviderVersion struct {
	Version     string             `json:"version"`
	Status      string             `json:"status"`
	Error       *string            `json:"error"`
	Tag         string             `json:"tag"`
	Protocols   []string           `json:"protocols"`
	Platforms   []ProviderPlatform `json:"platforms"`
	KeyID       *string            `json:"key_id"`
	Actor       string             `json:"actor"`
	CreatedAt   string             `json:"created_at"`
	PublishedAt *string            `json:"published_at"`
}

// RegistryProvider is one private provider with every version, newest first.
type RegistryProvider struct {
	Namespace string            `json:"namespace"`
	Type      string            `json:"type"`
	Source    string            `json:"source"`
	VCSRepo   string            `json:"vcs_repo"`
	CreatedAt *string           `json:"created_at"`
	Versions  []ProviderVersion `json:"versions"`
}

// RegistryProviderList is the body of the provider listing.
type RegistryProviderList struct {
	Providers []RegistryProvider `json:"providers"`
}

// RegistrySync is a queued import of a repository's tags or releases.
type RegistrySync struct {
	Source   string `json:"source"`
	Delivery string `json:"delivery"`
}

func registryPath(segments ...string) string {
	escaped := make([]string, len(segments))
	for i, segment := range segments {
		escaped[i] = url.PathEscape(segment)
	}
	return "/registry/" + strings.Join(escaped, "/")
}

// CreateModule connects a module to a repository. User sessions need a recent
// sign in; wpk_ keys are exempt.
func (c *Client) CreateModule(ctx context.Context, body ModuleCreate) (*Module, error) {
	var out Module
	if err := c.do(ctx, http.MethodPost, "/registry/modules", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetModule reads one module by namespace, name and provider.
func (c *Client) GetModule(ctx context.Context, namespace, name, provider string) (*Module, error) {
	var out Module
	if err := c.do(ctx, http.MethodGet, registryPath("modules", namespace, name, provider), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListModules reads every module in the registry.
func (c *Client) ListModules(ctx context.Context) ([]Module, error) {
	var out ModuleList
	if err := c.do(ctx, http.MethodGet, "/registry/modules", nil, &out); err != nil {
		return nil, err
	}
	return out.Modules, nil
}

// ResyncModule queues an import of every semantic version tag in the module's repository.
func (c *Client) ResyncModule(ctx context.Context, namespace, name, provider string) (*RegistrySync, error) {
	var out RegistrySync
	if err := c.do(ctx, http.MethodPost, registryPath("modules", namespace, name, provider, "resync"), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteModule removes a module with every version and stored tarball.
func (c *Client) DeleteModule(ctx context.Context, namespace, name, provider string) error {
	return c.do(ctx, http.MethodDelete, registryPath("modules", namespace, name, provider), nil, nil)
}

// CreateRegistryProvider connects a private provider to a repository. User
// sessions need a recent sign in; wpk_ keys are exempt.
func (c *Client) CreateRegistryProvider(ctx context.Context, body ProviderCreate) (*RegistryProvider, error) {
	var out RegistryProvider
	if err := c.do(ctx, http.MethodPost, "/registry/providers", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetRegistryProvider reads one private provider by namespace and type.
func (c *Client) GetRegistryProvider(ctx context.Context, namespace, providerType string) (*RegistryProvider, error) {
	var out RegistryProvider
	if err := c.do(ctx, http.MethodGet, registryPath("providers", namespace, providerType), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListRegistryProviders reads every private provider in the registry.
func (c *Client) ListRegistryProviders(ctx context.Context) ([]RegistryProvider, error) {
	var out RegistryProviderList
	if err := c.do(ctx, http.MethodGet, "/registry/providers", nil, &out); err != nil {
		return nil, err
	}
	return out.Providers, nil
}

// ResyncRegistryProvider queues an import of every release in the provider's repository.
func (c *Client) ResyncRegistryProvider(ctx context.Context, namespace, providerType string) (*RegistrySync, error) {
	var out RegistrySync
	if err := c.do(ctx, http.MethodPost, registryPath("providers", namespace, providerType, "resync"), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteRegistryProvider removes a private provider with every version.
func (c *Client) DeleteRegistryProvider(ctx context.Context, namespace, providerType string) error {
	return c.do(ctx, http.MethodDelete, registryPath("providers", namespace, providerType), nil, nil)
}
