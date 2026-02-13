package domain

// SearchRequest describes a search query across pools and accounts.
type SearchRequest struct {
	// Query is a free-text search matching name, CIDR, key, or description.
	Query string `json:"q,omitempty"`
	// CIDRContains finds pools whose CIDR contains this IP or prefix.
	CIDRContains string `json:"cidr_contains,omitempty"`
	// CIDRWithin finds pools that are within (contained by) this prefix.
	CIDRWithin string `json:"cidr_within,omitempty"`
	// Types filters results to specific entity types ("pool", "account").
	Types []string `json:"type,omitempty"`
	// Page is the 1-based page number.
	Page int `json:"page,omitempty"`
	// PageSize is the number of results per page.
	PageSize int `json:"page_size,omitempty"`
}

// SearchResultItem is a single search result that may be a pool or an account.
type SearchResultItem struct {
	Type        string `json:"type"`                   // "pool" or "account"
	ID          int64  `json:"id"`                     // entity ID
	Name        string `json:"name"`                   // pool name or account name
	CIDR        string `json:"cidr,omitempty"`         // pool CIDR
	Description string `json:"description,omitempty"`  // pool or account description
	Status      string `json:"status,omitempty"`       // pool status
	PoolType    string `json:"pool_type,omitempty"`    // pool type (supernet, region, etc.)
	AccountKey  string `json:"account_key,omitempty"`  // account key
	Provider    string `json:"provider,omitempty"`     // account provider
	ParentID    *int64 `json:"parent_id,omitempty"`    // pool parent ID
	AccountID   *int64 `json:"account_id,omitempty"`   // pool account ID
}

// SearchResponse is the paginated response for a search query.
type SearchResponse struct {
	Items    []SearchResultItem `json:"items"`
	Total    int                `json:"total"`
	Page     int                `json:"page"`
	PageSize int                `json:"page_size"`
}
