package oidc

import (
	"context"
	"fmt"

	gooidc "github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

// ProviderConfig holds configuration for creating an OIDC provider.
type ProviderConfig struct {
	IssuerURL    string
	ClientID     string
	ClientSecret string
	RedirectURL  string
	Scopes       []string // e.g., ["openid", "profile", "email"]
}

// Provider wraps OIDC discovery, token verification, and OAuth2 config.
type Provider struct {
	oidcProvider *gooidc.Provider
	verifier     *gooidc.IDTokenVerifier
	oauth2Config oauth2.Config
}

// NewProvider creates a Provider by performing OIDC discovery on the issuer URL.
func NewProvider(ctx context.Context, cfg ProviderConfig) (*Provider, error) {
	oidcProv, err := gooidc.NewProvider(ctx, cfg.IssuerURL)
	if err != nil {
		return nil, fmt.Errorf("oidc discovery: %w", err)
	}

	scopes := cfg.Scopes
	if len(scopes) == 0 {
		scopes = []string{gooidc.ScopeOpenID, "profile", "email"}
	}

	oauth2Cfg := oauth2.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		RedirectURL:  cfg.RedirectURL,
		Endpoint:     oidcProv.Endpoint(),
		Scopes:       scopes,
	}

	verifier := oidcProv.Verifier(&gooidc.Config{
		ClientID: cfg.ClientID,
	})

	return &Provider{
		oidcProvider: oidcProv,
		verifier:     verifier,
		oauth2Config: oauth2Cfg,
	}, nil
}

// AuthCodeURL generates the IdP redirect URL with the given state and options.
func (p *Provider) AuthCodeURL(state string, opts ...oauth2.AuthCodeOption) string {
	return p.oauth2Config.AuthCodeURL(state, opts...)
}

// Exchange exchanges an authorization code for tokens, verifies the ID token,
// and extracts claims.
func (p *Provider) Exchange(ctx context.Context, code string) (*Claims, error) {
	token, err := p.oauth2Config.Exchange(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("token exchange: %w", err)
	}

	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok {
		return nil, fmt.Errorf("no id_token in response")
	}

	idToken, err := p.verifier.Verify(ctx, rawIDToken)
	if err != nil {
		return nil, fmt.Errorf("verify id_token: %w", err)
	}

	var claims Claims
	if err := idToken.Claims(&claims); err != nil {
		return nil, fmt.Errorf("extract claims: %w", err)
	}
	claims.Issuer = idToken.Issuer

	return &claims, nil
}
