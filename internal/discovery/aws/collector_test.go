package aws

import (
	"context"
	"errors"
	"strings"
	"testing"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"

	"cloudpam/internal/discovery"
	"cloudpam/internal/domain"
)

// Verify Collector implements the discovery.Collector interface at compile time.
var _ discovery.Collector = (*Collector)(nil)

func TestProvider(t *testing.T) {
	c := New()
	if got := c.Provider(); got != "aws" {
		t.Errorf("Provider() = %q, want %q", got, "aws")
	}
}

func TestDiscoverReturnsResourcesWhenAllConfiguredRegionsSucceed(t *testing.T) {
	collector := newTestCollector(map[string]ec2API{
		"us-east-1": &fakeEC2{
			vpcs: []ec2types.Vpc{{
				VpcId:     awssdk.String("vpc-east"),
				CidrBlock: awssdk.String("10.0.0.0/16"),
				State:     ec2types.VpcStateAvailable,
			}},
		},
		"us-west-2": &fakeEC2{
			subnets: []ec2types.Subnet{{
				SubnetId:         awssdk.String("subnet-west"),
				VpcId:            awssdk.String("vpc-west"),
				CidrBlock:        awssdk.String("10.1.1.0/24"),
				AvailabilityZone: awssdk.String("us-west-2a"),
				State:            ec2types.SubnetStateAvailable,
			}},
		},
	})

	resources, err := collector.Discover(context.Background(), domain.Account{
		ID:      42,
		Regions: []string{"us-east-1", "us-west-2"},
	})
	if err != nil {
		t.Fatalf("Discover() unexpected error: %v", err)
	}
	if got, want := len(resources), 2; got != want {
		t.Fatalf("len(resources) = %d, want %d", got, want)
	}
	for _, res := range resources {
		if res.AccountID != 42 {
			t.Errorf("resource AccountID = %d, want 42", res.AccountID)
		}
		if res.Status != domain.DiscoveryStatusActive {
			t.Errorf("resource Status = %q, want %q", res.Status, domain.DiscoveryStatusActive)
		}
	}
}

func TestDiscoverFailsInsteadOfReturningPartialInventoryWhenRegionConfigFails(t *testing.T) {
	collector := newTestCollector(map[string]ec2API{
		"us-east-1": &fakeEC2{
			vpcs: []ec2types.Vpc{{
				VpcId:     awssdk.String("vpc-east"),
				CidrBlock: awssdk.String("10.0.0.0/16"),
				State:     ec2types.VpcStateAvailable,
			}},
		},
	})
	collector.loadConfig = func(_ context.Context, region string, _ awssdk.CredentialsProvider) (awssdk.Config, error) {
		if region == "us-west-2" {
			return awssdk.Config{}, errors.New("region disabled")
		}
		return awssdk.Config{Region: region}, nil
	}

	resources, err := collector.Discover(context.Background(), domain.Account{
		ID:      42,
		Regions: []string{"us-east-1", "us-west-2"},
	})
	if err == nil {
		t.Fatal("Discover() error = nil, want partial discovery error")
	}
	if resources != nil {
		t.Fatalf("Discover() returned %d partial resources, want nil", len(resources))
	}
	if got := err.Error(); !strings.Contains(got, "incomplete AWS discovery") || !strings.Contains(got, "load config for region us-west-2") {
		t.Fatalf("Discover() error = %q, want incomplete discovery config failure", got)
	}
}

func TestDiscoverFailsInsteadOfReturningPartialInventoryWhenResourceTypeFails(t *testing.T) {
	collector := newTestCollector(map[string]ec2API{
		"us-east-1": &fakeEC2{
			vpcs: []ec2types.Vpc{{
				VpcId:     awssdk.String("vpc-east"),
				CidrBlock: awssdk.String("10.0.0.0/16"),
				State:     ec2types.VpcStateAvailable,
			}},
			subnetErr: errors.New("ec2:DescribeSubnets denied"),
		},
	})

	resources, err := collector.Discover(context.Background(), domain.Account{
		ID:      42,
		Regions: []string{"us-east-1"},
	})
	if err == nil {
		t.Fatal("Discover() error = nil, want partial discovery error")
	}
	if resources != nil {
		t.Fatalf("Discover() returned %d partial resources, want nil", len(resources))
	}
	if got := err.Error(); !strings.Contains(got, "incomplete AWS discovery") || !strings.Contains(got, "discover subnets in region us-east-1") {
		t.Fatalf("Discover() error = %q, want incomplete discovery subnet failure", got)
	}
}

func newTestCollector(clients map[string]ec2API) *Collector {
	return &Collector{
		loadConfig: func(_ context.Context, region string, _ awssdk.CredentialsProvider) (awssdk.Config, error) {
			return awssdk.Config{Region: region}, nil
		},
		newEC2Client: func(cfg awssdk.Config) ec2API {
			client, ok := clients[cfg.Region]
			if !ok {
				return &fakeEC2{}
			}
			return client
		},
	}
}

type fakeEC2 struct {
	vpcs      []ec2types.Vpc
	vpcErr    error
	subnets   []ec2types.Subnet
	subnetErr error
	addresses []ec2types.Address
	addrErr   error
}

func (f *fakeEC2) DescribeVpcs(context.Context, *ec2.DescribeVpcsInput, ...func(*ec2.Options)) (*ec2.DescribeVpcsOutput, error) {
	if f.vpcErr != nil {
		return nil, f.vpcErr
	}
	return &ec2.DescribeVpcsOutput{Vpcs: f.vpcs}, nil
}

func (f *fakeEC2) DescribeSubnets(context.Context, *ec2.DescribeSubnetsInput, ...func(*ec2.Options)) (*ec2.DescribeSubnetsOutput, error) {
	if f.subnetErr != nil {
		return nil, f.subnetErr
	}
	return &ec2.DescribeSubnetsOutput{Subnets: f.subnets}, nil
}

func (f *fakeEC2) DescribeAddresses(context.Context, *ec2.DescribeAddressesInput, ...func(*ec2.Options)) (*ec2.DescribeAddressesOutput, error) {
	if f.addrErr != nil {
		return nil, f.addrErr
	}
	return &ec2.DescribeAddressesOutput{Addresses: f.addresses}, nil
}
