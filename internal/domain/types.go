package domain

import "time"

// Pool represents an address pool that can contain nested sub-pools.
type Pool struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	CIDR      string    `json:"cidr"`
	ParentID  *int64    `json:"parent_id,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// CreatePool is the input for creating a pool.
type CreatePool struct {
	Name     string `json:"name"`
	CIDR     string `json:"cidr"`
	ParentID *int64 `json:"parent_id,omitempty"`
}
