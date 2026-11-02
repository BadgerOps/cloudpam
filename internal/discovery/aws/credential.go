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

// defaultSTSRegion is used when neither the account nor the host config names a region.
const defaultSTSRegion = "us-east-1"

type stsAPI interface {
	stscreds.AssumeRoleAPIClient
	GetCallerIdentity(context.Context, *sts.GetCallerIdentityInput, ...func(*sts.Options)) (*sts.GetCallerIdentityOutput, error)
}

var (
	_ discovery.CredentialAware       = (*Collector)(nil)
	_ discovery.CredentialedCollector = (*assumedRoleCollector)(nil)
)

// WithCredential binds the host collector to an assumed role. The STS call runs as the host
// identity in the first account region, else the host default, else us-east-1.
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

// assumedRoleCollector runs every call as the assumed role; its credsProvider is never nil.
type assumedRoleCollector struct{ *Collector }

// Identity assumes the role, then asks STS who the resulting credentials are.
func (c *assumedRoleCollector) Identity(ctx context.Context, account domain.Account) (string, error) {
	if _, err := c.credsProvider.Retrieve(ctx); err != nil {
		return "", probeError("assume_role", err)
	}
	cfg, err := c.stsConfig(ctx, account)
	if err != nil {
		return "", err
	}
	out, err := c.stsClient(cfg).GetCallerIdentity(ctx, &sts.GetCallerIdentityInput{})
	if err != nil {
		return "", probeError("get_caller_identity", err)
	}
	return aws.ToString(out.Arn), nil
}

// probeError keeps only the API error code and HTTP status. Anything else, such as a
// network failure, is wrapped unchanged so discovery.diagnoseProbe can classify it.
func probeError(step string, err error) error {
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
