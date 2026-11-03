package llm

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// scriptedProvider answers with a fixed error or a fixed reply and counts calls.
type scriptedProvider struct {
	name      string
	available bool
	err       error
	reply     string
	calls     atomic.Int32
}

func (p *scriptedProvider) Name() string    { return p.name }
func (p *scriptedProvider) Available() bool { return p.available }

func (p *scriptedProvider) Complete(context.Context, []Message, Options) (*Response, error) {
	p.calls.Add(1)
	if p.err != nil {
		return nil, p.err
	}
	return &Response{Content: p.reply, FinishReason: "stop"}, nil
}

func (p *scriptedProvider) StreamComplete(context.Context, []Message, Options) (<-chan StreamEvent, error) {
	p.calls.Add(1)
	if p.err != nil {
		return nil, p.err
	}
	ch := make(chan StreamEvent, 4)
	ch <- StreamEvent{Delta: p.reply}
	ch <- StreamEvent{Done: true, FinishReason: "stop"}
	close(ch)
	return ch, nil
}

func ok(name, reply string) *scriptedProvider {
	return &scriptedProvider{name: name, available: true, reply: reply}
}

func failing(name string, err error) *scriptedProvider {
	return &scriptedProvider{name: name, available: true, err: err}
}

func chainOf(ps ...*scriptedProvider) Provider {
	links := make([]Link, len(ps))
	for i, p := range ps {
		links[i] = Link{Provider: p, Label: p.name}
	}
	return NewChain(links)
}

// drain collects the stream: the notice (if any), the text and the terminal event.
func drain(t *testing.T, events <-chan StreamEvent) (*FallbackNotice, string, StreamEvent) {
	t.Helper()
	var notice *FallbackNotice
	var text strings.Builder
	for evt := range events {
		if evt.Fallback != nil {
			if notice != nil || text.Len() > 0 {
				t.Fatal("the fallback notice must be the first and only notice")
			}
			notice = evt.Fallback
			continue
		}
		text.WriteString(evt.Delta)
		if evt.Done {
			return notice, text.String(), evt
		}
	}
	t.Fatal("stream closed without a done event")
	return nil, "", StreamEvent{}
}

func TestNewChainWithOneLinkIsTheProviderItself(t *testing.T) {
	p := ok("openai", "hi")
	if got := chainOf(p); got != Provider(p) {
		t.Fatalf("NewChain with one link = %T, want the link's provider", got)
	}
}

func TestChainUsesFirstLinkWhenItAnswers(t *testing.T) {
	primary, secondary := ok("anthropic", "from primary"), ok("openai", "from secondary")
	c := chainOf(primary, secondary)

	out, err := c.Complete(context.Background(), nil, Options{})
	if err != nil || out.Content != "from primary" {
		t.Fatalf("Complete = %+v, %v", out, err)
	}
	events, err := c.StreamComplete(context.Background(), nil, Options{})
	if err != nil {
		t.Fatalf("StreamComplete: %v", err)
	}
	notice, text, _ := drain(t, events)
	if notice != nil || text != "from primary" {
		t.Fatalf("stream = notice %+v text %q, want the primary's answer without a notice", notice, text)
	}
	if secondary.calls.Load() != 0 {
		t.Fatal("the secondary must not be called while the primary answers")
	}
	if c.Name() != "anthropic" {
		t.Fatalf("Name = %q, want the first link's", c.Name())
	}
}

func TestChainFallsBackOn5xxAndSurfacesTheNotice(t *testing.T) {
	primary := failing("anthropic", &StatusError{Status: 529})
	secondary := ok("openai", "from secondary")
	c := chainOf(primary, secondary)

	out, err := c.Complete(context.Background(), nil, Options{})
	if err != nil || out.Content != "from secondary" {
		t.Fatalf("Complete = %+v, %v", out, err)
	}

	events, err := c.StreamComplete(context.Background(), nil, Options{})
	if err != nil {
		t.Fatalf("StreamComplete: %v", err)
	}
	notice, text, last := drain(t, events)
	if notice == nil {
		t.Fatal("expected a fallback notice before the answer")
	}
	if notice.Provider != "openai" || len(notice.Failed) != 1 || notice.Failed[0].Label != "anthropic" {
		t.Fatalf("notice = %+v", *notice)
	}
	var statusErr *StatusError
	if !errors.As(notice.Failed[0].Err, &statusErr) || statusErr.Status != 529 {
		t.Fatalf("notice error = %v, want the primary's status error", notice.Failed[0].Err)
	}
	if text != "from secondary" || !last.Done || last.Err != nil {
		t.Fatalf("stream text %q terminal %+v", text, last)
	}
}

func TestChainFallsBackWhenTheEndpointIsUnreachable(t *testing.T) {
	// A closed listener gives a real connection refused through the OpenAI
	// provider's HTTP client.
	dead := httptest.NewServer(http.NotFoundHandler())
	deadURL := dead.URL
	dead.Close()
	primary := NewOpenAIProvider(Config{Endpoint: deadURL + "/v1", Model: "m"})
	secondary := ok("fallback", "answered")
	c := NewChain([]Link{{Provider: primary, Label: "openai m"}, {Provider: secondary, Label: "fallback"}})

	out, err := c.Complete(context.Background(), []Message{{Role: "user", Content: "hi"}}, Options{})
	if err != nil || out.Content != "answered" {
		t.Fatalf("Complete = %+v, %v", out, err)
	}
	events, err := c.StreamComplete(context.Background(), []Message{{Role: "user", Content: "hi"}}, Options{})
	if err != nil {
		t.Fatalf("StreamComplete: %v", err)
	}
	notice, _, _ := drain(t, events)
	if notice == nil || notice.Failed[0].Label != "openai m" {
		t.Fatalf("notice = %+v, want the unreachable primary named", notice)
	}
	var opErr *net.OpError
	if !errors.As(notice.Failed[0].Err, &opErr) {
		t.Fatalf("notice error = %v, want a network error", notice.Failed[0].Err)
	}
}

func TestChainDoesNotFallBackOnClientErrors(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
	}{
		{"401", &StatusError{Status: 401}},
		{"429", &StatusError{Status: 429}},
		{"blocked address", ErrBlockedAddress},
		{"invalid response", ErrInvalidResponse},
	} {
		t.Run(tc.name, func(t *testing.T) {
			secondary := ok("openai", "never")
			c := chainOf(failing("anthropic", tc.err), secondary)
			if _, err := c.Complete(context.Background(), nil, Options{}); !errors.Is(err, tc.err) {
				t.Fatalf("Complete err = %v, want %v", err, tc.err)
			}
			if _, err := c.StreamComplete(context.Background(), nil, Options{}); !errors.Is(err, tc.err) {
				t.Fatalf("StreamComplete err = %v, want %v", err, tc.err)
			}
			if secondary.calls.Load() != 0 {
				t.Fatal("a client error must not be retried on the next link")
			}
		})
	}
}

func TestChainDoesNotFallBackWhenTheContextIsDone(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	secondary := ok("openai", "never")
	c := chainOf(failing("anthropic", &StatusError{Status: 503}), secondary)
	if _, err := c.Complete(ctx, nil, Options{}); err == nil {
		t.Fatal("expected the primary's error")
	}
	if secondary.calls.Load() != 0 {
		t.Fatal("a cancelled request must not move to the next link")
	}
}

func TestChainReturnsTheLastErrorWhenEveryLinkFails(t *testing.T) {
	last := &StatusError{Status: 502}
	c := chainOf(failing("a", &StatusError{Status: 503}), failing("b", last))
	if _, err := c.Complete(context.Background(), nil, Options{}); !errors.Is(err, last) {
		t.Fatalf("Complete err = %v, want the last link's %v", err, last)
	}
	if _, err := c.StreamComplete(context.Background(), nil, Options{}); !errors.Is(err, last) {
		t.Fatalf("StreamComplete err = %v, want the last link's %v", err, last)
	}
}

func TestChainSkipsUnavailableLinksAndReportsAvailability(t *testing.T) {
	unconfigured := &scriptedProvider{name: "anthropic", reply: "never"}
	secondary := ok("openai", "answered")
	c := chainOf(unconfigured, secondary)
	if !c.Available() {
		t.Fatal("a chain with one configured link is available")
	}
	out, err := c.Complete(context.Background(), nil, Options{})
	if err != nil || out.Content != "answered" {
		t.Fatalf("Complete = %+v, %v", out, err)
	}
	if unconfigured.calls.Load() != 0 {
		t.Fatal("an unconfigured link must be skipped, not called")
	}
	events, err := c.StreamComplete(context.Background(), nil, Options{})
	if err != nil {
		t.Fatalf("StreamComplete: %v", err)
	}
	if notice, _, _ := drain(t, events); notice != nil {
		t.Fatalf("skipping an unconfigured link is not a fallback, got notice %+v", *notice)
	}

	none := chainOf(&scriptedProvider{name: "a"}, &scriptedProvider{name: "b"})
	if none.Available() {
		t.Fatal("a chain with no configured link is unavailable")
	}
	if _, err := none.Complete(context.Background(), nil, Options{}); err == nil {
		t.Fatal("expected an error when no link is available")
	}
}

func TestLinksExposesEachProvider(t *testing.T) {
	single := ok("openai", "x")
	if got := Links(single, "openai m"); len(got) != 1 || got[0].Provider != Provider(single) || got[0].Label != "openai m" {
		t.Fatalf("Links(single) = %+v", got)
	}
	a, b := ok("a", "x"), ok("b", "y")
	got := Links(chainOf(a, b), "ignored")
	if len(got) != 2 || got[0].Label != "a" || got[1].Label != "b" {
		t.Fatalf("Links(chain) = %+v", got)
	}
}
