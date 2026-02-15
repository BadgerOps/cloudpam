package aws

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/organizations"
	orgtypes "github.com/aws/aws-sdk-go-v2/service/organizations/types"
)

// OrgAccount represents an AWS account discovered via AWS Organizations.
type OrgAccount struct {
	ID    string // e.g. "123456789012"
	Name  string
	Email string
}

// ListOrgAccounts enumerates all active accounts in the AWS Organization.
// Uses the default credential chain (management account credentials).
func ListOrgAccounts(ctx context.Context) ([]OrgAccount, error) {
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, err
	}

	client := organizations.NewFromConfig(cfg)
	var accounts []OrgAccount

	paginator := organizations.NewListAccountsPaginator(client, &organizations.ListAccountsInput{})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, acct := range page.Accounts {
			if acct.Status != orgtypes.AccountStatusActive {
				continue
			}
			accounts = append(accounts, OrgAccount{
				ID:    derefString(acct.Id),
				Name:  derefString(acct.Name),
				Email: derefString(acct.Email),
			})
		}
	}

	return accounts, nil
}

func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
