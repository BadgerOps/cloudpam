package oidc

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
)

// mockOIDCServer creates an httptest.Server that serves OIDC discovery,
// JWKS, and token endpoints. It returns the server and a cleanup function.
func mockOIDCServer(t *testing.T) (*httptest.Server, *rsa.PrivateKey) {
	t.Helper()

	privKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}

	var srv *httptest.Server

	mux := http.NewServeMux()

	mux.HandleFunc("GET /.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		discovery := map[string]interface{}{
			"issuer":                 srv.URL,
			"authorization_endpoint": srv.URL + "/authorize",
			"token_endpoint":         srv.URL + "/token",
			"jwks_uri":               srv.URL + "/keys",
			"id_token_signing_alg_values_supported": []string{"RS256"},
			"subject_types_supported":               []string{"public"},
			"response_types_supported":              []string{"code"},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(discovery)
	})

	mux.HandleFunc("GET /keys", func(w http.ResponseWriter, r *http.Request) {
		jwks := jose.JSONWebKeySet{
			Keys: []jose.JSONWebKey{
				{
					Key:       &privKey.PublicKey,
					KeyID:     "test-key-1",
					Algorithm: string(jose.RS256),
					Use:       "sig",
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(jwks)
	})

	mux.HandleFunc("POST /token", func(w http.ResponseWriter, r *http.Request) {
		// Build a signed JWT ID token
		signerKey := jose.SigningKey{Algorithm: jose.RS256, Key: privKey}
		signerOpts := (&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", "test-key-1")
		signer, err := jose.NewSigner(signerKey, signerOpts)
		if err != nil {
			http.Error(w, fmt.Sprintf("create signer: %v", err), http.StatusInternalServerError)
			return
		}

		now := time.Now()
		claims := jwt.Claims{
			Issuer:    srv.URL,
			Subject:   "user-123",
			Audience:  jwt.Audience{"test-client-id"},
			IssuedAt:  jwt.NewNumericDate(now),
			Expiry:    jwt.NewNumericDate(now.Add(time.Hour)),
			NotBefore: jwt.NewNumericDate(now.Add(-time.Minute)),
		}
		extraClaims := map[string]interface{}{
			"email":  "alice@example.com",
			"name":   "Alice",
			"groups": []string{"cloudpam-admins", "developers"},
		}

		rawJWT, err := jwt.Signed(signer).Claims(claims).Claims(extraClaims).Serialize()
		if err != nil {
			http.Error(w, fmt.Sprintf("sign jwt: %v", err), http.StatusInternalServerError)
			return
		}

		tokenResponse := map[string]interface{}{
			"access_token": "mock-access-token",
			"token_type":   "Bearer",
			"id_token":     rawJWT,
			"expires_in":   3600,
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(tokenResponse)
	})

	srv = httptest.NewServer(mux)
	return srv, privKey
}

func TestNewProvider_Discovery(t *testing.T) {
	srv, _ := mockOIDCServer(t)
	defer srv.Close()

	ctx := context.Background()
	prov, err := NewProvider(ctx, ProviderConfig{
		IssuerURL:    srv.URL,
		ClientID:     "test-client-id",
		ClientSecret: "test-secret",
		RedirectURL:  "http://localhost:8080/auth/callback",
	})
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	if prov == nil {
		t.Fatal("expected non-nil provider")
	}
}

func TestNewProvider_InvalidIssuer(t *testing.T) {
	ctx := context.Background()
	_, err := NewProvider(ctx, ProviderConfig{
		IssuerURL:    "http://127.0.0.1:1/nonexistent",
		ClientID:     "test-client-id",
		ClientSecret: "test-secret",
		RedirectURL:  "http://localhost:8080/auth/callback",
	})
	if err == nil {
		t.Fatal("expected error for invalid issuer URL")
	}
}

func TestAuthCodeURL(t *testing.T) {
	srv, _ := mockOIDCServer(t)
	defer srv.Close()

	ctx := context.Background()
	prov, err := NewProvider(ctx, ProviderConfig{
		IssuerURL:    srv.URL,
		ClientID:     "test-client-id",
		ClientSecret: "test-secret",
		RedirectURL:  "http://localhost:8080/auth/callback",
		Scopes:       []string{"openid", "profile", "email"},
	})
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}

	url := prov.AuthCodeURL("random-state-123")

	checks := []string{
		"client_id=test-client-id",
		"redirect_uri=",
		"state=random-state-123",
		"scope=",
		"response_type=code",
	}
	for _, check := range checks {
		if !strings.Contains(url, check) {
			t.Errorf("AuthCodeURL missing %q in URL: %s", check, url)
		}
	}
}

func TestExchange_ValidCode(t *testing.T) {
	srv, _ := mockOIDCServer(t)
	defer srv.Close()

	ctx := context.Background()
	prov, err := NewProvider(ctx, ProviderConfig{
		IssuerURL:    srv.URL,
		ClientID:     "test-client-id",
		ClientSecret: "test-secret",
		RedirectURL:  "http://localhost:8080/auth/callback",
	})
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}

	claims, err := prov.Exchange(ctx, "mock-auth-code")
	if err != nil {
		t.Fatalf("Exchange: %v", err)
	}

	if claims.Subject != "user-123" {
		t.Errorf("expected sub=user-123, got %q", claims.Subject)
	}
	if claims.Email != "alice@example.com" {
		t.Errorf("expected email=alice@example.com, got %q", claims.Email)
	}
	if claims.Name != "Alice" {
		t.Errorf("expected name=Alice, got %q", claims.Name)
	}
	if claims.Issuer != srv.URL {
		t.Errorf("expected issuer=%s, got %q", srv.URL, claims.Issuer)
	}
	if len(claims.Groups) != 2 {
		t.Errorf("expected 2 groups, got %d", len(claims.Groups))
	}
}
