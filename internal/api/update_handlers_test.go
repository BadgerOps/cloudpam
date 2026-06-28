package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"cloudpam/internal/storage"
)

func TestUpdateServerCheckUpdates(t *testing.T) {
	origVersion := os.Getenv("APP_VERSION")
	t.Cleanup(func() {
		_ = os.Setenv("APP_VERSION", origVersion)
	})
	_ = os.Setenv("APP_VERSION", "v0.8.1")

	releaseAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{
				"tag_name":     "v0.8.2",
				"body":         "Patch release",
				"html_url":     "https://example.com/releases/v0.8.2",
				"published_at": "2026-03-31T12:00:00Z",
				"draft":        false,
				"prerelease":   false,
			},
			{
				"tag_name":     "v0.9.0",
				"body":         "Big release",
				"html_url":     "https://example.com/releases/v0.9.0",
				"published_at": "2026-04-01T12:00:00Z",
				"draft":        false,
				"prerelease":   false,
			},
		})
	}))
	defer releaseAPI.Close()

	updateSrv := newTestUpdateServer(t, releaseAPI)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/updates?force=true", nil)
	rr := httptest.NewRecorder()
	updateSrv.handleCheckUpdates(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rr.Code, rr.Body.String())
	}

	var resp struct {
		CurrentVersion  string `json:"current_version"`
		LatestVersion   string `json:"latest_version"`
		UpdateAvailable bool   `json:"update_available"`
		ReleaseNotes    string `json:"release_notes"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.CurrentVersion != "0.8.1" {
		t.Fatalf("expected current version 0.8.1, got %q", resp.CurrentVersion)
	}
	if resp.LatestVersion != "0.9.0" {
		t.Fatalf("expected latest version 0.9.0, got %q", resp.LatestVersion)
	}
	if !resp.UpdateAvailable {
		t.Fatal("expected update_available=true")
	}
	if resp.ReleaseNotes != "Big release" {
		t.Fatalf("expected release notes for highest version, got %q", resp.ReleaseNotes)
	}
}

func TestUpdateServerTriggerUpgrade(t *testing.T) {
	origVersion := os.Getenv("APP_VERSION")
	t.Cleanup(func() {
		_ = os.Setenv("APP_VERSION", origVersion)
	})
	_ = os.Setenv("APP_VERSION", "v0.8.1")

	releaseAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{
				"tag_name":     "v0.9.0",
				"body":         "Big release",
				"html_url":     "https://example.com/releases/v0.9.0",
				"published_at": "2026-04-01T12:00:00Z",
				"draft":        false,
				"prerelease":   false,
			},
		})
	}))
	defer releaseAPI.Close()

	updateSrv := newTestUpdateServer(t, releaseAPI)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/updates/upgrade", nil)
	rr := httptest.NewRecorder()
	updateSrv.handleTriggerUpgrade(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d body=%s", rr.Code, rr.Body.String())
	}

	requestPath := filepath.Join(updateSrv.controlDir, "upgrade-requested")
	raw, err := os.ReadFile(requestPath)
	if err != nil {
		t.Fatalf("read upgrade request: %v", err)
	}

	var reqFile map[string]any
	if err := json.Unmarshal(raw, &reqFile); err != nil {
		t.Fatalf("decode upgrade request: %v", err)
	}
	if got, ok := reqFile["upgrade_id"].(string); !ok || got == "" {
		t.Fatalf("expected upgrade_id in request file, got %v", reqFile["upgrade_id"])
	}
	if got := reqFile["target_version"]; got != "0.9.0" {
		t.Fatalf("expected target_version=0.9.0, got %v", got)
	}
	if got := reqFile["target_image_tag"]; got != "0.9.0" {
		t.Fatalf("expected target_image_tag=0.9.0, got %v", got)
	}
	if got := reqFile["target_release_tag"]; got != "v0.9.0" {
		t.Fatalf("expected target_release_tag=v0.9.0, got %v", got)
	}

	var resp map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode trigger response: %v", err)
	}
	if got, ok := resp["upgrade_id"].(string); !ok || got == "" || got != reqFile["upgrade_id"] {
		t.Fatalf("expected matching response upgrade_id, got %v request=%v", got, reqFile["upgrade_id"])
	}
}

func TestUpdateServerGetStatus(t *testing.T) {
	updateSrv := newTestUpdateServer(t, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/updates/status", nil)
	rr := httptest.NewRecorder()
	updateSrv.handleGetUpgradeStatus(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var idleResp map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &idleResp); err != nil {
		t.Fatalf("decode idle response: %v", err)
	}
	if idleResp["status"] != "idle" {
		t.Fatalf("expected idle status, got %v", idleResp["status"])
	}

	statusPath := filepath.Join(updateSrv.controlDir, "upgrade-status.json")
	if err := os.WriteFile(statusPath, []byte(`{"status":"running","progress":40}`), 0o644); err != nil {
		t.Fatalf("write status file: %v", err)
	}

	rr = httptest.NewRecorder()
	updateSrv.handleGetUpgradeStatus(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), `"status":"running"`) {
		t.Fatalf("expected running status, got %s", rr.Body.String())
	}

	if err := os.WriteFile(statusPath, []byte(`{"status":"completed","target_version":"0.9.0","finished_at":"`+time.Now().UTC().Format(time.RFC3339)+`"}`), 0o644); err != nil {
		t.Fatalf("write completed status file: %v", err)
	}
	rr = httptest.NewRecorder()
	updateSrv.handleGetUpgradeStatus(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var completedResp map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &completedResp); err != nil {
		t.Fatalf("decode completed response: %v", err)
	}
	if completedResp["status"] != "completed" {
		t.Fatalf("expected completed status, got %+v", completedResp)
	}
	if got, ok := completedResp["upgrade_id"].(string); !ok || got == "" {
		t.Fatalf("expected derived upgrade_id, got %+v", completedResp)
	}
}

func TestUpdateServerAcknowledgeCompletedStatus(t *testing.T) {
	updateSrv := newTestUpdateServer(t, nil)
	statusPath := filepath.Join(updateSrv.controlDir, upgradeStatusFile)
	if err := os.WriteFile(statusPath, []byte(`{"status":"completed","upgrade_id":"upg-test","target_version":"0.9.0","finished_at":"`+time.Now().UTC().Format(time.RFC3339)+`"}`), 0o644); err != nil {
		t.Fatalf("write completed status file: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/updates/status/ack", nil)
	rr := httptest.NewRecorder()
	updateSrv.handleAcknowledgeUpgradeStatus(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var ackResp map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &ackResp); err != nil {
		t.Fatalf("decode ack response: %v", err)
	}
	if ackResp["status"] != "idle" || ackResp["acknowledged"] != true || ackResp["upgrade_id"] != "upg-test" {
		t.Fatalf("unexpected ack response: %+v", ackResp)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/updates/status", nil)
	rr = httptest.NewRecorder()
	updateSrv.handleGetUpgradeStatus(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var statusResp map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &statusResp); err != nil {
		t.Fatalf("decode status response: %v", err)
	}
	if statusResp["status"] != "idle" || statusResp["acknowledged"] != true {
		t.Fatalf("expected acknowledged idle status, got %+v", statusResp)
	}
	if _, ok := statusResp["last_upgrade"].(map[string]any); !ok {
		t.Fatalf("expected last_upgrade payload, got %+v", statusResp["last_upgrade"])
	}
}

func TestUpdateServerCompletedStatusExpiresToIdle(t *testing.T) {
	updateSrv := newTestUpdateServer(t, nil)
	statusPath := filepath.Join(updateSrv.controlDir, upgradeStatusFile)
	oldFinishedAt := time.Now().UTC().Add(-completedUpgradeStatusTTL - time.Minute)
	if err := os.WriteFile(statusPath, []byte(`{"status":"completed","upgrade_id":"upg-old","target_version":"0.9.0","finished_at":"`+oldFinishedAt.Format(time.RFC3339)+`"}`), 0o644); err != nil {
		t.Fatalf("write completed status file: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/updates/status", nil)
	rr := httptest.NewRecorder()
	updateSrv.handleGetUpgradeStatus(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var statusResp map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &statusResp); err != nil {
		t.Fatalf("decode status response: %v", err)
	}
	if statusResp["status"] != "idle" || statusResp["completed_status_expired"] != true || statusResp["upgrade_id"] != "upg-old" {
		t.Fatalf("expected expired idle status, got %+v", statusResp)
	}
	if _, ok := statusResp["last_upgrade"].(map[string]any); !ok {
		t.Fatalf("expected last_upgrade payload, got %+v", statusResp["last_upgrade"])
	}
}

func TestUpdateServerAcknowledgeRequiresCompletedStatus(t *testing.T) {
	updateSrv := newTestUpdateServer(t, nil)
	statusPath := filepath.Join(updateSrv.controlDir, upgradeStatusFile)
	if err := os.WriteFile(statusPath, []byte(`{"status":"running","upgrade_id":"upg-running"}`), 0o644); err != nil {
		t.Fatalf("write running status file: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/updates/status/ack", nil)
	rr := httptest.NewRecorder()
	updateSrv.handleAcknowledgeUpgradeStatus(rr, req)
	if rr.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rr.Code, rr.Body.String())
	}
}

func newTestUpdateServer(t *testing.T, releaseAPI *httptest.Server) *UpdateServer {
	t.Helper()

	mux := http.NewServeMux()
	srv := NewServer(mux, storage.NewMemoryStore(), nil, nil, nil)
	updateSrv := NewUpdateServer(srv)
	updateSrv.RegisterUpdateRoutesNoAuth()

	controlDir := t.TempDir()
	updateSrv.controlDir = controlDir
	if releaseAPI != nil {
		updateSrv.client = releaseAPI.Client()
		updateSrv.releasesURL = releaseAPI.URL
	}
	return updateSrv
}
