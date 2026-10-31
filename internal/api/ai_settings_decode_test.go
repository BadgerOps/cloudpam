package api

import (
	"net/http"
	"testing"
)

// TestAISettingsPatchRejectsUnknownFields: a misspelled field such as "apikey"
// would otherwise be silently ignored and the caller would believe the key was
// saved.
func TestAISettingsPatchRejectsUnknownFields(t *testing.T) {
	f := setupAISettings(t, envDefaults(), true)

	got := decodeAPIError(t, f.patch(t, `{"apikey":"x"}`, http.StatusBadRequest))
	if got.Error != "invalid request body" || got.Detail != `unknown field "apikey"` {
		t.Fatalf("error = %+v", got)
	}
	if doc := f.storedDoc(t); doc.HasStoredValues() {
		t.Fatalf("rejected PATCH stored values: %+v", doc)
	}

	// Other decode failures keep the generic detail.
	got = decodeAPIError(t, f.patch(t, `{"model":`, http.StatusBadRequest))
	if got.Error != "invalid request body" || got.Detail != "expected a json object" {
		t.Fatalf("error for malformed json = %+v", got)
	}

	if s := decodeAISettings(t, f.patch(t, `{"model":"m"}`, http.StatusOK)); s.Model != "m" {
		t.Fatalf("known field not applied: %+v", s)
	}
}
