package aws

import (
	"context"
	"testing"
	"time"

	awssdk "github.com/aws/aws-sdk-go-v2/aws"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/organizations"
	orgtypes "github.com/aws/aws-sdk-go-v2/service/organizations/types"

	"cloudpam/internal/discovery"
	"cloudpam/internal/domain"
)

// limiterWithTokens returns a limiter holding exactly n tokens and refilling
// one per second, so a test can count the calls that get through before a
// short deadline fails the next wait.
func limiterWithTokens(t *testing.T, n int) *discovery.Limiter {
	t.Helper()
	if n > 0 {
		return discovery.NewLimiter(float64(n))
	}
	l := discovery.NewLimiter(1)
	if err := l.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	return l
}

func shortDeadline(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	t.Cleanup(cancel)
	return ctx
}

func threePageEC2() *pagedEC2 {
	vpc := func(id string) ec2types.Vpc {
		return ec2types.Vpc{VpcId: awssdk.String(id), CidrBlock: awssdk.String("10.0.0.0/16"), State: ec2types.VpcStateAvailable}
	}
	return &pagedEC2{
		vpcPages:    [][]ec2types.Vpc{{vpc("vpc-1")}, {vpc("vpc-2")}, {vpc("vpc-3")}},
		subnetPages: [][]ec2types.Subnet{{}},
	}
}

func TestDiscoverCountsEveryEC2PageAgainstTheLimiter(t *testing.T) {
	cases := []struct {
		name         string
		tokens       int
		wantVPCCalls int
	}{
		{name: "no token stops before the first call", tokens: 0, wantVPCCalls: 0},
		{name: "two tokens allow two pages", tokens: 2, wantVPCCalls: 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client := threePageEC2()
			collector := newTestCollector(map[string]ec2API{"us-east-1": client})
			collector.SetLimiter(limiterWithTokens(t, tc.tokens))

			_, err := collector.Discover(shortDeadline(t), domain.Account{ID: 1, Regions: []string{"us-east-1"}})
			if err == nil {
				t.Fatal("Discover() = nil error, want the limiter to fail the next page before the deadline")
			}
			if client.vpcCalls != tc.wantVPCCalls || client.subnetCalls != 0 || client.addrCalls != 0 {
				t.Fatalf("calls = vpc %d, subnet %d, addr %d; want vpc %d and none after",
					client.vpcCalls, client.subnetCalls, client.addrCalls, tc.wantVPCCalls)
			}
		})
	}
}

func TestDiscoverWithoutALimiterIsNotPaced(t *testing.T) {
	client := threePageEC2()
	collector := newTestCollector(map[string]ec2API{"us-east-1": client})
	collector.SetLimiter(nil)

	resources, err := collector.Discover(shortDeadline(t), domain.Account{ID: 1, Regions: []string{"us-east-1"}})
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	if len(resources) != 3 || client.vpcCalls != 3 || client.addrCalls != 1 {
		t.Fatalf("resources = %d, vpc calls = %d, addr calls = %d; want 3, 3, 1", len(resources), client.vpcCalls, client.addrCalls)
	}
}

func TestWithCredentialKeepsTheHostLimiter(t *testing.T) {
	client := threePageEC2()
	host := &Collector{
		loadConfig: func(_ context.Context, region string, cp awssdk.CredentialsProvider) (awssdk.Config, error) {
			return awssdk.Config{Region: region, Credentials: cp}, nil
		},
		newEC2Client: func(awssdk.Config) ec2API { return client },
		newSTSClient: func(awssdk.Config) stsAPI { return &fakeSTS{} },
	}
	host.SetLimiter(limiterWithTokens(t, 0))

	account := domain.Account{ID: 1, Provider: "aws", Regions: []string{"us-east-1"}}
	bound, err := host.WithCredential(context.Background(), account, testRole(t, ""))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := bound.Discover(shortDeadline(t), account); err == nil {
		t.Fatal("bound Discover() = nil error, want the host limiter to hold the call")
	}
	if client.vpcCalls != 0 {
		t.Fatalf("vpc calls = %d, want 0 while the host limiter is empty", client.vpcCalls)
	}
}

type pagedOrganizations struct {
	pages [][]orgtypes.Account
	calls int
}

func (p *pagedOrganizations) ListAccounts(_ context.Context, in *organizations.ListAccountsInput, _ ...func(*organizations.Options)) (*organizations.ListAccountsOutput, error) {
	p.calls++
	idx, err := pageIndex(in.NextToken)
	if err != nil {
		return nil, err
	}
	return &organizations.ListAccountsOutput{Accounts: p.pages[idx], NextToken: nextTokenFor(idx, len(p.pages))}, nil
}

func twoPageOrganizations() *pagedOrganizations {
	acct := func(id string) orgtypes.Account {
		return orgtypes.Account{Id: awssdk.String(id), Name: awssdk.String("acct " + id), Status: orgtypes.AccountStatusActive}
	}
	return &pagedOrganizations{pages: [][]orgtypes.Account{{acct("111111111111")}, {acct("222222222222")}}}
}

func TestListOrgAccountsCountsEveryPageAgainstTheLimiter(t *testing.T) {
	client := twoPageOrganizations()
	accounts, err := listOrgAccounts(shortDeadline(t), client, nil)
	if err != nil || len(accounts) != 2 || client.calls != 2 {
		t.Fatalf("unlimited: accounts = %d, calls = %d, err = %v; want 2, 2, nil", len(accounts), client.calls, err)
	}

	client = twoPageOrganizations()
	if _, err := listOrgAccounts(shortDeadline(t), client, limiterWithTokens(t, 1)); err == nil {
		t.Fatal("one token: err = nil, want the second page held")
	}
	if client.calls != 1 {
		t.Fatalf("one token: calls = %d, want 1", client.calls)
	}
}
