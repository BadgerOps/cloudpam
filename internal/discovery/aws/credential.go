package aws

import (
	"context"
	"errors"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
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
		newEC2Client:  c.newEC2Client,
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
	cfg, err := c.stsConfig(ctx, account)
	if err != nil {
		return "", err
	}
	out, err := c.stsClient(cfg).GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		return "", probeError(discovery.ProbeStepGetCallerIdentity, err)
	}
	return aws.ToString(out.Arn), nil
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
