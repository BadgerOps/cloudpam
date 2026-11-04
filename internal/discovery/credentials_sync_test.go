package discovery

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"cloudpam/internal/domain"
	"cloudpam/internal/storage"
)

type fakeAwareCollector struct {
	provider  string
	ran       []string
	boundWith []CredentialPayload
	identity  func(ctx context.Context) (string, error)
	bindErr   error
	boundErr  error
	hostErr   error
}

func (c *fakeAwareCollector) Provider() string { return c.provider }

func (c *fakeAwareCollector) Discover(context.Context, domain.Account) ([]domain.DiscoveredResource, error) {
	c.ran = append(c.ran, "host")
	return nil, nil
}

func (c *fakeAwareCollector) WithCredential(_ context.Context, _ domain.Account, cred CredentialPayload) (CredentialedCollector, error) {
	if c.bindErr != nil {
		return nil, c.bindErr
	}
	c.boundWith = append(c.boundWith, cred)
	return &fakeBoundCollector{host: c, cred: cred}, nil
}

type fakeBoundCollector struct {
	host *fakeAwareCollector
	cred CredentialPayload
}

func (c *fakeBoundCollector) Provider() string { return c.host.provider }

func (c *fakeBoundCollector) Discover(context.Context, domain.Account) ([]domain.DiscoveredResource, error) {
	c.host.ran = append(c.host.ran, "credential:"+string(c.cred.Type()))
	if c.host.boundErr != nil {
		return nil, c.host.boundErr
	}
	return []domain.DiscoveredResource{{
		ID: uuid.New(), Provider: c.host.provider, ResourceType: domain.ResourceTypeVPC, ResourceID: "vpc-bound",
		Status: domain.DiscoveryStatusActive, DiscoveredAt: time.Now().UTC(), LastSeenAt: time.Now().UTC(),
	}}, nil
}

func (c *fakeBoundCollector) Identity(ctx context.Context, _ domain.Account) (string, error) {
	if c.host.identity == nil {
		return "identity-of-" + string(c.cred.Type()), nil
	}
	return c.host.identity(ctx)
}

type syncFixture struct {
	svc   *SyncService
	vault *AccountCredentials
	ms    *storage.MemoryStore
	aws   *fakeAwareCollector
}

func newSyncFixture(t *testing.T) syncFixture {
	t.Helper()
	ms := storage.NewMemoryStore()
	vault := NewAccountCredentials(ms, testKey(t), true)
	svc := NewSyncService(storage.NewMemoryDiscoveryStore(ms))
	svc.SetCredentials(vault)
	aws := &fakeAwareCollector{provider: "aws"}
	svc.RegisterCollector(aws)
	return syncFixture{svc: svc, vault: vault, ms: ms, aws: aws}
}

func (f syncFixture) storeAWS(t *testing.T, account domain.Account) {
	t.Helper()
	if _, err := f.vault.Put(context.Background(), account, requestVariants(t)[domain.CredentialTypeAWSAssumeRole]); err != nil {
		t.Fatal(err)
	}
}

func TestSyncUsesStoredCredentialWhenPresent(t *testing.T) {
	f := newSyncFixture(t)
	account := createAccount(t, f.ms, "aws")
	f.storeAWS(t, account)

	job, err := f.svc.Sync(context.Background(), account)
	if err != nil {
		t.Fatalf("Sync() error = %v", err)
	}
	if job.Status != domain.SyncJobStatusCompleted || job.ResourcesFound != 1 || job.IdentitySource != "stored_credential" || job.Attempts != 1 {
		t.Fatalf("job = %+v", job)
	}
	if strings.Join(f.aws.ran, ",") != "credential:aws_assume_role" {
		t.Fatalf("collectors ran = %v, want only the credentialed one", f.aws.ran)
	}
	role, ok := f.aws.boundWith[0].(AWSAssumeRole)
	if !ok || role.RoleARN() != testRoleARN || role.ExternalID() != testExternalID {
		t.Fatalf("bound with %v", f.aws.boundWith[0])
	}
}

func TestSyncUsesHostIdentityWhenNoCredentialStored(t *testing.T) {
	f := newSyncFixture(t)
	account := createAccount(t, f.ms, "aws")

	if _, err := f.svc.Sync(context.Background(), account); err != nil {
		t.Fatalf("Sync() error = %v", err)
	}
	if strings.Join(f.aws.ran, ",") != "host" {
		t.Fatalf("collectors ran = %v, want host", f.aws.ran)
	}

	t.Run("and when no vault is configured", func(t *testing.T) {
		svc := NewSyncService(storage.NewMemoryDiscoveryStore(f.ms))
		aws := &fakeAwareCollector{provider: "aws"}
		svc.RegisterCollector(aws)
		f.storeAWS(t, account)
		if _, err := svc.Sync(context.Background(), account); err != nil {
			t.Fatalf("Sync() error = %v", err)
		}
		if strings.Join(aws.ran, ",") != "host" {
			t.Fatalf("collectors ran = %v, want host", aws.ran)
		}
	})
}

func TestSyncFailsTheJobOnACredentialItCannotUse(t *testing.T) {
	cases := []struct {
		name    string
		arrange func(t *testing.T, f *syncFixture) domain.Account
		want    error
	}{
		{
			name: "unreadable blob",
			arrange: func(t *testing.T, f *syncFixture) domain.Account {
				account := createAccount(t, f.ms, "aws")
				f.storeAWS(t, account)
				f.svc.SetCredentials(NewAccountCredentials(f.ms, testKey(t), true))
				return account
			},
			want: ErrCredentialUnreadable,
		},
		{
			name: "provider changed after the credential was saved",
			arrange: func(t *testing.T, f *syncFixture) domain.Account {
				account := createAccount(t, f.ms, "aws")
				f.storeAWS(t, account)
				f.svc.RegisterCollector(&fakeAwareCollector{provider: "gcp"})
				account.Provider = "gcp"
				return account
			},
			want: ErrCredentialProviderMismatch,
		},
		{
			name: "collector is not credential aware",
			arrange: func(t *testing.T, f *syncFixture) domain.Account {
				account := createAccount(t, f.ms, "aws")
				f.storeAWS(t, account)
				f.svc.RegisterCollector(staticCollector{})
				return account
			},
			want: ErrCredentialUnsupported,
		},
		{
			name: "binding fails",
			arrange: func(t *testing.T, f *syncFixture) domain.Account {
				account := createAccount(t, f.ms, "aws")
				f.storeAWS(t, account)
				f.aws.bindErr = errors.New("sts region unknown")
				return account
			},
			want: nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newSyncFixture(t)
			account := tc.arrange(t, &f)

			job, err := f.svc.Sync(context.Background(), account)
			if err == nil {
				t.Fatal("Sync() error = nil, want credential failure")
			}
			if tc.want != nil && !errors.Is(err, tc.want) {
				t.Fatalf("Sync() error = %v, want %v", err, tc.want)
			}
			if job == nil || job.Status != domain.SyncJobStatusFailed {
				t.Fatalf("job = %+v, want failed", job)
			}
			if !strings.HasPrefix(job.ErrorMessage, "credential: ") {
				t.Fatalf("job.ErrorMessage = %q, want credential: prefix", job.ErrorMessage)
			}
			if len(f.aws.ran) != 0 {
				t.Fatalf("collectors ran = %v, want none (no silent host fallback)", f.aws.ran)
			}
			if job.CompletedAt == nil {
				t.Fatal("job.CompletedAt = nil")
			}
			stored, err := f.svc.store.GetSyncJob(context.Background(), job.ID)
			if err != nil || stored.Status != domain.SyncJobStatusFailed {
				t.Fatalf("stored job = %+v, %v", stored, err)
			}
		})
	}
}

func TestSyncNamesACredentialRejectedDuringDiscover(t *testing.T) {
	cases := []struct {
		name     string
		boundErr error
		want     string
	}{
		{"probe error", fmt.Errorf("wrapped: %w", &ProbeError{Step: ProbeStepAssumeRole, Status: 403, Code: "AccessDenied"}), "credential: wrapped: assume_role: AccessDenied (http 403)"},
		{"other failure", errors.New("discover VPCs in region us-east-1: throttled"), "discover VPCs in region us-east-1: throttled"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newSyncFixture(t)
			account := createAccount(t, f.ms, "aws")
			f.storeAWS(t, account)
			f.aws.boundErr = tc.boundErr

			job, err := f.svc.Sync(context.Background(), account)
			if err == nil || job == nil || job.Status != domain.SyncJobStatusFailed {
				t.Fatalf("Sync() = %+v, %v, want a failed job", job, err)
			}
			if job.ErrorMessage != tc.want {
				t.Fatalf("job.ErrorMessage = %q, want %q", job.ErrorMessage, tc.want)
			}
			stored, err := f.svc.store.GetSyncJob(context.Background(), job.ID)
			if err != nil || stored.ErrorMessage != tc.want {
				t.Fatalf("stored job = %+v, %v, want ErrorMessage %q", stored, err, tc.want)
			}
		})
	}
}

func TestACredentialCopiedToAnotherAccountDoesNotOpen(t *testing.T) {
	f := newSyncFixture(t)
	ctx := context.Background()
	owner := createAccount(t, f.ms, "aws")
	f.storeAWS(t, owner)
	other, err := f.ms.CreateAccount(ctx, domain.CreateAccount{Key: "aws:other", Name: "other", Provider: "aws"})
	if err != nil {
		t.Fatal(err)
	}
	rec, err := f.vault.load(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.ms.PutAccountCredential(ctx, domain.SealedAccountCredential{
		AccountID: other.ID, Type: rec.Type, Summary: rec.Summary, PayloadEncrypted: rec.PayloadEncrypted,
	}); err != nil {
		t.Fatal(err)
	}

	res, err := f.svc.TestCredential(ctx, other)
	if err != nil || res.Status != domain.CredentialTestFailed || res.Error != ErrCredentialUnreadable.Error() {
		t.Fatalf("TestCredential() = %+v, %v, want failed with %q", res, err, ErrCredentialUnreadable.Error())
	}
	job, err := f.svc.Sync(ctx, other)
	if !errors.Is(err, ErrCredentialUnreadable) || job == nil || job.ErrorMessage != "credential: "+ErrCredentialUnreadable.Error() {
		t.Fatalf("Sync() = %+v, %v, want the job failed on the unreadable credential", job, err)
	}
	if _, err := f.svc.Sync(ctx, owner); err != nil {
		t.Fatalf("Sync() for the owning account error = %v", err)
	}
}

func TestTestCredentialRecordsTheResult(t *testing.T) {
	f := newSyncFixture(t)
	account := createAccount(t, f.ms, "aws")
	f.storeAWS(t, account)

	res, err := f.svc.TestCredential(context.Background(), account)
	if err != nil {
		t.Fatalf("TestCredential() error = %v", err)
	}
	if res.Status != domain.CredentialTestOK || res.Identity != "identity-of-aws_assume_role" || res.Error != "" || res.TestedAt.IsZero() {
		t.Fatalf("result = %+v", res)
	}
	view, err := f.vault.Get(context.Background(), account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if view.LastTest == nil || *view.LastTest != res.CredentialTest {
		t.Fatalf("stored LastTest = %+v, want %+v", view.LastTest, res.CredentialTest)
	}
	if len(f.aws.ran) != 0 {
		t.Fatalf("TestCredential ran Discover: %v", f.aws.ran)
	}
}

func TestTestCredentialReturnsFailedResultOnProbeError(t *testing.T) {
	f := newSyncFixture(t)
	account := createAccount(t, f.ms, "aws")
	f.storeAWS(t, account)
	f.aws.identity = func(context.Context) (string, error) {
		return "", fmt.Errorf("sts: %w", &ProbeError{Step: "assume_role", Status: 403, Code: "AccessDenied"})
	}

	res, err := f.svc.TestCredential(context.Background(), account)
	if err != nil {
		t.Fatalf("TestCredential() error = %v, want failed result instead", err)
	}
	if res.Status != domain.CredentialTestFailed || res.Identity != "" {
		t.Fatalf("result = %+v", res)
	}
	if res.Error != "assume_role: AccessDenied (http 403)" {
		t.Fatalf("result.Error = %q", res.Error)
	}
	view, _ := f.vault.Get(context.Background(), account.ID)
	if view.LastTest == nil || view.LastTest.Status != domain.CredentialTestFailed || view.LastTest.Error != res.Error {
		t.Fatalf("stored LastTest = %+v", view.LastTest)
	}
}

func TestTestCredentialDropsTheResultWhenTheCredentialWasReplaced(t *testing.T) {
	f := newSyncFixture(t)
	account := createAccount(t, f.ms, "aws")
	f.storeAWS(t, account)
	f.aws.identity = func(context.Context) (string, error) {
		f.storeAWS(t, account)
		return "arn:aws:sts::123456789012:assumed-role/CloudPAMDiscovery/cloudpam", nil
	}

	res, err := f.svc.TestCredential(context.Background(), account)
	if err != nil {
		t.Fatalf("TestCredential() error = %v", err)
	}
	if res.Status != domain.CredentialTestOK {
		t.Fatalf("result = %+v", res)
	}
	view, _ := f.vault.Get(context.Background(), account.ID)
	if view.LastTest != nil {
		t.Fatalf("result landed on the newer save: %+v", view.LastTest)
	}
}

func TestTestCredentialDoesNotRecordAfterTheClientWentAway(t *testing.T) {
	f := newSyncFixture(t)
	account := createAccount(t, f.ms, "aws")
	f.storeAWS(t, account)
	ctx, cancel := context.WithCancel(context.Background())
	f.aws.identity = func(context.Context) (string, error) {
		cancel()
		return "", context.Canceled
	}

	res, err := f.svc.TestCredential(ctx, account)
	if err != nil {
		t.Fatalf("TestCredential() error = %v", err)
	}
	if res.Status != domain.CredentialTestFailed || res.Error != "canceled" {
		t.Fatalf("result = %+v", res)
	}
	view, _ := f.vault.Get(context.Background(), account.ID)
	if view.LastTest != nil {
		t.Fatalf("recorded after cancellation: %+v", view.LastTest)
	}
}

func TestTestCredentialReportsConfigurationProblemsAsFailedResults(t *testing.T) {
	cases := []struct {
		name    string
		arrange func(t *testing.T, f *syncFixture) domain.Account
		want    error
	}{
		{"unreadable", func(t *testing.T, f *syncFixture) domain.Account {
			account := createAccount(t, f.ms, "aws")
			f.storeAWS(t, account)
			f.svc.SetCredentials(NewAccountCredentials(f.ms, testKey(t), true))
			return account
		}, ErrCredentialUnreadable},
		{"mismatch", func(t *testing.T, f *syncFixture) domain.Account {
			account := createAccount(t, f.ms, "aws")
			f.storeAWS(t, account)
			account.Provider = "gcp"
			return account
		}, ErrCredentialProviderMismatch},
		{"unsupported", func(t *testing.T, f *syncFixture) domain.Account {
			account := createAccount(t, f.ms, "aws")
			f.storeAWS(t, account)
			f.svc.RegisterCollector(staticCollector{})
			return account
		}, ErrCredentialUnsupported},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newSyncFixture(t)
			account := tc.arrange(t, &f)
			res, err := f.svc.TestCredential(context.Background(), account)
			if err != nil {
				t.Fatalf("TestCredential() error = %v, want failed result", err)
			}
			if res.Status != domain.CredentialTestFailed || res.Error != tc.want.Error() {
				t.Fatalf("result = %+v, want error %q", res, tc.want.Error())
			}
		})
	}
}

func TestTestCredentialWithoutACredential(t *testing.T) {
	f := newSyncFixture(t)
	account := createAccount(t, f.ms, "aws")
	if _, err := f.svc.TestCredential(context.Background(), account); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("TestCredential() error = %v, want ErrNotFound", err)
	}
	svc := NewSyncService(storage.NewMemoryDiscoveryStore(f.ms))
	if _, err := svc.TestCredential(context.Background(), account); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("TestCredential() without a vault error = %v, want ErrNotFound", err)
	}
}

func TestDiagnoseProbeFailsClosed(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"probe error with code and status", &ProbeError{Step: "token", Status: 400, Code: "invalid_grant"}, "token: invalid_grant (http 400)"},
		{"probe error code only", fmt.Errorf("wrapped: %w", &ProbeError{Step: "assume_role", Code: "AccessDenied"}), "assume_role: AccessDenied"},
		{"probe error status only", &ProbeError{Step: "get_project", Status: 403}, "get_project: http 403"},
		{"probe error empty", &ProbeError{Step: "get_caller_identity"}, "get_caller_identity: failed"},
		{"probe code is clipped", &ProbeError{Step: "token", Code: "bad code <script>" + strings.Repeat("x", 100)}, "token: badcodescript" + strings.Repeat("x", 64-len("badcodescript"))},
		{"deadline", fmt.Errorf("sts: %w", context.DeadlineExceeded), "timed out after 15s"},
		{"canceled", context.Canceled, "canceled"},
		{"dial failure", &net.OpError{Op: "dial", Err: errors.New("connection refused to 10.0.0.1:443")}, "endpoint unreachable"},
		{"url error", &url.Error{Op: "Post", URL: "https://oauth2.googleapis.com/token", Err: errors.New("no such host")}, "endpoint unreachable"},
		{"sentinel", fmt.Errorf("x: %w", ErrCredentialProviderMismatch), ErrCredentialProviderMismatch.Error()},
		{"anything else", errors.New("AccessDenied: User arn:aws:iam::123:user/bob is not authorized; external id was ext-secret-7"), "failed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := diagnoseProbe(tc.err); got != tc.want {
				t.Fatalf("diagnoseProbe() = %q, want %q", got, tc.want)
			}
		})
	}
}
