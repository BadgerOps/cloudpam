// Package aws provides an AWS VPC/subnet/EIP discovery collector.
package aws

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"cloudpam/internal/domain"
	"cloudpam/internal/observability"
)

// startEC2Span opens a client span around a group of EC2 API calls. With
// tracing disabled the shared tracer is the OpenTelemetry no-op, so the cost is
// an interface call against a network round trip.
func startEC2Span(ctx context.Context, operation, region string) (context.Context, trace.Span) {
	return observability.Tracer().Start(ctx, "aws.ec2."+operation,
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String("cloud.provider", "aws"),
			attribute.String("cloud.region", displayRegion(region)),
			attribute.String("rpc.service", "ec2"),
			attribute.String("rpc.method", operation),
		),
	)
}

// endEC2Span records the outcome of an EC2 call group and closes the span.
func endEC2Span(span trace.Span, resourceCount int, err error) {
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	} else {
		span.SetAttributes(attribute.Int("aws.resource_count", resourceCount))
	}
	span.End()
}

// Collector discovers AWS VPCs, subnets, and Elastic IPs.
type Collector struct {
	credsProvider aws.CredentialsProvider
	loadConfig    func(context.Context, string, aws.CredentialsProvider) (aws.Config, error)
	newEC2Client  func(aws.Config) ec2API
}

type ec2API interface {
	DescribeVpcs(context.Context, *ec2.DescribeVpcsInput, ...func(*ec2.Options)) (*ec2.DescribeVpcsOutput, error)
	DescribeSubnets(context.Context, *ec2.DescribeSubnetsInput, ...func(*ec2.Options)) (*ec2.DescribeSubnetsOutput, error)
	DescribeAddresses(context.Context, *ec2.DescribeAddressesInput, ...func(*ec2.Options)) (*ec2.DescribeAddressesOutput, error)
}

// New creates a new AWS collector using the default credential chain.
func New() *Collector {
	return &Collector{}
}

// NewWithCredentials creates a new AWS collector using the given credentials provider.
// This is used for cross-account discovery via STS AssumeRole.
func NewWithCredentials(cp aws.CredentialsProvider) *Collector {
	return &Collector{credsProvider: cp}
}

// Provider returns "aws".
func (c *Collector) Provider() string { return "aws" }

// Discover discovers VPCs, subnets, and Elastic IPs for the given account.
// Authentication uses the default AWS credential chain (env vars, instance profile, etc.).
// The account's Regions field determines which regions to query. If empty, uses default config region.
func (c *Collector) Discover(ctx context.Context, account domain.Account) (resources []domain.DiscoveredResource, err error) {
	ctx, span := observability.Tracer().Start(ctx, "aws.discover",
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String("cloud.provider", "aws"),
			attribute.String("cloudpam.account.key", account.Key),
			attribute.Int("cloud.region.count", len(account.Regions)),
		),
	)
	defer func() { endEC2Span(span, len(resources), err) }()

	regions := account.Regions
	if len(regions) == 0 {
		// If no regions specified, use default config (single region)
		regions = []string{""}
	}

	var allResources []domain.DiscoveredResource
	var errs []error
	now := time.Now().UTC()

	// Discover in each region
	for _, region := range regions {
		cfg, err := c.loadConfigForRegion(ctx, region)
		if err != nil {
			errs = append(errs, fmt.Errorf("load config for region %s: %w", displayRegion(region), err))
			continue
		}

		client := c.ec2Client(cfg)

		// Get actual region from config (in case it was empty)
		actualRegion := cfg.Region
		if actualRegion == "" {
			actualRegion = region
		}

		// Discover VPCs
		vpcs, err := c.discoverVPCs(ctx, client, account, actualRegion, now)
		if err != nil {
			errs = append(errs, fmt.Errorf("discover VPCs in region %s: %w", displayRegion(actualRegion), err))
			continue
		}
		allResources = append(allResources, vpcs...)

		// Discover subnets
		subnets, err := c.discoverSubnets(ctx, client, account, actualRegion, now)
		if err != nil {
			errs = append(errs, fmt.Errorf("discover subnets in region %s: %w", displayRegion(actualRegion), err))
			continue
		}
		allResources = append(allResources, subnets...)

		// Discover Elastic IPs
		eips, err := c.discoverElasticIPs(ctx, client, account, actualRegion, now)
		if err != nil {
			errs = append(errs, fmt.Errorf("discover Elastic IPs in region %s: %w", displayRegion(actualRegion), err))
			continue
		}
		allResources = append(allResources, eips...)
	}

	if len(errs) > 0 {
		return nil, fmt.Errorf("incomplete AWS discovery: %w", errors.Join(errs...))
	}

	return allResources, nil
}

func (c *Collector) loadConfigForRegion(ctx context.Context, region string) (aws.Config, error) {
	if c.loadConfig != nil {
		return c.loadConfig(ctx, region, c.credsProvider)
	}

	return loadDefaultConfigForRegion(ctx, region, c.credsProvider)
}

func loadDefaultConfigForRegion(ctx context.Context, region string, credsProvider aws.CredentialsProvider) (aws.Config, error) {
	var opts []func(*config.LoadOptions) error

	// Set region if provided
	if region != "" {
		opts = append(opts, config.WithRegion(region))
	}

	// Use injected credentials if available (cross-account AssumeRole)
	if credsProvider != nil {
		opts = append(opts, config.WithCredentialsProvider(credsProvider))
	}

	return config.LoadDefaultConfig(ctx, opts...)
}

func (c *Collector) ec2Client(cfg aws.Config) ec2API {
	if c.newEC2Client != nil {
		return c.newEC2Client(cfg)
	}
	return ec2.NewFromConfig(cfg)
}

func displayRegion(region string) string {
	if strings.TrimSpace(region) == "" {
		return "default"
	}
	return region
}

// nextPageToken returns the token for the next page, or "" when the listing is
// exhausted. A token identical to the current one is treated as exhausted so a
// misbehaving endpoint cannot spin the caller forever.
func nextPageToken(current, next *string) string {
	token := aws.ToString(next)
	if token == "" || token == aws.ToString(current) {
		return ""
	}
	return token
}

func (c *Collector) discoverVPCs(ctx context.Context, client ec2API, account domain.Account, region string, now time.Time) (resources []domain.DiscoveredResource, err error) {
	ctx, span := startEC2Span(ctx, "DescribeVpcs", region)
	defer func() { endEC2Span(span, len(resources), err) }()

	var token *string
	for {
		out, err := client.DescribeVpcs(ctx, &ec2.DescribeVpcsInput{NextToken: token})
		if err != nil {
			return nil, err
		}

		for _, vpc := range out.Vpcs {
			name := extractTagName(vpc.Tags)
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

		next := nextPageToken(token, out.NextToken)
		if next == "" {
			break
		}
		token = aws.String(next)
	}
	return resources, nil
}

func (c *Collector) discoverSubnets(ctx context.Context, client ec2API, account domain.Account, region string, now time.Time) (resources []domain.DiscoveredResource, err error) {
	ctx, span := startEC2Span(ctx, "DescribeSubnets", region)
	defer func() { endEC2Span(span, len(resources), err) }()

	var token *string
	for {
		out, err := client.DescribeSubnets(ctx, &ec2.DescribeSubnetsInput{NextToken: token})
		if err != nil {
			return nil, err
		}

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
				Region:           region,
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

		next := nextPageToken(token, out.NextToken)
		if next == "" {
			break
		}
		token = aws.String(next)
	}
	return resources, nil
}

// discoverElasticIPs issues a single DescribeAddresses call: unlike
// DescribeVpcs and DescribeSubnets, the EC2 DescribeAddresses response carries
// no NextToken and is not paginated.
func (c *Collector) discoverElasticIPs(ctx context.Context, client ec2API, account domain.Account, region string, now time.Time) (resources []domain.DiscoveredResource, err error) {
	ctx, span := startEC2Span(ctx, "DescribeAddresses", region)
	defer func() { endEC2Span(span, len(resources), err) }()

	out, err := client.DescribeAddresses(ctx, &ec2.DescribeAddressesInput{})
	if err != nil {
		return nil, err
	}

	for _, addr := range out.Addresses {
		name := extractTagName(addr.Tags)
		allocID := aws.ToString(addr.AllocationId)
		publicIP := aws.ToString(addr.PublicIp)

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
