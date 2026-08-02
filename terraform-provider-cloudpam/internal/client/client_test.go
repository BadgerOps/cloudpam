package client

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newTestClient(t *testing.T, h http.Handler) (*Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c, err := New(srv.URL, "cpam_testkey")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return c, srv
}

func TestNewValidatesInput(t *testing.T) {
	tests := []struct {
		name     string
		endpoint string
		apiKey   string
		wantErr  string
	}{
		{name: "empty endpoint", endpoint: "", apiKey: "cpam_x", wantErr: "endpoint is required"},
		{name: "missing scheme", endpoint: "cloudpam.example.com", apiKey: "cpam_x", wantErr: "scheme must be http or https"},
		{name: "bad scheme", endpoint: "ftp://cloudpam.example.com", apiKey: "cpam_x", wantErr: "scheme must be http or https"},
		{name: "missing host", endpoint: "https://", apiKey: "cpam_x", wantErr: "missing host"},
		{name: "empty api key", endpoint: "https://cloudpam.example.com", apiKey: "   ", wantErr: "api_key is required"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := New(tc.endpoint, tc.apiKey)
			if err == nil {
				t.Fatalf("New() expected error containing %q, got nil", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("New() error = %q, want it to contain %q", err.Error(), tc.wantErr)
			}
		})
	}
}

func TestNewNormalisesEndpoint(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{in: "https://cloudpam.example.com", want: "https://cloudpam.example.com"},
		{in: "https://cloudpam.example.com/", want: "https://cloudpam.example.com"},
		{in: "https://cloudpam.example.com/api/v1", want: "https://cloudpam.example.com"},
		{in: "https://cloudpam.example.com/api/v1/", want: "https://cloudpam.example.com"},
		{in: "http://localhost:8080/cloudpam", want: "http://localhost:8080/cloudpam"},
		{in: " https://cloudpam.example.com ", want: "https://cloudpam.example.com"},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			c, err := New(tc.in, "cpam_x")
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			if got := c.BaseURL(); got != tc.want {
				t.Fatalf("BaseURL() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestRequestHeaders asserts the provider authenticates exactly the way
// DualAuthMiddleware expects: Authorization: Bearer cpam_...
func TestRequestHeaders(t *testing.T) {
	var gotAuth, gotAccept, gotUA, gotContentType, gotPath string
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotAccept = r.Header.Get("Accept")
		gotUA = r.Header.Get("User-Agent")
		gotContentType = r.Header.Get("Content-Type")
		gotPath = r.URL.Path
		writeJSON(t, w, http.StatusCreated, Pool{ID: 1, Name: "p", CIDR: "10.0.0.0/16"})
	}))

	if _, err := c.CreatePool(context.Background(), PoolCreate{Name: "p", CIDR: "10.0.0.0/16"}); err != nil {
		t.Fatalf("CreatePool() error = %v", err)
	}

	if gotAuth != "Bearer cpam_testkey" {
		t.Errorf("Authorization = %q, want %q", gotAuth, "Bearer cpam_testkey")
	}
	if gotAccept != "application/json" {
		t.Errorf("Accept = %q, want application/json", gotAccept)
	}
	if gotContentType != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", gotContentType)
	}
	if !strings.HasPrefix(gotUA, "terraform-provider-cloudpam") {
		t.Errorf("User-Agent = %q, want terraform-provider-cloudpam prefix", gotUA)
	}
	if gotPath != "/api/v1/pools" {
		t.Errorf("path = %q, want /api/v1/pools", gotPath)
	}
}

func TestWithUserAgent(t *testing.T) {
	var gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		writeJSON(t, w, http.StatusOK, []Pool{})
	}))
	defer srv.Close()

	c, err := New(srv.URL, "cpam_x", WithUserAgent("terraform-provider-cloudpam/1.2.3"))
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if _, err := c.ListPools(context.Background()); err != nil {
		t.Fatalf("ListPools() error = %v", err)
	}
	if gotUA != "terraform-provider-cloudpam/1.2.3" {
		t.Fatalf("User-Agent = %q", gotUA)
	}
}

func TestAPIErrorMapping(t *testing.T) {
	tests := []struct {
		name        string
		status      int
		body        string
		contentType string
		wantMessage string
		wantDetail  string
		wantBody    string
		wantInError []string
	}{
		{
			name:        "structured api error with detail",
			status:      http.StatusBadRequest,
			body:        `{"error":"invalid sub-pool cidr","detail":"child not within parent"}`,
			wantMessage: "invalid sub-pool cidr",
			wantDetail:  "child not within parent",
			wantInError: []string{"400", "invalid sub-pool cidr", "child not within parent"},
		},
		{
			name:        "structured api error without detail",
			status:      http.StatusConflict,
			body:        `{"error":"pool has children"}`,
			wantMessage: "pool has children",
			wantInError: []string{"409", "pool has children"},
		},
		{
			name:        "unauthorized",
			status:      http.StatusUnauthorized,
			body:        `{"error":"unauthorized","detail":"invalid API key"}`,
			wantMessage: "unauthorized",
			wantDetail:  "invalid API key",
			wantInError: []string{"401", "unauthorized", "invalid API key"},
		},
		{
			name:        "non json body falls back to raw text",
			status:      http.StatusBadGateway,
			body:        "upstream exploded",
			wantBody:    "upstream exploded",
			wantInError: []string{"502", "upstream exploded"},
		},
		{
			name:        "empty body falls back to status text",
			status:      http.StatusInternalServerError,
			body:        "",
			wantInError: []string{"500", "Internal Server Error"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))

			_, err := c.GetPool(context.Background(), 7)
			if err == nil {
				t.Fatal("GetPool() expected error, got nil")
			}
			var apiErr *APIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("error %v is not *APIError", err)
			}
			if apiErr.StatusCode != tc.status {
				t.Errorf("StatusCode = %d, want %d", apiErr.StatusCode, tc.status)
			}
			if apiErr.Message != tc.wantMessage {
				t.Errorf("Message = %q, want %q", apiErr.Message, tc.wantMessage)
			}
			if apiErr.Detail != tc.wantDetail {
				t.Errorf("Detail = %q, want %q", apiErr.Detail, tc.wantDetail)
			}
			if tc.wantBody != "" && apiErr.Body != tc.wantBody {
				t.Errorf("Body = %q, want %q", apiErr.Body, tc.wantBody)
			}
			if apiErr.Method != http.MethodGet {
				t.Errorf("Method = %q, want GET", apiErr.Method)
			}
			if apiErr.Path != "/api/v1/pools/7" {
				t.Errorf("Path = %q, want /api/v1/pools/7", apiErr.Path)
			}
			for _, want := range tc.wantInError {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q missing %q", err.Error(), want)
				}
			}
			// Only 404 responses should satisfy ErrNotFound.
			if IsNotFound(err) {
				t.Errorf("IsNotFound() = true for status %d", tc.status)
			}
		})
	}
}

func TestNotFoundIsErrNotFound(t *testing.T) {
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeRaw(t, w, http.StatusNotFound, `{"error":"not found"}`)
	}))

	for _, tc := range []struct {
		name string
		call func() error
	}{
		{"GetPool", func() error { _, err := c.GetPool(context.Background(), 1); return err }},
		{"GetAccount", func() error { _, err := c.GetAccount(context.Background(), 1); return err }},
		{"UpdatePool", func() error { _, err := c.UpdatePool(context.Background(), 1, PoolUpdate{}); return err }},
		{"UpdateAccount", func() error { _, err := c.UpdateAccount(context.Background(), 1, AccountUpdate{}); return err }},
		{"DeletePool", func() error { return c.DeletePool(context.Background(), 1, false) }},
		{"DeleteAccount", func() error { return c.DeleteAccount(context.Background(), 1, false) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.call()
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !errors.Is(err, ErrNotFound) {
				t.Fatalf("errors.Is(err, ErrNotFound) = false for %v", err)
			}
			if !IsNotFound(err) {
				t.Fatalf("IsNotFound() = false for %v", err)
			}
		})
	}
}

func TestTransportErrorIsNotNotFound(t *testing.T) {
	c, err := New("http://127.0.0.1:1", "cpam_x")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	_, err = c.GetPool(context.Background(), 1)
	if err == nil {
		t.Fatal("expected transport error, got nil")
	}
	if IsNotFound(err) {
		t.Fatalf("IsNotFound() = true for transport error %v", err)
	}
	if !strings.Contains(err.Error(), "/api/v1/pools/1") {
		t.Fatalf("error %q should mention the request path", err.Error())
	}
}

func TestContextCancellationIsPropagated(t *testing.T) {
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusOK, Pool{ID: 1})
	}))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.GetPool(ctx, 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("GetPool() error = %v, want context.Canceled", err)
	}
}

func TestDecodeErrorIsReported(t *testing.T) {
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeRaw(t, w, http.StatusOK, `{"id": "not-a-number"}`)
	}))
	_, err := c.GetPool(context.Background(), 1)
	if err == nil {
		t.Fatal("expected decode error, got nil")
	}
	if !strings.Contains(err.Error(), "decode response") {
		t.Fatalf("error = %q, want it to mention decode response", err.Error())
	}
}
