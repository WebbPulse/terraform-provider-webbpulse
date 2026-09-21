// Package client is a small typed HTTP client for the WebbPulse Terraform
// control plane API. It knows the API's error envelope and nothing about
// Terraform, so it can be exercised on its own.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DefaultHost is the staging control plane, used when no host is configured.
const DefaultHost = "https://api.staging.terraform.webbpulse.com"

// APIPath is the version prefix every route in this client sits under.
const APIPath = "/api/v1"

const defaultTimeout = 60 * time.Second

// Client talks to one control plane. It is safe for concurrent use.
type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
	userAgent  string
}

// Option adjusts a Client at construction.
type Option func(*Client)

// WithHTTPClient replaces the underlying HTTP client, which is what a test
// pointing at an httptest server uses.
func WithHTTPClient(httpClient *http.Client) Option {
	return func(c *Client) { c.httpClient = httpClient }
}

// WithUserAgent sets the User-Agent header the client sends.
func WithUserAgent(userAgent string) Option {
	return func(c *Client) { c.userAgent = userAgent }
}

// New builds a client for one host and token. The host may be given with or
// without a scheme and with or without the /api/v1 suffix; both are normalized.
func New(host, token string, opts ...Option) (*Client, error) {
	if strings.TrimSpace(token) == "" {
		return nil, errors.New("a token is required")
	}
	baseURL, err := normalizeHost(host)
	if err != nil {
		return nil, err
	}
	c := &Client{
		baseURL:    baseURL,
		token:      token,
		httpClient: &http.Client{Timeout: defaultTimeout},
		userAgent:  "terraform-provider-webbpulse",
	}
	for _, opt := range opts {
		opt(c)
	}
	return c, nil
}

// BaseURL is the fully qualified API root this client sends to, including the
// version prefix.
func (c *Client) BaseURL() string { return c.baseURL }

func normalizeHost(host string) (string, error) {
	trimmed := strings.TrimSpace(host)
	if trimmed == "" {
		trimmed = DefaultHost
	}
	if !strings.Contains(trimmed, "://") {
		trimmed = "https://" + trimmed
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "", fmt.Errorf("host is not a valid URL: %w", err)
	}
	if parsed.Host == "" {
		return "", fmt.Errorf("host is not a valid URL: %q", host)
	}
	path := strings.TrimSuffix(parsed.Path, "/")
	if !strings.HasSuffix(path, APIPath) {
		path += APIPath
	}
	parsed.Path = path
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return strings.TrimSuffix(parsed.String(), "/"), nil
}

// Error is a non success response, carrying the API's own envelope fields so a
// caller can match on ErrorCode rather than on prose.
type Error struct {
	StatusCode int
	Message    string
	ErrorCode  string
	RequestID  string
}

// Error implements the error interface.
func (e *Error) Error() string {
	parts := []string{fmt.Sprintf("HTTP %d", e.StatusCode)}
	if e.ErrorCode != "" {
		parts = append(parts, e.ErrorCode)
	}
	if e.Message != "" {
		parts = append(parts, e.Message)
	}
	out := strings.Join(parts, ": ")
	if e.RequestID != "" {
		out += fmt.Sprintf(" (request id %s)", e.RequestID)
	}
	return out
}

// IsNotFound reports whether an error is a 404, which is what tells a read to
// drop the resource from state rather than fail.
func IsNotFound(err error) bool {
	var apiErr *Error
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode == http.StatusNotFound
	}
	return false
}

// ErrorCode returns the API's stable error code for an error, or "" when the
// error did not come from the API.
func ErrorCode(err error) string {
	var apiErr *Error
	if errors.As(err, &apiErr) {
		return apiErr.ErrorCode
	}
	return ""
}

type errorEnvelope struct {
	Status    int    `json:"status"`
	Message   string `json:"message"`
	ErrorCode string `json:"error_code"`
	RequestID string `json:"request_id"`
	Detail    any    `json:"detail"`
}

func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encoding request body: %w", err)
		}
		reader = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.userAgent)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("calling %s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading response from %s %s: %w", method, path, err)
	}

	if resp.StatusCode >= 400 {
		return decodeError(resp, payload)
	}

	if out == nil || resp.StatusCode == http.StatusNoContent || len(bytes.TrimSpace(payload)) == 0 {
		return nil
	}
	if err := json.Unmarshal(payload, out); err != nil {
		return fmt.Errorf("decoding response from %s %s: %w", method, path, err)
	}
	return nil
}

func decodeError(resp *http.Response, payload []byte) error {
	apiErr := &Error{
		StatusCode: resp.StatusCode,
		RequestID:  resp.Header.Get("X-Request-ID"),
	}

	var envelope errorEnvelope
	if err := json.Unmarshal(payload, &envelope); err == nil {
		apiErr.Message = envelope.Message
		apiErr.ErrorCode = envelope.ErrorCode
		if envelope.RequestID != "" {
			apiErr.RequestID = envelope.RequestID
		}
		applyDetail(apiErr, envelope.Detail)
	}

	if apiErr.Message == "" {
		apiErr.Message = strings.TrimSpace(string(payload))
	}
	if apiErr.Message == "" {
		apiErr.Message = http.StatusText(resp.StatusCode)
	}
	return apiErr
}

func applyDetail(apiErr *Error, detail any) {
	switch value := detail.(type) {
	case string:
		if apiErr.Message == "" {
			apiErr.Message = value
		}
	case map[string]any:
		if message, ok := value["message"].(string); ok && apiErr.Message == "" {
			apiErr.Message = message
		}
		if code, ok := value["error_code"].(string); ok && apiErr.ErrorCode == "" {
			apiErr.ErrorCode = code
		}
	}
}
