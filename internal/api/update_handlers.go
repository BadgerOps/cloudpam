package api

import (
	"encoding/json"
	"errors"
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
	updateCacheTTL       = time.Hour
	defaultControlDir    = "/var/lib/cloudpam-control"
	defaultCloudPAMRepo  = "BadgerOps/cloudpam"
	defaultGitHubAPIRoot = "https://api.github.com"
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
	client       *http.Client
	controlDir   string
	releasesURL  string
	releasesRepo string
	cache        updateCache
}

func NewUpdateServer(srv *Server) *UpdateServer {
	repo := getenvDefault("CLOUDPAM_RELEASE_REPO", defaultCloudPAMRepo)
	apiRoot := strings.TrimRight(getenvDefault("CLOUDPAM_GITHUB_API_ROOT", defaultGitHubAPIRoot), "/")
	return &UpdateServer{
		Server:       srv,
		client:       &http.Client{Timeout: 10 * time.Second},
		controlDir:   getenvDefault("CLOUDPAM_CONTROL_DIR", defaultControlDir),
		releasesRepo: repo,
		releasesURL:  apiRoot + "/repos/" + repo + "/releases",
	}
}

func (us *UpdateServer) RegisterProtectedUpdateRoutes(dualMW func(http.Handler) http.Handler, slogger *slog.Logger) {
	adminRead := RequirePermissionMiddleware(auth.ResourceSettings, auth.ActionRead, slogger)
	adminWrite := RequirePermissionMiddleware(auth.ResourceSettings, auth.ActionWrite, slogger)

	us.mux.Handle("GET /api/v1/updates",
		dualMW(adminRead(http.HandlerFunc(us.handleCheckUpdates))))
	us.mux.Handle("POST /api/v1/updates/upgrade",
		dualMW(adminWrite(http.HandlerFunc(us.handleTriggerUpgrade))))
	us.mux.Handle("GET /api/v1/updates/status",
		dualMW(adminRead(http.HandlerFunc(us.handleGetUpgradeStatus))))
}

func (us *UpdateServer) RegisterUpdateRoutesNoAuth() {
	us.mux.HandleFunc("GET /api/v1/updates", us.handleCheckUpdates)
	us.mux.HandleFunc("POST /api/v1/updates/upgrade", us.handleTriggerUpgrade)
	us.mux.HandleFunc("GET /api/v1/updates/status", us.handleGetUpgradeStatus)
}

func (us *UpdateServer) handleCheckUpdates(w http.ResponseWriter, r *http.Request) {
	force, _ := strconv.ParseBool(r.URL.Query().Get("force"))

	releases, stale, err := us.loadReleases(force)
	if err != nil && len(releases) == 0 {
		writeJSON(w, http.StatusOK, map[string]any{
			"update_available": false,
			"error":            "could not fetch releases",
			"checked_at":       time.Now().UTC().Format(time.RFC3339),
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

	resp := map[string]any{
		"current_version":  currentVersion,
		"latest_version":   latestVersion,
		"update_available": updateAvailable,
		"release_notes":    latest.Body,
		"release_url":      latest.HTMLURL,
		"published_at":     latest.PublishedAt,
		"checked_at":       time.Now().UTC().Format(time.RFC3339),
	}
	if stale || err != nil {
		resp["warning"] = "using stale release metadata"
	}
	writeJSON(w, http.StatusOK, resp)
}

func (us *UpdateServer) handleTriggerUpgrade(w http.ResponseWriter, r *http.Request) {
	statusPath := filepath.Join(us.controlDir, "upgrade-status.json")
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

	request := map[string]any{
		"requested_at":       time.Now().UTC().Format(time.RFC3339),
		"current_version":    cleanVersion(currentAppVersion()),
		"target_version":     cleanVersion(latest.TagName),
		"target_release_tag": latest.TagName,
		"target_image_tag":   cleanVersion(latest.TagName),
		"release_url":        latest.HTMLURL,
	}
	if user := auth.UserFromContext(r.Context()); user != nil {
		request["requested_by"] = user.Username
	}

	if err := os.MkdirAll(us.controlDir, 0o755); err != nil {
		us.writeErr(r.Context(), w, http.StatusInternalServerError, "failed to prepare control dir", err.Error())
		return
	}

	requestPath := filepath.Join(us.controlDir, "upgrade-requested")
	f, err := os.Create(requestPath)
	if err != nil {
		us.writeErr(r.Context(), w, http.StatusInternalServerError, "failed to write upgrade request", err.Error())
		return
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(request); err != nil {
		us.writeErr(r.Context(), w, http.StatusInternalServerError, "failed to write upgrade request", err.Error())
		return
	}

	us.logAudit(r.Context(), "update", "system", cleanVersion(latest.TagName), "cloudpam_upgrade", http.StatusAccepted)
	writeJSON(w, http.StatusAccepted, map[string]any{
		"status":         "upgrade_requested",
		"target_version": cleanVersion(latest.TagName),
	})
}

func (us *UpdateServer) handleGetUpgradeStatus(w http.ResponseWriter, r *http.Request) {
	statusPath := filepath.Join(us.controlDir, "upgrade-status.json")
	status, err := readJSONFile(statusPath)
	if errors.Is(err, os.ErrNotExist) {
		writeJSON(w, http.StatusOK, map[string]any{"status": "idle"})
		return
	}
	if err != nil {
		us.writeErr(r.Context(), w, http.StatusInternalServerError, "failed to read upgrade status", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, status)
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
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		err = errors.New("github api returned " + resp.Status)
		if len(us.cache.releases) > 0 {
			return append([]updateRelease(nil), us.cache.releases...), true, err
		}
		return nil, false, err
	}

	var releases []updateRelease
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		if len(us.cache.releases) > 0 {
			return append([]updateRelease(nil), us.cache.releases...), true, err
		}
		return nil, false, err
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

func cleanVersion(v string) string {
	v = strings.TrimSpace(v)
	if strings.HasPrefix(v, "v") {
		return v[1:]
	}
	return v
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
	defer f.Close()

	var data map[string]any
	if err := json.NewDecoder(f).Decode(&data); err != nil {
		return nil, err
	}
	return data, nil
}

func getenvDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
