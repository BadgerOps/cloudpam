package api

import "context"

type contextKey string

const (
	requestIDContextKey contextKey = "requestID"
	clientIPContextKey  contextKey = "clientIP"
)

// WithRequestID stores the provided request ID in the context.
func WithRequestID(ctx context.Context, requestID string) context.Context {
	if requestID == "" {
		return ctx
	}
	return context.WithValue(ctx, requestIDContextKey, requestID)
}

// RequestIDFromContext retrieves the request ID from context if present.
func RequestIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v, ok := ctx.Value(requestIDContextKey).(string); ok {
		return v
	}
	return ""
}

// WithClientIP stores the resolved client IP address in the context.
func WithClientIP(ctx context.Context, ip string) context.Context {
	if ip == "" {
		return ctx
	}
	return context.WithValue(ctx, clientIPContextKey, ip)
}

// ClientIPFromContext retrieves the client IP address from context if present.
func ClientIPFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v, ok := ctx.Value(clientIPContextKey).(string); ok {
		return v
	}
	return ""
}

func appendRequestID(ctx context.Context, attrs []any) []any {
	if rid := RequestIDFromContext(ctx); rid != "" {
		attrs = append(attrs, "request_id", rid)
	}
	return attrs
}
