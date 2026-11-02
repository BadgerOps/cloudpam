package discovery

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"cloudpam/internal/domain"
	"cloudpam/internal/secrets"
	"cloudpam/internal/storage"
)

var (
	ErrCredentialKeyEphemeral     = errors.New("cannot store a credential: CLOUDPAM_OIDC_ENCRYPTION_KEY is not set, so the stored credential would be lost at restart; set the key")
	ErrCredentialUnreadable       = errors.New("stored credential cannot be decrypted; check CLOUDPAM_OIDC_ENCRYPTION_KEY or save the credential again")
	ErrCredentialProviderMismatch = errors.New("credential type does not match the account provider")
	ErrCredentialUnsupported      = errors.New("provider does not support stored credentials")
)

// CredentialInputError names an invalid request field. Detail states the rule, never the value.
type CredentialInputError struct{ Field, Detail string }

func (e *CredentialInputError) Error() string { return e.Field + ": " + e.Detail }

// CredentialPayload is a decrypted credential. Sealed: only this package's types implement it.
// Payload values have no exported fields and implement fmt.Formatter without the secret.
type CredentialPayload interface {
	Type() domain.CredentialType
	Provider() string
	summary() domain.CredentialSummary
	canonical() domain.PutAccountCredentialRequest
}

// AWSAssumeRole is a role CloudPAM assumes with the host identity.
type AWSAssumeRole struct{ roleARN, externalID string }

var roleARNPattern = regexp.MustCompile(`^arn:aws[a-z-]*:iam::\d{12}:role/.+$`)

// maxExternalIDLength is the STS limit for ExternalId.
const maxExternalIDLength = 1224

// NewAWSAssumeRole validates the role ARN and external ID.
func NewAWSAssumeRole(roleARN, externalID string) (AWSAssumeRole, error) {
	if !roleARNPattern.MatchString(roleARN) {
		return AWSAssumeRole{}, &CredentialInputError{Field: "aws_assume_role.role_arn", Detail: "must look like arn:aws:iam::<12-digit account>:role/<name>"}
	}
	if len(externalID) > maxExternalIDLength {
		return AWSAssumeRole{}, &CredentialInputError{Field: "aws_assume_role.external_id", Detail: fmt.Sprintf("must be at most %d characters", maxExternalIDLength)}
	}
	return AWSAssumeRole{roleARN: roleARN, externalID: externalID}, nil
}

func (p AWSAssumeRole) Type() domain.CredentialType { return domain.CredentialTypeAWSAssumeRole }
func (p AWSAssumeRole) Provider() string            { return p.Type().Provider() }
func (p AWSAssumeRole) RoleARN() string             { return p.roleARN }
func (p AWSAssumeRole) ExternalID() string          { return p.externalID }

func (p AWSAssumeRole) summary() domain.CredentialSummary {
	return domain.CredentialSummary{RoleARN: p.roleARN, HasExternalID: p.externalID != ""}
}

func (p AWSAssumeRole) canonical() domain.PutAccountCredentialRequest {
	return domain.PutAccountCredentialRequest{
		Type:          p.Type(),
		AWSAssumeRole: &domain.AWSAssumeRoleInput{RoleARN: p.roleARN, ExternalID: p.externalID},
	}
}

// Format prints the payload without the external ID.
func (p AWSAssumeRole) Format(f fmt.State, _ rune) {
	_, _ = fmt.Fprintf(f, "AWSAssumeRole{role_arn:%s has_external_id:%t}", p.roleARN, p.externalID != "")
}

// GCPServiceAccount keeps only the fields discovery needs. token_uri is dropped at parse.
type GCPServiceAccount struct {
	clientEmail, projectID, privateKeyID string
	privateKey                           []byte
}

// ParseGCPServiceAccountKey validates a service-account key file.
func ParseGCPServiceAccountKey(keyJSON []byte) (GCPServiceAccount, error) {
	const field = "gcp_service_account.key_json"
	var key struct {
		Type         string `json:"type"`
		ClientEmail  string `json:"client_email"`
		ProjectID    string `json:"project_id"`
		PrivateKeyID string `json:"private_key_id"`
		PrivateKey   string `json:"private_key"`
	}
	if err := json.Unmarshal(keyJSON, &key); err != nil {
		return GCPServiceAccount{}, &CredentialInputError{Field: field, Detail: "must be a JSON object"}
	}
	if key.Type != "service_account" {
		return GCPServiceAccount{}, &CredentialInputError{Field: field, Detail: `must have "type": "service_account"`}
	}
	for name, value := range map[string]string{"client_email": key.ClientEmail, "project_id": key.ProjectID, "private_key": key.PrivateKey} {
		if strings.TrimSpace(value) == "" {
			return GCPServiceAccount{}, &CredentialInputError{Field: field, Detail: "must include " + name}
		}
	}
	if err := checkRSAPEM([]byte(key.PrivateKey)); err != nil {
		return GCPServiceAccount{}, &CredentialInputError{Field: field, Detail: "private_key must be a PEM-encoded RSA private key"}
	}
	return GCPServiceAccount{
		clientEmail:  key.ClientEmail,
		projectID:    key.ProjectID,
		privateKeyID: key.PrivateKeyID,
		privateKey:   []byte(key.PrivateKey),
	}, nil
}

func checkRSAPEM(pemBytes []byte) error {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return errors.New("no PEM block")
	}
	if _, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return nil
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return err
	}
	if _, ok := parsed.(*rsa.PrivateKey); !ok {
		return errors.New("not an RSA key")
	}
	return nil
}

func (p GCPServiceAccount) Type() domain.CredentialType {
	return domain.CredentialTypeGCPServiceAccount
}
func (p GCPServiceAccount) Provider() string     { return p.Type().Provider() }
func (p GCPServiceAccount) ClientEmail() string  { return p.clientEmail }
func (p GCPServiceAccount) ProjectID() string    { return p.projectID }
func (p GCPServiceAccount) PrivateKeyID() string { return p.privateKeyID }

// PrivateKey returns a copy of the PEM-encoded private key.
func (p GCPServiceAccount) PrivateKey() []byte { return append([]byte(nil), p.privateKey...) }

func (p GCPServiceAccount) summary() domain.CredentialSummary {
	return domain.CredentialSummary{ClientEmail: p.clientEmail, ProjectID: p.projectID}
}

func (p GCPServiceAccount) canonical() domain.PutAccountCredentialRequest {
	key, _ := json.Marshal(map[string]string{
		"type":           "service_account",
		"client_email":   p.clientEmail,
		"project_id":     p.projectID,
		"private_key_id": p.privateKeyID,
		"private_key":    string(p.privateKey),
	})
	return domain.PutAccountCredentialRequest{
		Type:              p.Type(),
		GCPServiceAccount: &domain.GCPServiceAccountInput{KeyJSON: string(key)},
	}
}

// Format prints the payload without the private key.
func (p GCPServiceAccount) Format(f fmt.State, _ rune) {
	_, _ = fmt.Fprintf(f, "GCPServiceAccount{client_email:%s project_id:%s private_key_id:%s}", p.clientEmail, p.projectID, p.privateKeyID)
}

// parseCredentialRequest is the one place a request becomes a payload. Exactly the
// object named by Type must be set.
func parseCredentialRequest(req domain.PutAccountCredentialRequest) (CredentialPayload, error) {
	switch req.Type {
	case domain.CredentialTypeAWSAssumeRole:
		if req.AWSAssumeRole == nil || req.GCPServiceAccount != nil {
			return nil, &CredentialInputError{Field: "aws_assume_role", Detail: "must be the only object set for type aws_assume_role"}
		}
		return NewAWSAssumeRole(req.AWSAssumeRole.RoleARN, req.AWSAssumeRole.ExternalID)
	case domain.CredentialTypeGCPServiceAccount:
		if req.GCPServiceAccount == nil || req.AWSAssumeRole != nil {
			return nil, &CredentialInputError{Field: "gcp_service_account", Detail: "must be the only object set for type gcp_service_account"}
		}
		return ParseGCPServiceAccountKey([]byte(req.GCPServiceAccount.KeyJSON))
	default:
		names := make([]string, 0, len(domain.ValidCredentialTypes()))
		for _, t := range domain.ValidCredentialTypes() {
			names = append(names, string(t))
		}
		return nil, &CredentialInputError{Field: "type", Detail: "must be one of " + strings.Join(names, ", ")}
	}
}

// checkProvider enforces the type-to-provider rule at save time and at use time,
// because PATCH /accounts/{id} can change provider after the credential was saved.
func checkProvider(p CredentialPayload, account domain.Account) error {
	if p.Provider() != account.Provider {
		return fmt.Errorf("%w: %s credential on %q account", ErrCredentialProviderMismatch, p.Type(), account.Provider)
	}
	return nil
}

// AccountCredentials owns parsing, the provider rule, sealing, and the ephemeral-key refusal.
type AccountCredentials struct {
	store   storage.AccountCredentialStore
	key     []byte
	durable bool
}

// NewAccountCredentials builds the vault. durable is false when the key was generated at
// startup, in which case Put refuses so no credential is stored that a restart would lose.
func NewAccountCredentials(store storage.AccountCredentialStore, key []byte, durable bool) *AccountCredentials {
	return &AccountCredentials{store: store, key: key, durable: durable}
}

// Get returns the API view of an account's credential, or storage.ErrNotFound.
func (c *AccountCredentials) Get(ctx context.Context, accountID int64) (domain.AccountCredential, error) {
	rec, err := c.load(ctx, accountID)
	return rec.AccountCredential, err
}

// Put parses, checks the provider, seals, and stores. Errors: *CredentialInputError,
// ErrCredentialProviderMismatch, ErrCredentialKeyEphemeral, storage.ErrNotFound.
func (c *AccountCredentials) Put(ctx context.Context, account domain.Account, req domain.PutAccountCredentialRequest) (domain.AccountCredential, error) {
	p, err := parseCredentialRequest(req)
	if err != nil {
		return domain.AccountCredential{}, err
	}
	if err := checkProvider(p, account); err != nil {
		return domain.AccountCredential{}, err
	}
	if !c.durable {
		return domain.AccountCredential{}, ErrCredentialKeyEphemeral
	}
	plain, err := json.Marshal(p.canonical())
	if err != nil {
		return domain.AccountCredential{}, err
	}
	sealed, err := secrets.Encrypt(string(plain), c.key)
	if err != nil {
		return domain.AccountCredential{}, fmt.Errorf("seal credential: %w", err)
	}
	rec, err := c.store.PutAccountCredential(ctx, domain.SealedAccountCredential{
		AccountID:        account.ID,
		Type:             p.Type(),
		Summary:          p.summary(),
		PayloadEncrypted: sealed,
	})
	return rec.AccountCredential, err
}

// Delete removes the credential and reports whether one existed.
func (c *AccountCredentials) Delete(ctx context.Context, accountID int64) (bool, error) {
	return c.store.DeleteAccountCredential(ctx, accountID)
}

func (c *AccountCredentials) load(ctx context.Context, accountID int64) (domain.AccountCredentialRecord, error) {
	return c.store.GetAccountCredential(ctx, accountID)
}

func (c *AccountCredentials) unseal(rec domain.AccountCredentialRecord) (CredentialPayload, error) {
	plain, err := secrets.Decrypt(rec.PayloadEncrypted, c.key)
	if err != nil {
		return nil, ErrCredentialUnreadable
	}
	var req domain.PutAccountCredentialRequest
	if err := json.Unmarshal([]byte(plain), &req); err != nil {
		return nil, ErrCredentialUnreadable
	}
	p, err := parseCredentialRequest(req)
	if err != nil || p.Type() != rec.Type {
		return nil, ErrCredentialUnreadable
	}
	return p, nil
}

func (c *AccountCredentials) recordTest(ctx context.Context, rec domain.AccountCredentialRecord, test domain.CredentialTest) error {
	return c.store.RecordAccountCredentialTest(ctx, rec.AccountID, rec.PayloadEncrypted, test)
}
