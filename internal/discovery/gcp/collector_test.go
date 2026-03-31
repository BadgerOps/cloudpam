package gcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"cloudpam/internal/discovery"
	"cloudpam/internal/domain"
)

// Verify Collector implements the discovery.Collector interface at compile time.
var _ discovery.Collector = (*Collector)(nil)

func TestProvider(t *testing.T) {
	c := New()
	if got := c.Provider(); got != "gcp" {
		t.Errorf("Provider() = %q, want %q", got, "gcp")
	}
}

func TestProjectID(t *testing.T) {
	tests := []struct {
		name    string
		account domain.Account
		want    string
	}{
		{
			name:    "uses ExternalID when set",
			account: domain.Account{ExternalID: "my-gcp-project", Key: "gcp:other"},
			want:    "my-gcp-project",
		},
		{
			name:    "strips gcp: prefix from Key",
			account: domain.Account{Key: "gcp:my-project"},
			want:    "my-project",
		},
		{
			name:    "uses Key as-is without prefix",
			account: domain.Account{Key: "bare-project"},
			want:    "bare-project",
		},
		{
			name:    "empty returns empty",
			account: domain.Account{},
			want:    "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ProjectID(tt.account); got != tt.want {
				t.Errorf("ProjectID() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRegionFromScope(t *testing.T) {
	tests := []struct {
		scope string
		want  string
	}{
		{"regions/us-central1", "us-central1"},
		{"regions/europe-west1", "europe-west1"},
		{"global", "global"},
	}

	for _, tt := range tests {
		t.Run(tt.scope, func(t *testing.T) {
			if got := RegionFromScope(tt.scope); got != tt.want {
				t.Errorf("RegionFromScope(%q) = %q, want %q", tt.scope, got, tt.want)
			}
		})
	}
}

func TestLastPathComponent(t *testing.T) {
	tests := []struct {
		url  string
		want string
	}{
		{"https://compute.googleapis.com/projects/p/global/networks/default", "default"},
		{"projects/p/regions/us-central1/subnetworks/my-subnet", "my-subnet"},
		{"simple", "simple"},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			if got := LastPathComponent(tt.url); got != tt.want {
				t.Errorf("LastPathComponent(%q) = %q, want %q", tt.url, got, tt.want)
			}
		})
	}
}

func TestDiscover_EmptyProject(t *testing.T) {
	c := New()
	_, err := c.Discover(context.Background(), domain.Account{ID: 1})
	if err == nil {
		t.Fatal("expected error for empty project, got nil")
	}
	if got := err.Error(); got != "account 1 has no external_id or key for GCP project" {
		t.Errorf("unexpected error: %s", got)
	}
}

func TestDiscover_MockAPI(t *testing.T) {
	// Set up a mock GCP Compute API server
	mux := http.NewServeMux()

	mux.HandleFunc("/compute/v1/projects/test-project/global/networks", func(w http.ResponseWriter, r *http.Request) {
		resp := networkList{
			Items: []network{
				{
					ID:                   123456,
					Name:                 "default",
					AutoCreateSubnetwork: true,
					RoutingConfig:        routingConfig{RoutingMode: "REGIONAL"},
				},
				{
					ID:                   789012,
					Name:                 "custom-vpc",
					AutoCreateSubnetwork: false,
					IPv4Range:            "10.128.0.0/9",
					RoutingConfig:        routingConfig{RoutingMode: "GLOBAL"},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Fatalf("encode network list response: %v", err)
		}
	})

	mux.HandleFunc("/compute/v1/projects/test-project/aggregated/subnetworks", func(w http.ResponseWriter, r *http.Request) {
		resp := aggregatedSubnetworkList{
			Items: map[string]subnetworksScopedList{
				"regions/us-central1": {
					Subnetworks: []subnetwork{
						{
							ID:                    111,
							Name:                  "default-subnet",
							Network:               "https://compute.googleapis.com/compute/v1/projects/test-project/global/networks/default",
							IpCidrRange:           "10.128.0.0/20",
							GatewayAddress:        "10.128.0.1",
							Purpose:               "PRIVATE",
							StackType:             "IPV4_ONLY",
							PrivateIpGoogleAccess: true,
						},
					},
				},
				"regions/europe-west1": {
					Subnetworks: []subnetwork{
						{
							ID:             222,
							Name:           "eu-subnet",
							Network:        "https://compute.googleapis.com/compute/v1/projects/test-project/global/networks/custom-vpc",
							IpCidrRange:    "10.132.0.0/20",
							GatewayAddress: "10.132.0.1",
						},
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Fatalf("encode subnetwork list response: %v", err)
		}
	})

	mux.HandleFunc("/compute/v1/projects/test-project/aggregated/addresses", func(w http.ResponseWriter, r *http.Request) {
		resp := aggregatedAddressList{
			Items: map[string]addressesScopedList{
				"regions/us-central1": {
					Addresses: []address{
						{
							ID:          333,
							Name:        "my-static-ip",
							Address:     "35.192.0.1",
							AddressType: "EXTERNAL",
							Status:      "IN_USE",
							NetworkTier: "PREMIUM",
							IpVersion:   "IPV4",
						},
						{
							// Internal address — should be skipped
							ID:          444,
							Name:        "internal-addr",
							Address:     "10.0.0.5",
							AddressType: "INTERNAL",
							Status:      "IN_USE",
						},
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Fatalf("encode address list response: %v", err)
		}
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	// Override the base URL to point to our mock server
	origBaseURL := computeBaseURL
	// We can't override the const, so we'll use a custom HTTP client approach
	// Create a collector with the mock server as the HTTP client
	collector := NewWithHTTPClient(server.Client())

	// We need to redirect requests to our mock server.
	// Use a custom transport that rewrites the URL.
	collector.httpClient = &http.Client{
		Transport: &rewriteTransport{
			base:    server.Client().Transport,
			baseURL: server.URL,
		},
	}
	_ = origBaseURL

	account := domain.Account{
		ID:         1,
		ExternalID: "test-project",
		Provider:   "gcp",
	}

	resources, err := collector.Discover(context.Background(), account)
	if err != nil {
		t.Fatalf("Discover() error: %v", err)
	}

	// Count by type
	counts := map[domain.CloudResourceType]int{}
	for _, r := range resources {
		counts[r.ResourceType]++
		// Verify common fields
		if r.AccountID != 1 {
			t.Errorf("resource %s: AccountID = %d, want 1", r.Name, r.AccountID)
		}
		if r.Provider != "gcp" {
			t.Errorf("resource %s: Provider = %q, want gcp", r.Name, r.Provider)
		}
		if r.Status != domain.DiscoveryStatusActive {
			t.Errorf("resource %s: Status = %q, want active", r.Name, r.Status)
		}
	}

	if counts[domain.ResourceTypeVPC] != 2 {
		t.Errorf("VPC count = %d, want 2", counts[domain.ResourceTypeVPC])
	}
	if counts[domain.ResourceTypeSubnet] != 2 {
		t.Errorf("Subnet count = %d, want 2", counts[domain.ResourceTypeSubnet])
	}
	if counts[domain.ResourceTypeElasticIP] != 1 {
		t.Errorf("ElasticIP count = %d, want 1 (internal should be skipped)", counts[domain.ResourceTypeElasticIP])
	}

	// Verify specific resources
	for _, r := range resources {
		switch r.Name {
		case "custom-vpc":
			if r.CIDR != "10.128.0.0/9" {
				t.Errorf("custom-vpc CIDR = %q, want 10.128.0.0/9", r.CIDR)
			}
			if r.Region != "global" {
				t.Errorf("custom-vpc Region = %q, want global", r.Region)
			}
		case "default-subnet":
			if r.CIDR != "10.128.0.0/20" {
				t.Errorf("default-subnet CIDR = %q, want 10.128.0.0/20", r.CIDR)
			}
			if r.Region != "us-central1" {
				t.Errorf("default-subnet Region = %q, want us-central1", r.Region)
			}
			if r.ParentResourceID == nil || *r.ParentResourceID != "default" {
				t.Errorf("default-subnet ParentResourceID = %v, want 'default'", r.ParentResourceID)
			}
		case "my-static-ip":
			if r.CIDR != "35.192.0.1/32" {
				t.Errorf("my-static-ip CIDR = %q, want 35.192.0.1/32", r.CIDR)
			}
		}
	}
}

func TestDiscover_RegionFilter(t *testing.T) {
	mux := http.NewServeMux()

	mux.HandleFunc("/compute/v1/projects/test-project/global/networks", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(networkList{})
	})

	mux.HandleFunc("/compute/v1/projects/test-project/aggregated/subnetworks", func(w http.ResponseWriter, r *http.Request) {
		resp := aggregatedSubnetworkList{
			Items: map[string]subnetworksScopedList{
				"regions/us-central1": {
					Subnetworks: []subnetwork{
						{ID: 111, Name: "us-subnet", IpCidrRange: "10.0.0.0/20", Network: "networks/default"},
					},
				},
				"regions/europe-west1": {
					Subnetworks: []subnetwork{
						{ID: 222, Name: "eu-subnet", IpCidrRange: "10.1.0.0/20", Network: "networks/default"},
					},
				},
			},
		}
		json.NewEncoder(w).Encode(resp)
	})

	mux.HandleFunc("/compute/v1/projects/test-project/aggregated/addresses", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(aggregatedAddressList{})
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	collector := &Collector{
		httpClient: &http.Client{
			Transport: &rewriteTransport{baseURL: server.URL},
		},
	}

	account := domain.Account{
		ID:         1,
		ExternalID: "test-project",
		Provider:   "gcp",
		Regions:    []string{"us-central1"}, // Only US
	}

	resources, err := collector.Discover(context.Background(), account)
	if err != nil {
		t.Fatalf("Discover() error: %v", err)
	}

	if len(resources) != 1 {
		t.Fatalf("expected 1 resource (only us-central1), got %d", len(resources))
	}
	if resources[0].Name != "us-subnet" {
		t.Errorf("expected us-subnet, got %s", resources[0].Name)
	}
}

// rewriteTransport rewrites request URLs to point to a local test server.
type rewriteTransport struct {
	base    http.RoundTripper
	baseURL string
}

func (t *rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Rewrite the URL to point to the test server
	newURL := t.baseURL + req.URL.Path
	if req.URL.RawQuery != "" {
		newURL += "?" + req.URL.RawQuery
	}

	newReq, err := http.NewRequestWithContext(req.Context(), req.Method, newURL, req.Body)
	if err != nil {
		return nil, err
	}
	newReq.Header = req.Header

	transport := t.base
	if transport == nil {
		transport = http.DefaultTransport
	}
	return transport.RoundTrip(newReq)
}
