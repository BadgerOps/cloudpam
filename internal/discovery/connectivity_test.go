package discovery

import (
	"context"
	"errors"
	"testing"

	"cloudpam/internal/domain"
	"cloudpam/internal/storage"
)

func (c *fakeAwareCollector) Identity(context.Context, domain.Account) (string, error) {
	if c.hostErr != nil {
		return "", c.hostErr
	}
	return "host-identity", nil
}

func TestTestConnectivityRunsAsTheIdentityASyncWouldUse(t *testing.T) {
	f := newSyncFixture(t)
	hostOnly := createAccount(t, f.ms, "aws")
	withCredential, err := f.ms.CreateAccount(context.Background(), domain.CreateAccount{Key: "aws:with-credential", Name: "with credential", Provider: "aws"})
	if err != nil {
		t.Fatal(err)
	}
	f.storeAWS(t, withCredential)

	res, err := f.svc.TestConnectivity(context.Background(), hostOnly)
	if err != nil {
		t.Fatalf("TestConnectivity(host) error = %v", err)
	}
	if res.Status != domain.CredentialTestOK || res.Source != domain.ConnectivitySourceHost || res.Identity != "host-identity" || res.TestedAt.IsZero() || res.LatencyMS < 0 {
		t.Fatalf("host result = %+v", res)
	}

	res, err = f.svc.TestConnectivity(context.Background(), withCredential)
	if err != nil {
		t.Fatalf("TestConnectivity(stored) error = %v", err)
	}
	if res.Status != domain.CredentialTestOK || res.Source != domain.ConnectivitySourceStoredCredential || res.Identity != "identity-of-aws_assume_role" {
		t.Fatalf("stored result = %+v", res)
	}
	view, err := f.vault.Get(context.Background(), withCredential.ID)
	if err != nil {
		t.Fatal(err)
	}
	if view.LastTest == nil || *view.LastTest != res.CredentialTest {
		t.Fatalf("credential LastTest = %+v, want %+v", view.LastTest, res.CredentialTest)
	}
	if len(f.aws.ran) != 0 {
		t.Fatalf("TestConnectivity ran Discover: %v", f.aws.ran)
	}
}

func TestTestConnectivityReportsAFailedProbeAsAResult(t *testing.T) {
	f := newSyncFixture(t)
	account := createAccount(t, f.ms, "aws")
	f.aws.hostErr = &ProbeError{Step: ProbeStepGetCallerIdentity, Status: 403, Code: "InvalidClientTokenId"}

	res, err := f.svc.TestConnectivity(context.Background(), account)
	if err != nil {
		t.Fatalf("TestConnectivity() error = %v, want a failed result", err)
	}
	if res.Status != domain.CredentialTestFailed || res.Identity != "" || res.Source != domain.ConnectivitySourceHost {
		t.Fatalf("result = %+v", res)
	}
	if res.Error != "get_caller_identity: InvalidClientTokenId (http 403)" {
		t.Fatalf("result.Error = %q", res.Error)
	}
}

func TestTestConnectivityWithoutAProbeForTheProvider(t *testing.T) {
	f := newSyncFixture(t)
	account := createAccount(t, f.ms, "azure")
	if _, err := f.svc.TestConnectivity(context.Background(), account); !errors.Is(err, ErrConnectivityUnsupported) {
		t.Fatalf("TestConnectivity() error = %v, want ErrConnectivityUnsupported", err)
	}
}

func TestTestConnectivityWithoutAVaultUsesTheHost(t *testing.T) {
	svc := NewSyncService(storage.NewMemoryDiscoveryStore(storage.NewMemoryStore()))
	svc.RegisterCollector(&fakeAwareCollector{provider: "aws"})
	res, err := svc.TestConnectivity(context.Background(), domain.Account{ID: 1, Provider: "aws"})
	if err != nil || res.Source != domain.ConnectivitySourceHost || res.Status != domain.CredentialTestOK {
		t.Fatalf("TestConnectivity() = %+v, %v", res, err)
	}
}
