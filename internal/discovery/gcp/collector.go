// Package gcp provides a GCP VPC network/subnetwork/address discovery collector.
// It uses the GCP Compute Engine REST API directly to avoid heavy SDK dependencies.
package gcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"

	"cloudpam/internal/domain"
)

const (
	computeBaseURL = "https://compute.googleapis.com/compute/v1"
	computeScope   = "https://www.googleapis.com/auth/compute.readonly"
)

// Collector discovers GCP VPC networks, subnetworks, and external addresses.
type Collector struct {
	tokenSource oauth2.TokenSource
	httpClient  *http.Client
}

// New creates a new GCP collector using Application Default Credentials.
func New() *Collector {
	return &Collector{}
}

// NewWithTokenSource creates a new GCP collector with an explicit token source.
// This allows injecting custom credentials or test mocks.
func NewWithTokenSource(ts oauth2.TokenSource) *Collector {
	return &Collector{tokenSource: ts}
}

// NewWithHTTPClient creates a new GCP collector with a custom HTTP client.
// This is primarily useful for testing.
func NewWithHTTPClient(client *http.Client) *Collector {
	return &Collector{httpClient: client}
}

// Provider returns "gcp".
func (c *Collector) Provider() string { return "gcp" }

// Discover discovers VPC networks, subnetworks, and external addresses for the given account.
// The account's ExternalID (or Key) is used as the GCP project ID.
// If account.Regions is set, only subnetworks and addresses in those regions are returned.
func (c *Collector) Discover(ctx context.Context, account domain.Account) ([]domain.DiscoveredResource, error) {
	project := ProjectID(account)
	if project == "" {
		return nil, fmt.Errorf("account %d has no external_id or key for GCP project", account.ID)
	}

	client, err := c.getHTTPClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("create http client: %w", err)
	}

	regionSet := make(map[string]bool, len(account.Regions))
	for _, r := range account.Regions {
		regionSet[r] = true
	}

	var all []domain.DiscoveredResource
	now := time.Now().UTC()

	// Discover VPC networks (global resource)
	networks, err := discoverNetworks(ctx, client, account, project, now)
	if err != nil {
		fmt.Printf("failed to discover GCP networks for project %s: %v\n", project, err)
	} else {
		all = append(all, networks...)
	}

	// Discover subnetworks (regional, via aggregated list)
	subnets, err := discoverSubnetworks(ctx, client, account, project, regionSet, now)
	if err != nil {
		fmt.Printf("failed to discover GCP subnetworks for project %s: %v\n", project, err)
	} else {
		all = append(all, subnets...)
	}

	// Discover external addresses (regional, via aggregated list)
	addrs, err := discoverAddresses(ctx, client, account, project, regionSet, now)
	if err != nil {
		fmt.Printf("failed to discover GCP addresses for project %s: %v\n", project, err)
	} else {
		all = append(all, addrs...)
	}

	return all, nil
}

func (c *Collector) getHTTPClient(ctx context.Context) (*http.Client, error) {
	if c.httpClient != nil {
		return c.httpClient, nil
	}
	if c.tokenSource != nil {
		return oauth2.NewClient(ctx, c.tokenSource), nil
	}
	// Use Application Default Credentials
	ts, err := google.DefaultTokenSource(ctx, computeScope)
	if err != nil {
		return nil, fmt.Errorf("get default credentials: %w", err)
	}
	return oauth2.NewClient(ctx, ts), nil
}

// --- GCP Compute REST API response types ---

type networkList struct {
	Items         []network `json:"items"`
	NextPageToken string    `json:"nextPageToken"`
}

type network struct {
	ID                   uint64        `json:"id,string"`
	Name                 string        `json:"name"`
	IPv4Range            string        `json:"IPv4Range"`
	AutoCreateSubnetwork bool          `json:"autoCreateSubnetworks"`
	RoutingConfig        routingConfig `json:"routingConfig"`
	SelfLink             string        `json:"selfLink"`
}

type routingConfig struct {
	RoutingMode string `json:"routingMode"`
}

type aggregatedSubnetworkList struct {
	Items         map[string]subnetworksScopedList `json:"items"`
	NextPageToken string                           `json:"nextPageToken"`
}

type subnetworksScopedList struct {
	Subnetworks []subnetwork `json:"subnetworks"`
}

type subnetwork struct {
	ID                    uint64 `json:"id,string"`
	Name                  string `json:"name"`
	Network               string `json:"network"`
	IpCidrRange           string `json:"ipCidrRange"`
	GatewayAddress        string `json:"gatewayAddress"`
	Region                string `json:"region"`
	Purpose               string `json:"purpose"`
	StackType             string `json:"stackType"`
	PrivateIpGoogleAccess bool   `json:"privateIpGoogleAccess"`
}

type aggregatedAddressList struct {
	Items         map[string]addressesScopedList `json:"items"`
	NextPageToken string                         `json:"nextPageToken"`
}

type addressesScopedList struct {
	Addresses []address `json:"addresses"`
}

type address struct {
	ID          uint64 `json:"id,string"`
	Name        string `json:"name"`
	Address     string `json:"address"`
	AddressType string `json:"addressType"`
	Status      string `json:"status"`
	NetworkTier string `json:"networkTier"`
	IpVersion   string `json:"ipVersion"`
	Purpose     string `json:"purpose"`
}

// --- Discovery functions ---

func discoverNetworks(ctx context.Context, client *http.Client, account domain.Account, project string, now time.Time) ([]domain.DiscoveredResource, error) {
	var resources []domain.DiscoveredResource

	pageToken := ""
	for {
		url := fmt.Sprintf("%s/projects/%s/global/networks", computeBaseURL, project)
		if pageToken != "" {
			url += "?pageToken=" + pageToken
		}

		var result networkList
		if err := doGet(ctx, client, url, &result); err != nil {
			return nil, err
		}

		for _, net := range result.Items {
			meta := map[string]string{
				"auto_create_subnetworks": fmt.Sprintf("%v", net.AutoCreateSubnetwork),
			}
			if net.RoutingConfig.RoutingMode != "" {
				meta["routing_mode"] = net.RoutingConfig.RoutingMode
			}

			cidr := net.IPv4Range // empty for custom-mode networks

			resources = append(resources, domain.DiscoveredResource{
				ID:           uuid.New(),
				AccountID:    account.ID,
				Provider:     "gcp",
				Region:       "global",
				ResourceType: domain.ResourceTypeVPC,
				ResourceID:   fmt.Sprintf("%d", net.ID),
				Name:         net.Name,
				CIDR:         cidr,
				Status:       domain.DiscoveryStatusActive,
				Metadata:     meta,
				DiscoveredAt: now,
				LastSeenAt:   now,
			})
		}

		if result.NextPageToken == "" {
			break
		}
		pageToken = result.NextPageToken
	}

	return resources, nil
}

func discoverSubnetworks(ctx context.Context, client *http.Client, account domain.Account, project string, regionSet map[string]bool, now time.Time) ([]domain.DiscoveredResource, error) {
	var resources []domain.DiscoveredResource

	pageToken := ""
	for {
		url := fmt.Sprintf("%s/projects/%s/aggregated/subnetworks", computeBaseURL, project)
		if pageToken != "" {
			url += "?pageToken=" + pageToken
		}

		var result aggregatedSubnetworkList
		if err := doGet(ctx, client, url, &result); err != nil {
			return nil, err
		}

		for scope, item := range result.Items {
			region := RegionFromScope(scope)
			if len(regionSet) > 0 && !regionSet[region] {
				continue
			}

			for _, subnet := range item.Subnetworks {
				networkName := LastPathComponent(subnet.Network)
				meta := map[string]string{
					"network":        networkName,
					"gateway":        subnet.GatewayAddress,
					"private_access": fmt.Sprintf("%v", subnet.PrivateIpGoogleAccess),
				}
				if subnet.Purpose != "" {
					meta["purpose"] = subnet.Purpose
				}
				if subnet.StackType != "" {
					meta["stack_type"] = subnet.StackType
				}

				parentRef := networkName

				resources = append(resources, domain.DiscoveredResource{
					ID:               uuid.New(),
					AccountID:        account.ID,
					Provider:         "gcp",
					Region:           region,
					ResourceType:     domain.ResourceTypeSubnet,
					ResourceID:       fmt.Sprintf("%d", subnet.ID),
					Name:             subnet.Name,
					CIDR:             subnet.IpCidrRange,
					ParentResourceID: &parentRef,
					Status:           domain.DiscoveryStatusActive,
					Metadata:         meta,
					DiscoveredAt:     now,
					LastSeenAt:       now,
				})
			}
		}

		if result.NextPageToken == "" {
			break
		}
		pageToken = result.NextPageToken
	}

	return resources, nil
}

func discoverAddresses(ctx context.Context, client *http.Client, account domain.Account, project string, regionSet map[string]bool, now time.Time) ([]domain.DiscoveredResource, error) {
	var resources []domain.DiscoveredResource

	pageToken := ""
	for {
		url := fmt.Sprintf("%s/projects/%s/aggregated/addresses", computeBaseURL, project)
		if pageToken != "" {
			url += "?pageToken=" + pageToken
		}

		var result aggregatedAddressList
		if err := doGet(ctx, client, url, &result); err != nil {
			return nil, err
		}

		for scope, item := range result.Items {
			region := RegionFromScope(scope)
			if len(regionSet) > 0 && !regionSet[region] {
				continue
			}

			for _, addr := range item.Addresses {
				// Only discover EXTERNAL addresses (static external IPs)
				if addr.AddressType != "EXTERNAL" {
					continue
				}

				cidr := ""
				if addr.Address != "" {
					cidr = addr.Address + "/32"
				}

				meta := map[string]string{
					"status":       addr.Status,
					"address_type": addr.AddressType,
				}
				if addr.NetworkTier != "" {
					meta["network_tier"] = addr.NetworkTier
				}
				if addr.IpVersion != "" {
					meta["ip_version"] = addr.IpVersion
				}
				if addr.Purpose != "" {
					meta["purpose"] = addr.Purpose
				}

				resources = append(resources, domain.DiscoveredResource{
					ID:           uuid.New(),
					AccountID:    account.ID,
					Provider:     "gcp",
					Region:       region,
					ResourceType: domain.ResourceTypeElasticIP,
					ResourceID:   fmt.Sprintf("%d", addr.ID),
					Name:         addr.Name,
					CIDR:         cidr,
					Status:       domain.DiscoveryStatusActive,
					Metadata:     meta,
					DiscoveredAt: now,
					LastSeenAt:   now,
				})
			}
		}

		if result.NextPageToken == "" {
			break
		}
		pageToken = result.NextPageToken
	}

	return resources, nil
}

// doGet performs a GET request and decodes the JSON response.
func doGet(ctx context.Context, client *http.Client, url string, dest any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("http get %s: %w", url, err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("http %d from %s: %s", resp.StatusCode, url, body)
	}

	if err := json.NewDecoder(resp.Body).Decode(dest); err != nil {
		return fmt.Errorf("decode response from %s: %w", url, err)
	}
	return nil
}

// ProjectID extracts the GCP project ID from the account.
// It uses ExternalID if set, otherwise falls back to Key (stripping "gcp:" prefix).
func ProjectID(account domain.Account) string {
	if account.ExternalID != "" {
		return account.ExternalID
	}
	key := account.Key
	if strings.HasPrefix(key, "gcp:") {
		return strings.TrimPrefix(key, "gcp:")
	}
	return key
}

// RegionFromScope extracts the region name from a GCP aggregated list scope key.
// Scope keys look like "regions/us-central1" or "global".
func RegionFromScope(scope string) string {
	if strings.HasPrefix(scope, "regions/") {
		return strings.TrimPrefix(scope, "regions/")
	}
	return scope
}

// LastPathComponent returns the last component of a resource URL path.
// For example, ".../projects/my-project/global/networks/default" returns "default".
func LastPathComponent(url string) string {
	if idx := strings.LastIndex(url, "/"); idx >= 0 {
		return url[idx+1:]
	}
	return url
}
