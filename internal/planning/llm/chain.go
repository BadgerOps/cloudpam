package llm

import (
	"context"
	"errors"
	"net"
)

// Link is one provider in a Chain. Label names it in notices and test
// results, as "<kind> <model>", and never carries a key or endpoint.
type Link struct {
	Provider Provider
	Label    string
}

// FailedLink is one link the chain gave up on before a later link answered.
// Err is the raw provider error; callers sanitize it with DiagnoseError.
type FailedLink struct {
	Label string
	Err   error
}

// FallbackNotice is sent as the first event of a stream that a later link
// answered after an earlier one failed.
type FallbackNotice struct {
	Failed   []FailedLink
	Provider string // label of the link now answering
}

// Chain is a Provider over an ordered list of providers. A request goes to
// the first available link; when that link cannot be reached or answers 5xx
// the next link is tried, and the stream then opens with a FallbackNotice.
// A failure after streaming has started is not retried elsewhere, because
// part of the answer has already been delivered.
type Chain struct {
	links []Link
}

// NewChain returns the single provider itself when links has one entry, so
// a configuration without fallbacks behaves exactly as before.
func NewChain(links []Link) Provider {
	if len(links) == 1 {
		return links[0].Provider
	}
	return &Chain{links: links}
}

// Links returns the chain's links in order, or just p when p is not a chain,
// so callers that test each provider on its own need no type switch.
func Links(p Provider, label string) []Link {
	if c, ok := p.(*Chain); ok {
		return c.links
	}
	return []Link{{Provider: p, Label: label}}
}

func (c *Chain) Name() string { return c.links[0].Provider.Name() }

// Available reports whether any link is configured.
func (c *Chain) Available() bool {
	for _, l := range c.links {
		if l.Provider.Available() {
			return true
		}
	}
	return false
}

var errNoProviderAvailable = errors.New("no provider available")

// fallbackEligible reports whether err is a failure the next link may
// recover from: the provider could not be reached (dial, DNS or timeout) or
// answered 5xx. A cancelled context, a refused (blocked) address, a 4xx and a
// malformed reply are not retried elsewhere.
func fallbackEligible(ctx context.Context, err error) bool {
	if ctx.Err() != nil || errors.Is(err, ErrBlockedAddress) {
		return false
	}
	var statusErr *StatusError
	if errors.As(err, &statusErr) {
		return statusErr.Status >= 500
	}
	var opErr *net.OpError
	var dnsErr *net.DNSError
	var netErr net.Error
	return errors.As(err, &opErr) || errors.As(err, &dnsErr) || (errors.As(err, &netErr) && netErr.Timeout())
}

func (c *Chain) Complete(ctx context.Context, messages []Message, opts Options) (*Response, error) {
	err := errNoProviderAvailable
	for _, l := range c.links {
		if !l.Provider.Available() {
			continue
		}
		var out *Response
		out, err = l.Provider.Complete(ctx, messages, opts)
		if err == nil {
			return out, nil
		}
		if !fallbackEligible(ctx, err) {
			return nil, err
		}
	}
	return nil, err
}

func (c *Chain) StreamComplete(ctx context.Context, messages []Message, opts Options) (<-chan StreamEvent, error) {
	err := errNoProviderAvailable
	var failed []FailedLink
	for _, l := range c.links {
		if !l.Provider.Available() {
			continue
		}
		var events <-chan StreamEvent
		events, err = l.Provider.StreamComplete(ctx, messages, opts)
		if err == nil {
			if len(failed) == 0 {
				return events, nil
			}
			return withNotice(events, &FallbackNotice{Failed: failed, Provider: l.Label}), nil
		}
		if !fallbackEligible(ctx, err) {
			return nil, err
		}
		failed = append(failed, FailedLink{Label: l.Label, Err: err})
	}
	return nil, err
}

// withNotice prepends the notice to events. The forwarding goroutine ends
// when the source closes; the providers close their channel after the
// terminal event, so it never outlives the stream.
func withNotice(events <-chan StreamEvent, notice *FallbackNotice) <-chan StreamEvent {
	out := make(chan StreamEvent, 64)
	go func() {
		defer close(out)
		out <- StreamEvent{Fallback: notice}
		for evt := range events {
			out <- evt
		}
	}()
	return out
}
