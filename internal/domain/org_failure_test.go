package domain

import (
	"errors"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestOrgAccountFailureClipsByCharacter(t *testing.T) {
	f := NewOrgAccountFailure("gcp", "p", errors.New(strings.Repeat("é", 400)))
	if n := utf8.RuneCountInString(f.Error); n != 300 || !utf8.ValidString(f.Error) {
		t.Fatalf("error has %d characters (valid utf-8: %v), want 300", n, utf8.ValidString(f.Error))
	}
	if got, want := f.Message(), "discovery failed for gcp:p: "+f.Error; got != want {
		t.Fatalf("Message() = %q, want %q", got, want)
	}
	if got, want := (OrgAccountFailure{Provider: "aws", Error: "list failed"}).Message(), "discovery failed for aws: list failed"; got != want {
		t.Fatalf("Message() = %q, want %q", got, want)
	}
}
