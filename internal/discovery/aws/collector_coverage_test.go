package aws

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"

	"cloudpam/internal/domain"
)

// covIsolateAWSEnv points the SDK at empty config files and disables IMDS so
// config loading never touches the developer's real AWS setup or the network.
func covIsolateAWSEnv(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("AWS_CONFIG_FILE", filepath.Join(dir, "config"))
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", filepath.Join(dir, "credentials"))
	t.Setenv("AWS_EC2_METADATA_DISABLED", "true")
	t.Setenv("AWS_PROFILE", "")
	t.Setenv("AWS_REGION", "")
	t.Setenv("AWS_DEFAULT_REGION", "")
	t.Setenv("AWS_ACCESS_KEY_ID", "test-access-key")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "test-secret-key")
	t.Setenv("AWS_SESSION_TOKEN", "")
}

func TestCovExtractTagName(t *testing.T) {
	tests := []struct {
		name string
		tags []ec2types.Tag
		want string
	}{
		{"nil tags", nil, ""},
		{"empty tags", []ec2types.Tag{}, ""},
		{
			"name tag present",
			[]ec2types.Tag{{Key: awssdk.String("Name"), Value: awssdk.String("prod-vpc")}},
			"prod-vpc",
		},
		{
			"name tag after other tags",
			[]ec2types.Tag{
				{Key: awssdk.String("env"), Value: awssdk.String("prod")},
				{Key: awssdk.String("Name"), Value: awssdk.String("core")},
			},
			"core",
		},
		{
			"no name tag",
			[]ec2types.Tag{{Key: awssdk.String("env"), Value: awssdk.String("prod")}},
			"",
		},
		{
			"name tag is case sensitive",
			[]ec2types.Tag{{Key: awssdk.String("name"), Value: awssdk.String("lower")}},
			"",
		},
		{
			"name tag with nil value",
			[]ec2types.Tag{{Key: awssdk.String("Name")}},
			"",
		},
		{
			"tag with nil key",
			[]ec2types.Tag{{Value: awssdk.String("orphan")}, {Key: awssdk.String("Name"), Value: awssdk.String("ok")}},
			"ok",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := extractTagName(tc.tags); got != tc.want {
				t.Errorf("extractTagName() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestCovDisplayRegion(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"", "default"},
		{"   ", "default"},
		{"\t\n", "default"},
		{"us-east-1", "us-east-1"},
	}
	for _, tc := range tests {
		if got := displayRegion(tc.in); got != tc.want {
			t.Errorf("displayRegion(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestCovDerefString(t *testing.T) {
	if got := derefString(nil); got != "" {
		t.Errorf("derefString(nil) = %q, want empty", got)
	}
	if got := derefString(awssdk.String("value")); got != "value" {
		t.Errorf("derefString() = %q, want value", got)
	}
}

func TestCovDiscoverMapsVPCMetadata(t *testing.T) {
	collector := newTestCollector(map[string]ec2API{
		"us-east-1": &fakeEC2{
			vpcs: []ec2types.Vpc{
				{
					VpcId:     awssdk.String("vpc-default"),
					CidrBlock: awssdk.String("172.31.0.0/16"),
					State:     ec2types.VpcStateAvailable,
					IsDefault: awssdk.Bool(true),
					Tags:      []ec2types.Tag{{Key: awssdk.String("Name"), Value: awssdk.String("default-vpc")}},
				},
				{
					VpcId:     awssdk.String("vpc-custom"),
					CidrBlock: awssdk.String("10.0.0.0/16"),
					State:     ec2types.VpcStatePending,
					IsDefault: awssdk.Bool(false),
				},
			},
		},
	})

	resources, err := collector.Discover(context.Background(), domain.Account{ID: 3, Regions: []string{"us-east-1"}})
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	if len(resources) != 2 {
		t.Fatalf("resources = %d, want 2", len(resources))
	}

	byID := map[string]domain.DiscoveredResource{}
	for _, r := range resources {
		byID[r.ResourceID] = r
	}

	def := byID["vpc-default"]
	if def.Name != "default-vpc" {
		t.Errorf("default vpc Name = %q, want default-vpc", def.Name)
	}
	if def.CIDR != "172.31.0.0/16" {
		t.Errorf("default vpc CIDR = %q", def.CIDR)
	}
	if def.Region != "us-east-1" {
		t.Errorf("default vpc Region = %q", def.Region)
	}
	if def.Metadata["state"] != string(ec2types.VpcStateAvailable) {
		t.Errorf("default vpc state metadata = %q", def.Metadata["state"])
	}
	if def.Metadata["is_default"] != "true" {
		t.Errorf("is_default metadata = %q, want true", def.Metadata["is_default"])
	}

	custom := byID["vpc-custom"]
	if custom.Name != "" {
		t.Errorf("custom vpc Name = %q, want empty when untagged", custom.Name)
	}
	if _, ok := custom.Metadata["is_default"]; ok {
		t.Errorf("is_default metadata present for a non-default VPC: %v", custom.Metadata)
	}
	if custom.Metadata["state"] != string(ec2types.VpcStatePending) {
		t.Errorf("custom vpc state metadata = %q", custom.Metadata["state"])
	}
}

func TestCovDiscoverMapsSubnetMetadata(t *testing.T) {
	collector := newTestCollector(map[string]ec2API{
		"eu-west-1": &fakeEC2{
			subnets: []ec2types.Subnet{
				{
					SubnetId:                awssdk.String("subnet-with-count"),
					VpcId:                   awssdk.String("vpc-1"),
					CidrBlock:               awssdk.String("10.0.1.0/24"),
					AvailabilityZone:        awssdk.String("eu-west-1a"),
					State:                   ec2types.SubnetStateAvailable,
					AvailableIpAddressCount: awssdk.Int32(251),
					Tags:                    []ec2types.Tag{{Key: awssdk.String("Name"), Value: awssdk.String("app-a")}},
				},
				{
					SubnetId:  awssdk.String("subnet-no-count"),
					VpcId:     awssdk.String("vpc-1"),
					CidrBlock: awssdk.String("10.0.2.0/24"),
				},
			},
		},
	})

	resources, err := collector.Discover(context.Background(), domain.Account{ID: 8, Regions: []string{"eu-west-1"}})
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}

	byID := map[string]domain.DiscoveredResource{}
	for _, r := range resources {
		byID[r.ResourceID] = r
	}

	withCount := byID["subnet-with-count"]
	if withCount.ResourceType != domain.ResourceTypeSubnet {
		t.Errorf("ResourceType = %q, want subnet", withCount.ResourceType)
	}
	if withCount.ParentResourceID == nil || *withCount.ParentResourceID != "vpc-1" {
		t.Errorf("ParentResourceID = %v, want vpc-1", withCount.ParentResourceID)
	}
	if withCount.Name != "app-a" {
		t.Errorf("Name = %q, want app-a", withCount.Name)
	}
	if withCount.Metadata["availability_zone"] != "eu-west-1a" {
		t.Errorf("availability_zone = %q", withCount.Metadata["availability_zone"])
	}
	if withCount.Metadata["available_ips"] != "251" {
		t.Errorf("available_ips = %q, want 251", withCount.Metadata["available_ips"])
	}

	noCount := byID["subnet-no-count"]
	if _, ok := noCount.Metadata["available_ips"]; ok {
		t.Errorf("available_ips present when the API omitted the count: %v", noCount.Metadata)
	}
	if noCount.Metadata["availability_zone"] != "" {
		t.Errorf("availability_zone = %q, want empty", noCount.Metadata["availability_zone"])
	}
}

func TestCovDiscoverMapsElasticIPMetadata(t *testing.T) {
	collector := newTestCollector(map[string]ec2API{
		"us-east-1": &fakeEC2{
			addresses: []ec2types.Address{
				{
					AllocationId:  awssdk.String("eipalloc-attached"),
					PublicIp:      awssdk.String("203.0.113.7"),
					Domain:        ec2types.DomainTypeVpc,
					InstanceId:    awssdk.String("i-123"),
					AssociationId: awssdk.String("eipassoc-1"),
					Tags:          []ec2types.Tag{{Key: awssdk.String("Name"), Value: awssdk.String("nat-eip")}},
				},
				{
					AllocationId: awssdk.String("eipalloc-unattached"),
					Domain:       ec2types.DomainTypeStandard,
				},
			},
		},
	})

	resources, err := collector.Discover(context.Background(), domain.Account{ID: 5, Regions: []string{"us-east-1"}})
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	if len(resources) != 2 {
		t.Fatalf("resources = %d, want 2", len(resources))
	}

	byID := map[string]domain.DiscoveredResource{}
	for _, r := range resources {
		byID[r.ResourceID] = r
	}

	attached := byID["eipalloc-attached"]
	if attached.ResourceType != domain.ResourceTypeElasticIP {
		t.Errorf("ResourceType = %q, want elastic ip", attached.ResourceType)
	}
	if attached.CIDR != "203.0.113.7/32" {
		t.Errorf("CIDR = %q, want the public IP as a /32", attached.CIDR)
	}
	if attached.Name != "nat-eip" {
		t.Errorf("Name = %q", attached.Name)
	}
	if attached.Metadata["domain"] != string(ec2types.DomainTypeVpc) {
		t.Errorf("domain = %q", attached.Metadata["domain"])
	}
	if attached.Metadata["instance_id"] != "i-123" {
		t.Errorf("instance_id = %q", attached.Metadata["instance_id"])
	}
	if attached.Metadata["association_id"] != "eipassoc-1" {
		t.Errorf("association_id = %q", attached.Metadata["association_id"])
	}

	unattached := byID["eipalloc-unattached"]
	if unattached.CIDR != "" {
		t.Errorf("CIDR = %q, want empty when there is no public IP", unattached.CIDR)
	}
	if _, ok := unattached.Metadata["instance_id"]; ok {
		t.Errorf("instance_id present for an unattached EIP: %v", unattached.Metadata)
	}
	if _, ok := unattached.Metadata["association_id"]; ok {
		t.Errorf("association_id present for an unassociated EIP: %v", unattached.Metadata)
	}
}

func TestCovDiscoverReportsPerCallFailures(t *testing.T) {
	tests := []struct {
		name    string
		client  *fakeEC2
		wantErr string
	}{
		{
			"vpc failure",
			&fakeEC2{vpcErr: errors.New("ec2:DescribeVpcs denied")},
			"discover VPCs in region us-east-1",
		},
		{
			"subnet failure",
			&fakeEC2{subnetErr: errors.New("ec2:DescribeSubnets denied")},
			"discover subnets in region us-east-1",
		},
		{
			"elastic ip failure",
			&fakeEC2{addrErr: errors.New("ec2:DescribeAddresses denied")},
			"discover Elastic IPs in region us-east-1",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			collector := newTestCollector(map[string]ec2API{"us-east-1": tc.client})
			resources, err := collector.Discover(context.Background(), domain.Account{ID: 1, Regions: []string{"us-east-1"}})
			if err == nil {
				t.Fatal("Discover() error = nil, want failure")
			}
			if resources != nil {
				t.Fatalf("resources = %d, want nil on failure", len(resources))
			}
			if !strings.Contains(err.Error(), "incomplete AWS discovery") {
				t.Errorf("error = %q, want incomplete AWS discovery", err.Error())
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error = %q, want %q", err.Error(), tc.wantErr)
			}
		})
	}
}

func TestCovDiscoverAggregatesFailuresAcrossRegions(t *testing.T) {
	collector := newTestCollector(map[string]ec2API{
		"us-east-1": &fakeEC2{vpcErr: errors.New("east denied")},
		"us-west-2": &fakeEC2{subnetErr: errors.New("west denied")},
	})

	_, err := collector.Discover(context.Background(), domain.Account{ID: 1, Regions: []string{"us-east-1", "us-west-2"}})
	if err == nil {
		t.Fatal("Discover() error = nil, want failure")
	}
	if !strings.Contains(err.Error(), "discover VPCs in region us-east-1") {
		t.Errorf("error = %q, want the us-east-1 failure", err.Error())
	}
	if !strings.Contains(err.Error(), "discover subnets in region us-west-2") {
		t.Errorf("error = %q, want the us-west-2 failure to also be reported", err.Error())
	}
}

func TestCovDiscoverWithoutRegionsUsesDefaultConfigRegion(t *testing.T) {
	client := &fakeEC2{
		vpcs: []ec2types.Vpc{{VpcId: awssdk.String("vpc-default-region"), CidrBlock: awssdk.String("10.9.0.0/16")}},
	}
	collector := &Collector{
		loadConfig: func(_ context.Context, region string, _ awssdk.CredentialsProvider) (awssdk.Config, error) {
			if region != "" {
				t.Errorf("loadConfig region = %q, want empty when the account lists no regions", region)
			}
			return awssdk.Config{}, nil
		},
		newEC2Client: func(awssdk.Config) ec2API { return client },
	}

	resources, err := collector.Discover(context.Background(), domain.Account{ID: 2})
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	if len(resources) != 1 {
		t.Fatalf("resources = %d, want 1", len(resources))
	}
	if resources[0].Region != "" {
		t.Errorf("Region = %q, want empty when neither the account nor the config names a region", resources[0].Region)
	}
}

func TestCovDiscoverWithoutRegionsReportsDefaultRegionInErrors(t *testing.T) {
	collector := &Collector{
		loadConfig: func(context.Context, string, awssdk.CredentialsProvider) (awssdk.Config, error) {
			return awssdk.Config{}, errors.New("no region configured")
		},
	}

	_, err := collector.Discover(context.Background(), domain.Account{ID: 2})
	if err == nil {
		t.Fatal("Discover() error = nil, want failure")
	}
	if !strings.Contains(err.Error(), "load config for region default") {
		t.Errorf("error = %q, want the empty region rendered as \"default\"", err.Error())
	}
}

func TestCovDiscoverPrefersConfigRegionOverRequestedRegion(t *testing.T) {
	client := &fakeEC2{vpcs: []ec2types.Vpc{{VpcId: awssdk.String("vpc-1"), CidrBlock: awssdk.String("10.0.0.0/16")}}}
	collector := &Collector{
		loadConfig: func(context.Context, string, awssdk.CredentialsProvider) (awssdk.Config, error) {
			return awssdk.Config{Region: "resolved-region"}, nil
		},
		newEC2Client: func(awssdk.Config) ec2API { return client },
	}

	resources, err := collector.Discover(context.Background(), domain.Account{ID: 1, Regions: []string{"requested-region"}})
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	if resources[0].Region != "resolved-region" {
		t.Errorf("Region = %q, want the region resolved by the SDK config", resources[0].Region)
	}
}

func TestCovNewWithCredentialsStoresProvider(t *testing.T) {
	creds := credentials.NewStaticCredentialsProvider("AKID", "SECRET", "TOKEN")
	collector := NewWithCredentials(creds)
	if collector.credsProvider == nil {
		t.Fatal("credsProvider = nil, want the supplied provider")
	}

	var seen awssdk.CredentialsProvider
	collector.loadConfig = func(_ context.Context, _ string, cp awssdk.CredentialsProvider) (awssdk.Config, error) {
		seen = cp
		return awssdk.Config{Region: "us-east-1"}, nil
	}
	collector.newEC2Client = func(awssdk.Config) ec2API { return &fakeEC2{} }

	if _, err := collector.Discover(context.Background(), domain.Account{ID: 1, Regions: []string{"us-east-1"}}); err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	if seen == nil {
		t.Fatal("credentials provider was not passed through to config loading")
	}
	got, err := seen.Retrieve(context.Background())
	if err != nil {
		t.Fatalf("Retrieve() error = %v", err)
	}
	if got.AccessKeyID != "AKID" || got.SecretAccessKey != "SECRET" || got.SessionToken != "TOKEN" {
		t.Fatalf("forwarded credentials = %+v, want the static test credentials", got)
	}
}

func TestCovNewUsesDefaultCredentialChain(t *testing.T) {
	if c := New(); c.credsProvider != nil {
		t.Fatal("credsProvider != nil, want the default credential chain")
	}
}

func TestCovEC2ClientBuildsRealClientByDefault(t *testing.T) {
	client := (&Collector{}).ec2Client(awssdk.Config{Region: "us-east-1"})
	if client == nil {
		t.Fatal("ec2Client() = nil, want a real EC2 client")
	}
	if _, ok := client.(*ec2.Client); !ok {
		t.Fatalf("ec2Client() = %T, want *ec2.Client", client)
	}
}

func TestCovLoadConfigForRegionUsesDefaultLoaderWhenUnset(t *testing.T) {
	covIsolateAWSEnv(t)

	cfg, err := New().loadConfigForRegion(context.Background(), "eu-central-1")
	if err != nil {
		t.Fatalf("loadConfigForRegion() error = %v", err)
	}
	if cfg.Region != "eu-central-1" {
		t.Fatalf("cfg.Region = %q, want eu-central-1", cfg.Region)
	}
}

func TestCovLoadConfigForRegionInjectsCredentials(t *testing.T) {
	covIsolateAWSEnv(t)

	creds := credentials.NewStaticCredentialsProvider("INJECTED", "INJECTED-SECRET", "")
	cfg, err := NewWithCredentials(creds).loadConfigForRegion(context.Background(), "us-west-1")
	if err != nil {
		t.Fatalf("loadConfigForRegion() error = %v", err)
	}
	if cfg.Region != "us-west-1" {
		t.Errorf("cfg.Region = %q, want us-west-1", cfg.Region)
	}
	got, err := cfg.Credentials.Retrieve(context.Background())
	if err != nil {
		t.Fatalf("Retrieve() error = %v", err)
	}
	if got.AccessKeyID != "INJECTED" {
		t.Fatalf("AccessKeyID = %q, want the injected credentials to win over the environment", got.AccessKeyID)
	}
}

func TestCovLoadConfigForRegionHonoursEmptyRegion(t *testing.T) {
	covIsolateAWSEnv(t)
	t.Setenv("AWS_REGION", "ap-southeast-2")

	cfg, err := New().loadConfigForRegion(context.Background(), "")
	if err != nil {
		t.Fatalf("loadConfigForRegion() error = %v", err)
	}
	if cfg.Region != "ap-southeast-2" {
		t.Fatalf("cfg.Region = %q, want the environment region when no region is requested", cfg.Region)
	}
}

func TestCovAssumeRoleBuildsProviderWithoutCallingSTS(t *testing.T) {
	covIsolateAWSEnv(t)
	t.Setenv("AWS_REGION", "us-east-1")

	provider, err := AssumeRole(context.Background(), "123456789012", "CloudPAMDiscoveryRole", "")
	if err != nil {
		t.Fatalf("AssumeRole() error = %v", err)
	}
	if provider == nil {
		t.Fatal("provider = nil, want an assume-role credentials provider")
	}

	withExternalID, err := AssumeRole(context.Background(), "123456789012", "CloudPAMDiscoveryRole", "ext-1")
	if err != nil {
		t.Fatalf("AssumeRole() with external id error = %v", err)
	}
	if withExternalID == nil {
		t.Fatal("provider = nil for the external-id variant")
	}
}
