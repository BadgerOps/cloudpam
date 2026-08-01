// Package client implements a minimal HTTP client for the CloudPAM REST API.
//
// It covers only the resources the Terraform provider manages: pools and
// accounts. Authentication uses the same scheme as the CloudPAM server's
// DualAuthMiddleware: an API key presented as "Authorization: Bearer cpam_...".
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

// APIKeyPrefix is the prefix every CloudPAM API key carries. The server rejects
// bearer tokens that do not start with it.
const APIKeyPrefix = "cpam_"

// DefaultTimeout bounds a single API request.
const DefaultTimeout = 30 * time.Second

// ErrNotFound is returned (wrapped in an *APIError) when the API answers 404.
// Callers use errors.Is(err, client.ErrNotFound) to implement the Terraform
// "drop it from state" behaviour on read.
var ErrNotFound = errors.New("resource not found")

// APIError describes a non-2xx response from the CloudPAM API.
type APIError struct {
	StatusCode int
	Method     string
	Path       string
	// Message mirrors the server's `error` field.
	Message string
	// Detail mirrors the server's optional `detail` field.
	Detail string
	// Body holds the raw response body when it was not a JSON api error.
	Body string
}

func (e *APIError) Error() string {
	msg := e.Message
	if msg == "" {
		msg = strings.TrimSpace(e.Body)
	}
	if msg == "" {
		msg = http.StatusText(e.StatusCode)
	}
	if e.Detail != "" {
		return fmt.Sprintf("%s %s: %d %s: %s (%s)", e.Method, e.Path, e.StatusCode, http.StatusText(e.StatusCode), msg, e.Detail)
	}
	return fmt.Sprintf("%s %s: %d %s: %s", e.Method, e.Path, e.StatusCode, http.StatusText(e.StatusCode), msg)
}

// Is lets errors.Is(err, ErrNotFound) succeed for 404 responses.
func (e *APIError) Is(target error) bool {
	return target == ErrNotFound && e.StatusCode == http.StatusNotFound
}

// IsNotFound reports whether err represents a 404 from the CloudPAM API.
func IsNotFound(err error) bool { return errors.Is(err, ErrNotFound) }

// Client talks to a CloudPAM server.
type Client struct {
	baseURL   *url.URL
	apiKey    string
	userAgent string
	httpc     *http.Client
}

// Option customises a Client.
type Option func(*Client)

// WithHTTPClient overrides the underlying *http.Client (used by tests).
func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) {
		if h != nil {
			c.httpc = h
		}
	}
}

// WithUserAgent sets the User-Agent header sent with every request.
func WithUserAgent(ua string) Option {
	return func(c *Client) {
		if ua != "" {
			c.userAgent = ua
		}
	}
}

// New builds a Client. endpoint is the CloudPAM base URL (for example
// "https://cloudpam.example.com"); a trailing "/api/v1" is tolerated and
// stripped. apiKey is a CloudPAM API key.
func New(endpoint, apiKey string, opts ...Option) (*Client, error) {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return nil, errors.New("endpoint is required")
	}
	u, err := url.Parse(endpoint)
	if err != nil {
		return nil, fmt.Errorf("invalid endpoint %q: %w", endpoint, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("invalid endpoint %q: scheme must be http or https", endpoint)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("invalid endpoint %q: missing host", endpoint)
	}
	u.Path = strings.TrimSuffix(strings.TrimSuffix(strings.TrimRight(u.Path, "/"), "/api/v1"), "/")
	u.RawQuery = ""
	u.Fragment = ""

	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return nil, errors.New("api_key is required")
	}

	c := &Client{
		baseURL:   u,
		apiKey:    apiKey,
		userAgent: "terraform-provider-cloudpam",
		httpc:     &http.Client{Timeout: DefaultTimeout},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c, nil
}

// BaseURL returns the normalised server base URL.
func (c *Client) BaseURL() string { return c.baseURL.String() }

// do performs an API call. body, when non-nil, is JSON encoded. out, when
// non-nil, receives the decoded JSON response.
func (c *Client) do(ctx context.Context, method, path string, query url.Values, body any, out any) error {
	var reader io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode request body: %w", err)
		}
		reader = bytes.NewReader(buf)
	}

	u := *c.baseURL
	u.Path = c.baseURL.Path + path
	if len(query) > 0 {
		u.RawQuery = query.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, method, u.String(), reader)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.userAgent)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpc.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return newAPIError(method, path, resp)
	}
	if out == nil || resp.StatusCode == http.StatusNoContent {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("%s %s: decode response: %w", method, path, err)
	}
	return nil
}

func newAPIError(method, path string, resp *http.Response) *APIError {
	apiErr := &APIError{StatusCode: resp.StatusCode, Method: method, Path: path}
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var payload struct {
		Error  string `json:"error"`
		Detail string `json:"detail"`
	}
	if len(raw) > 0 && json.Unmarshal(raw, &payload) == nil && payload.Error != "" {
		apiErr.Message = payload.Error
		apiErr.Detail = payload.Detail
		return apiErr
	}
	apiErr.Body = string(raw)
	return apiErr
}
