package observability

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

func TestNewLogger(t *testing.T) {
	tests := []struct {
		name   string
		cfg    Config
		logMsg string
		level  string
		want   bool // whether we expect the message to appear
	}{
		{
			name:   "info level logs info",
			cfg:    Config{Level: "info", Format: "json"},
			logMsg: "test message",
			level:  "info",
			want:   true,
		},
		{
			name:   "info level does not log debug",
			cfg:    Config{Level: "info", Format: "json"},
			logMsg: "test message",
			level:  "debug",
			want:   false,
		},
		{
			name:   "debug level logs debug",
			cfg:    Config{Level: "debug", Format: "json"},
			logMsg: "test message",
			level:  "debug",
			want:   true,
		},
		{
			name:   "error level logs error",
			cfg:    Config{Level: "error", Format: "json"},
			logMsg: "test message",
			level:  "error",
			want:   true,
		},
		{
			name:   "error level does not log warn",
			cfg:    Config{Level: "error", Format: "json"},
			logMsg: "test message",
			level:  "warn",
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := &bytes.Buffer{}
			tt.cfg.Output = buf
			logger := NewLogger(tt.cfg)

			switch tt.level {
			case "debug":
				logger.Debug(tt.logMsg)
			case "info":
				logger.Info(tt.logMsg)
			case "warn":
				logger.Warn(tt.logMsg)
			case "error":
				logger.Error(tt.logMsg)
			}

			got := strings.Contains(buf.String(), tt.logMsg)
			if got != tt.want {
				t.Errorf("expected message presence=%v, got=%v, output=%s", tt.want, got, buf.String())
			}
		})
	}
}

func TestLoggerJSONFormat(t *testing.T) {
	buf := &bytes.Buffer{}
	cfg := Config{Level: "info", Format: "json", Output: buf}
	logger := NewLogger(cfg)

	logger.Info("test message", "key", "value")

	var logEntry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &logEntry); err != nil {
		t.Fatalf("failed to parse JSON log: %v, output: %s", err, buf.String())
	}

	if logEntry["msg"] != "test message" {
		t.Errorf("expected msg='test message', got=%v", logEntry["msg"])
	}
	if logEntry["key"] != "value" {
		t.Errorf("expected key='value', got=%v", logEntry["key"])
	}
}

func TestLoggerTextFormat(t *testing.T) {
	buf := &bytes.Buffer{}
	cfg := Config{Level: "info", Format: "text", Output: buf}
	logger := NewLogger(cfg)

	logger.Info("test message", "key", "value")

	output := buf.String()
	if !strings.Contains(output, "test message") {
		t.Errorf("expected 'test message' in output, got: %s", output)
	}
	if !strings.Contains(output, "key=value") {
		t.Errorf("expected 'key=value' in output, got: %s", output)
	}
}

func TestLoggerWith(t *testing.T) {
	buf := &bytes.Buffer{}
	cfg := Config{Level: "info", Format: "json", Output: buf}
	logger := NewLogger(cfg)

	childLogger := logger.With("static_key", "static_value")
	childLogger.Info("test message")

	var logEntry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &logEntry); err != nil {
		t.Fatalf("failed to parse JSON log: %v", err)
	}

	if logEntry["static_key"] != "static_value" {
		t.Errorf("expected static_key='static_value', got=%v", logEntry["static_key"])
	}
}

func TestLoggerWithComponent(t *testing.T) {
	buf := &bytes.Buffer{}
	cfg := Config{Level: "info", Format: "json", Output: buf}
	logger := NewLogger(cfg)

	componentLogger := logger.WithComponent("http")
	componentLogger.Info("test message")

	var logEntry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &logEntry); err != nil {
		t.Fatalf("failed to parse JSON log: %v", err)
	}

	if logEntry["component"] != "http" {
		t.Errorf("expected component='http', got=%v", logEntry["component"])
	}
}

func TestLoggerContextMethods(t *testing.T) {
	buf := &bytes.Buffer{}
	cfg := Config{Level: "debug", Format: "json", Output: buf}
	logger := NewLogger(cfg)

	ctx := context.Background()
	ctx = WithRequestID(ctx, "req-123")
	ctx = WithComponent(ctx, "storage")

	logger.InfoContext(ctx, "test message")

	var logEntry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &logEntry); err != nil {
		t.Fatalf("failed to parse JSON log: %v", err)
	}

	if logEntry["request_id"] != "req-123" {
		t.Errorf("expected request_id='req-123', got=%v", logEntry["request_id"])
	}
	if logEntry["component"] != "storage" {
		t.Errorf("expected component='storage', got=%v", logEntry["component"])
	}
}

func TestWithRequestID(t *testing.T) {
	tests := []struct {
		name      string
		requestID string
		want      string
	}{
		{
			name:      "stores request id",
			requestID: "req-123",
			want:      "req-123",
		},
		{
			name:      "empty request id returns original context",
			requestID: "",
			want:      "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			ctx = WithRequestID(ctx, tt.requestID)
			got := RequestIDFromContext(ctx)
			if got != tt.want {
				t.Errorf("RequestIDFromContext() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRequestIDFromContextNilContext(t *testing.T) {
	got := RequestIDFromContext(nil) //nolint:staticcheck // testing nil context handling
	if got != "" {
		t.Errorf("expected empty string for nil context, got %q", got)
	}
}

func TestWithComponent(t *testing.T) {
	tests := []struct {
		name      string
		component string
		want      string
	}{
		{
			name:      "stores component",
			component: "http",
			want:      "http",
		},
		{
			name:      "empty component returns original context",
			component: "",
			want:      "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			ctx = WithComponent(ctx, tt.component)
			got := ComponentFromContext(ctx)
			if got != tt.want {
				t.Errorf("ComponentFromContext() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestComponentFromContextNilContext(t *testing.T) {
	got := ComponentFromContext(nil) //nolint:staticcheck // testing nil context handling
	if got != "" {
		t.Errorf("expected empty string for nil context, got %q", got)
	}
}

func TestFromContext(t *testing.T) {
	buf := &bytes.Buffer{}
	cfg := Config{Level: "info", Format: "json", Output: buf}
	baseLogger := NewLogger(cfg)

	ctx := context.Background()
	ctx = WithRequestID(ctx, "req-456")

	logger := FromContext(ctx, baseLogger)
	logger.Info("test message")

	var logEntry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &logEntry); err != nil {
		t.Fatalf("failed to parse JSON log: %v", err)
	}

	if logEntry["request_id"] != "req-456" {
		t.Errorf("expected request_id='req-456', got=%v", logEntry["request_id"])
	}
}

func TestFromContextNilLogger(t *testing.T) {
	ctx := context.Background()
	logger := FromContext(ctx, nil)
	if logger == nil {
		t.Error("expected non-nil logger")
	}
}

func TestFromContextEmptyContext(t *testing.T) {
	buf := &bytes.Buffer{}
	cfg := Config{Level: "info", Format: "json", Output: buf}
	baseLogger := NewLogger(cfg)

	ctx := context.Background()
	logger := FromContext(ctx, baseLogger)
	logger.Info("test message")

	// Should still log without request_id
	var logEntry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &logEntry); err != nil {
		t.Fatalf("failed to parse JSON log: %v", err)
	}

	if logEntry["msg"] != "test message" {
		t.Errorf("expected msg='test message', got=%v", logEntry["msg"])
	}
}

func TestNewLoggerFromSlog(t *testing.T) {
	buf := &bytes.Buffer{}
	slogger := slog.New(slog.NewJSONHandler(buf, nil))

	logger := NewLoggerFromSlog(slogger)
	logger.Info("test message")

	if !strings.Contains(buf.String(), "test message") {
		t.Errorf("expected 'test message' in output, got: %s", buf.String())
	}
}

func TestNewLoggerFromSlogNil(t *testing.T) {
	logger := NewLoggerFromSlog(nil)
	if logger == nil {
		t.Error("expected non-nil logger for nil slog input")
	}
}

func TestLoggerSlog(t *testing.T) {
	cfg := Config{Level: "info", Format: "json"}
	logger := NewLogger(cfg)

	slogger := logger.Slog()
	if slogger == nil {
		t.Error("expected non-nil *slog.Logger from Slog()")
	}
}

func TestParseLevel(t *testing.T) {
	tests := []struct {
		input string
		want  slog.Level
	}{
		{"debug", slog.LevelDebug},
		{"DEBUG", slog.LevelDebug},
		{"info", slog.LevelInfo},
		{"INFO", slog.LevelInfo},
		{"warn", slog.LevelWarn},
		{"WARN", slog.LevelWarn},
		{"warning", slog.LevelWarn},
		{"WARNING", slog.LevelWarn},
		{"error", slog.LevelError},
		{"ERROR", slog.LevelError},
		{"invalid", slog.LevelInfo}, // default
		{"", slog.LevelInfo},        // default
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := parseLevel(tt.input)
			if got != tt.want {
				t.Errorf("parseLevel(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Level != "info" {
		t.Errorf("expected Level='info', got=%s", cfg.Level)
	}
	if cfg.Format != "json" {
		t.Errorf("expected Format='json', got=%s", cfg.Format)
	}
	if cfg.AddSource != false {
		t.Errorf("expected AddSource=false, got=%v", cfg.AddSource)
	}
}

func TestLoggerAllLevelMethods(t *testing.T) {
	buf := &bytes.Buffer{}
	cfg := Config{Level: "debug", Format: "json", Output: buf}
	logger := NewLogger(cfg)

	ctx := context.Background()

	// Test all non-context methods
	logger.Debug("debug msg")
	logger.Info("info msg")
	logger.Warn("warn msg")
	logger.Error("error msg")

	// Test all context methods
	logger.DebugContext(ctx, "debug ctx msg")
	logger.InfoContext(ctx, "info ctx msg")
	logger.WarnContext(ctx, "warn ctx msg")
	logger.ErrorContext(ctx, "error ctx msg")

	output := buf.String()

	expected := []string{
		"debug msg",
		"info msg",
		"warn msg",
		"error msg",
		"debug ctx msg",
		"info ctx msg",
		"warn ctx msg",
		"error ctx msg",
	}

	for _, msg := range expected {
		if !strings.Contains(output, msg) {
			t.Errorf("expected %q in output, got: %s", msg, output)
		}
	}
}

func TestAppendContextFieldsNilContext(t *testing.T) {
	args := appendContextFields(nil, []any{"key", "value"}) //nolint:staticcheck // testing nil context handling
	if len(args) != 2 {
		t.Errorf("expected 2 args for nil context, got %d", len(args))
	}
}

func TestContextKeyRoundTrip(t *testing.T) {
	// Test that the context functions work correctly within the package
	ctx := context.Background()
	ctx = WithRequestID(ctx, "test-123")

	// Verify the value can be retrieved
	got := RequestIDFromContext(ctx)
	if got != "test-123" {
		t.Errorf("expected 'test-123', got %q", got)
	}
}
