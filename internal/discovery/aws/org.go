package aws

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/organizations"
	orgtypes "github.com/aws/aws-sdk-go-v2/service/organizations/types"

	"cloudpam/internal/discovery"
)

// OrgAccount represents an AWS account discovered via AWS Organizations.
type OrgAccount struct {
	ID    string // e.g. "123456789012"
	Name  string
	Email string
}

// ListOrgAccounts enumerates all active accounts in the AWS Organization.
// Uses the default credential chain (management account credentials). The
// limiter paces the ListAccounts pages; nil is unlimited.
func ListOrgAccounts(ctx context.Context, limiter *discovery.Limiter) ([]OrgAccount, error) {
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return nil, err
	}
	return listOrgAccounts(ctx, organizations.NewFromConfig(cfg), limiter)
}

func listOrgAccounts(ctx context.Context, client organizations.ListAccountsAPIClient, limiter *discovery.Limiter) ([]OrgAccount, error) {
	var accounts []OrgAccount

	paginator := organizations.NewListAccountsPaginator(client, &organizations.ListAccountsInput{})
	for paginator.HasMorePages() {
		if err := limiter.Wait(ctx); err != nil {
			return nil, err
		}
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
