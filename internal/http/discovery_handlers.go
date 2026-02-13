package http

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"cloudpam/internal/auth"
	"cloudpam/internal/discovery"
	"cloudpam/internal/domain"
	"cloudpam/internal/storage"
)

// DiscoveryServer handles discovery-related API endpoints.
type DiscoveryServer struct {
	srv         *Server
	store       storage.DiscoveryStore
	syncService *discovery.SyncService
}

// NewDiscoveryServer creates a new DiscoveryServer.
func NewDiscoveryServer(srv *Server, store storage.DiscoveryStore, syncService *discovery.SyncService) *DiscoveryServer {
	return &DiscoveryServer{srv: srv, store: store, syncService: syncService}
}

// RegisterDiscoveryRoutes registers discovery routes without RBAC.
func (d *DiscoveryServer) RegisterDiscoveryRoutes() {
	d.srv.mux.HandleFunc("/api/v1/discovery/resources", d.handleResources)
	d.srv.mux.HandleFunc("/api/v1/discovery/resources/", d.handleResourcesSubroutes)
	d.srv.mux.HandleFunc("/api/v1/discovery/sync", d.handleSync)
	d.srv.mux.HandleFunc("/api/v1/discovery/sync/", d.handleSyncSubroutes)
}

// RegisterProtectedDiscoveryRoutes registers discovery routes with RBAC.
func (d *DiscoveryServer) RegisterProtectedDiscoveryRoutes(dualMW Middleware, logger *slog.Logger) {
	readMW := RequirePermissionMiddleware(auth.ResourceDiscovery, auth.ActionRead, logger)

	d.srv.mux.Handle("/api/v1/discovery/resources", dualMW(readMW(http.HandlerFunc(d.handleResources))))
	d.srv.mux.Handle("/api/v1/discovery/resources/", dualMW(d.protectedResourcesSubroutes(logger)))
	d.srv.mux.Handle("/api/v1/discovery/sync", dualMW(d.protectedSyncHandler(logger)))
	d.srv.mux.Handle("/api/v1/discovery/sync/", dualMW(readMW(http.HandlerFunc(d.handleSyncSubroutes))))
}

// handleResources handles GET /api/v1/discovery/resources
func (d *DiscoveryServer) handleResources(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		d.srv.writeErr(r.Context(), w, http.StatusMethodNotAllowed, "method not allowed", "")
		return
	}

	q := r.URL.Query()
	accountID, err := strconv.ParseInt(q.Get("account_id"), 10, 64)
	if err != nil || accountID < 1 {
		d.srv.writeErr(r.Context(), w, http.StatusBadRequest, "account_id is required and must be a positive integer", "")
		return
	}

	page, _ := strconv.Atoi(q.Get("page"))
	pageSize, _ := strconv.Atoi(q.Get("page_size"))

	filters := domain.DiscoveryFilters{
		Provider:     q.Get("provider"),
		Region:       q.Get("region"),
		ResourceType: q.Get("resource_type"),
		Status:       q.Get("status"),
		Page:         page,
		PageSize:     pageSize,
	}

	if linked := q.Get("linked"); linked != "" {
		val := linked == "true" || linked == "1"
		filters.HasPool = &val
	}

	items, total, err := d.store.ListDiscoveredResources(r.Context(), accountID, filters)
	if err != nil {
		d.srv.writeErr(r.Context(), w, http.StatusInternalServerError, "list resources failed", err.Error())
		return
	}

	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 50
	}

	if items == nil {
		items = []domain.DiscoveredResource{}
	}
	writeJSON(w, http.StatusOK, domain.DiscoveryResourcesResponse{
		Items:    items,
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	})
}

// handleResourcesSubroutes handles /api/v1/discovery/resources/{id}[/link]
func (d *DiscoveryServer) handleResourcesSubroutes(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/discovery/resources/")

	// Check for /link suffix
	if strings.HasSuffix(path, "/link") {
		idStr := strings.TrimSuffix(path, "/link")
		id, err := uuid.Parse(idStr)
		if err != nil {
			d.srv.writeErr(r.Context(), w, http.StatusBadRequest, "invalid resource id", "")
			return
		}
		d.handleLink(w, r, id)
		return
	}

	// Single resource by ID
	id, err := uuid.Parse(path)
	if err != nil {
		d.srv.writeErr(r.Context(), w, http.StatusBadRequest, "invalid resource id", "")
		return
	}

	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		d.srv.writeErr(r.Context(), w, http.StatusMethodNotAllowed, "method not allowed", "")
		return
	}

	res, err := d.store.GetDiscoveredResource(r.Context(), id)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			d.srv.writeErr(r.Context(), w, http.StatusNotFound, "resource not found", "")
			return
		}
		d.srv.writeErr(r.Context(), w, http.StatusInternalServerError, "get resource failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// handleLink handles POST/DELETE /api/v1/discovery/resources/{id}/link
func (d *DiscoveryServer) handleLink(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	switch r.Method {
	case http.MethodPost:
		var body struct {
			PoolID int64 `json:"pool_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.PoolID < 1 {
			d.srv.writeErr(r.Context(), w, http.StatusBadRequest, "pool_id is required and must be a positive integer", "")
			return
		}

		// Verify pool exists
		_, found, err := d.srv.store.GetPool(r.Context(), body.PoolID)
		if err != nil {
			d.srv.writeErr(r.Context(), w, http.StatusInternalServerError, "pool lookup failed", err.Error())
			return
		}
		if !found {
			d.srv.writeErr(r.Context(), w, http.StatusNotFound, "pool not found", "")
			return
		}

		if err := d.store.LinkResourceToPool(r.Context(), id, body.PoolID); err != nil {
			if errors.Is(err, storage.ErrNotFound) {
				d.srv.writeErr(r.Context(), w, http.StatusNotFound, "resource not found", "")
				return
			}
			d.srv.writeErr(r.Context(), w, http.StatusInternalServerError, "link failed", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "linked"})

	case http.MethodDelete:
		if err := d.store.UnlinkResource(r.Context(), id); err != nil {
			if errors.Is(err, storage.ErrNotFound) {
				d.srv.writeErr(r.Context(), w, http.StatusNotFound, "resource not found", "")
				return
			}
			d.srv.writeErr(r.Context(), w, http.StatusInternalServerError, "unlink failed", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "unlinked"})

	default:
		w.Header().Set("Allow", strings.Join([]string{http.MethodPost, http.MethodDelete}, ", "))
		d.srv.writeErr(r.Context(), w, http.StatusMethodNotAllowed, "method not allowed", "")
	}
}

// handleSync handles POST /api/v1/discovery/sync (trigger) and GET (list jobs)
func (d *DiscoveryServer) handleSync(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		d.triggerSync(w, r)
	case http.MethodGet:
		d.listSyncJobs(w, r)
	default:
		w.Header().Set("Allow", strings.Join([]string{http.MethodGet, http.MethodPost}, ", "))
		d.srv.writeErr(r.Context(), w, http.StatusMethodNotAllowed, "method not allowed", "")
	}
}

func (d *DiscoveryServer) triggerSync(w http.ResponseWriter, r *http.Request) {
	var body struct {
		AccountID int64 `json:"account_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.AccountID < 1 {
		d.srv.writeErr(r.Context(), w, http.StatusBadRequest, "account_id is required and must be a positive integer", "")
		return
	}

	// Verify account exists
	account, found, err := d.srv.store.GetAccount(r.Context(), body.AccountID)
	if err != nil {
		d.srv.writeErr(r.Context(), w, http.StatusInternalServerError, "account lookup failed", err.Error())
		return
	}
	if !found {
		d.srv.writeErr(r.Context(), w, http.StatusNotFound, "account not found", "")
		return
	}

	if d.syncService == nil {
		d.srv.writeErr(r.Context(), w, http.StatusServiceUnavailable, "sync service not available", "")
		return
	}

	job, err := d.syncService.Sync(r.Context(), account)
	if err != nil {
		// Job was still created even on failure — return it
		if job != nil {
			writeJSON(w, http.StatusOK, job)
			return
		}
		d.srv.writeErr(r.Context(), w, http.StatusInternalServerError, "sync failed", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, job)
}

func (d *DiscoveryServer) listSyncJobs(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	accountID, err := strconv.ParseInt(q.Get("account_id"), 10, 64)
	if err != nil || accountID < 1 {
		d.srv.writeErr(r.Context(), w, http.StatusBadRequest, "account_id is required and must be a positive integer", "")
		return
	}

	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit < 1 {
		limit = 20
	}

	jobs, err := d.store.ListSyncJobs(r.Context(), accountID, limit)
	if err != nil {
		d.srv.writeErr(r.Context(), w, http.StatusInternalServerError, "list sync jobs failed", err.Error())
		return
	}

	if jobs == nil {
		jobs = []domain.SyncJob{}
	}
	writeJSON(w, http.StatusOK, domain.SyncJobsResponse{Items: jobs})
}

// handleSyncSubroutes handles GET /api/v1/discovery/sync/{id}
func (d *DiscoveryServer) handleSyncSubroutes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		d.srv.writeErr(r.Context(), w, http.StatusMethodNotAllowed, "method not allowed", "")
		return
	}

	idStr := strings.TrimPrefix(r.URL.Path, "/api/v1/discovery/sync/")
	id, err := uuid.Parse(idStr)
	if err != nil {
		d.srv.writeErr(r.Context(), w, http.StatusBadRequest, "invalid sync job id", "")
		return
	}

	job, err := d.store.GetSyncJob(r.Context(), id)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			d.srv.writeErr(r.Context(), w, http.StatusNotFound, "sync job not found", "")
			return
		}
		d.srv.writeErr(r.Context(), w, http.StatusInternalServerError, "get sync job failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, job)
}

// protectedResourcesSubroutes returns a handler for /api/v1/discovery/resources/ with RBAC.
func (d *DiscoveryServer) protectedResourcesSubroutes(logger *slog.Logger) http.Handler {
	readMW := RequirePermissionMiddleware(auth.ResourceDiscovery, auth.ActionRead, logger)
	writeMW := RequirePermissionMiddleware(auth.ResourceDiscovery, auth.ActionUpdate, logger)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/api/v1/discovery/resources/")

		// Link/unlink needs write permission
		if strings.HasSuffix(path, "/link") {
			writeMW(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				d.handleResourcesSubroutes(w, r)
			})).ServeHTTP(w, r)
			return
		}

		// Single resource GET needs read permission
		readMW(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			d.handleResourcesSubroutes(w, r)
		})).ServeHTTP(w, r)
	})
}

// protectedSyncHandler returns a handler for /api/v1/discovery/sync with RBAC.
func (d *DiscoveryServer) protectedSyncHandler(logger *slog.Logger) http.Handler {
	readMW := RequirePermissionMiddleware(auth.ResourceDiscovery, auth.ActionRead, logger)
	writeMW := RequirePermissionMiddleware(auth.ResourceDiscovery, auth.ActionCreate, logger)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			readMW(http.HandlerFunc(d.listSyncJobs)).ServeHTTP(w, r)
		case http.MethodPost:
			writeMW(http.HandlerFunc(d.triggerSync)).ServeHTTP(w, r)
		default:
			w.Header().Set("Allow", strings.Join([]string{http.MethodGet, http.MethodPost}, ", "))
			d.srv.writeErr(r.Context(), w, http.StatusMethodNotAllowed, "method not allowed", "")
		}
	})
}
