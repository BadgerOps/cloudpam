package client

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
)

// Account mirrors domain.Account from the CloudPAM server.
type Account struct {
	ID          int64    `json:"id"`
	Key         string   `json:"key"`
	Name        string   `json:"name"`
	Provider    string   `json:"provider,omitempty"`
	ExternalID  string   `json:"external_id,omitempty"`
	Description string   `json:"description,omitempty"`
	Platform    string   `json:"platform,omitempty"`
	Tier        string   `json:"tier,omitempty"`
	Environment string   `json:"environment,omitempty"`
	Regions     []string `json:"regions,omitempty"`
	CreatedAt   string   `json:"created_at,omitempty"`
	UpdatedAt   string   `json:"updated_at,omitempty"`
}

// AccountCreate mirrors domain.CreateAccount.
type AccountCreate struct {
	Key         string   `json:"key"`
	Name        string   `json:"name"`
	Provider    string   `json:"provider,omitempty"`
	ExternalID  string   `json:"external_id,omitempty"`
	Description string   `json:"description,omitempty"`
	Platform    string   `json:"platform,omitempty"`
	Tier        string   `json:"tier,omitempty"`
	Environment string   `json:"environment,omitempty"`
	Regions     []string `json:"regions,omitempty"`
}

// AccountUpdate is the PATCH /api/v1/accounts/{id} body.
//
// The server applies last-write-wins semantics to the optional string fields
// (an empty string clears them) and only replaces regions when the key decodes
// to a non-nil slice. Every field is therefore serialised without omitempty so
// that clearing a value in Terraform actually clears it server-side.
//
// `key` is deliberately absent: the storage layer ignores it, so the provider
// marks the attribute as requiring replacement instead.
type AccountUpdate struct {
	Name        string   `json:"name"`
	Provider    string   `json:"provider"`
	ExternalID  string   `json:"external_id"`
	Description string   `json:"description"`
	Platform    string   `json:"platform"`
	Tier        string   `json:"tier"`
	Environment string   `json:"environment"`
	Regions     []string `json:"regions"`
}

// ListAccounts returns every account visible to the API key.
func (c *Client) ListAccounts(ctx context.Context) ([]Account, error) {
	var out []Account
	if err := c.do(ctx, http.MethodGet, "/api/v1/accounts", nil, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetAccount fetches a single account. A missing account yields an error
// satisfying errors.Is(err, ErrNotFound).
func (c *Client) GetAccount(ctx context.Context, id int64) (*Account, error) {
	var out Account
	if err := c.do(ctx, http.MethodGet, accountPath(id), nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CreateAccount creates an account.
func (c *Client) CreateAccount(ctx context.Context, in AccountCreate) (*Account, error) {
	var out Account
	if err := c.do(ctx, http.MethodPost, "/api/v1/accounts", nil, in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateAccount patches an account.
func (c *Client) UpdateAccount(ctx context.Context, id int64, in AccountUpdate) (*Account, error) {
	if in.Regions == nil {
		// A nil slice marshals to null, which the server reads as "leave
		// regions alone". An empty slice is what actually clears them.
		in.Regions = []string{}
	}
	var out Account
	if err := c.do(ctx, http.MethodPatch, accountPath(id), nil, in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteAccount removes an account. When force is true, pools referencing the
// account are removed too (otherwise the server answers 409).
func (c *Client) DeleteAccount(ctx context.Context, id int64, force bool) error {
	var q url.Values
	if force {
		q = url.Values{"force": []string{"true"}}
	}
	return c.do(ctx, http.MethodDelete, accountPath(id), q, nil, nil)
}

func accountPath(id int64) string {
	return fmt.Sprintf("/api/v1/accounts/%s", strconv.FormatInt(id, 10))
}
