package main

import (
	"testing"

	"github.com/getsentry/sentry-go"
)

func TestScrubSentryEventRemovesBodiesAndAuthHeaders(t *testing.T) {
	event := &sentry.Event{
		Message: "boom",
		Request: &sentry.Request{
			URL:     "https://cloudpam.example/api/v1/settings/ai",
			Method:  "PATCH",
			Data:    `{"api_key":"sk-x"}`,
			Cookies: "cloudpam_session=abc",
			Headers: map[string]string{
				"Authorization": "Bearer cpk_secret",
				"cookie":        "cloudpam_session=abc",
				"X-Csrf-Token":  "csrf-secret",
				"Content-Type":  "application/json",
			},
		},
	}

	got := scrubSentryEvent(event, nil)
	if got == nil {
		t.Fatal("scrubSentryEvent dropped the event; it must only scrub it")
	}
	if got.Request.Data != "" {
		t.Errorf("Request.Data = %q, want empty", got.Request.Data)
	}
	if got.Request.Cookies != "" {
		t.Errorf("Request.Cookies = %q, want empty", got.Request.Cookies)
	}
	for _, h := range []string{"Authorization", "cookie", "X-Csrf-Token"} {
		if v, ok := got.Request.Headers[h]; ok {
			t.Errorf("header %s = %q survived scrubbing", h, v)
		}
	}
	if got.Request.Headers["Content-Type"] != "application/json" {
		t.Errorf("non-sensitive header was removed: %v", got.Request.Headers)
	}
	if got.Request.URL == "" || got.Request.Method != "PATCH" || got.Message != "boom" {
		t.Errorf("scrubbing changed unrelated fields: %+v", got)
	}
}

func TestScrubSentryEventWithoutRequest(t *testing.T) {
	event := &sentry.Event{Message: "no request"}
	if got := scrubSentryEvent(event, nil); got != event {
		t.Fatal("event without a request must pass through unchanged")
	}
	if scrubSentryEvent(nil, nil) != nil {
		t.Fatal("nil event must stay nil")
	}
}

func TestSentryClientOptionsInstallScrubHooks(t *testing.T) {
	opts := sentryClientOptions("https://key@sentry.example/1")
	if opts.BeforeSend == nil || opts.BeforeSendTransaction == nil {
		t.Fatal("BeforeSend and BeforeSendTransaction must both scrub events")
	}
	for name, hook := range map[string]func(*sentry.Event, *sentry.EventHint) *sentry.Event{
		"BeforeSend":            opts.BeforeSend,
		"BeforeSendTransaction": opts.BeforeSendTransaction,
	} {
		ev := hook(&sentry.Event{Request: &sentry.Request{Data: `{"password":"hunter2"}`}}, nil)
		if ev.Request.Data != "" {
			t.Errorf("%s left the request body in place", name)
		}
	}
	if opts.Dsn != "https://key@sentry.example/1" || opts.TracesSampleRate != 1.0 || !opts.AttachStacktrace {
		t.Errorf("unexpected options: %+v", opts)
	}
}
