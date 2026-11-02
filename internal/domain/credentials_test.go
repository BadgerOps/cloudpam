package domain

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestCredentialTypeDerivesFromTable(t *testing.T) {
	cases := []struct {
		typ      CredentialType
		valid    bool
		provider string
	}{
		{CredentialTypeAWSAssumeRole, true, "aws"},
		{CredentialTypeGCPServiceAccount, true, "gcp"},
		{CredentialType("azure_sp"), false, ""},
		{CredentialType(""), false, ""},
	}
	for _, tc := range cases {
		if got := tc.typ.Valid(); got != tc.valid {
			t.Errorf("%q.Valid() = %v, want %v", tc.typ, got, tc.valid)
		}
		if got := tc.typ.Provider(); got != tc.provider {
			t.Errorf("%q.Provider() = %q, want %q", tc.typ, got, tc.provider)
		}
	}
}

func TestValidCredentialTypesIsStableAndComplete(t *testing.T) {
	got := ValidCredentialTypes()
	if len(got) != len(credentialTypes) {
		t.Fatalf("ValidCredentialTypes() has %d entries, table has %d", len(got), len(credentialTypes))
	}
	for i := 1; i < len(got); i++ {
		if got[i-1] >= got[i] {
			t.Fatalf("ValidCredentialTypes() not sorted: %v", got)
		}
	}
	for _, typ := range got {
		if !typ.Valid() {
			t.Errorf("ValidCredentialTypes() returned invalid type %q", typ)
		}
	}
}

func TestAccountCredentialJSONCarriesNoCiphertext(t *testing.T) {
	rec := AccountCredentialRecord{
		AccountCredential: AccountCredential{
			AccountID: 7,
			Type:      CredentialTypeAWSAssumeRole,
			Summary:   CredentialSummary{RoleARN: "arn:aws:iam::123456789012:role/x", HasExternalID: true},
			LastTest:  &CredentialTest{TestedAt: time.Unix(0, 0).UTC(), Status: CredentialTestFailed, Error: "assume_role: AccessDenied"},
		},
		PayloadEncrypted: "deadbeef",
	}
	b, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "deadbeef") {
		t.Fatalf("record JSON leaks ciphertext: %s", b)
	}
	for _, want := range []string{`"account_id":7`, `"type":"aws_assume_role"`, `"has_external_id":true`, `"last_test":{`, `"status":"failed"`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("record JSON missing %s: %s", want, b)
		}
	}
	untested, _ := json.Marshal(AccountCredential{AccountID: 1, Type: CredentialTypeGCPServiceAccount})
	if strings.Contains(string(untested), "last_test") {
		t.Fatalf("untested credential should omit last_test: %s", untested)
	}
}
