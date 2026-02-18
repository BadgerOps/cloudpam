// Package observability provides structured logging, metrics, and tracing.
package observability

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"
)

// contextKey is a private type for context keys to avoid collisions.
type contextKey string

const (
	// requestIDKey is the context key for request IDs.
	// This matches the key used in internal/api/context.go for compatibility.
	requestIDKey contextKey = "requestID"
	// componentKey is the context key for component names.
	componentKey contextKey = "component"
)

// Logger defines the interface for structured logging.
type Logger interface {
	// Debug logs at debug level.
	Debug(msg string, args ...any)
	// Info logs at info level.
	Info(msg string, args ...any)
	// Warn logs at warning level.
	Warn(msg string, args ...any)
	// Error logs at error level.
	Error(msg string, args ...any)

	// DebugContext logs at debug level with context.
	DebugContext(ctx context.Context, msg string, args ...any)
	// InfoContext logs at info level with context.
	InfoContext(ctx context.Context, msg string, args ...any)
	// WarnContext logs at warning level with context.
	WarnContext(ctx context.Context, msg string, args ...any)
	// ErrorContext logs at error level with context.
	ErrorContext(ctx context.Context, msg string, args ...any)

	// With returns a new Logger with the given attributes.
	With(args ...any) Logger
	// WithComponent returns a new Logger with the component field set.
	WithComponent(name string) Logger

	// Slog returns the underlying *slog.Logger for compatibility.
	Slog() *slog.Logger
}

// Config holds configuration for the logger.
type Config struct {
	// Level is the minimum log level (debug, info, warn, error).
	Level string
	// Format is the output format (json, text).
	Format string
	// Output is the destination for logs (defaults to os.Stdout).
	Output io.Writer
	// AddSource adds source file and line to log entries.
	AddSource bool
}

// DefaultConfig returns the default logger configuration.
func DefaultConfig() Config {
	return Config{
		Level:     "info",
		Format:    "json",
		Output:    os.Stdout,
		AddSource: false,
	}
}

// ConfigFromEnv creates a Config from environment variables.
// CLOUDPAM_LOG_LEVEL: debug, info, warn, error (default: info)
// CLOUDPAM_LOG_FORMAT: json, text (default: json)
func ConfigFromEnv() Config {
	cfg := DefaultConfig()
	if level := os.Getenv("CLOUDPAM_LOG_LEVEL"); level != "" {
		cfg.Level = level
	}
	if format := os.Getenv("CLOUDPAM_LOG_FORMAT"); format != "" {
		cfg.Format = format
	}
	return cfg
}

// defaultLogger is the package-level default logger.
type defaultLogger struct {
	slogger *slog.Logger
}

// NewLogger creates a new Logger with the given configuration.
func NewLogger(cfg Config) Logger {
	if cfg.Output == nil {
		cfg.Output = os.Stdout
	}

	level := parseLevel(cfg.Level)
	opts := &slog.HandlerOptions{
		Level:     level,
		AddSource: cfg.AddSource,
	}

	var handler slog.Handler
	switch strings.ToLower(cfg.Format) {
	case "text":
		handler = slog.NewTextHandler(cfg.Output, opts)
	default:
		handler = slog.NewJSONHandler(cfg.Output, opts)
	}

	return &defaultLogger{
		slogger: slog.New(handler),
	}
}

// NewLoggerFromSlog creates a Logger wrapping an existing *slog.Logger.
func NewLoggerFromSlog(l *slog.Logger) Logger {
	if l == nil {
		l = slog.Default()
	}
	return &defaultLogger{slogger: l}
}

// parseLevel converts a string log level to slog.Level.
func parseLevel(s string) slog.Level {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// Debug logs at debug level.
func (l *defaultLogger) Debug(msg string, args ...any) {
	l.slogger.Debug(msg, args...)
}

// Info logs at info level.
func (l *defaultLogger) Info(msg string, args ...any) {
	l.slogger.Info(msg, args...)
}

// Warn logs at warning level.
func (l *defaultLogger) Warn(msg string, args ...any) {
	l.slogger.Warn(msg, args...)
}

// Error logs at error level.
func (l *defaultLogger) Error(msg string, args ...any) {
	l.slogger.Error(msg, args...)
}

// DebugContext logs at debug level with context.
func (l *defaultLogger) DebugContext(ctx context.Context, msg string, args ...any) {
	args = appendContextFields(ctx, args)
	l.slogger.DebugContext(ctx, msg, args...)
}

// InfoContext logs at info level with context.
func (l *defaultLogger) InfoContext(ctx context.Context, msg string, args ...any) {
	args = appendContextFields(ctx, args)
	l.slogger.InfoContext(ctx, msg, args...)
}

// WarnContext logs at warning level with context.
func (l *defaultLogger) WarnContext(ctx context.Context, msg string, args ...any) {
	args = appendContextFields(ctx, args)
	l.slogger.WarnContext(ctx, msg, args...)
}

// ErrorContext logs at error level with context.
func (l *defaultLogger) ErrorContext(ctx context.Context, msg string, args ...any) {
	args = appendContextFields(ctx, args)
	l.slogger.ErrorContext(ctx, msg, args...)
}

// With returns a new Logger with the given attributes.
func (l *defaultLogger) With(args ...any) Logger {
	return &defaultLogger{slogger: l.slogger.With(args...)}
}

// WithComponent returns a new Logger with the component field set.
func (l *defaultLogger) WithComponent(name string) Logger {
	return l.With("component", name)
}

// Slog returns the underlying *slog.Logger for compatibility.
func (l *defaultLogger) Slog() *slog.Logger {
	return l.slogger
}

// appendContextFields extracts fields from context and appends them to args.
func appendContextFields(ctx context.Context, args []any) []any {
	if ctx == nil {
		return args
	}
	if reqID := RequestIDFromContext(ctx); reqID != "" {
		args = append(args, "request_id", reqID)
	}
	if component := ComponentFromContext(ctx); component != "" {
		args = append(args, "component", component)
	}
	return args
}

// WithRequestID stores the request ID in the context.
func WithRequestID(ctx context.Context, requestID string) context.Context {
	if requestID == "" {
		return ctx
	}
	return context.WithValue(ctx, requestIDKey, requestID)
}

// RequestIDFromContext retrieves the request ID from context.
func RequestIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v, ok := ctx.Value(requestIDKey).(string); ok {
		return v
	}
	return ""
}

// WithComponent stores the component name in the context.
func WithComponent(ctx context.Context, component string) context.Context {
	if component == "" {
		return ctx
	}
	return context.WithValue(ctx, componentKey, component)
}

// ComponentFromContext retrieves the component name from context.
func ComponentFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v, ok := ctx.Value(componentKey).(string); ok {
		return v
	}
	return ""
}

// FromContext returns a Logger that will include context fields in all log entries.
// This is useful when you want automatic request_id inclusion.
func FromContext(ctx context.Context, l Logger) Logger {
	if l == nil {
		l = NewLogger(DefaultConfig())
	}
	args := appendContextFields(ctx, nil)
	if len(args) > 0 {
		return l.With(args...)
	}
	return l
}
