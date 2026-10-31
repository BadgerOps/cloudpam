package main

import (
	"strings"

	"github.com/getsentry/sentry-go"
)

// sentryScrubbedHeaders are removed from every event's request (compared
// case-insensitively). They carry session cookies, API keys and CSRF tokens.
var sentryScrubbedHeaders = map[string]struct{}{
	"authorization": {},
	"cookie":        {},
	"x-csrf-token":  {},
}

// sentryClientOptions builds the Sentry client options. Both error events and
// transactions pass through scrubSentryEvent.
func sentryClientOptions(dsn string) sentry.ClientOptions {
	return sentry.ClientOptions{
		Dsn:                   dsn,
		Environment:           envOr("SENTRY_ENVIRONMENT", "production"),
		Release:               envOr("APP_VERSION", "dev"),
		TracesSampleRate:      1.0, // Capture 100% of transactions for performance monitoring
		AttachStacktrace:      true,
		BeforeSend:            scrubSentryEvent,
		BeforeSendTransaction: scrubSentryEvent,
	}
}

// scrubSentryEvent removes the request body, cookies and authentication
// headers. Scope.SetRequest tees request bodies into Request.Data, which would
// otherwise ship passwords, OIDC client secrets and AI provider keys.
func scrubSentryEvent(event *sentry.Event, _ *sentry.EventHint) *sentry.Event {
	if event == nil || event.Request == nil {
		return event
	}
	event.Request.Data = ""
	event.Request.Cookies = ""
	for name := range event.Request.Headers {
		if _, ok := sentryScrubbedHeaders[strings.ToLower(name)]; ok {
			delete(event.Request.Headers, name)
		}
	}
	return event
}
