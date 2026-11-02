package domain

import (
	"sort"
	"time"
)

// CredentialType names the kind of stored cloud credential an account carries.
type CredentialType string

const (
	CredentialTypeAWSAssumeRole     CredentialType = "aws_assume_role"
	CredentialTypeGCPServiceAccount CredentialType = "gcp_service_account"
)

// credentialTypes is the one table every type-keyed rule derives from.
var credentialTypes = map[CredentialType]string{
	CredentialTypeAWSAssumeRole:     "aws",
	CredentialTypeGCPServiceAccount: "gcp",
}

// Valid reports whether t is a known credential type.
func (t CredentialType) Valid() bool {
	_, ok := credentialTypes[t]
	return ok
}

// Provider returns the account provider the type belongs to, or "" for an unknown type.
func (t CredentialType) Provider() string {
	return credentialTypes[t]
}

// ValidCredentialTypes returns every known type in a stable order, for errors and OpenAPI.
func ValidCredentialTypes() []CredentialType {
	out := make([]CredentialType, 0, len(credentialTypes))
	for t := range credentialTypes {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// CredentialSummary is the non-secret part of a credential, stored in clear for display.
type CredentialSummary struct {
	RoleARN       string `json:"role_arn,omitempty"`
	HasExternalID bool   `json:"has_external_id,omitempty"`
	ClientEmail   string `json:"client_email,omitempty"`
	ProjectID     string `json:"project_id,omitempty"`
}

// CredentialTestStatus is the outcome of a credential test.
type CredentialTestStatus string

const (
	CredentialTestOK     CredentialTestStatus = "ok"
	CredentialTestFailed CredentialTestStatus = "failed"
)

// CredentialTest is one test outcome. Error is sanitized text, never a value or a body.
type CredentialTest struct {
	TestedAt time.Time            `json:"tested_at"`
	Status   CredentialTestStatus `json:"status"`
	Error    string               `json:"error,omitempty"`
}

// CredentialTestResult is the response of POST .../credentials/test.
type CredentialTestResult struct {
	CredentialTest
	Identity string `json:"identity,omitempty"` // assumed-role ARN or service-account email; empty on failure
}

// AccountCredential is the API view. It has no field that can hold a secret or ciphertext.
type AccountCredential struct {
	AccountID int64             `json:"account_id"`
	Type      CredentialType    `json:"type"`
	Summary   CredentialSummary `json:"summary"`
	LastTest  *CredentialTest   `json:"last_test,omitempty"` // nil until tested after the last save
	CreatedAt time.Time         `json:"created_at"`
	UpdatedAt time.Time         `json:"updated_at"`
}

// AccountCredentialRecord is the stored row. Only storage and discovery.AccountCredentials handle it.
type AccountCredentialRecord struct {
	AccountCredential
	PayloadEncrypted string `json:"-"`
}

// SealedAccountCredential is the input to PutAccountCredential. No test or time fields:
// the store stamps times and clears any previous test result.
type SealedAccountCredential struct {
	AccountID        int64
	Type             CredentialType
	Summary          CredentialSummary
	PayloadEncrypted string
}

// PutAccountCredentialRequest is the PUT body and the canonical form sealed at rest.
// Exactly the object named by Type must be set.
type PutAccountCredentialRequest struct {
	Type              CredentialType          `json:"type"`
	AWSAssumeRole     *AWSAssumeRoleInput     `json:"aws_assume_role,omitempty"`
	GCPServiceAccount *GCPServiceAccountInput `json:"gcp_service_account,omitempty"`
}

// AWSAssumeRoleInput is the aws_assume_role request body.
type AWSAssumeRoleInput struct {
	RoleARN    string `json:"role_arn"`
	ExternalID string `json:"external_id,omitempty"`
}

// GCPServiceAccountInput is the gcp_service_account request body.
type GCPServiceAccountInput struct {
	KeyJSON string `json:"key_json"`
}
