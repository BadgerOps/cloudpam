package llm

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"syscall"
	"time"
)

// ErrBlockedAddress reports a connection or redirect to an address the AI
// provider client refuses: link-local and cloud metadata addresses, and the
// unspecified address. It is raised at connect time, so a host name that
// resolves to such an address is refused as well as a literal one.
var ErrBlockedAddress = errors.New("endpoint resolves to a blocked address")

// ec2IPv6Metadata is the IPv6 address of the EC2 instance metadata service.
var ec2IPv6Metadata = netip.MustParseAddr("fd00:ec2::254")

// gcpMetadataHost is the host name of the GCE metadata service.
const gcpMetadataHost = "metadata.google.internal"

// maxRedirects bounds how many redirects one provider request follows.
const maxRedirects = 3

// IsBlockedAddr reports an address the provider client must not connect to:
// link-local unicast (169.254.0.0/16, which holds the cloud metadata services,
// and fe80::/10), the EC2 IPv6 metadata address and the unspecified address
// (0.0.0.0, ::). Loopback and private ranges stay allowed for local Ollama or
// vLLM. IPv4-mapped IPv6 addresses and zones are normalized first.
func IsBlockedAddr(addr netip.Addr) bool {
	addr = addr.Unmap().WithZone("")
	return addr.IsLinkLocalUnicast() || addr.IsUnspecified() || addr == ec2IPv6Metadata
}

// IsBlockedHost reports a URL host that is a blocked literal address or the
// GCE metadata host name (case-insensitive, with one trailing dot allowed).
// Other host names are not resolved here; the dialer checks what they resolve
// to.
func IsBlockedHost(host string) bool {
	if addr, err := netip.ParseAddr(host); err == nil {
		return IsBlockedAddr(addr)
	}
	return strings.TrimSuffix(strings.ToLower(host), ".") == gcpMetadataHost
}

// blockedAddrControl is the net.Dialer Control hook: address is the resolved
// "host:port" about to be connected, so a host name resolving to a metadata
// address is refused here.
func blockedAddrControl(_, address string, _ syscall.RawConn) error {
	ap, err := netip.ParseAddrPort(address)
	if err != nil {
		return fmt.Errorf("refusing dial to unparsable address: %w", err)
	}
	if IsBlockedAddr(ap.Addr()) {
		return ErrBlockedAddress
	}
	return nil
}

// checkRedirect follows at most maxRedirects redirects and only to http(s)
// URLs whose host is not a blocked literal address; resolution is covered by
// the dialer. The http.Client keeps its default behaviour of dropping the
// Authorization header on cross-host redirects.
func checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= maxRedirects {
		return fmt.Errorf("stopped after %d redirects", maxRedirects)
	}
	return validateRedirectTarget(req.URL)
}

func validateRedirectTarget(u *url.URL) error {
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("refusing redirect to %s url", u.Scheme)
	}
	if IsBlockedHost(u.Hostname()) {
		return ErrBlockedAddress
	}
	return nil
}

// newHTTPClient builds the provider's HTTP client: the default transport with
// a dialer that refuses blocked addresses, and the redirect policy above.
func newHTTPClient() *http.Client {
	dialer := &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
		Control:   blockedAddrControl,
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = dialer.DialContext
	return &http.Client{Transport: transport, CheckRedirect: checkRedirect}
}
