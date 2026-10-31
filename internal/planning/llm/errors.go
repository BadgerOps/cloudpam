package llm

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"time"
)

// ErrInvalidResponse marks a provider reply that could not be decoded or held
// no usable completion.
var ErrInvalidResponse = errors.New("invalid response from provider")

// StatusError reports a non-200 HTTP status from the provider. It deliberately
// carries no response body: provider bodies can echo request details.
type StatusError struct {
	Status int
}

func (e *StatusError) Error() string { return fmt.Sprintf("api error (status %d)", e.Status) }

// discardBody drains a bounded amount of an unused body so the connection can
// be reused, without keeping any of its content.
func discardBody(r io.Reader) {
	_, _ = io.Copy(io.Discard, io.LimitReader(r, 64<<10))
}

// DiagnoseError maps a provider error to a short, sanitized message that is
// safe to return to clients and store: it never includes response bodies, URLs,
// headers or keys. timeout is the deadline the caller applied.
func DiagnoseError(err error, timeout time.Duration) string {
	if err == nil {
		return ""
	}
	var statusErr *StatusError
	if errors.As(err, &statusErr) {
		return fmt.Sprintf("provider returned HTTP %d", statusErr.Status)
	}
	var netErr net.Error
	if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &netErr) && netErr.Timeout()) {
		return fmt.Sprintf("request timed out after %s", timeout)
	}
	if errors.Is(err, ErrInvalidResponse) {
		return "invalid response from provider"
	}
	var opErr *net.OpError
	var dnsErr *net.DNSError
	if errors.As(err, &opErr) || errors.As(err, &dnsErr) {
		return "endpoint unreachable"
	}
	return "provider request failed"
}
