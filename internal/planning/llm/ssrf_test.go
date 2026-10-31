package llm

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestIsBlockedAddr(t *testing.T) {
	for addr, want := range map[string]bool{
		"169.254.169.254":        true, // cloud metadata (IPv4)
		"169.254.1.2":            true,
		"fe80::1":                true,
		"fe80::1%eth0":           true,
		"fd00:ec2::254":          true, // EC2 metadata (IPv6)
		"::ffff:169.254.169.254": true, // IPv4-mapped
		"0.0.0.0":                true,
		"::":                     true,
		"127.0.0.1":              false,
		"::1":                    false,
		"10.1.2.3":               false,
		"192.168.1.5":            false,
		"8.8.8.8":                false,
		"2001:db8::1":            false,
		"fd00:ec2::255":          false,
		"::ffff:10.0.0.1":        false,
		"169.253.255.255":        false,
		"fec0::1":                false,
	} {
		if got := IsBlockedAddr(netip.MustParseAddr(addr)); got != want {
			t.Errorf("IsBlockedAddr(%s) = %v, want %v", addr, got, want)
		}
	}
}

func TestIsBlockedHost(t *testing.T) {
	for host, want := range map[string]bool{
		"169.254.169.254":            true,
		"metadata.google.internal":   true,
		"Metadata.Google.Internal.":  true,
		"metadata.google.internal..": false, // not a valid trailing-dot form of the name
		"localhost":                  false,
		"ollama":                     false,
		"127.0.0.1":                  false,
	} {
		if got := IsBlockedHost(host); got != want {
			t.Errorf("IsBlockedHost(%q) = %v, want %v", host, got, want)
		}
	}
}

func TestDialControlRefusesBlockedAddresses(t *testing.T) {
	for _, address := range []string{"169.254.169.254:80", "[fe80::1]:80", "0.0.0.0:80", "[fd00:ec2::254]:443"} {
		err := blockedAddrControl("tcp", address, nil)
		if !errors.Is(err, ErrBlockedAddress) {
			t.Fatalf("Control(%s) = %v, want ErrBlockedAddress", address, err)
		}
		if got := DiagnoseError(err, time.Minute); got != "endpoint resolves to a blocked address" {
			t.Fatalf("DiagnoseError(Control(%s)) = %q", address, got)
		}
	}
	for _, address := range []string{"127.0.0.1:1", "[::1]:11434", "10.0.0.5:8000"} {
		if err := blockedAddrControl("tcp", address, nil); err != nil {
			t.Fatalf("Control(%s) = %v, want nil", address, err)
		}
	}
	if err := blockedAddrControl("tcp", "not-an-address", nil); err == nil {
		t.Fatal("Control must refuse an address it cannot parse")
	}
}

// TestProviderRefusesDialToBlockedLiteralAddress drives the dialer end to end:
// no listener is needed because the connection is refused before it is made.
func TestProviderRefusesDialToBlockedLiteralAddress(t *testing.T) {
	p := NewOpenAIProvider(Config{Model: "m", Endpoint: "http://0.0.0.0:1/v1"})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := p.Complete(ctx, []Message{{Role: "user", Content: "hi"}}, Options{})
	if !errors.Is(err, ErrBlockedAddress) {
		t.Fatalf("Complete err = %v, want ErrBlockedAddress", err)
	}
	if got := DiagnoseError(err, time.Minute); got != "endpoint resolves to a blocked address" {
		t.Fatalf("DiagnoseError = %q", got)
	}
}

func TestProviderRefusesRedirectToBlockedAddress(t *testing.T) {
	var hits atomic.Int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		http.Redirect(w, r, "http://169.254.169.254/latest/meta-data/", http.StatusFound)
	}))
	defer ts.Close()
	p := NewOpenAIProvider(Config{APIKey: "sk-secret", Model: "m", Endpoint: ts.URL + "/v1"})
	msgs := []Message{{Role: "user", Content: "hi"}}

	_, err := p.Complete(context.Background(), msgs, Options{})
	if !errors.Is(err, ErrBlockedAddress) {
		t.Fatalf("Complete err = %v, want ErrBlockedAddress", err)
	}
	if strings.Contains(err.Error(), "sk-secret") {
		t.Fatal("error leaks the api key")
	}
	if _, err := p.StreamComplete(context.Background(), msgs, Options{}); !errors.Is(err, ErrBlockedAddress) {
		t.Fatalf("StreamComplete err = %v, want ErrBlockedAddress", err)
	}
	if got := hits.Load(); got != 2 {
		t.Fatalf("origin requests = %d, want exactly one per call (no retry, no follow)", got)
	}
}

func TestProviderStopsAfterThreeRedirects(t *testing.T) {
	var hits atomic.Int32
	var ts *httptest.Server
	ts = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := hits.Add(1)
		http.Redirect(w, r, fmt.Sprintf("%s/hop/%d", ts.URL, n), http.StatusFound)
	}))
	defer ts.Close()
	p := NewOpenAIProvider(Config{Model: "m", Endpoint: ts.URL + "/v1"})

	_, err := p.Complete(context.Background(), []Message{{Role: "user", Content: "hi"}}, Options{})
	if err == nil {
		t.Fatal("Complete followed an endless redirect chain")
	}
	if errors.Is(err, ErrBlockedAddress) {
		t.Fatalf("redirect limit reported as a blocked address: %v", err)
	}
	// The first request plus exactly three followed redirects; the fourth
	// redirect is refused without being followed.
	if got := hits.Load(); got != 4 {
		t.Fatalf("origin requests = %d, want exactly 4", got)
	}
}

// proxyRecorder is an httptest server standing in for an egress proxy: it
// counts the requests it receives and answers each with a valid completion.
func proxyRecorder(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"via proxy"},"finish_reason":"stop"}]}`)
	}))
	t.Cleanup(proxy.Close)
	return proxy, &hits
}

// forceProxy makes every provider request select proxy for the test's
// duration. http.ProxyFromEnvironment caches the environment on first use in
// a process, so the package-level hook is replaced instead of setting
// HTTP_PROXY.
func forceProxy(t *testing.T, proxy *httptest.Server) {
	t.Helper()
	proxyURL, err := url.Parse(proxy.URL)
	if err != nil {
		t.Fatal(err)
	}
	prev := proxyFromEnvironment
	proxyFromEnvironment = func(*http.Request) (*url.URL, error) { return proxyURL, nil }
	t.Cleanup(func() { proxyFromEnvironment = prev })
}

func TestProviderRefusesBlockedAddressBeforeProxy(t *testing.T) {
	proxy, hits := proxyRecorder(t)
	forceProxy(t, proxy)

	p := NewOpenAIProvider(Config{APIKey: "sk-secret", Model: "m", Endpoint: "http://169.254.169.254/v1"})
	_, err := p.Complete(context.Background(), []Message{{Role: "user", Content: "hi"}}, Options{})
	if !errors.Is(err, ErrBlockedAddress) {
		t.Fatalf("Complete err = %v, want ErrBlockedAddress", err)
	}
	if strings.Contains(err.Error(), "sk-secret") {
		t.Fatal("error leaks the api key")
	}
	if got := DiagnoseError(err, time.Minute); got != "endpoint resolves to a blocked address" {
		t.Fatalf("DiagnoseError = %q", got)
	}
	if got := hits.Load(); got != 0 {
		t.Fatalf("proxy requests = %d, want 0: a blocked host must be refused before the proxy is used", got)
	}
}

func TestProviderUsesProxyForAllowedHost(t *testing.T) {
	proxy, hits := proxyRecorder(t)
	forceProxy(t, proxy)

	// localhost resolves to loopback, which stays allowed. Nothing listens on
	// the port: the proxy answers, which proves the request went through it.
	p := NewOpenAIProvider(Config{Model: "m", Endpoint: "http://localhost:9/v1"})
	resp, err := p.Complete(context.Background(), []Message{{Role: "user", Content: "hi"}}, Options{})
	if err != nil {
		t.Fatalf("Complete through the proxy: %v", err)
	}
	if resp.Content != "via proxy" {
		t.Fatalf("content = %q, want the proxy's reply", resp.Content)
	}
	if got := hits.Load(); got != 1 {
		t.Fatalf("proxy requests = %d, want exactly 1", got)
	}
}

func TestProviderRefusesRedirectToNonHTTPScheme(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", "ftp://files.example/v1/chat/completions")
		w.WriteHeader(http.StatusFound)
	}))
	defer ts.Close()
	p := NewOpenAIProvider(Config{Model: "m", Endpoint: ts.URL + "/v1"})
	if _, err := p.Complete(context.Background(), []Message{{Role: "user", Content: "hi"}}, Options{}); err == nil {
		t.Fatal("Complete followed a redirect to a non-http scheme")
	}
}

func TestProviderFollowsRedirectToAllowedHost(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"moved"},"finish_reason":"stop"}]}`)
	}))
	defer target.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+r.URL.Path, http.StatusTemporaryRedirect)
	}))
	defer origin.Close()

	p := NewOpenAIProvider(Config{Model: "m", Endpoint: origin.URL + "/v1"})
	resp, err := p.Complete(context.Background(), []Message{{Role: "user", Content: "hi"}}, Options{})
	if err != nil {
		t.Fatalf("Complete through an allowed redirect: %v", err)
	}
	if resp.Content != "moved" {
		t.Fatalf("content = %q, want the redirected reply", resp.Content)
	}
}
