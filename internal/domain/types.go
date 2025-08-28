package domain

import "time"

// Pool represents an address pool that can contain nested sub-pools.
type Pool struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	CIDR      string    `json:"cidr"`
	ParentID  *int64    `json:"parent_id,omitempty"`
	AccountID *int64    `json:"account_id,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// CreatePool is the input for creating a pool.
type CreatePool struct {
	Name      string `json:"name"`
	CIDR      string `json:"cidr"`
	ParentID  *int64 `json:"parent_id,omitempty"`
	AccountID *int64 `json:"account_id,omitempty"`
}

// Account represents a cloud account or project to which pools can be assigned.
// It uses a generic shape to support AWS accounts, GCP projects, etc.
type Account struct {
	ID          int64     `json:"id"`
	Key         string    `json:"key"` // unique key like "aws:123456789012" or "gcp:my-project"
	Name        string    `json:"name"`
	Provider    string    `json:"provider,omitempty"`
	ExternalID  string    `json:"external_id,omitempty"`
	Description string    `json:"description,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

// CreateAccount is the input for creating an account.
type CreateAccount struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Provider    string `json:"provider,omitempty"`
	ExternalID  string `json:"external_id,omitempty"`
	Description string `json:"description,omitempty"`
}
