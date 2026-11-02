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

	return newAssumeRoleProvider(sts.NewFromConfig(cfg), roleARN, externalID, ""), nil
}

// newAssumeRoleProvider is the one assume-role builder. An empty sessionName keeps the
// SDK's generated name.
func newAssumeRoleProvider(client stscreds.AssumeRoleAPIClient, roleARN, externalID, sessionName string) *stscreds.AssumeRoleProvider {
	return stscreds.NewAssumeRoleProvider(client, roleARN, func(o *stscreds.AssumeRoleOptions) {
		if externalID != "" {
			o.ExternalID = aws.String(externalID)
		}
		if sessionName != "" {
			o.RoleSessionName = sessionName
		}
	})
}
