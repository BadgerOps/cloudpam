package observability

import (
	"os"
	"testing"
)

func TestLoggerConfigFromEnv(t *testing.T) {
	tests := []struct {
		name       string
		level      string
		format     string
		wantLevel  string
		wantFormat string
	}{
		{name: "unset falls back to defaults", wantLevel: "info", wantFormat: "json"},
		{name: "level override", level: "debug", wantLevel: "debug", wantFormat: "json"},
		{name: "format override", format: "text", wantLevel: "info", wantFormat: "text"},
		{name: "both overridden", level: "error", format: "text", wantLevel: "error", wantFormat: "text"},
		{name: "unknown values pass through", level: "loud", format: "yaml", wantLevel: "loud", wantFormat: "yaml"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("CLOUDPAM_LOG_LEVEL", tc.level)
			t.Setenv("CLOUDPAM_LOG_FORMAT", tc.format)

			cfg := ConfigFromEnv()
			if cfg.Level != tc.wantLevel {
				t.Errorf("Level = %q, want %q", cfg.Level, tc.wantLevel)
			}
			if cfg.Format != tc.wantFormat {
				t.Errorf("Format = %q, want %q", cfg.Format, tc.wantFormat)
			}
			if cfg.Output != os.Stdout {
				t.Errorf("Output = %v, want os.Stdout", cfg.Output)
			}
			if cfg.AddSource {
				t.Error("AddSource should stay false; it is not configurable from the environment")
			}
		})
	}
}
