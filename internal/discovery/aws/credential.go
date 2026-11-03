package aws

import (
	"context"
	"errors"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	smithy "github.com/aws/smithy-go"

	"cloudpam/internal/discovery"
	"cloudpam/internal/domain"
)

const defaultSTSRegion = "us-east-1"

type stsAPI interface {
	stscreds.AssumeRoleAPIClient
	GetCallerIdentity(context.Context, *sts.GetCallerIdentityInput, ...func(*sts.Options)) (*sts.GetCallerIdentityOutput, error)
}

var (
	_ discovery.CredentialAware       = (*Collector)(nil)
	_ discovery.IdentityProber        = (*Collector)(nil)
	_ discovery.CredentialedCollector = (*assumedRoleCollector)(nil)
)

// WithCredential binds the host collector to an assumed role.
func (c *Collector) WithCredential(ctx context.Context, account domain.Account, cred discovery.CredentialPayload) (discovery.CredentialedCollector, error) {
	role, ok := cred.(discovery.AWSAssumeRole)
	if !ok {
		return nil, fmt.Errorf("%w: %s credential on the aws collector", discovery.ErrCredentialProviderMismatch, cred.Type())
	}
	cfg, err := c.stsConfig(ctx, account)
	if err != nil {
		return nil, err
	}
	provider := newAssumeRoleProvider(c.stsClient(cfg), role.RoleARN(), role.ExternalID(), fmt.Sprintf("cloudpam-%d", account.ID))
	return &assumedRoleCollector{Collector: &Collector{
		credsProvider: aws.NewCredentialsCache(provider),
		loadConfig:    c.loadConfig,
		newEC2Client:  func(cfg aws.Config) ec2API { return stsRedactingEC2{client: c.ec2Client(cfg)} },
		newSTSClient:  c.newSTSClient,
	}}, nil
}

func (c *Collector) stsConfig(ctx context.Context, account domain.Account) (aws.Config, error) {
	region := ""
	if len(account.Regions) > 0 {
		region = account.Regions[0]
	}
	cfg, err := c.loadConfigForRegion(ctx, region)
	if err != nil {
		return aws.Config{}, fmt.Errorf("load aws config for sts: %w", err)
	}
	if cfg.Region == "" {
		cfg.Region = defaultSTSRegion
	}
	return cfg, nil
}

func (c *Collector) stsClient(cfg aws.Config) stsAPI {
	if c.newSTSClient != nil {
		return c.newSTSClient(cfg)
	}
	return sts.NewFromConfig(cfg)
}

type assumedRoleCollector struct{ *Collector }

func (c *assumedRoleCollector) Identity(ctx context.Context, account domain.Account) (string, error) {
	if _, err := c.credsProvider.Retrieve(ctx); err != nil {
		return "", probeError(discovery.ProbeStepAssumeRole, err)
	}
	return c.Collector.Identity(ctx, account)
}

// Identity returns the ARN that discovery runs as.
func (c *Collector) Identity(ctx context.Context, account domain.Account) (string, error) {
	cfg, err := c.stsConfig(ctx, account)
	if err != nil {
		return "", err
	}
	// The SDK names no sentinel for an empty credential chain, so resolve it first
	// to report that case apart from a rejected call.
	if cfg.Credentials != nil {
		if _, err := cfg.Credentials.Retrieve(ctx); err != nil && ctx.Err() == nil {
			return "", &discovery.ProbeError{Step: discovery.ProbeStepCredentials, Code: "unavailable"}
		}
	}
	out, err := c.stsClient(cfg).GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		return "", probeError(discovery.ProbeStepGetCallerIdentity, err)
	}
	return aws.ToString(out.Arn), nil
}

type stsRedactingEC2 struct{ client ec2API }

func (e stsRedactingEC2) DescribeVpcs(ctx context.Context, in *ec2.DescribeVpcsInput, opts ...func(*ec2.Options)) (*ec2.DescribeVpcsOutput, error) {
	out, err := e.client.DescribeVpcs(ctx, in, opts...)
	return out, redactSTSFailure(err)
}

func (e stsRedactingEC2) DescribeSubnets(ctx context.Context, in *ec2.DescribeSubnetsInput, opts ...func(*ec2.Options)) (*ec2.DescribeSubnetsOutput, error) {
	out, err := e.client.DescribeSubnets(ctx, in, opts...)
	return out, redactSTSFailure(err)
}

func (e stsRedactingEC2) DescribeAddresses(ctx context.Context, in *ec2.DescribeAddressesInput, opts ...func(*ec2.Options)) (*ec2.DescribeAddressesOutput, error) {
	out, err := e.client.DescribeAddresses(ctx, in, opts...)
	return out, redactSTSFailure(err)
}

func redactSTSFailure(err error) error {
	if op := stsOperationError(err); op != nil {
		return probeError(discovery.ProbeStepAssumeRole, op)
	}
	return err
}

// stsOperationError searches the whole tree because errors.As stops at the EC2
// OperationError that wraps the credential failure.
func stsOperationError(err error) *smithy.OperationError {
	if op, ok := err.(*smithy.OperationError); ok && op.ServiceID == sts.ServiceID {
		return op
	}
	switch e := err.(type) {
	case interface{ Unwrap() error }:
		return stsOperationError(e.Unwrap())
	case interface{ Unwrap() []error }:
		for _, inner := range e.Unwrap() {
			if op := stsOperationError(inner); op != nil {
				return op
			}
		}
	}
	return nil
}

func probeError(step discovery.ProbeStep, err error) error {
	pe := &discovery.ProbeError{Step: step}
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		pe.Code = apiErr.ErrorCode()
	}
	var respErr *awshttp.ResponseError
	if errors.As(err, &respErr) {
		pe.Status = respErr.HTTPStatusCode()
	}
	if pe.Code == "" && pe.Status == 0 {
		return fmt.Errorf("%s: %w", step, err)
	}
	return pe
}
