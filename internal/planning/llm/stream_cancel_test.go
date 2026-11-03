package llm

import (
	"context"
	"testing"
	"time"
)

// heldStream returns pending data payloads, well beyond the 64-slot channel
// buffer, from a stub that keeps the response open until the client leaves.
func heldStream(t *testing.T, payload string) *sseStub {
	t.Helper()
	const pending = 500
	data := make([]string, pending)
	for i := range data {
		data[i] = payload
	}
	return newSSEStub(t, true, data...)
}

// TestAnthropicStreamEndsWhenContextIsCancelled is the #375 contract for the
// Anthropic producer: once a consumer stops reading and ctx ends, the producer
// must end its span and close the channel without anyone reading.
func TestAnthropicStreamEndsWhenContextIsCancelled(t *testing.T) {
	exporter := recordedSpans()
	exporter.Reset()
	stub := heldStream(t, `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"late"}}`)
	p := NewAnthropicProvider(Config{APIKey: "k", Model: "m", Endpoint: stub.URL})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := p.StreamComplete(ctx, anthropicMessages, Options{})
	if err != nil {
		t.Fatalf("StreamComplete: %v", err)
	}
	select {
	case <-ch:
	case <-time.After(3 * time.Second):
		t.Fatal("no event within 3s")
	}
	cancel()

	deadline := time.Now().Add(3 * time.Second)
	for !streamSpanEnded(exporter, stub.URL) {
		if time.Now().After(deadline) {
			t.Fatal("stream span still open 3s after the context was cancelled: the producer is stuck")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestChainFallbackStreamEndsWhenContextIsCancelled: the forwarder that
// prepends the fallback notice must give up on a full channel once ctx ends,
// like the providers do. A forwarder that blocks instead stays parked on its
// send and, once read again, delivers the whole source buffer after it.
func TestChainFallbackStreamEndsWhenContextIsCancelled(t *testing.T) {
	stub := heldStream(t, chunk("late", ""))
	chain := NewChain([]Link{
		{Provider: failing("primary", &StatusError{Status: 503}), Label: "primary"},
		{Provider: NewOpenAIProvider(Config{APIKey: "k", Model: "m", Endpoint: stub.URL}), Label: "fallback"},
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := chain.StreamComplete(ctx, []Message{{Role: "user", Content: "hi"}}, Options{})
	if err != nil {
		t.Fatalf("StreamComplete: %v", err)
	}
	select {
	case <-ch:
	case <-time.After(3 * time.Second):
		t.Fatal("no event within 3s")
	}
	// Let the source fill both buffers before the consumer walks away.
	time.Sleep(100 * time.Millisecond)
	cancel()
	select {
	case <-stub.disconnected:
	case <-time.After(3 * time.Second):
		t.Fatal("response body was not closed after the context was cancelled")
	}
	time.Sleep(100 * time.Millisecond)

	after := 0
	timeout := time.After(3 * time.Second)
	for open := true; open; {
		select {
		case _, ok := <-ch:
			if !ok {
				open = false
				break
			}
			after++
		case <-timeout:
			t.Fatal("stream channel not closed after the context was cancelled")
		}
	}
	if after > cap(ch) {
		t.Fatalf("%d events arrived after cancel, more than the %d-slot buffer: the forwarder kept sending past ctx", after, cap(ch))
	}
}
