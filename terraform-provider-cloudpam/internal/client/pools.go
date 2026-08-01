package client

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
)

// Pool mirrors domain.Pool from the CloudPAM server.
type Pool struct {
	ID          int64             `json:"id"`
	Name        string            `json:"name"`
	CIDR        string            `json:"cidr"`
	ParentID    *int64            `json:"parent_id,omitempty"`
	AccountID   *int64            `json:"account_id,omitempty"`
	Type        string            `json:"type,omitempty"`
	Status      string            `json:"status,omitempty"`
	Source      string            `json:"source,omitempty"`
	Description string            `json:"description,omitempty"`
	Tags        map[string]string `json:"tags,omitempty"`
	CreatedAt   string            `json:"created_at,omitempty"`
	UpdatedAt   string            `json:"updated_at,omitempty"`
}

// PoolCreate mirrors domain.CreatePool. Nil ParentID/AccountID are omitted so
// the server treats them as "unset" rather than an explicit null.
type PoolCreate struct {
	Name        string            `json:"name"`
	CIDR        string            `json:"cidr"`
	ParentID    *int64            `json:"parent_id,omitempty"`
	AccountID   *int64            `json:"account_id,omitempty"`
	Type        string            `json:"type,omitempty"`
	Status      string            `json:"status,omitempty"`
	Source      string            `json:"source,omitempty"`
	Description string            `json:"description,omitempty"`
	Tags        map[string]string `json:"tags,omitempty"`
}

// PoolUpdate describes a PATCH /api/v1/pools/{id} request.
//
// The server distinguishes three states for account_id:
//
//	key absent      -> keep the pool's current account assignment
//	key present, null -> clear the account assignment
//	key present, N    -> assign account N
//
// A Go struct with `json:"account_id"` (no omitempty) — which is what
// domain.UpdatePool uses server-side — cannot express the first state, so the
// request body is assembled as a map instead. Set SetAccountID to include the
// key; leave AccountID nil at the same time to send an explicit null.
type PoolUpdate struct {
	Name        *string
	Type        *string
	Status      *string
	Description *string
	Tags        *map[string]string

	SetAccountID bool
	AccountID    *int64
}

// body renders the PATCH payload. Only explicitly-set fields are included.
func (u PoolUpdate) body() map[string]any {
	m := make(map[string]any, 6)
	if u.Name != nil {
		m["name"] = *u.Name
	}
	if u.Type != nil {
		m["type"] = *u.Type
	}
	if u.Status != nil {
		m["status"] = *u.Status
	}
	if u.Description != nil {
		m["description"] = *u.Description
	}
	if u.Tags != nil {
		m["tags"] = *u.Tags
	}
	if u.SetAccountID {
		// A nil *int64 marshals to JSON null, which clears the assignment.
		m["account_id"] = u.AccountID
	}
	return m
}

// ListPools returns every pool visible to the API key.
func (c *Client) ListPools(ctx context.Context) ([]Pool, error) {
	var out []Pool
	if err := c.do(ctx, http.MethodGet, "/api/v1/pools", nil, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetPool fetches a single pool. A missing pool yields an error satisfying
// errors.Is(err, ErrNotFound).
func (c *Client) GetPool(ctx context.Context, id int64) (*Pool, error) {
	var out Pool
	if err := c.do(ctx, http.MethodGet, poolPath(id), nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CreatePool creates a pool.
func (c *Client) CreatePool(ctx context.Context, in PoolCreate) (*Pool, error) {
	var out Pool
	if err := c.do(ctx, http.MethodPost, "/api/v1/pools", nil, in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdatePool patches a pool. Note that cidr, parent_id and source are immutable
// server-side; the provider marks those attributes as requiring replacement.
func (c *Client) UpdatePool(ctx context.Context, id int64, in PoolUpdate) (*Pool, error) {
	var out Pool
	if err := c.do(ctx, http.MethodPatch, poolPath(id), nil, in.body(), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeletePool removes a pool. When force is true, child pools are deleted too
// (otherwise the server answers 409 for a pool that still has children).
func (c *Client) DeletePool(ctx context.Context, id int64, force bool) error {
	var q url.Values
	if force {
		q = url.Values{"force": []string{"true"}}
	}
	return c.do(ctx, http.MethodDelete, poolPath(id), q, nil, nil)
}

func poolPath(id int64) string {
	return fmt.Sprintf("/api/v1/pools/%s", strconv.FormatInt(id, 10))
}
