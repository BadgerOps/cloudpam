package api

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"cloudpam/internal/auth"
)

const (
	updateCacheTTL            = time.Hour
	completedUpgradeStatusTTL = 10 * time.Minute
	defaultControlDir         = "/var/lib/cloudpam-control"
	defaultCloudPAMRepo       = "BadgerOps/cloudpam"
	defaultGitHubAPIRoot      = "https://api.github.com"
	upgradeStatusFile         = "upgrade-status.json"
	upgradeAckFile            = "upgrade-status-ack.json"
	upgradeRequestedFile      = "upgrade-requested"
)

type updateRelease struct {
	TagName     string `json:"tag_name"`
	Body        string `json:"body"`
	HTMLURL     string `json:"html_url"`
	PublishedAt string `json:"published_at"`
	Draft       bool   `json:"draft"`
	Prerelease  bool   `json:"prerelease"`
}

type updateCache struct {
	mu        sync.Mutex
	releases  []updateRelease
	fetchedAt time.Time
}

type UpdateServer struct {
	*Server
	client      *http.Client
	controlDir  string
	releasesURL string
	cache       updateCache
}

func configuredControlDir() (string, bool) {
	if v := strings.TrimSpace(os.Getenv("CLOUDPAM_CONTROL_DIR")); v != "" {
		return v, true
	}
	if v := strings.TrimSpace(os.Getenv("CLOUDPAM_UPGRADE_DATA_DIR")); v != "" {
		return v, true
	}
	return defaultControlDir, true
}

func NewUpdateServer(srv *Server) *UpdateServer {
	repo := getenvDefault("CLOUDPAM_RELEASE_REPO", defaultCloudPAMRepo)
	apiRoot := strings.TrimRight(getenvDefault("CLOUDPAM_GITHUB_API_ROOT", defaultGitHubAPIRoot), "/")
	controlDir, _ := configuredControlDir()
	return &UpdateServer{
		Server:      srv,
		client:      &http.Client{Timeout: 10 * time.Second},
		controlDir:  controlDir,
		releasesURL: apiRoot + "/repos/" + repo + "/releases",
	}
}

func (us *UpdateServer) RegisterProtectedUpdateRoutes(dualMW func(http.Handler) http.Handler, slogger *slog.Logger) {
	adminRead := RequirePermissionMiddleware(auth.ResourceSettings, auth.ActionRead, slogger)
	adminWrite := RequirePermissionMiddleware(auth.ResourceSettings, auth.ActionWrite, slogger)

	us.handleOpenAPIRoute("GET /api/v1/updates",
		dualMW(adminRead(http.HandlerFunc(us.handleCheckUpdates))))
	us.handleOpenAPIRoute("POST /api/v1/updates/upgrade",
		dualMW(adminWrite(http.HandlerFunc(us.handleTriggerUpgrade))))
	us.handleOpenAPIRoute("GET /api/v1/updates/status",
		dualMW(adminRead(http.HandlerFunc(us.handleGetUpgradeStatus))))
	us.handleOpenAPIRoute("POST /api/v1/updates/status/ack",
		dualMW(adminWrite(http.HandlerFunc(us.handleAcknowledgeUpgradeStatus))))
}

func (us *UpdateServer) RegisterUpdateRoutesNoAuth() {
	us.handleOpenAPIRouteFunc("GET /api/v1/updates", us.handleCheckUpdates)
	us.handleOpenAPIRouteFunc("POST /api/v1/updates/upgrade", us.handleTriggerUpgrade)
	us.handleOpenAPIRouteFunc("GET /api/v1/updates/status", us.handleGetUpgradeStatus)
	us.handleOpenAPIRouteFunc("POST /api/v1/updates/status/ack", us.handleAcknowledgeUpgradeStatus)
}

func (us *UpdateServer) handleCheckUpdates(w http.ResponseWriter, r *http.Request) {
	force, _ := strconv.ParseBool(r.URL.Query().Get("force"))

	releases, stale, err := us.loadReleases(force)
	if err != nil && len(releases) == 0 {
		writeJSON(w, http.StatusOK, map[string]any{
			"update_available":  false,
			"error":             "could not fetch releases",
			"checked_at":        time.Now().UTC().Format(time.RFC3339),
			"upgrade_supported": false,
		})
		return
	}

	latest, ok := latestStableRelease(releases)
	currentVersion := cleanVersion(currentAppVersion())
	latestVersion := ""
	updateAvailable := false
	if ok {
		latestVersion = cleanVersion(latest.TagName)
		updateAvailable = compareVersions(currentVersion, latestVersion)
	}
	upgradeSupported := strings.TrimSpace(us.controlDir) != ""

	resp := map[string]any{
		"current_version":   currentVersion,
		"latest_version":    latestVersion,
		"update_available":  updateAvailable,
		"release_notes":     latest.Body,
		"release_url":       latest.HTMLURL,
		"published_at":      latest.PublishedAt,
		"checked_at":        time.Now().UTC().Format(time.RFC3339),
		"upgrade_supported": upgradeSupported,
	}
	if stale || err != nil {
		resp["warning"] = "using stale release metadata"
	}
	writeJSON(w, http.StatusOK, resp)
}

func (us *UpdateServer) handleTriggerUpgrade(w http.ResponseWriter, r *http.Request) {
	controlDir := strings.TrimSpace(us.controlDir)
	if controlDir == "" {
		us.writeErr(r.Context(), w, http.StatusNotImplemented, "in-app upgrade is not enabled", "set CLOUDPAM_CONTROL_DIR to enable file-triggered upgrades")
		return
	}

	statusPath := filepath.Join(controlDir, upgradeStatusFile)
	if status, err := readJSONFile(statusPath); err == nil {
		if state, _ := status["status"].(string); state == "running" {
			us.writeErr(r.Context(), w, http.StatusConflict, "upgrade already in progress", "")
			return
		}
	}

	releases, _, err := us.loadReleases(false)
	if err != nil && len(releases) == 0 {
		us.writeErr(r.Context(), w, http.StatusInternalServerError, "failed to fetch release metadata", err.Error())
		return
	}

	latest, ok := latestStableRelease(releases)
	if !ok {
		us.writeErr(r.Context(), w, http.StatusInternalServerError, "no stable release found", "")
		return
	}

	now := time.Now().UTC()
	upgradeID := newUpgradeID(now)
	request := map[string]any{
		"upgrade_id":         upgradeID,
		"requested_at":       now.Format(time.RFC3339),
		"current_version":    cleanVersion(currentAppVersion()),
		"target_version":     cleanVersion(latest.TagName),
		"target_release_tag": latest.TagName,
		"target_image_tag":   cleanVersion(latest.TagName),
		"release_url":        latest.HTMLURL,
	}
	if user := auth.UserFromContext(r.Context()); user != nil {
		request["requested_by"] = user.Username
	}

	if err := os.MkdirAll(controlDir, 0o755); err != nil {
		us.writeErr(r.Context(), w, http.StatusInternalServerError, "failed to prepare control dir", err.Error())
		return
	}

	requestPath := filepath.Join(controlDir, upgradeRequestedFile)
	f, err := os.Create(requestPath)
	if err != nil {
		us.writeErr(r.Context(), w, http.StatusInternalServerError, "failed to write upgrade request", err.Error())
		return
	}

	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(request); err != nil {
		if closeErr := f.Close(); closeErr != nil {
			us.logger.Warn("failed to close upgrade request file", "path", requestPath, "error", closeErr)
		}
		us.writeErr(r.Context(), w, http.StatusInternalServerError, "failed to write upgrade request", err.Error())
		return
	}
	if err := f.Close(); err != nil {
		us.writeErr(r.Context(), w, http.StatusInternalServerError, "failed to finalize upgrade request", err.Error())
		return
	}

	us.logAudit(r.Context(), "update", "system", cleanVersion(latest.TagName), "cloudpam_upgrade", http.StatusAccepted)
	writeJSON(w, http.StatusAccepted, map[string]any{
		"status":         "upgrade_requested",
		"upgrade_id":     upgradeID,
		"target_version": cleanVersion(latest.TagName),
		"message":        "Upgrade request submitted",
	})
}

func (us *UpdateServer) handleGetUpgradeStatus(w http.ResponseWriter, r *http.Request) {
	controlDir := strings.TrimSpace(us.controlDir)
	if controlDir == "" {
		writeJSON(w, http.StatusOK, map[string]any{
			"status":    "unsupported",
			"supported": false,
		})
		return
	}

	statusPath := filepath.Join(controlDir, upgradeStatusFile)
	status, err := readJSONFile(statusPath)
	if errors.Is(err, os.ErrNotExist) {
		writeJSON(w, http.StatusOK, map[string]any{
			"status":    "idle",
			"supported": true,
		})
		return
	}
	if err != nil {
		us.writeErr(r.Context(), w, http.StatusInternalServerError, "failed to read upgrade status", err.Error())
		return
	}
	info, _ := os.Stat(statusPath)
	ensureUpgradeID(status)
	if idle, ok := us.completedUpgradeIdleResponse(controlDir, status, info); ok {
		writeJSON(w, http.StatusOK, idle)
		return
	}
	status["supported"] = true
	writeJSON(w, http.StatusOK, status)
}

func (us *UpdateServer) handleAcknowledgeUpgradeStatus(w http.ResponseWriter, r *http.Request) {
	controlDir := strings.TrimSpace(us.controlDir)
	if controlDir == "" {
		writeJSON(w, http.StatusOK, map[string]any{
			"status":    "unsupported",
			"supported": false,
		})
		return
	}

	statusPath := filepath.Join(controlDir, upgradeStatusFile)
	status, err := readJSONFile(statusPath)
	if errors.Is(err, os.ErrNotExist) {
		writeJSON(w, http.StatusOK, map[string]any{
			"status":    "idle",
			"supported": true,
		})
		return
	}
	if err != nil {
		us.writeErr(r.Context(), w, http.StatusInternalServerError, "failed to read upgrade status", err.Error())
		return
	}
	if normalizeUpgradeStatus(status["status"]) != "completed" {
		us.writeErr(r.Context(), w, http.StatusConflict, "upgrade is not completed", "")
		return
	}

	upgradeID := ensureUpgradeID(status)
	ack := map[string]any{
		"upgrade_id":      upgradeID,
		"acknowledged_at": time.Now().UTC().Format(time.RFC3339),
	}
	if target, ok := status["target_version"].(string); ok && strings.TrimSpace(target) != "" {
		ack["target_version"] = target
	}
	if err := writeJSONFile(filepath.Join(controlDir, upgradeAckFile), ack); err != nil {
		us.writeErr(r.Context(), w, http.StatusInternalServerError, "failed to acknowledge upgrade status", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":       "idle",
		"supported":    true,
		"upgrade_id":   upgradeID,
		"acknowledged": true,
		"last_upgrade": status,
	})
}

func (us *UpdateServer) completedUpgradeIdleResponse(controlDir string, status map[string]any, info os.FileInfo) (map[string]any, bool) {
	if normalizeUpgradeStatus(status["status"]) != "completed" {
		return nil, false
	}
	upgradeID := ensureUpgradeID(status)
	if ackedUpgradeStatus(controlDir, upgradeID) {
		return map[string]any{
			"status":       "idle",
			"supported":    true,
			"upgrade_id":   upgradeID,
			"acknowledged": true,
			"last_upgrade": status,
		}, true
	}
	completedAt := completedUpgradeTime(status, info)
	if !completedAt.IsZero() && time.Since(completedAt) > completedUpgradeStatusTTL {
		return map[string]any{
			"status":                   "idle",
			"supported":                true,
			"upgrade_id":               upgradeID,
			"completed_status_expired": true,
			"last_upgrade":             status,
		}, true
	}
	return nil, false
}

func (us *UpdateServer) loadReleases(force bool) ([]updateRelease, bool, error) {
	us.cache.mu.Lock()
	defer us.cache.mu.Unlock()

	if !force && len(us.cache.releases) > 0 && time.Since(us.cache.fetchedAt) < updateCacheTTL {
		return append([]updateRelease(nil), us.cache.releases...), false, nil
	}

	resp, err := us.client.Get(us.releasesURL)
	if err != nil {
		if len(us.cache.releases) > 0 {
			return append([]updateRelease(nil), us.cache.releases...), true, err
		}
		return nil, false, err
	}

	if resp.StatusCode != http.StatusOK {
		closeErr := resp.Body.Close()
		err = errors.New("github api returned " + resp.Status)
		if closeErr != nil {
			err = errors.Join(err, closeErr)
		}
		if len(us.cache.releases) > 0 {
			return append([]updateRelease(nil), us.cache.releases...), true, err
		}
		return nil, false, err
	}

	var releases []updateRelease
	decodeErr := json.NewDecoder(resp.Body).Decode(&releases)
	closeErr := resp.Body.Close()
	if decodeErr != nil {
		err = decodeErr
		if closeErr != nil {
			err = errors.Join(err, closeErr)
		}
		if len(us.cache.releases) > 0 {
			return append([]updateRelease(nil), us.cache.releases...), true, err
		}
		return nil, false, err
	}
	if closeErr != nil {
		if len(us.cache.releases) > 0 {
			return append([]updateRelease(nil), us.cache.releases...), true, closeErr
		}
		return nil, false, closeErr
	}

	us.cache.releases = append([]updateRelease(nil), releases...)
	us.cache.fetchedAt = time.Now()
	return append([]updateRelease(nil), releases...), false, nil
}

func latestStableRelease(releases []updateRelease) (updateRelease, bool) {
	var latest updateRelease
	found := false
	for _, release := range releases {
		if release.Draft || release.Prerelease {
			continue
		}
		if !isSemverTag(release.TagName) {
			continue
		}
		if !found || versionGreater(parseVersion(release.TagName), parseVersion(latest.TagName)) {
			latest = release
			found = true
		}
	}
	return latest, found
}

func currentAppVersion() string {
	return getenvDefault("APP_VERSION", "dev")
}

func isSemverTag(v string) bool {
	parts := strings.Split(cleanVersion(v), ".")
	if len(parts) != 3 {
		return false
	}
	for _, part := range parts {
		if _, err := strconv.Atoi(part); err != nil {
			return false
		}
	}
	return true
}

func parseVersion(v string) [3]int {
	var out [3]int
	parts := strings.Split(cleanVersion(v), ".")
	for i := 0; i < len(parts) && i < 3; i++ {
		out[i], _ = strconv.Atoi(parts[i])
	}
	return out
}

func compareVersions(current, latest string) bool {
	return versionGreater(parseVersion(latest), parseVersion(current))
}

func versionGreater(a, b [3]int) bool {
	for i := 0; i < len(a); i++ {
		if a[i] > b[i] {
			return true
		}
		if a[i] < b[i] {
			return false
		}
	}
	return false
}

func readJSONFile(path string) (map[string]any, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}

	var data map[string]any
	decodeErr := json.NewDecoder(f).Decode(&data)
	closeErr := f.Close()
	if decodeErr != nil {
		if closeErr != nil {
			return nil, errors.Join(decodeErr, closeErr)
		}
		return nil, decodeErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	return data, nil
}

func writeJSONFile(path string, data map[string]any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(data); err != nil {
		if closeErr := f.Close(); closeErr != nil {
			return errors.Join(err, closeErr)
		}
		return err
	}
	return f.Close()
}

func newUpgradeID(t time.Time) string {
	return "upg_" + t.UTC().Format("20060102T150405.000000000Z")
}

func ensureUpgradeID(status map[string]any) string {
	if id, ok := status["upgrade_id"].(string); ok && strings.TrimSpace(id) != "" {
		return strings.TrimSpace(id)
	}
	id := derivedUpgradeID(status)
	status["upgrade_id"] = id
	return id
}

func derivedUpgradeID(status map[string]any) string {
	fields := []string{
		stringValue(status["status"]),
		stringValue(status["message"]),
		stringValue(status["current_version"]),
		stringValue(status["target_version"]),
		stringValue(status["target_image_tag"]),
		stringValue(status["requested_at"]),
		stringValue(status["started_at"]),
		stringValue(status["updated_at"]),
		stringValue(status["release_url"]),
	}
	sum := sha1.Sum([]byte(strings.Join(fields, "|")))
	return "upg_" + hex.EncodeToString(sum[:])[:16]
}

func ackedUpgradeStatus(controlDir string, upgradeID string) bool {
	ack, err := readJSONFile(filepath.Join(controlDir, upgradeAckFile))
	if err != nil {
		return false
	}
	ackID, _ := ack["upgrade_id"].(string)
	return strings.TrimSpace(ackID) == upgradeID
}

func completedUpgradeTime(status map[string]any, info os.FileInfo) time.Time {
	for _, key := range []string{"finished_at", "completed_at", "updated_at"} {
		if parsed, ok := parseStatusTime(status[key]); ok {
			return parsed
		}
	}
	if info != nil {
		return info.ModTime()
	}
	return time.Time{}
}

func parseStatusTime(value any) (time.Time, bool) {
	text := stringValue(value)
	if text == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		parsed, err := time.Parse(layout, text)
		if err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}

func normalizeUpgradeStatus(value any) string {
	return strings.ToLower(strings.TrimSpace(stringValue(value)))
}

func stringValue(value any) string {
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	case fmt.Stringer:
		return strings.TrimSpace(v.String())
	default:
		return ""
	}
}

func getenvDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
