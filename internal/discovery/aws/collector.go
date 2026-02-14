// Package aws provides an AWS VPC/subnet/EIP discovery collector.
package aws

import (
	"context"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/google/uuid"

	"cloudpam/internal/domain"
)

// Collector discovers AWS VPCs, subnets, and Elastic IPs.
type Collector struct{}

// New creates a new AWS collector.
func New() *Collector {
	return &Collector{}
}

// Provider returns "aws".
func (c *Collector) Provider() string { return "aws" }

// Discover discovers VPCs, subnets, and Elastic IPs for the given account.
// Authentication uses the default AWS credential chain (env vars, instance profile, etc.).
// The account's Regions field determines which region to query.
func (c *Collector) Discover(ctx context.Context, account domain.Account) ([]domain.DiscoveredResource, error) {
	cfg, err := c.loadConfig(ctx, account)
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}

	client := ec2.NewFromConfig(cfg)
	var resources []domain.DiscoveredResource
	now := time.Now().UTC()

	// Discover VPCs
	vpcs, err := c.discoverVPCs(ctx, client, account, now)
	if err != nil {
		return nil, fmt.Errorf("discover vpcs: %w", err)
	}
	resources = append(resources, vpcs...)

	// Discover subnets
	subnets, err := c.discoverSubnets(ctx, client, account, now)
	if err != nil {
		return nil, fmt.Errorf("discover subnets: %w", err)
	}
	resources = append(resources, subnets...)

	// Discover Elastic IPs
	eips, err := c.discoverElasticIPs(ctx, client, account, now)
	if err != nil {
		return nil, fmt.Errorf("discover elastic ips: %w", err)
	}
	resources = append(resources, eips...)

	return resources, nil
}

func (c *Collector) loadConfig(ctx context.Context, account domain.Account) (aws.Config, error) {
	var opts []func(*config.LoadOptions) error

	// Use first region from account if set
	if len(account.Regions) > 0 {
		opts = append(opts, config.WithRegion(account.Regions[0]))
	}

	return config.LoadDefaultConfig(ctx, opts...)
}

func (c *Collector) discoverVPCs(ctx context.Context, client *ec2.Client, account domain.Account, now time.Time) ([]domain.DiscoveredResource, error) {
	out, err := client.DescribeVpcs(ctx, &ec2.DescribeVpcsInput{})
	if err != nil {
		return nil, err
	}

	var resources []domain.DiscoveredResource
	for _, vpc := range out.Vpcs {
		name := extractTagName(vpc.Tags)
		region := firstRegion(account)
		meta := map[string]string{
			"state": string(vpc.State),
		}
		if vpc.IsDefault != nil && *vpc.IsDefault {
			meta["is_default"] = "true"
		}

		resources = append(resources, domain.DiscoveredResource{
			ID:           uuid.New(),
			AccountID:    account.ID,
			Provider:     "aws",
			Region:       region,
			ResourceType: domain.ResourceTypeVPC,
			ResourceID:   aws.ToString(vpc.VpcId),
			Name:         name,
			CIDR:         aws.ToString(vpc.CidrBlock),
			Status:       domain.DiscoveryStatusActive,
			Metadata:     meta,
			DiscoveredAt: now,
			LastSeenAt:   now,
		})
	}
	return resources, nil
}

func (c *Collector) discoverSubnets(ctx context.Context, client *ec2.Client, account domain.Account, now time.Time) ([]domain.DiscoveredResource, error) {
	out, err := client.DescribeSubnets(ctx, &ec2.DescribeSubnetsInput{})
	if err != nil {
		return nil, err
	}

	var resources []domain.DiscoveredResource
	for _, subnet := range out.Subnets {
		name := extractTagName(subnet.Tags)
		vpcID := aws.ToString(subnet.VpcId)
		az := aws.ToString(subnet.AvailabilityZone)
		meta := map[string]string{
			"availability_zone": az,
			"state":             string(subnet.State),
		}
		if subnet.AvailableIpAddressCount != nil {
			meta["available_ips"] = fmt.Sprintf("%d", *subnet.AvailableIpAddressCount)
		}

		resources = append(resources, domain.DiscoveredResource{
			ID:               uuid.New(),
			AccountID:        account.ID,
			Provider:         "aws",
			Region:           az,
			ResourceType:     domain.ResourceTypeSubnet,
			ResourceID:       aws.ToString(subnet.SubnetId),
			Name:             name,
			CIDR:             aws.ToString(subnet.CidrBlock),
			ParentResourceID: &vpcID,
			Status:           domain.DiscoveryStatusActive,
			Metadata:         meta,
			DiscoveredAt:     now,
			LastSeenAt:       now,
		})
	}
	return resources, nil
}

func (c *Collector) discoverElasticIPs(ctx context.Context, client *ec2.Client, account domain.Account, now time.Time) ([]domain.DiscoveredResource, error) {
	out, err := client.DescribeAddresses(ctx, &ec2.DescribeAddressesInput{})
	if err != nil {
		return nil, err
	}

	var resources []domain.DiscoveredResource
	for _, addr := range out.Addresses {
		name := extractTagName(addr.Tags)
		allocID := aws.ToString(addr.AllocationId)
		publicIP := aws.ToString(addr.PublicIp)
		region := firstRegion(account)

		cidr := ""
		if publicIP != "" {
			cidr = publicIP + "/32"
		}

		meta := map[string]string{
			"domain": string(addr.Domain),
		}
		if addr.InstanceId != nil {
			meta["instance_id"] = *addr.InstanceId
		}
		if addr.AssociationId != nil {
			meta["association_id"] = *addr.AssociationId
		}

		resources = append(resources, domain.DiscoveredResource{
			ID:           uuid.New(),
			AccountID:    account.ID,
			Provider:     "aws",
			Region:       region,
			ResourceType: domain.ResourceTypeElasticIP,
			ResourceID:   allocID,
			Name:         name,
			CIDR:         cidr,
			Status:       domain.DiscoveryStatusActive,
			Metadata:     meta,
			DiscoveredAt: now,
			LastSeenAt:   now,
		})
	}
	return resources, nil
}

// extractTagName extracts the "Name" tag from a list of EC2 tags.
func extractTagName(tags []ec2types.Tag) string {
	for _, tag := range tags {
		if aws.ToString(tag.Key) == "Name" {
			return aws.ToString(tag.Value)
		}
	}
	return ""
}

// firstRegion returns the first region from account or empty string.
func firstRegion(account domain.Account) string {
	if len(account.Regions) > 0 {
		return account.Regions[0]
	}
	return ""
}
