package aws

import (
	"context"
	"fmt"
	"testing"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"

	"cloudpam/internal/domain"
)

// pagedEC2 serves DescribeVpcs and DescribeSubnets one page at a time, keyed by
// the NextToken the collector echoes back. An unknown token is an error so a
// collector that drops pagination is caught rather than silently truncating.
type pagedEC2 struct {
	vpcPages    [][]ec2types.Vpc
	subnetPages [][]ec2types.Subnet
	addresses   []ec2types.Address

	vpcCalls    int
	subnetCalls int
	addrCalls   int
}

func pageIndex(token *string) (int, error) {
	if token == nil {
		return 0, nil
	}
	var idx int
	if _, err := fmt.Sscanf(*token, "page-%d", &idx); err != nil {
		return 0, fmt.Errorf("unexpected page token %q", *token)
	}
	return idx, nil
}

func nextTokenFor(idx, total int) *string {
	if idx+1 >= total {
		return nil
	}
	return awssdk.String(fmt.Sprintf("page-%d", idx+1))
}

func (f *pagedEC2) DescribeVpcs(_ context.Context, in *ec2.DescribeVpcsInput, _ ...func(*ec2.Options)) (*ec2.DescribeVpcsOutput, error) {
	f.vpcCalls++
	idx, err := pageIndex(in.NextToken)
	if err != nil {
		return nil, err
	}
	if idx >= len(f.vpcPages) {
		return nil, fmt.Errorf("vpc page %d out of range", idx)
	}
	return &ec2.DescribeVpcsOutput{Vpcs: f.vpcPages[idx], NextToken: nextTokenFor(idx, len(f.vpcPages))}, nil
}

func (f *pagedEC2) DescribeSubnets(_ context.Context, in *ec2.DescribeSubnetsInput, _ ...func(*ec2.Options)) (*ec2.DescribeSubnetsOutput, error) {
	f.subnetCalls++
	idx, err := pageIndex(in.NextToken)
	if err != nil {
		return nil, err
	}
	if idx >= len(f.subnetPages) {
		return nil, fmt.Errorf("subnet page %d out of range", idx)
	}
	return &ec2.DescribeSubnetsOutput{Subnets: f.subnetPages[idx], NextToken: nextTokenFor(idx, len(f.subnetPages))}, nil
}

func (f *pagedEC2) DescribeAddresses(_ context.Context, _ *ec2.DescribeAddressesInput, _ ...func(*ec2.Options)) (*ec2.DescribeAddressesOutput, error) {
	f.addrCalls++
	return &ec2.DescribeAddressesOutput{Addresses: f.addresses}, nil
}

func TestDiscoverFollowsPaginationAcrossAllPages(t *testing.T) {
	client := &pagedEC2{
		vpcPages: [][]ec2types.Vpc{
			{{VpcId: awssdk.String("vpc-1"), CidrBlock: awssdk.String("10.0.0.0/16"), State: ec2types.VpcStateAvailable}},
			{{VpcId: awssdk.String("vpc-2"), CidrBlock: awssdk.String("10.1.0.0/16"), State: ec2types.VpcStateAvailable}},
			{{VpcId: awssdk.String("vpc-3"), CidrBlock: awssdk.String("10.2.0.0/16"), State: ec2types.VpcStateAvailable}},
		},
		subnetPages: [][]ec2types.Subnet{
			{{SubnetId: awssdk.String("subnet-1"), VpcId: awssdk.String("vpc-1"), CidrBlock: awssdk.String("10.0.1.0/24")}},
			{{SubnetId: awssdk.String("subnet-2"), VpcId: awssdk.String("vpc-2"), CidrBlock: awssdk.String("10.1.1.0/24")}},
		},
		addresses: []ec2types.Address{{AllocationId: awssdk.String("eipalloc-1"), PublicIp: awssdk.String("203.0.113.10")}},
	}
	collector := newTestCollector(map[string]ec2API{"us-east-1": client})

	resources, err := collector.Discover(context.Background(), domain.Account{ID: 7, Regions: []string{"us-east-1"}})
	if err != nil {
		t.Fatalf("Discover() unexpected error: %v", err)
	}

	byType := map[domain.CloudResourceType][]string{}
	for _, res := range resources {
		byType[res.ResourceType] = append(byType[res.ResourceType], res.ResourceID)
	}

	if got, want := len(byType[domain.ResourceTypeVPC]), 3; got != want {
		t.Errorf("VPCs = %v, want %d entries", byType[domain.ResourceTypeVPC], want)
	}
	if got, want := len(byType[domain.ResourceTypeSubnet]), 2; got != want {
		t.Errorf("subnets = %v, want %d entries", byType[domain.ResourceTypeSubnet], want)
	}
	if got, want := len(byType[domain.ResourceTypeElasticIP]), 1; got != want {
		t.Errorf("elastic IPs = %v, want %d entries", byType[domain.ResourceTypeElasticIP], want)
	}

	if client.vpcCalls != 3 {
		t.Errorf("DescribeVpcs calls = %d, want 3", client.vpcCalls)
	}
	if client.subnetCalls != 2 {
		t.Errorf("DescribeSubnets calls = %d, want 2", client.subnetCalls)
	}
	// DescribeAddresses is not paginated by EC2, so it must be called once.
	if client.addrCalls != 1 {
		t.Errorf("DescribeAddresses calls = %d, want 1", client.addrCalls)
	}
}

// repeatTokenEC2 always returns the same NextToken, mimicking a broken endpoint.
type repeatTokenEC2 struct {
	vpcCalls int
}

func (f *repeatTokenEC2) DescribeVpcs(_ context.Context, _ *ec2.DescribeVpcsInput, _ ...func(*ec2.Options)) (*ec2.DescribeVpcsOutput, error) {
	f.vpcCalls++
	if f.vpcCalls > 10 {
		return nil, fmt.Errorf("pagination did not terminate")
	}
	return &ec2.DescribeVpcsOutput{
		Vpcs:      []ec2types.Vpc{{VpcId: awssdk.String("vpc-loop"), CidrBlock: awssdk.String("10.0.0.0/16")}},
		NextToken: awssdk.String("stuck"),
	}, nil
}

func (f *repeatTokenEC2) DescribeSubnets(context.Context, *ec2.DescribeSubnetsInput, ...func(*ec2.Options)) (*ec2.DescribeSubnetsOutput, error) {
	return &ec2.DescribeSubnetsOutput{}, nil
}

func (f *repeatTokenEC2) DescribeAddresses(context.Context, *ec2.DescribeAddressesInput, ...func(*ec2.Options)) (*ec2.DescribeAddressesOutput, error) {
	return &ec2.DescribeAddressesOutput{}, nil
}

func TestDiscoverStopsOnRepeatedPageToken(t *testing.T) {
	client := &repeatTokenEC2{}
	collector := newTestCollector(map[string]ec2API{"us-east-1": client})

	resources, err := collector.Discover(context.Background(), domain.Account{ID: 7, Regions: []string{"us-east-1"}})
	if err != nil {
		t.Fatalf("Discover() unexpected error: %v", err)
	}
	if got, want := len(resources), 2; got != want {
		t.Fatalf("len(resources) = %d, want %d", got, want)
	}
	if client.vpcCalls != 2 {
		t.Errorf("DescribeVpcs calls = %d, want 2 (first page plus one repeat)", client.vpcCalls)
	}
}
