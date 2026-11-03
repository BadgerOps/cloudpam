package aws

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	ststypes "github.com/aws/aws-sdk-go-v2/service/sts/types"
	smithy "github.com/aws/smithy-go"
	smithyhttp "github.com/aws/smithy-go/transport/http"

	"cloudpam/internal/discovery"
	"cloudpam/internal/domain"
)

const (
	testRoleARN    = "arn:aws:iam::123456789012:role/CloudPAMDiscovery"
	testSessionARN = "arn:aws:sts::123456789012:assumed-role/CloudPAMDiscovery/cloudpam-9"
)

type fakeSTS struct {
	assumeIn    *sts.AssumeRoleInput
	assumeErr   error
	identityErr error
}

func (f *fakeSTS) AssumeRole(_ context.Context, in *sts.AssumeRoleInput, _ ...func(*sts.Options)) (*sts.AssumeRoleOutput, error) {
	f.assumeIn = in
	if f.assumeErr != nil {
		return nil, f.assumeErr
	}
	return &sts.AssumeRoleOutput{Credentials: &ststypes.Credentials{
		AccessKeyId:     awssdk.String("ASIAROLE"),
		SecretAccessKey: awssdk.String("role-secret"),
		SessionToken:    awssdk.String("role-token"),
		Expiration:      awssdk.Time(time.Now().Add(time.Hour)),
	}}, nil
}

func (f *fakeSTS) GetCallerIdentity(context.Context, *sts.GetCallerIdentityInput, ...func(*sts.Options)) (*sts.GetCallerIdentityOutput, error) {
	if f.identityErr != nil {
		return nil, f.identityErr
	}
	return &sts.GetCallerIdentityOutput{Arn: awssdk.String(testSessionARN)}, nil
}

type loadCall struct {
	region string
	creds  awssdk.CredentialsProvider
}

type credentialHarness struct {
	collector *Collector
	sts       *fakeSTS
	ec2       *fakeEC2
	loads     []loadCall
	stsCfgs   []awssdk.Config
}

func newCredentialHarness(t *testing.T, hostRegion string) *credentialHarness {
	t.Helper()
	h := &credentialHarness{sts: &fakeSTS{}, ec2: &fakeEC2{}}
	h.collector = &Collector{
		loadConfig: func(_ context.Context, region string, cp awssdk.CredentialsProvider) (awssdk.Config, error) {
			h.loads = append(h.loads, loadCall{region: region, creds: cp})
			if region == "" {
				region = hostRegion
			}
			return awssdk.Config{Region: region, Credentials: cp}, nil
		},
		newEC2Client: func(awssdk.Config) ec2API { return h.ec2 },
		newSTSClient: func(cfg awssdk.Config) stsAPI {
			h.stsCfgs = append(h.stsCfgs, cfg)
			return h.sts
		},
	}
	return h
}

func testRole(t *testing.T, externalID string) discovery.AWSAssumeRole {
	t.Helper()
	role, err := discovery.NewAWSAssumeRole(testRoleARN, externalID)
	if err != nil {
		t.Fatal(err)
	}
	return role
}

func TestWithCredentialBindsAnAssumedRoleThroughTheSeams(t *testing.T) {
	h := newCredentialHarness(t, "us-west-2")
	ctx := context.Background()
	account := domain.Account{ID: 9, Provider: "aws", Regions: []string{"eu-west-1", "us-east-2"}}

	bound, err := h.collector.WithCredential(ctx, account, testRole(t, "ext-1"))
	if err != nil {
		t.Fatalf("WithCredential() error = %v", err)
	}
	if len(h.loads) != 1 || h.loads[0].region != "eu-west-1" || h.loads[0].creds != nil {
		t.Fatalf("STS config loads = %+v, want one host-identity load in the first account region", h.loads)
	}
	if len(h.stsCfgs) != 1 || h.stsCfgs[0].Region != "eu-west-1" {
		t.Fatalf("STS client configs = %+v", h.stsCfgs)
	}
	if h.sts.assumeIn != nil {
		t.Fatal("WithCredential called AssumeRole; it must be lazy")
	}

	identity, err := bound.Identity(ctx, account)
	if err != nil {
		t.Fatalf("Identity() error = %v", err)
	}
	if identity != testSessionARN {
		t.Fatalf("Identity() = %q, want %q", identity, testSessionARN)
	}
	in := h.sts.assumeIn
	if in == nil || awssdk.ToString(in.RoleArn) != testRoleARN || awssdk.ToString(in.ExternalId) != "ext-1" || awssdk.ToString(in.RoleSessionName) != "cloudpam-9" {
		t.Fatalf("AssumeRole input = %+v", in)
	}
	if len(h.loads) != 2 || h.loads[1].creds == nil {
		t.Fatalf("GetCallerIdentity config loads = %+v, want a second load carrying the assumed-role credentials", h.loads)
	}
	creds, err := h.loads[1].creds.Retrieve(ctx)
	if err != nil || creds.AccessKeyID != "ASIAROLE" {
		t.Fatalf("assumed credentials = %+v, %v", creds, err)
	}

	h.ec2.vpcs = append(h.ec2.vpcs, ec2types.Vpc{VpcId: awssdk.String("vpc-1"), CidrBlock: awssdk.String("10.0.0.0/16"), State: ec2types.VpcStateAvailable})
	resources, err := bound.Discover(ctx, account)
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	if len(resources) != 2 {
		t.Fatalf("Discover() returned %d resources, want one VPC per region", len(resources))
	}
	for _, call := range h.loads[2:] {
		if call.creds == nil {
			t.Fatalf("Discover loaded config without the assumed-role credentials: %+v", h.loads)
		}
	}
	if bound.Provider() != "aws" {
		t.Fatalf("Provider() = %q", bound.Provider())
	}
}

func TestWithCredentialOmitsExternalIDAndFallsBackToDefaultRegion(t *testing.T) {
	h := newCredentialHarness(t, "")
	ctx := context.Background()
	account := domain.Account{ID: 3, Provider: "aws"}

	bound, err := h.collector.WithCredential(ctx, account, testRole(t, ""))
	if err != nil {
		t.Fatalf("WithCredential() error = %v", err)
	}
	if h.stsCfgs[0].Region != defaultSTSRegion {
		t.Fatalf("STS region = %q, want %q when the account and host name none", h.stsCfgs[0].Region, defaultSTSRegion)
	}
	if _, err := bound.Identity(ctx, account); err != nil {
		t.Fatalf("Identity() error = %v", err)
	}
	if h.sts.assumeIn.ExternalId != nil {
		t.Fatalf("ExternalId = %q, want nil", awssdk.ToString(h.sts.assumeIn.ExternalId))
	}
}

func TestWithCredentialRejectsAForeignPayload(t *testing.T) {
	h := newCredentialHarness(t, "us-east-1")
	_, err := h.collector.WithCredential(context.Background(), domain.Account{Provider: "aws"}, testGCPPayload(t))
	if !errors.Is(err, discovery.ErrCredentialProviderMismatch) {
		t.Fatalf("WithCredential() error = %v, want ErrCredentialProviderMismatch", err)
	}
	if len(h.loads) != 0 {
		t.Fatal("WithCredential loaded config before checking the payload type")
	}
}

func stsAPIError(status int, code string) error {
	return &smithy.OperationError{ServiceID: "STS", OperationName: "AssumeRole", Err: &awshttp.ResponseError{
		ResponseError: &smithyhttp.ResponseError{
			Response: &smithyhttp.Response{Response: &http.Response{StatusCode: status}},
			Err:      &smithy.GenericAPIError{Code: code, Message: "User is not authorized; ExternalId ext-1 did not match"},
		},
		RequestID: "req-1",
	}}
}

func TestIdentityReducesProviderFailuresToProbeErrors(t *testing.T) {
	cases := []struct {
		name     string
		arrange  func(f *fakeSTS)
		wantStep discovery.ProbeStep
		wantCode string
		wantHTTP int
	}{
		{"assume role denied", func(f *fakeSTS) { f.assumeErr = stsAPIError(403, "AccessDenied") }, "assume_role", "AccessDenied", 403},
		{"assume role api error without http", func(f *fakeSTS) { f.assumeErr = &smithy.GenericAPIError{Code: "ExpiredToken"} }, "assume_role", "ExpiredToken", 0},
		{"caller identity failure", func(f *fakeSTS) { f.identityErr = stsAPIError(400, "InvalidClientTokenId") }, "get_caller_identity", "InvalidClientTokenId", 400},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newCredentialHarness(t, "us-east-1")
			tc.arrange(h.sts)
			bound, _ := h.collector.WithCredential(context.Background(), domain.Account{ID: 1}, testRole(t, "ext-1"))
			identity, err := bound.Identity(context.Background(), domain.Account{ID: 1})
			if identity != "" {
				t.Fatalf("Identity() = %q, want empty on failure", identity)
			}
			var pe *discovery.ProbeError
			if !errors.As(err, &pe) {
				t.Fatalf("Identity() error = %v, want *ProbeError", err)
			}
			if pe.Step != tc.wantStep || pe.Code != tc.wantCode || pe.Status != tc.wantHTTP {
				t.Fatalf("ProbeError = %+v, want step %s code %s http %d", *pe, tc.wantStep, tc.wantCode, tc.wantHTTP)
			}
			if strings.Contains(err.Error(), "ext-1") || strings.Contains(err.Error(), "not authorized") {
				t.Fatalf("error leaks the provider message: %v", err)
			}
		})
	}
}

func TestIdentityPassesNetworkFailuresThroughForDiagnosis(t *testing.T) {
	h := newCredentialHarness(t, "us-east-1")
	h.sts.assumeErr = &smithy.OperationError{ServiceID: "STS", OperationName: "AssumeRole", Err: &net.OpError{Op: "dial", Err: errors.New("connection refused")}}
	bound, _ := h.collector.WithCredential(context.Background(), domain.Account{ID: 1}, testRole(t, ""))
	_, err := bound.Identity(context.Background(), domain.Account{ID: 1})
	var pe *discovery.ProbeError
	if errors.As(err, &pe) {
		t.Fatalf("network failure became a ProbeError: %+v", *pe)
	}
	var netErr net.Error
	if !errors.As(err, &netErr) {
		t.Fatalf("Identity() error = %v, want the net.Error preserved", err)
	}
	if !strings.HasPrefix(err.Error(), "assume_role: ") {
		t.Fatalf("Identity() error = %q, want the step prefix", err.Error())
	}
}

func TestDiscoverReducesAFailedAssumeRoleToAProbeError(t *testing.T) {
	realEC2 := func(cfg awssdk.Config) ec2API { return ec2.NewFromConfig(cfg) }
	cases := []struct {
		name string
		ec2  func(awssdk.Config) ec2API
	}{
		{"in every region", realEC2},
		{"after another region failed in EC2", func(cfg awssdk.Config) ec2API {
			if cfg.Region == "us-east-1" {
				return &fakeEC2{vpcErr: &smithy.OperationError{ServiceID: "EC2", OperationName: "DescribeVpcs", Err: &smithy.GenericAPIError{Code: "AuthFailure"}}}
			}
			return realEC2(cfg)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newCredentialHarness(t, "us-east-1")
			h.sts.assumeErr = stsAPIError(403, "AccessDenied")
			h.collector.newEC2Client = tc.ec2
			account := domain.Account{ID: 1, Provider: "aws", Regions: []string{"us-east-1", "eu-west-1"}}
			bound, err := h.collector.WithCredential(context.Background(), account, testRole(t, "ext-1"))
			if err != nil {
				t.Fatal(err)
			}

			_, err = bound.Discover(context.Background(), account)
			var pe *discovery.ProbeError
			if !errors.As(err, &pe) || pe.Step != discovery.ProbeStepAssumeRole || pe.Code != "AccessDenied" || pe.Status != 403 {
				t.Fatalf("Discover() error = %v, want an assume_role ProbeError", err)
			}
			if !strings.HasPrefix(err.Error(), "assume_role: ") || strings.Contains(err.Error(), "ext-1") || strings.Contains(err.Error(), "not authorized") {
				t.Fatalf("Discover() error = %q, want the step without the STS message", err.Error())
			}
		})
	}
}

func TestAssumeRoleKeepsTheAgentContract(t *testing.T) {
	covIsolateAWSEnv(t)
	t.Setenv("AWS_REGION", "us-east-1")
	var provider awssdk.CredentialsProvider
	provider, err := AssumeRole(context.Background(), "123456789012", "CloudPAMDiscoveryRole", "ext")
	if err != nil || provider == nil {
		t.Fatalf("AssumeRole() = %v, %v", provider, err)
	}
}

func testGCPPayload(t *testing.T) discovery.GCPServiceAccount {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatal(err)
	}
	der, _ := x509.MarshalPKCS8PrivateKey(key)
	b, _ := json.Marshal(map[string]string{
		"type":         "service_account",
		"project_id":   "proj",
		"client_email": "sa@proj.iam.gserviceaccount.com",
		"private_key":  string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})),
	})
	sa, err := discovery.ParseGCPServiceAccountKey(b)
	if err != nil {
		t.Fatal(err)
	}
	return sa
}
