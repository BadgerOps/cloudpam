package discovery

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"cloudpam/internal/domain"
	"cloudpam/internal/secrets"
	"cloudpam/internal/storage"
)

const (
	testRoleARN    = "arn:aws:iam::123456789012:role/CloudPAMDiscovery"
	testExternalID = "ext-secret-7"
)

func testServiceAccountKey(t *testing.T) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	pemKey := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
	b, _ := json.Marshal(map[string]string{
		"type":           "service_account",
		"project_id":     "proj-1",
		"private_key_id": "kid-1",
		"private_key":    string(pemKey),
		"client_email":   "sa@proj-1.iam.gserviceaccount.com",
		"token_uri":      "http://127.0.0.1:1/evil",
	})
	return string(b)
}

func testKey(t *testing.T) []byte {
	t.Helper()
	key, err := secrets.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func requestVariants(t *testing.T) map[domain.CredentialType]domain.PutAccountCredentialRequest {
	t.Helper()
	return map[domain.CredentialType]domain.PutAccountCredentialRequest{
		domain.CredentialTypeAWSAssumeRole: {
			Type:          domain.CredentialTypeAWSAssumeRole,
			AWSAssumeRole: &domain.AWSAssumeRoleInput{RoleARN: testRoleARN, ExternalID: testExternalID},
		},
		domain.CredentialTypeGCPServiceAccount: {
			Type:              domain.CredentialTypeGCPServiceAccount,
			GCPServiceAccount: &domain.GCPServiceAccountInput{KeyJSON: testServiceAccountKey(t)},
		},
	}
}

func newVault(t *testing.T, durable bool) (*AccountCredentials, *storage.MemoryStore, []byte) {
	t.Helper()
	ms := storage.NewMemoryStore()
	key := testKey(t)
	return NewAccountCredentials(ms, key, durable), ms, key
}

func createAccount(t *testing.T, ms *storage.MemoryStore, provider string) domain.Account {
	t.Helper()
	a, err := ms.CreateAccount(context.Background(), domain.CreateAccount{Key: provider + ":" + t.Name(), Name: t.Name(), Provider: provider})
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestEveryRequestVariantRoundTripsThroughPutAndUnseal(t *testing.T) {
	variants := requestVariants(t)
	types := domain.ValidCredentialTypes()
	if len(variants) != len(types) {
		t.Fatalf("requestVariants covers %d types, credentialTypes has %d", len(variants), len(types))
	}
	if n := reflect.TypeOf(domain.PutAccountCredentialRequest{}).NumField(); n != len(types)+1 {
		t.Fatalf("PutAccountCredentialRequest has %d fields, want %d (type + one per credential type)", n, len(types)+1)
	}

	vault, ms, _ := newVault(t, true)
	ctx := context.Background()
	for _, typ := range types {
		req := variants[typ]
		account := createAccount(t, ms, typ.Provider())
		view, err := vault.Put(ctx, account, req)
		if err != nil {
			t.Fatalf("Put(%s) error = %v", typ, err)
		}
		if view.Type != typ || view.AccountID != account.ID || view.LastTest != nil {
			t.Fatalf("Put(%s) returned %+v", typ, view)
		}

		rec, err := vault.load(ctx, account.ID)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(rec.PayloadEncrypted, testExternalID) || strings.Contains(rec.PayloadEncrypted, "PRIVATE KEY") {
			t.Fatalf("stored payload for %s is not sealed", typ)
		}

		p, err := vault.unseal(rec)
		if err != nil {
			t.Fatalf("unseal(%s) error = %v", typ, err)
		}
		if p.Type() != typ || p.Provider() != account.Provider {
			t.Fatalf("unseal(%s) gave type %s provider %s", typ, p.Type(), p.Provider())
		}
		if got := p.summary(); got != rec.Summary {
			t.Fatalf("summary drift for %s: stored %+v, unsealed %+v", typ, rec.Summary, got)
		}

		again, err := parseCredentialRequest(p.canonical())
		if err != nil {
			t.Fatalf("canonical(%s) does not parse: %v", typ, err)
		}
		if !reflect.DeepEqual(again, p) {
			t.Fatalf("canonical(%s) round trip changed the payload", typ)
		}
	}
}

func TestPutStoresOnlyTheSummaryInClear(t *testing.T) {
	vault, ms, _ := newVault(t, true)
	ctx := context.Background()

	aws := createAccount(t, ms, "aws")
	view, err := vault.Put(ctx, aws, requestVariants(t)[domain.CredentialTypeAWSAssumeRole])
	if err != nil {
		t.Fatal(err)
	}
	if view.Summary.RoleARN != testRoleARN || !view.Summary.HasExternalID || view.Summary.ClientEmail != "" {
		t.Fatalf("aws summary = %+v", view.Summary)
	}

	gcp := createAccount(t, ms, "gcp")
	view, err = vault.Put(ctx, gcp, requestVariants(t)[domain.CredentialTypeGCPServiceAccount])
	if err != nil {
		t.Fatal(err)
	}
	if view.Summary.ClientEmail != "sa@proj-1.iam.gserviceaccount.com" || view.Summary.ProjectID != "proj-1" || view.Summary.RoleARN != "" {
		t.Fatalf("gcp summary = %+v", view.Summary)
	}

	got, err := vault.Get(ctx, gcp.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got != view {
		t.Fatalf("Get() = %+v, want %+v", got, view)
	}
}

func TestPutRejectsBadInput(t *testing.T) {
	vault, ms, _ := newVault(t, true)
	ctx := context.Background()
	aws := createAccount(t, ms, "aws")
	gcp := createAccount(t, ms, "gcp")
	goodKey := testServiceAccountKey(t)
	mutateKey := func(edit func(m map[string]string)) string {
		var m map[string]string
		_ = json.Unmarshal([]byte(goodKey), &m)
		edit(m)
		b, _ := json.Marshal(m)
		return string(b)
	}
	awsReq := func(arn, ext string) domain.PutAccountCredentialRequest {
		return domain.PutAccountCredentialRequest{Type: domain.CredentialTypeAWSAssumeRole, AWSAssumeRole: &domain.AWSAssumeRoleInput{RoleARN: arn, ExternalID: ext}}
	}
	gcpReq := func(key string) domain.PutAccountCredentialRequest {
		return domain.PutAccountCredentialRequest{Type: domain.CredentialTypeGCPServiceAccount, GCPServiceAccount: &domain.GCPServiceAccountInput{KeyJSON: key}}
	}

	cases := []struct {
		name    string
		account domain.Account
		req     domain.PutAccountCredentialRequest
		field   string
	}{
		{"unknown type", aws, domain.PutAccountCredentialRequest{Type: "azure_sp"}, "type"},
		{"empty type", aws, domain.PutAccountCredentialRequest{}, "type"},
		{"aws object missing", aws, domain.PutAccountCredentialRequest{Type: domain.CredentialTypeAWSAssumeRole}, "aws_assume_role"},
		{"aws with gcp object too", aws, domain.PutAccountCredentialRequest{Type: domain.CredentialTypeAWSAssumeRole, AWSAssumeRole: &domain.AWSAssumeRoleInput{RoleARN: testRoleARN}, GCPServiceAccount: &domain.GCPServiceAccountInput{}}, "aws_assume_role"},
		{"gcp object missing", gcp, domain.PutAccountCredentialRequest{Type: domain.CredentialTypeGCPServiceAccount}, "gcp_service_account"},
		{"gcp with aws object too", gcp, domain.PutAccountCredentialRequest{Type: domain.CredentialTypeGCPServiceAccount, GCPServiceAccount: &domain.GCPServiceAccountInput{KeyJSON: goodKey}, AWSAssumeRole: &domain.AWSAssumeRoleInput{}}, "gcp_service_account"},
		{"arn not a role", aws, awsReq("arn:aws:iam::123456789012:user/bob", ""), "aws_assume_role.role_arn"},
		{"arn short account", aws, awsReq("arn:aws:iam::1234:role/x", ""), "aws_assume_role.role_arn"},
		{"arn empty name", aws, awsReq("arn:aws:iam::123456789012:role/", ""), "aws_assume_role.role_arn"},
		{"external id too long", aws, awsReq(testRoleARN, strings.Repeat("x", maxExternalIDLength+1)), "aws_assume_role.external_id"},
		{"key not json", gcp, gcpReq("{nope"), "gcp_service_account.key_json"},
		{"key wrong type", gcp, gcpReq(mutateKey(func(m map[string]string) { m["type"] = "authorized_user" })), "gcp_service_account.key_json"},
		{"key missing email", gcp, gcpReq(mutateKey(func(m map[string]string) { delete(m, "client_email") })), "gcp_service_account.key_json"},
		{"key missing project", gcp, gcpReq(mutateKey(func(m map[string]string) { m["project_id"] = " " })), "gcp_service_account.key_json"},
		{"key missing private key", gcp, gcpReq(mutateKey(func(m map[string]string) { delete(m, "private_key") })), "gcp_service_account.key_json"},
		{"key not pem", gcp, gcpReq(mutateKey(func(m map[string]string) { m["private_key"] = "not pem" })), "gcp_service_account.key_json"},
		{"key pem not a key", gcp, gcpReq(mutateKey(func(m map[string]string) {
			m["private_key"] = string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: []byte("junk")}))
		})), "gcp_service_account.key_json"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := vault.Put(ctx, tc.account, tc.req)
			var in *CredentialInputError
			if !errors.As(err, &in) {
				t.Fatalf("Put() error = %v, want *CredentialInputError", err)
			}
			if in.Field != tc.field {
				t.Fatalf("Field = %q, want %q", in.Field, tc.field)
			}
			if strings.Contains(err.Error(), testExternalID) || strings.Contains(err.Error(), "PRIVATE KEY") {
				t.Fatalf("error leaks a value: %v", err)
			}
		})
	}

	t.Run("arn with partition suffix is accepted", func(t *testing.T) {
		if _, err := NewAWSAssumeRole("arn:aws-us-gov:iam::123456789012:role/path/To/Role", ""); err != nil {
			t.Fatalf("NewAWSAssumeRole() error = %v", err)
		}
	})
	t.Run("pkcs1 key is accepted", func(t *testing.T) {
		key, _ := rsa.GenerateKey(rand.Reader, 1024)
		pkcs1 := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
		if _, err := ParseGCPServiceAccountKey([]byte(mutateKey(func(m map[string]string) { m["private_key"] = string(pkcs1) }))); err != nil {
			t.Fatalf("ParseGCPServiceAccountKey() error = %v", err)
		}
	})
}

func TestPutEnforcesProviderAndDurableKeyAndLiveAccount(t *testing.T) {
	ctx := context.Background()
	variants := requestVariants(t)

	t.Run("provider mismatch", func(t *testing.T) {
		vault, ms, _ := newVault(t, true)
		gcp := createAccount(t, ms, "gcp")
		_, err := vault.Put(ctx, gcp, variants[domain.CredentialTypeAWSAssumeRole])
		if !errors.Is(err, ErrCredentialProviderMismatch) {
			t.Fatalf("Put() error = %v, want ErrCredentialProviderMismatch", err)
		}
	})
	t.Run("ephemeral key refuses after validation", func(t *testing.T) {
		vault, ms, _ := newVault(t, false)
		aws := createAccount(t, ms, "aws")
		_, err := vault.Put(ctx, aws, variants[domain.CredentialTypeAWSAssumeRole])
		if !errors.Is(err, ErrCredentialKeyEphemeral) {
			t.Fatalf("Put() error = %v, want ErrCredentialKeyEphemeral", err)
		}
		_, err = vault.Put(ctx, aws, domain.PutAccountCredentialRequest{Type: "bogus"})
		var in *CredentialInputError
		if !errors.As(err, &in) {
			t.Fatalf("Put() with bad input on ephemeral key error = %v, want input error first", err)
		}
		if _, err := vault.Get(ctx, aws.ID); !errors.Is(err, storage.ErrNotFound) {
			t.Fatalf("Get() after refused Put error = %v, want ErrNotFound", err)
		}
	})
	t.Run("deleted account", func(t *testing.T) {
		vault, ms, _ := newVault(t, true)
		aws := createAccount(t, ms, "aws")
		if _, err := ms.DeleteAccount(ctx, aws.ID); err != nil {
			t.Fatal(err)
		}
		_, err := vault.Put(ctx, aws, variants[domain.CredentialTypeAWSAssumeRole])
		if !errors.Is(err, storage.ErrNotFound) {
			t.Fatalf("Put() error = %v, want ErrNotFound", err)
		}
	})
}

func TestUnsealFailsClosed(t *testing.T) {
	ctx := context.Background()
	vault, ms, key := newVault(t, true)
	aws := createAccount(t, ms, "aws")
	if _, err := vault.Put(ctx, aws, requestVariants(t)[domain.CredentialTypeAWSAssumeRole]); err != nil {
		t.Fatal(err)
	}
	rec, _ := vault.load(ctx, aws.ID)

	t.Run("wrong key", func(t *testing.T) {
		other := NewAccountCredentials(ms, testKey(t), true)
		if _, err := other.unseal(rec); !errors.Is(err, ErrCredentialUnreadable) {
			t.Fatalf("unseal() error = %v, want ErrCredentialUnreadable", err)
		}
	})
	t.Run("garbage ciphertext", func(t *testing.T) {
		bad := rec
		bad.PayloadEncrypted = "zz"
		if _, err := vault.unseal(bad); !errors.Is(err, ErrCredentialUnreadable) {
			t.Fatalf("unseal() error = %v, want ErrCredentialUnreadable", err)
		}
	})
	t.Run("plaintext that is not a request", func(t *testing.T) {
		sealed, _ := secrets.Encrypt(`{"type":"aws_assume_role"}`, key)
		bad := rec
		bad.PayloadEncrypted = sealed
		if _, err := vault.unseal(bad); !errors.Is(err, ErrCredentialUnreadable) {
			t.Fatalf("unseal() error = %v, want ErrCredentialUnreadable", err)
		}
	})
	t.Run("row type disagrees with payload", func(t *testing.T) {
		bad := rec
		bad.Type = domain.CredentialTypeGCPServiceAccount
		if _, err := vault.unseal(bad); !errors.Is(err, ErrCredentialUnreadable) {
			t.Fatalf("unseal() error = %v, want ErrCredentialUnreadable", err)
		}
	})
}

func TestPayloadsFormatWithoutSecrets(t *testing.T) {
	role, err := NewAWSAssumeRole(testRoleARN, testExternalID)
	if err != nil {
		t.Fatal(err)
	}
	sa, err := ParseGCPServiceAccountKey([]byte(testServiceAccountKey(t)))
	if err != nil {
		t.Fatal(err)
	}
	for _, verb := range []string{"%v", "%+v", "%#v", "%s"} {
		for _, p := range []CredentialPayload{role, sa, &role, &sa} {
			out := fmt.Sprintf(verb, p)
			if strings.Contains(out, testExternalID) || strings.Contains(out, "PRIVATE KEY") {
				t.Fatalf("fmt.Sprintf(%q) leaks a secret: %s", verb, out)
			}
		}
	}
	if out := fmt.Sprint(role); !strings.Contains(out, testRoleARN) || !strings.Contains(out, "has_external_id:true") {
		t.Fatalf("AWSAssumeRole prints %q", out)
	}
	if out := fmt.Sprint(sa); !strings.Contains(out, "sa@proj-1.iam.gserviceaccount.com") || !strings.Contains(out, "kid-1") {
		t.Fatalf("GCPServiceAccount prints %q", out)
	}
	if sa.PrivateKeyID() != "kid-1" || sa.ProjectID() != "proj-1" {
		t.Fatalf("accessors: %s %s", sa.PrivateKeyID(), sa.ProjectID())
	}
	pk := sa.PrivateKey()
	pk[0] = 'X'
	if sa.PrivateKey()[0] == 'X' {
		t.Fatal("PrivateKey() shares its backing array with the payload")
	}
	if role.RoleARN() != testRoleARN || role.ExternalID() != testExternalID {
		t.Fatalf("accessors: %s %s", role.RoleARN(), role.ExternalID())
	}
}

func TestDeleteReportsExistence(t *testing.T) {
	ctx := context.Background()
	vault, ms, _ := newVault(t, true)
	aws := createAccount(t, ms, "aws")
	if ok, err := vault.Delete(ctx, aws.ID); err != nil || ok {
		t.Fatalf("Delete() before put = %v, %v", ok, err)
	}
	if _, err := vault.Put(ctx, aws, requestVariants(t)[domain.CredentialTypeAWSAssumeRole]); err != nil {
		t.Fatal(err)
	}
	if ok, err := vault.Delete(ctx, aws.ID); err != nil || !ok {
		t.Fatalf("Delete() after put = %v, %v", ok, err)
	}
}
