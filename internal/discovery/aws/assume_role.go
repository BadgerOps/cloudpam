package aws

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

// AssumeRole returns a credentials provider that assumes the given role in the target account.
func AssumeRole(ctx context.Context, accountID, roleName, externalID string) (aws.CredentialsProvider, error) {
	roleARN := fmt.Sprintf("arn:aws:iam::%s:role/%s", accountID, roleName)

	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}

	stsClient := sts.NewFromConfig(cfg)

	var opts []func(*stscreds.AssumeRoleOptions)
	if externalID != "" {
		opts = append(opts, func(o *stscreds.AssumeRoleOptions) {
			o.ExternalID = &externalID
		})
	}

	provider := stscreds.NewAssumeRoleProvider(stsClient, roleARN, opts...)
	return provider, nil
}
