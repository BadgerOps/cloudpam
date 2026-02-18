package auth

import (
	"strings"
	"testing"
)

func TestValidatePassword(t *testing.T) {
	tests := []struct {
		name      string
		password  string
		minLength int
		wantErr   bool
		errMsg    string
	}{
		{
			name:      "too short (11 chars)",
			password:  "abcdefghijk",
			minLength: 0,
			wantErr:   true,
			errMsg:    "at least 12",
		},
		{
			name:      "exactly 12 chars",
			password:  "abcdefghijkl",
			minLength: 0,
			wantErr:   false,
		},
		{
			name:      "at max (72 chars)",
			password:  strings.Repeat("a", 72),
			minLength: 0,
			wantErr:   false,
		},
		{
			name:      "over max (73 chars)",
			password:  strings.Repeat("a", 73),
			minLength: 0,
			wantErr:   true,
			errMsg:    "at most 72",
		},
		{
			name:      "empty password",
			password:  "",
			minLength: 0,
			wantErr:   true,
			errMsg:    "at least 12",
		},
		{
			name:      "custom min length (8) with 8 chars",
			password:  "abcdefgh",
			minLength: 8,
			wantErr:   false,
		},
		{
			name:      "custom min length (8) with 7 chars",
			password:  "abcdefg",
			minLength: 8,
			wantErr:   true,
			errMsg:    "at least 8",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePassword(tt.password, tt.minLength)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tt.errMsg != "" && !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("expected error to contain %q, got %q", tt.errMsg, err.Error())
				}
			} else if err != nil {
				t.Errorf("expected nil error, got %v", err)
			}
		})
	}
}
