package gcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"golang.org/x/oauth2"

	"cloudpam/internal/discovery"
)

const (
	iamCredentialsBaseURL = "https://iamcredentials.googleapis.com/v1"

	// IAMCredentialsScope is the scope the caller's credentials need to mint an
	// impersonated token.
	IAMCredentialsScope = "https://www.googleapis.com/auth/cloud-platform"

	projectPlaceholder = "{project}"
)

var serviceAccountEmail = regexp.MustCompile(`^[^@\s{}]+@[^@\s{}]+\.[^@\s{}]+$`)

// ServiceAccountFor substitutes project into an impersonation template. A
// template without the {project} placeholder names one shared service account.
func ServiceAccountFor(template, project string) string {
	return strings.ReplaceAll(template, projectPlaceholder, project)
}

// CheckServiceAccountTemplate rejects a template that does not yield an email
// address once {project} is substituted.
func CheckServiceAccountTemplate(template string) error {
	if !serviceAccountEmail.MatchString(ServiceAccountFor(template, "project-id")) {
		return fmt.Errorf("service account %q is not an email address (use {project} for the project id)", template)
	}
	return nil
}

// ImpersonatedTokenSource returns a token source that mints Compute read-only
// access tokens for email through the IAM Credentials API. client must be
// authenticated as an identity holding roles/iam.serviceAccountTokenCreator on
// email, with IAMCredentialsScope. Tokens are cached until they expire.
func ImpersonatedTokenSource(ctx context.Context, client *http.Client, email string, limiter *discovery.Limiter) oauth2.TokenSource {
	return oauth2.ReuseTokenSource(nil, &impersonatedSource{ctx: ctx, client: client, email: email, limiter: limiter})
}

type impersonatedSource struct {
	ctx     context.Context
	client  *http.Client
	email   string
	limiter *discovery.Limiter
}

func (s *impersonatedSource) Token() (*oauth2.Token, error) {
	if err := s.limiter.Wait(s.ctx); err != nil {
		return nil, err
	}
	body, err := json.Marshal(map[string]any{
		"scope":    []string{computeScope},
		"lifetime": "3600s",
	})
	if err != nil {
		return nil, err
	}
	endpoint := fmt.Sprintf("%s/projects/-/serviceAccounts/%s:generateAccessToken", iamCredentialsBaseURL, url.PathEscape(s.email))
	req, err := http.NewRequestWithContext(s.ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("impersonate %s: build request: %w", s.email, err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("impersonate %s: %w", s.email, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("impersonate %s: %w", s.email, &discovery.HTTPError{Status: resp.StatusCode})
	}

	var out struct {
		AccessToken string    `json:"accessToken"`
		ExpireTime  time.Time `json:"expireTime"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("impersonate %s: decode response: %w", s.email, err)
	}
	if out.AccessToken == "" {
		return nil, fmt.Errorf("impersonate %s: response carried no access token", s.email)
	}
	return &oauth2.Token{AccessToken: out.AccessToken, TokenType: "Bearer", Expiry: out.ExpireTime}, nil
}
