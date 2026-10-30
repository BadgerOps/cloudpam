package api_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"cloudpam/internal/api"
	"cloudpam/internal/audit"
	"cloudpam/internal/auth"
	"cloudpam/internal/testutil"
)

func TestAuditEventsCarryClientIP(t *testing.T) {
	tests := []struct {
		name           string
		trustedProxies string
		remoteAddr     string
		forwardedFor   string
		wantIP         string
	}{
		{
			name:       "direct peer",
			remoteAddr: "203.0.113.7:51000",
			wantIP:     "203.0.113.7",
		},
		{
			name:           "forwarded by trusted proxy",
			trustedProxies: "10.0.0.0/8",
			remoteAddr:     "10.0.0.5:443",
			forwardedFor:   "198.51.100.23",
			wantIP:         "198.51.100.23",
		},
		{
			name:           "forwarded header from untrusted peer",
			trustedProxies: "10.0.0.0/8",
			remoteAddr:     "203.0.113.7:51000",
			forwardedFor:   "198.51.100.23",
			wantIP:         "203.0.113.7",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			proxies, err := api.ParseTrustedProxies(tt.trustedProxies)
			if err != nil {
				t.Fatalf("parse trusted proxies: %v", err)
			}
			c := testutil.NewTestServer(t, testutil.TestServerConfig{
				EnableAuth:     true,
				EnableAudit:    true,
				TrustedProxies: proxies,
			})
			defer c.Cleanup()

			createLoginUser(t, c.UserStore, "auditee", "correct-horse-battery")
			login := serveFrom(c, tt.remoteAddr, tt.forwardedFor, http.MethodPost, "/api/v1/auth/login", "",
				`{"username":"auditee","password":"correct-horse-battery"}`)
			if login.Code != http.StatusOK {
				t.Fatalf("login status = %d, body %s", login.Code, login.Body.String())
			}

			apiKey, _ := testutil.CreateTestAPIKey(t, c.KeyStore, "pool writer", []string{"pools:write"})
			create := serveFrom(c, tt.remoteAddr, tt.forwardedFor, http.MethodPost, "/api/v1/pools", apiKey,
				`{"name":"audited","cidr":"10.42.0.0/16"}`)
			if create.Code != http.StatusCreated {
				t.Fatalf("pool create status = %d, body %s", create.Code, create.Body.String())
			}

			assertEventIPs(t, c.AuditLogger, audit.ActionLogin, audit.ResourceSession, tt.wantIP)
			assertEventIPs(t, c.AuditLogger, audit.ActionCreate, audit.ResourcePool, tt.wantIP)
		})
	}
}

func createLoginUser(t *testing.T, store auth.UserStore, username, password string) {
	t.Helper()
	hash, err := auth.HashPassword(password)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	now := time.Now().UTC()
	if err := store.Create(context.Background(), &auth.User{
		ID:           "user-" + username,
		Username:     username,
		Email:        username + "@example.test",
		Role:         auth.RoleAdmin,
		PasswordHash: hash,
		IsActive:     true,
		CreatedAt:    now,
		UpdatedAt:    now,
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}
}

func serveFrom(c *testutil.TestServerComponents, remoteAddr, forwardedFor, method, path, apiKey, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.RemoteAddr = remoteAddr
	req.Header.Set("Content-Type", "application/json")
	if forwardedFor != "" {
		req.Header.Set("X-Forwarded-For", forwardedFor)
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	rec := httptest.NewRecorder()
	c.Server.Config.Handler.ServeHTTP(rec, req)
	return rec
}

func assertEventIPs(t *testing.T, logger audit.AuditLogger, action, resourceType, wantIP string) {
	t.Helper()
	events, _, err := logger.List(context.Background(), audit.ListOptions{
		Limit:        100,
		Action:       action,
		ResourceType: resourceType,
	})
	if err != nil {
		t.Fatalf("list audit events: %v", err)
	}
	if len(events) == 0 {
		t.Fatalf("no %s/%s audit event recorded", action, resourceType)
	}
	for _, e := range events {
		if e.IPAddress != wantIP {
			t.Errorf("%s/%s audit event ip_address = %q, want %q", action, resourceType, e.IPAddress, wantIP)
		}
	}
}
