package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/netip"
	"sort"
	"strconv"
	"strings"
	"time"

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
	keyStore    auth.KeyStore
}

// NewDiscoveryServer creates a new DiscoveryServer.
func NewDiscoveryServer(srv *Server, store storage.DiscoveryStore, syncService *discovery.SyncService, keyStore auth.KeyStore) *DiscoveryServer {
	return &DiscoveryServer{srv: srv, store: store, syncService: syncService, keyStore: keyStore}
}

// RegisterDiscoveryRoutes registers discovery routes without RBAC.
func (d *DiscoveryServer) RegisterDiscoveryRoutes() {
	d.srv.handleOpenAPIRouteFunc("/api/v1/discovery/resources", d.handleResources)
	d.srv.handleOpenAPIRouteFunc("/api/v1/discovery/resources/", d.handleResourcesSubroutes)
	d.srv.handleOpenAPIRouteFunc("/api/v1/discovery/import", d.handleImportDiscoveredSchema)
	d.srv.handleOpenAPIRouteFunc("/api/v1/discovery/import/", d.handleImportSubroutes)
	d.srv.handleOpenAPIRouteFunc("/api/v1/discovery/sync", d.handleSync)
	d.srv.handleOpenAPIRouteFunc("/api/v1/discovery/sync/", d.handleSyncSubroutes)
	d.srv.handleOpenAPIRouteFunc("/api/v1/discovery/ingest", d.handleIngest)
	d.srv.handleOpenAPIRouteFunc("/api/v1/discovery/ingest/org", d.handleOrgIngest)
	d.srv.handleOpenAPIRouteFunc("/api/v1/discovery/agents", d.handleListAgents)
	d.srv.handleOpenAPIRouteFunc("/api/v1/discovery/agents/", d.handleAgentsSubroutes)
}

// RegisterProtectedDiscoveryRoutes registers discovery routes with RBAC.
func (d *DiscoveryServer) RegisterProtectedDiscoveryRoutes(dualMW Middleware, logger *slog.Logger) {
	readMW := RequirePermissionMiddleware(auth.ResourceDiscovery, auth.ActionRead, logger)
	createMW := RequirePermissionMiddleware(auth.ResourceDiscovery, auth.ActionCreate, logger)

	d.srv.handleOpenAPIRoute("/api/v1/discovery/resources", dualMW(readMW(http.HandlerFunc(d.handleResources))))
	d.srv.handleOpenAPIRoute("/api/v1/discovery/resources/", dualMW(d.protectedResourcesSubroutes(logger)))
	d.srv.handleOpenAPIRoute("/api/v1/discovery/import", dualMW(createMW(http.HandlerFunc(d.handleImportDiscoveredSchema))))
	d.srv.handleOpenAPIRoute("/api/v1/discovery/import/", dualMW(createMW(http.HandlerFunc(d.handleImportSubroutes))))
	d.srv.handleOpenAPIRoute("/api/v1/discovery/sync", dualMW(d.protectedSyncHandler(logger)))
	d.srv.handleOpenAPIRoute("/api/v1/discovery/sync/", dualMW(readMW(http.HandlerFunc(d.handleSyncSubroutes))))

	// Agent endpoints — all go through dualMW (API key or session auth)
	d.srv.handleOpenAPIRoute("/api/v1/discovery/ingest", dualMW(createMW(http.HandlerFunc(d.handleIngest))))
	d.srv.handleOpenAPIRoute("/api/v1/discovery/ingest/org", dualMW(createMW(http.HandlerFunc(d.handleOrgIngest))))
	d.srv.handleOpenAPIRoute("/api/v1/discovery/agents", dualMW(readMW(http.HandlerFunc(d.handleListAgents))))
	d.srv.handleOpenAPIRoute("/api/v1/discovery/agents/", dualMW(d.protectedAgentsSubroutes(logger)))
}

// protectedAgentsSubroutes returns a handler for /api/v1/discovery/agents/ with RBAC.
func (d *DiscoveryServer) protectedAgentsSubroutes(logger *slog.Logger) http.Handler {
	readMW := RequirePermissionMiddleware(auth.ResourceDiscovery, auth.ActionRead, logger)
	createMW := RequirePermissionMiddleware(auth.ResourceDiscovery, auth.ActionCreate, logger)
	updateMW := RequirePermissionMiddleware(auth.ResourceDiscovery, auth.ActionUpdate, logger)
	deleteMW := RequirePermissionMiddleware(auth.ResourceDiscovery, auth.ActionDelete, logger)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/api/v1/discovery/agents/")
		path = strings.TrimRight(path, "/")

		switch {
		case path == "provision":
			// Only admins/operators can provision agents
			createMW(http.HandlerFunc(d.handleProvisionAgent)).ServeHTTP(w, r)
		case path == "register":
			// Agents register with their provisioned API key (discovery:write → create)
			createMW(http.HandlerFunc(d.handleAgentRegister)).ServeHTTP(w, r)
		case path == "heartbeat":
			createMW(http.HandlerFunc(d.handleAgentHeartbeat)).ServeHTTP(w, r)
		case strings.HasSuffix(path, "/approve"), strings.HasSuffix(path, "/reject"):
			updateMW(http.HandlerFunc(d.handleAgentsSubroutes)).ServeHTTP(w, r)
		case r.Method == http.MethodDelete:
			deleteMW(http.HandlerFunc(d.handleDeleteAgent)).ServeHTTP(w, r)
		default:
			readMW(http.HandlerFunc(d.handleGetAgent)).ServeHTTP(w, r)
		}
	})
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

type discoveryImportResponse struct {
	AccountsImported int      `json:"accounts_imported"`
	PoolsCreated     int      `json:"pools_created"`
	ResourcesLinked  int      `json:"resources_linked"`
	Skipped          int      `json:"skipped"`
	Errors           []string `json:"errors"`
}

// handleImportSubroutes handles preview/apply endpoints for selected discovery imports.
func (d *DiscoveryServer) handleImportSubroutes(w http.ResponseWriter, r *http.Request) {
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/discovery/import/"), "/")
	switch path {
	case "preview":
		d.handleImportPreview(w, r)
	case "apply":
		d.handleImportApply(w, r)
	default:
		d.srv.writeErr(r.Context(), w, http.StatusNotFound, "not found", "")
	}
}

func (d *DiscoveryServer) handleImportPreview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		d.srv.writeErr(r.Context(), w, http.StatusMethodNotAllowed, "method not allowed", "")
		return
	}

	var req domain.DiscoveryImportPreviewRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		d.srv.writeErr(r.Context(), w, http.StatusBadRequest, "invalid request body", err.Error())
		return
	}
	if err := validateDiscoveryImportSelection(req.AccountID, req.ResourceIDs); err != nil {
		d.srv.writeErr(r.Context(), w, http.StatusBadRequest, err.Error(), "")
		return
	}

	resp, err := d.previewDiscoveryImport(r.Context(), req)
	if err != nil {
		d.srv.writeErr(r.Context(), w, http.StatusInternalServerError, "preview import failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (d *DiscoveryServer) handleImportApply(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		d.srv.writeErr(r.Context(), w, http.StatusMethodNotAllowed, "method not allowed", "")
		return
	}

	var req domain.DiscoveryImportApplyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		d.srv.writeErr(r.Context(), w, http.StatusBadRequest, "invalid request body", err.Error())
		return
	}
	if err := validateDiscoveryImportSelection(req.AccountID, req.ResourceIDs); err != nil {
		d.srv.writeErr(r.Context(), w, http.StatusBadRequest, err.Error(), "")
		return
	}

	resp, err := d.applyDiscoveryImport(r.Context(), req, discoveryImportApplyOptions{})
	if err != nil {
		d.srv.writeErr(r.Context(), w, http.StatusInternalServerError, "apply import failed", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

type discoveryImportApplyOptions struct {
	AllowBlocked bool
}

func (d *DiscoveryServer) applyDiscoveryImport(ctx context.Context, req domain.DiscoveryImportApplyRequest, opts discoveryImportApplyOptions) (domain.DiscoveryImportApplyResponse, error) {
	previewReq := domain.DiscoveryImportPreviewRequest(req)
	preview, err := d.previewDiscoveryImport(ctx, previewReq)
	if err != nil {
		return domain.DiscoveryImportApplyResponse{}, err
	}

	resp := domain.DiscoveryImportApplyResponse{Preview: preview, Errors: []string{}}
	items := append([]domain.DiscoveryImportPreviewItem{}, preview.Items...)
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].ResourceType == items[j].ResourceType {
			return items[i].ProviderResourceID < items[j].ProviderResourceID
		}
		if items[i].ResourceType == domain.ResourceTypeVPC {
			return true
		}
		if items[j].ResourceType == domain.ResourceTypeVPC {
			return false
		}
		return items[i].ResourceType < items[j].ResourceType
	})

	createdPoolByProviderID := make(map[string]int64)
	for _, item := range items {
		if item.Status != "importable" && !canForceDiscoveryImport(item, opts) {
			resp.Skipped++
			continue
		}
		res, err := d.store.GetDiscoveredResource(ctx, item.ResourceID)
		if err != nil {
			resp.Errors = append(resp.Errors, fmt.Sprintf("%s: load resource: %v", item.ResourceID, err))
			resp.Skipped++
			continue
		}

		if item.ProposedPoolID != nil && (item.ProposedAction == "link_pool" || opts.AllowBlocked) {
			if err := d.store.LinkResourceToPool(ctx, item.ResourceID, *item.ProposedPoolID); err != nil {
				resp.Errors = append(resp.Errors, fmt.Sprintf("%s: link pool: %v", res.ResourceID, err))
				resp.Skipped++
				continue
			}
			createdPoolByProviderID[res.ResourceID] = *item.ProposedPoolID
			resp.ResourcesLinked++
			resp.LinkedResourceIDs = append(resp.LinkedResourceIDs, item.ResourceID)
			continue
		}

		parentID := item.ProposedParentPoolID
		if parentID == nil && res.ParentResourceID != nil {
			if id, ok := createdPoolByProviderID[*res.ParentResourceID]; ok {
				parentID = &id
			}
		}
		if res.ResourceType == domain.ResourceTypeSubnet && res.ParentResourceID != nil && parentID == nil {
			resp.Errors = append(resp.Errors, fmt.Sprintf("%s: parent %s was not imported or linked", res.ResourceID, *res.ParentResourceID))
			resp.Skipped++
			continue
		}
		pool, err := d.createDiscoveredPool(ctx, req.AccountID, *res, parentID)
		if err != nil {
			resp.Errors = append(resp.Errors, fmt.Sprintf("%s: create pool: %v", res.ResourceID, err))
			resp.Skipped++
			continue
		}
		if err := d.store.LinkResourceToPool(ctx, item.ResourceID, pool.ID); err != nil {
			resp.Errors = append(resp.Errors, fmt.Sprintf("%s: link new pool: %v", res.ResourceID, err))
			resp.Skipped++
			continue
		}
		createdPoolByProviderID[res.ResourceID] = pool.ID
		resp.PoolsCreated++
		resp.ResourcesLinked++
		resp.CreatedPoolIDs = append(resp.CreatedPoolIDs, pool.ID)
		resp.LinkedResourceIDs = append(resp.LinkedResourceIDs, item.ResourceID)
	}

	return resp, nil
}

func canForceDiscoveryImport(item domain.DiscoveryImportPreviewItem, opts discoveryImportApplyOptions) bool {
	if !opts.AllowBlocked {
		return false
	}
	if item.Status == "not_found" || item.Status == "already_linked" || item.Status == "linked_only" {
		return false
	}
	if item.ResourceType != domain.ResourceTypeVPC && item.ResourceType != domain.ResourceTypeSubnet {
		return false
	}
	if item.CIDR == "" {
		return false
	}
	_, err := netip.ParsePrefix(item.CIDR)
	return err == nil
}

func validateDiscoveryImportSelection(accountID int64, resourceIDs []uuid.UUID) error {
	if accountID < 1 {
		return fmt.Errorf("account_id is required and must be a positive integer")
	}
	if len(resourceIDs) == 0 {
		return fmt.Errorf("resource_ids must include at least one discovered resource")
	}
	return nil
}

func (d *DiscoveryServer) previewDiscoveryImport(ctx context.Context, req domain.DiscoveryImportPreviewRequest) (domain.DiscoveryImportPreviewResponse, error) {
	if _, found, err := d.srv.store.GetAccount(ctx, req.AccountID); err != nil {
		return domain.DiscoveryImportPreviewResponse{}, err
	} else if !found {
		return domain.DiscoveryImportPreviewResponse{}, fmt.Errorf("account not found")
	}

	pools, err := d.srv.store.ListPools(ctx)
	if err != nil {
		return domain.DiscoveryImportPreviewResponse{}, err
	}
	poolsByID := make(map[int64]domain.Pool, len(pools))
	for _, pool := range pools {
		poolsByID[pool.ID] = pool
	}
	var selectedPool *domain.Pool
	if req.PoolID != nil {
		pool, ok := poolsByID[*req.PoolID]
		if !ok {
			return domain.DiscoveryImportPreviewResponse{}, fmt.Errorf("selected pool not found")
		}
		selectedPool = &pool
	}

	accountResources, err := d.listAllDiscoveredResources(ctx, req.AccountID, domain.DiscoveryFilters{})
	if err != nil {
		return domain.DiscoveryImportPreviewResponse{}, err
	}
	resourceByProviderID := make(map[string]domain.DiscoveredResource, len(accountResources))
	for _, res := range accountResources {
		resourceByProviderID[res.ResourceID] = res
	}

	selectedProviderIDs := make(map[string]bool, len(req.ResourceIDs))
	selectedResources := make([]domain.DiscoveredResource, 0, len(req.ResourceIDs))
	for _, id := range req.ResourceIDs {
		res, err := d.store.GetDiscoveredResource(ctx, id)
		if err != nil {
			continue
		}
		if res.AccountID == req.AccountID {
			selectedProviderIDs[res.ResourceID] = true
			selectedResources = append(selectedResources, *res)
		}
	}
	duplicates, err := d.duplicateCIDRsByAccount(ctx, req.AccountID, selectedResources)
	if err != nil {
		return domain.DiscoveryImportPreviewResponse{}, err
	}

	resp := domain.DiscoveryImportPreviewResponse{Items: []domain.DiscoveryImportPreviewItem{}, SelectedPoolID: req.PoolID}
	for _, id := range req.ResourceIDs {
		item := domain.DiscoveryImportPreviewItem{
			ResourceID:      id,
			Status:          "blocked",
			ProposedAction:  "none",
			Issues:          []string{},
			Evidence:        []string{},
			ConflictPoolIDs: []int64{},
		}

		res, err := d.store.GetDiscoveredResource(ctx, id)
		if err != nil {
			if errors.Is(err, storage.ErrNotFound) {
				item.Status = "not_found"
				item.Issues = append(item.Issues, "not_found")
				addDiscoveryPreviewItem(&resp, item)
				continue
			}
			return domain.DiscoveryImportPreviewResponse{}, err
		}

		item.ProviderResourceID = res.ResourceID
		item.Name = res.Name
		item.Provider = res.Provider
		item.Region = res.Region
		item.ResourceType = res.ResourceType
		item.CIDR = res.CIDR
		item.LinkedPoolID = res.PoolID

		if res.AccountID != req.AccountID {
			item.Issues = append(item.Issues, "account_mismatch")
			item.Evidence = append(item.Evidence, fmt.Sprintf("resource belongs to account %d", res.AccountID))
			addDiscoveryPreviewItem(&resp, item)
			continue
		}
		if res.PoolID != nil {
			item.Status = "already_linked"
			item.ProposedAction = "none"
			item.Issues = append(item.Issues, "already_linked")
			addDiscoveryPreviewItem(&resp, item)
			continue
		}
		if res.Status != domain.DiscoveryStatusActive {
			item.Issues = append(item.Issues, "stale_resource")
		}
		if res.ResourceType != domain.ResourceTypeVPC && res.ResourceType != domain.ResourceTypeSubnet {
			item.Status = "linked_only"
			item.ProposedAction = "link_only"
			item.ProposedManagedType = "network_object"
			item.Issues = append(item.Issues, "network_object_only")
			addDiscoveryPreviewItem(&resp, item)
			continue
		}

		if _, err := netip.ParsePrefix(res.CIDR); res.CIDR == "" || err != nil {
			item.Issues = append(item.Issues, "invalid_cidr")
		}
		if selectedPool != nil && res.CIDR != "" {
			if !cidrContains(selectedPool.CIDR, res.CIDR) {
				item.Issues = append(item.Issues, "outside_pool")
				item.Evidence = append(item.Evidence, fmt.Sprintf("%s is not inside selected pool %s", res.CIDR, selectedPool.CIDR))
			} else if res.ResourceType == domain.ResourceTypeVPC {
				item.ProposedParentPoolID = &selectedPool.ID
			}
		}

		if res.ResourceType == domain.ResourceTypeSubnet {
			if res.ParentResourceID != nil {
				parent, ok := resourceByProviderID[*res.ParentResourceID]
				switch {
				case !ok:
					item.Issues = append(item.Issues, "missing_parent")
					item.Evidence = append(item.Evidence, "provider parent "+*res.ParentResourceID+" was not discovered")
				case parent.PoolID != nil:
					item.ProposedParentPoolID = parent.PoolID
				case selectedProviderIDs[parent.ResourceID]:
					item.Evidence = append(item.Evidence, "parent "+parent.ResourceID+" is selected for import")
				default:
					item.Issues = append(item.Issues, "missing_parent")
					item.Evidence = append(item.Evidence, "provider parent "+parent.ResourceID+" is discovered but not linked or selected")
				}
			}
			if item.ProposedParentPoolID == nil && selectedPool != nil && cidrContains(selectedPool.CIDR, res.CIDR) {
				item.ProposedParentPoolID = &selectedPool.ID
			}
		}

		d.classifyPoolRelationships(&item, res, pools)
		if ids := duplicates[res.CIDR]; len(ids) > 0 {
			item.Issues = append(item.Issues, "duplicate_cidr")
			item.DuplicateResourceIDs = ids
			item.Evidence = append(item.Evidence, fmt.Sprintf("%s is also discovered in another account", res.CIDR))
		}

		if len(item.Issues) > 0 || len(item.ConflictPoolIDs) > 0 {
			if hasAnyIssue(item.Issues, "duplicate_cidr") || len(item.ConflictPoolIDs) > 0 {
				item.Status = "conflict"
			} else {
				item.Status = "blocked"
			}
			addDiscoveryPreviewItem(&resp, item)
			continue
		}

		item.Status = "importable"
		item.ProposedManagedType = "discovered_pool"
		if item.ProposedPoolID != nil {
			item.ProposedAction = "link_pool"
		} else {
			item.ProposedAction = "create_pool"
		}
		addDiscoveryPreviewItem(&resp, item)
	}

	return resp, nil
}

func addDiscoveryPreviewItem(resp *domain.DiscoveryImportPreviewResponse, item domain.DiscoveryImportPreviewItem) {
	resp.Items = append(resp.Items, item)
	switch item.Status {
	case "importable":
		resp.Importable++
	case "linked_only":
		resp.LinkedOnly++
	case "already_linked":
		resp.AlreadyLinked++
	case "conflict":
		resp.ConflictCount++
		resp.Blocked++
	default:
		resp.Blocked++
	}
}

func (d *DiscoveryServer) classifyPoolRelationships(item *domain.DiscoveryImportPreviewItem, res *domain.DiscoveredResource, pools []domain.Pool) {
	bestParentBits := -1
	for _, pool := range pools {
		if pool.CIDR == "" || res.CIDR == "" {
			continue
		}
		if cidrEqual(pool.CIDR, res.CIDR) {
			if pool.AccountID != nil && *pool.AccountID != res.AccountID {
				item.ConflictPoolIDs = append(item.ConflictPoolIDs, pool.ID)
				item.Evidence = append(item.Evidence, fmt.Sprintf("exact CIDR exists in pool %d assigned to another account", pool.ID))
				continue
			}
			item.ProposedPoolID = &pool.ID
			item.Evidence = append(item.Evidence, fmt.Sprintf("exact pool match %d", pool.ID))
			continue
		}
		if cidrContains(pool.CIDR, res.CIDR) {
			if bits := prefixLength(pool.CIDR); bits > bestParentBits {
				bestParentBits = bits
				parentID := pool.ID
				if item.ProposedParentPoolID == nil {
					item.ProposedParentPoolID = &parentID
				}
			}
			continue
		}
		if cidrOverlaps(pool.CIDR, res.CIDR) {
			item.ConflictPoolIDs = append(item.ConflictPoolIDs, pool.ID)
			item.Evidence = append(item.Evidence, fmt.Sprintf("%s overlaps existing pool %d (%s)", res.CIDR, pool.ID, pool.CIDR))
		}
	}
}

func (d *DiscoveryServer) duplicateCIDRsByAccount(ctx context.Context, accountID int64, selected []domain.DiscoveredResource) (map[string][]uuid.UUID, error) {
	cidrs := make(map[string]bool)
	for _, res := range selected {
		if res.CIDR != "" {
			cidrs[res.CIDR] = true
		}
	}
	out := make(map[string][]uuid.UUID, len(cidrs))
	if len(cidrs) == 0 {
		return out, nil
	}
	accounts, err := d.srv.store.ListAccounts(ctx)
	if err != nil {
		return nil, err
	}
	for _, account := range accounts {
		if account.ID == accountID {
			continue
		}
		resources, err := d.listAllDiscoveredResources(ctx, account.ID, domain.DiscoveryFilters{
			Status: string(domain.DiscoveryStatusActive),
		})
		if err != nil {
			return nil, err
		}
		for _, res := range resources {
			if cidrs[res.CIDR] {
				out[res.CIDR] = append(out[res.CIDR], res.ID)
			}
		}
	}
	return out, nil
}

func (d *DiscoveryServer) listAllDiscoveredResources(ctx context.Context, accountID int64, filters domain.DiscoveryFilters) ([]domain.DiscoveredResource, error) {
	const pageSize = 1000
	var out []domain.DiscoveredResource
	count := 0
	filters.PageSize = pageSize
	for page := 1; ; page++ {
		filters.Page = page
		resources, total, err := d.store.ListDiscoveredResources(ctx, accountID, filters)
		if err != nil {
			return nil, err
		}
		out = append(out, resources...)
		count += len(resources)
		if len(resources) == 0 || count >= total {
			break
		}
	}
	if out == nil {
		out = []domain.DiscoveredResource{}
	}
	return out, nil
}

func (d *DiscoveryServer) createDiscoveredPool(ctx context.Context, accountID int64, res domain.DiscoveredResource, parentID *int64) (domain.Pool, error) {
	poolType := domain.PoolTypeSubnet
	if res.ResourceType == domain.ResourceTypeVPC {
		poolType = domain.PoolTypeVPC
	}
	name := strings.TrimSpace(res.Name)
	if name == "" {
		name = res.ResourceID
	}
	return d.srv.store.CreatePool(ctx, domain.CreatePool{
		Name:      name,
		CIDR:      res.CIDR,
		ParentID:  parentID,
		AccountID: &accountID,
		Type:      poolType,
		Status:    domain.PoolStatusActive,
		Source:    domain.PoolSourceDiscovered,
		Description: fmt.Sprintf("Imported from %s discovery resource %s in %s",
			res.Provider, res.ResourceID, res.Region),
		Tags: map[string]string{
			"discovery_resource_id": res.ResourceID,
			"discovery_provider":    res.Provider,
			"discovery_region":      res.Region,
			"discovery_type":        string(res.ResourceType),
		},
	})
}

func hasAnyIssue(issues []string, targets ...string) bool {
	for _, issue := range issues {
		for _, target := range targets {
			if issue == target {
				return true
			}
		}
	}
	return false
}

func cidrEqual(a string, b string) bool {
	pa, errA := netip.ParsePrefix(a)
	pb, errB := netip.ParsePrefix(b)
	return errA == nil && errB == nil && pa.Masked() == pb.Masked()
}

func cidrContains(parent string, child string) bool {
	p, errP := netip.ParsePrefix(parent)
	c, errC := netip.ParsePrefix(child)
	if errP != nil || errC != nil || p.Addr().BitLen() != c.Addr().BitLen() {
		return false
	}
	return p.Bits() <= c.Bits() && p.Masked().Contains(c.Masked().Addr())
}

func cidrOverlaps(a string, b string) bool {
	pa, errA := netip.ParsePrefix(a)
	pb, errB := netip.ParsePrefix(b)
	if errA != nil || errB != nil || pa.Addr().BitLen() != pb.Addr().BitLen() {
		return false
	}
	return pa.Masked().Contains(pb.Masked().Addr()) || pb.Masked().Contains(pa.Masked().Addr())
}

func prefixLength(cidr string) int {
	p, err := netip.ParsePrefix(cidr)
	if err != nil {
		return -1
	}
	return p.Bits()
}

// handleImportDiscoveredSchema imports active discovered VPC/subnet resources as pools.
func (d *DiscoveryServer) handleImportDiscoveredSchema(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		d.srv.writeErr(r.Context(), w, http.StatusMethodNotAllowed, "method not allowed", "")
		return
	}

	var body struct {
		AccountID int64 `json:"account_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.AccountID < 1 {
		d.srv.writeErr(r.Context(), w, http.StatusBadRequest, "account_id is required and must be a positive integer", "")
		return
	}

	if _, found, err := d.srv.store.GetAccount(r.Context(), body.AccountID); err != nil {
		d.srv.writeErr(r.Context(), w, http.StatusInternalServerError, "account lookup failed", err.Error())
		return
	} else if !found {
		d.srv.writeErr(r.Context(), w, http.StatusNotFound, "account not found", "")
		return
	}

	resources, err := d.listAllDiscoveredResources(r.Context(), body.AccountID, domain.DiscoveryFilters{
		Status: string(domain.DiscoveryStatusActive),
	})
	if err != nil {
		d.srv.writeErr(r.Context(), w, http.StatusInternalServerError, "list resources failed", err.Error())
		return
	}

	resp := d.importDiscoveredPools(r.Context(), body.AccountID, resources)
	writeJSON(w, http.StatusOK, resp)
}

func (d *DiscoveryServer) importDiscoveredPools(ctx context.Context, accountID int64, resources []domain.DiscoveredResource) discoveryImportResponse {
	resp := discoveryImportResponse{Errors: []string{}}
	sort.SliceStable(resources, func(i, j int) bool {
		if resources[i].ResourceType == resources[j].ResourceType {
			return resources[i].ResourceID < resources[j].ResourceID
		}
		if resources[i].ResourceType == domain.ResourceTypeVPC {
			return true
		}
		if resources[j].ResourceType == domain.ResourceTypeVPC {
			return false
		}
		return resources[i].ResourceType < resources[j].ResourceType
	})

	existingPools, err := d.srv.store.ListPools(ctx)
	if err != nil {
		resp.Errors = append(resp.Errors, "list pools: "+err.Error())
		return resp
	}
	poolByDiscoveryID := make(map[string]int64)
	poolByShape := make(map[string]int64)
	for _, pool := range existingPools {
		if pool.Tags != nil {
			if id := pool.Tags["discovery_resource_id"]; id != "" {
				poolByDiscoveryID[id] = pool.ID
			}
		}
		poolByShape[poolShapeKey(pool.AccountID, pool.ParentID, string(pool.Type), pool.CIDR)] = pool.ID
	}

	resourceToPool := make(map[string]int64)
	for _, res := range resources {
		if res.PoolID != nil {
			resourceToPool[res.ResourceID] = *res.PoolID
			resp.Skipped++
			continue
		}
		if res.CIDR == "" || (res.ResourceType != domain.ResourceTypeVPC && res.ResourceType != domain.ResourceTypeSubnet) {
			resp.Skipped++
			continue
		}
		if _, err := netip.ParsePrefix(res.CIDR); err != nil {
			resp.Errors = append(resp.Errors, fmt.Sprintf("%s: invalid CIDR %q", res.ResourceID, res.CIDR))
			resp.Skipped++
			continue
		}

		poolType := domain.PoolTypeSubnet
		if res.ResourceType == domain.ResourceTypeVPC {
			poolType = domain.PoolTypeVPC
		}

		var parentID *int64
		if res.ParentResourceID != nil {
			if id, ok := resourceToPool[*res.ParentResourceID]; ok {
				parentID = &id
			}
		}

		if id, ok := poolByDiscoveryID[res.ResourceID]; ok {
			if err := d.store.LinkResourceToPool(ctx, res.ID, id); err != nil {
				resp.Errors = append(resp.Errors, fmt.Sprintf("%s: link existing pool: %v", res.ResourceID, err))
			} else {
				resourceToPool[res.ResourceID] = id
				resp.ResourcesLinked++
			}
			continue
		}
		if id, ok := poolByShape[poolShapeKey(&accountID, parentID, string(poolType), res.CIDR)]; ok {
			if err := d.store.LinkResourceToPool(ctx, res.ID, id); err != nil {
				resp.Errors = append(resp.Errors, fmt.Sprintf("%s: link matching pool: %v", res.ResourceID, err))
			} else {
				resourceToPool[res.ResourceID] = id
				resp.ResourcesLinked++
			}
			continue
		}

		name := strings.TrimSpace(res.Name)
		if name == "" {
			name = res.ResourceID
		}
		pool, err := d.srv.store.CreatePool(ctx, domain.CreatePool{
			Name:      name,
			CIDR:      res.CIDR,
			ParentID:  parentID,
			AccountID: &accountID,
			Type:      poolType,
			Status:    domain.PoolStatusActive,
			Source:    domain.PoolSourceDiscovered,
			Description: fmt.Sprintf("Imported from %s discovery resource %s in %s",
				res.Provider, res.ResourceID, res.Region),
			Tags: map[string]string{
				"discovery_resource_id": res.ResourceID,
				"discovery_provider":    res.Provider,
				"discovery_region":      res.Region,
				"discovery_type":        string(res.ResourceType),
			},
		})
		if err != nil {
			resp.Errors = append(resp.Errors, fmt.Sprintf("%s: create pool: %v", res.ResourceID, err))
			resp.Skipped++
			continue
		}
		if err := d.store.LinkResourceToPool(ctx, res.ID, pool.ID); err != nil {
			resp.Errors = append(resp.Errors, fmt.Sprintf("%s: link new pool: %v", res.ResourceID, err))
		} else {
			resp.ResourcesLinked++
		}
		resourceToPool[res.ResourceID] = pool.ID
		poolByDiscoveryID[res.ResourceID] = pool.ID
		poolByShape[poolShapeKey(pool.AccountID, pool.ParentID, string(pool.Type), pool.CIDR)] = pool.ID
		resp.PoolsCreated++
	}

	return resp
}

func poolShapeKey(accountID *int64, parentID *int64, poolType string, cidr string) string {
	account := int64(0)
	parent := int64(0)
	if accountID != nil {
		account = *accountID
	}
	if parentID != nil {
		parent = *parentID
	}
	return fmt.Sprintf("%d|%d|%s|%s", account, parent, poolType, cidr)
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
		AccountID int64  `json:"account_id"`
		AgentID   string `json:"agent_id"`
		AllAgents bool   `json:"all_agents"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		d.srv.writeErr(r.Context(), w, http.StatusBadRequest, "invalid request body", err.Error())
		return
	}

	if d.syncService == nil {
		d.srv.writeErr(r.Context(), w, http.StatusServiceUnavailable, "sync service not available", "")
		return
	}

	if body.AllAgents {
		jobs, err := d.queueAllHealthyAgentSyncs(r.Context())
		if err != nil {
			d.srv.writeErr(r.Context(), w, http.StatusInternalServerError, "queue agent syncs failed", err.Error())
			return
		}
		if len(jobs) == 0 {
			d.srv.writeErr(r.Context(), w, http.StatusConflict, "no healthy discovery agents are connected", "")
			return
		}
		writeJSON(w, http.StatusOK, domain.SyncJobsResponse{Items: jobs})
		return
	}

	if body.AgentID != "" {
		agentID, err := uuid.Parse(body.AgentID)
		if err != nil {
			d.srv.writeErr(r.Context(), w, http.StatusBadRequest, "invalid agent_id", "")
			return
		}
		agent, err := d.store.GetAgent(r.Context(), agentID)
		if err != nil {
			if errors.Is(err, storage.ErrNotFound) {
				d.srv.writeErr(r.Context(), w, http.StatusNotFound, "agent not found", "")
				return
			}
			d.srv.writeErr(r.Context(), w, http.StatusInternalServerError, "get agent failed", err.Error())
			return
		}
		if computeAgentStatus(agent.LastSeenAt, time.Now().UTC()) != domain.AgentStatusHealthy {
			d.srv.writeErr(r.Context(), w, http.StatusConflict, "agent is not healthy", "")
			return
		}
		accountID, err := d.resolveAgentSyncAccountID(r.Context(), *agent, body.AccountID)
		if err != nil {
			if errors.Is(err, storage.ErrNotFound) {
				d.srv.writeErr(r.Context(), w, http.StatusNotFound, "agent account not found", "")
				return
			}
			d.srv.writeErr(r.Context(), w, http.StatusInternalServerError, "account lookup failed", err.Error())
			return
		}
		job, err := d.createAgentSyncJob(r.Context(), accountID, agent.ID)
		if err != nil {
			d.srv.writeErr(r.Context(), w, http.StatusInternalServerError, "create agent sync job failed", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, job)
		return
	}

	if body.AccountID < 1 {
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

	if agent := d.selectConnectedAgent(r.Context(), body.AccountID); agent != nil {
		job, err := d.createAgentSyncJob(r.Context(), account.ID, agent.ID)
		if err != nil {
			d.srv.writeErr(r.Context(), w, http.StatusInternalServerError, "create agent sync job failed", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, job)
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

func (d *DiscoveryServer) createAgentSyncJob(ctx context.Context, accountID int64, agentID uuid.UUID) (domain.SyncJob, error) {
	now := time.Now().UTC()
	return d.store.CreateSyncJob(ctx, domain.SyncJob{
		ID:        uuid.New(),
		AccountID: accountID,
		Status:    domain.SyncJobStatusPending,
		Source:    "agent",
		AgentID:   &agentID,
		CreatedAt: now,
	})
}

func (d *DiscoveryServer) queueAllHealthyAgentSyncs(ctx context.Context) ([]domain.SyncJob, error) {
	agents, err := d.store.ListAgents(ctx, 0)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	jobs := make([]domain.SyncJob, 0, len(agents))
	seen := make(map[uuid.UUID]bool)
	for i := range agents {
		agent := agents[i]
		if seen[agent.ID] || computeAgentStatus(agent.LastSeenAt, now) != domain.AgentStatusHealthy {
			continue
		}
		accountID, err := d.resolveAgentSyncAccountID(ctx, agent, 0)
		if errors.Is(err, storage.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		job, err := d.createAgentSyncJob(ctx, accountID, agent.ID)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
		seen[agent.ID] = true
	}
	return jobs, nil
}

func (d *DiscoveryServer) resolveAgentSyncAccountID(ctx context.Context, agent domain.DiscoveryAgent, requestedAccountID int64) (int64, error) {
	if requestedAccountID > 0 {
		if _, found, err := d.srv.store.GetAccount(ctx, requestedAccountID); err != nil {
			return 0, err
		} else if !found {
			return 0, storage.ErrNotFound
		}
		return requestedAccountID, nil
	}

	if agent.AccountID > 0 {
		if _, found, err := d.srv.store.GetAccount(ctx, agent.AccountID); err != nil {
			return 0, err
		} else if found {
			return agent.AccountID, nil
		}

		account, err := d.srv.store.GetAccountByKey(ctx, "aws:"+strconv.FormatInt(agent.AccountID, 10))
		if err == nil {
			return account.ID, nil
		}
		if !errors.Is(err, storage.ErrNotFound) {
			return 0, err
		}
	}

	return 0, storage.ErrNotFound
}

func (d *DiscoveryServer) selectConnectedAgent(ctx context.Context, accountID int64) *domain.DiscoveryAgent {
	agents, err := d.store.ListAgents(ctx, 0)
	if err != nil {
		return nil
	}
	now := time.Now().UTC()
	var fallback *domain.DiscoveryAgent
	for i := range agents {
		agent := agents[i]
		if computeAgentStatus(agent.LastSeenAt, now) != domain.AgentStatusHealthy {
			continue
		}
		if agent.AccountID == accountID {
			return &agent
		}
		if fallback == nil {
			fallback = &agent
		}
	}
	return fallback
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

// handleIngest handles POST /api/v1/discovery/ingest
// Accepts discovered resources from remote agents and creates a sync job.
func (d *DiscoveryServer) handleIngest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		d.srv.writeErr(r.Context(), w, http.StatusMethodNotAllowed, "method not allowed", "")
		return
	}

	var req domain.IngestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		d.srv.writeErr(r.Context(), w, http.StatusBadRequest, "invalid request body", err.Error())
		return
	}

	if req.AccountID < 1 {
		d.srv.writeErr(r.Context(), w, http.StatusBadRequest, "account_id is required and must be a positive integer", "")
		return
	}

	// Verify account exists
	account, found, err := d.srv.store.GetAccount(r.Context(), req.AccountID)
	if err != nil {
		d.srv.writeErr(r.Context(), w, http.StatusInternalServerError, "account lookup failed", err.Error())
		return
	}
	if !found {
		d.srv.writeErr(r.Context(), w, http.StatusNotFound, "account not found", "")
		return
	}

	// Create or continue sync job
	nowTime := time.Now().UTC()
	var job domain.SyncJob
	if req.SyncJobID != nil {
		existing, getErr := d.store.GetSyncJob(r.Context(), *req.SyncJobID)
		if getErr != nil {
			d.srv.writeErr(r.Context(), w, http.StatusBadRequest, "sync job not found", getErr.Error())
			return
		}
		job = *existing
		job.Status = domain.SyncJobStatusRunning
		job.Source = "agent"
		job.AgentID = req.AgentID
		job.StartedAt = &nowTime
	} else {
		job = domain.SyncJob{
			ID:        uuid.New(),
			AccountID: account.ID,
			Status:    domain.SyncJobStatusRunning,
			Source:    "agent",
			AgentID:   req.AgentID,
			StartedAt: &nowTime,
			CreatedAt: nowTime,
		}
		job, err = d.store.CreateSyncJob(r.Context(), job)
		if err != nil {
			d.srv.writeErr(r.Context(), w, http.StatusInternalServerError, "create sync job failed", err.Error())
			return
		}
	}

	// Process resources using shared logic
	created, updated, staleCount, processErr := d.syncService.ProcessResources(r.Context(), account.ID, req.Resources, nowTime)

	// Update job with results
	completedAtTime := time.Now().UTC()
	completedAt := &completedAtTime
	job.CompletedAt = completedAt
	job.ResourcesFound = len(req.Resources)
	job.ResourcesCreated = created
	job.ResourcesUpdated = updated
	job.ResourcesDeleted = staleCount
	if processErr != nil {
		job.Status = domain.SyncJobStatusFailed
		job.ErrorMessage = processErr.Error()
		_ = d.store.UpdateSyncJob(r.Context(), job)
		d.srv.writeErr(r.Context(), w, http.StatusInternalServerError, "process resources failed", processErr.Error())
		return
	}
	job.Status = domain.SyncJobStatusCompleted
	_ = d.store.UpdateSyncJob(r.Context(), job)

	resp := domain.IngestResponse{
		JobID:            job.ID,
		ResourcesFound:   job.ResourcesFound,
		ResourcesCreated: job.ResourcesCreated,
		ResourcesUpdated: job.ResourcesUpdated,
		ResourcesDeleted: job.ResourcesDeleted,
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleAgentHeartbeat handles POST /api/v1/discovery/agents/heartbeat
func (d *DiscoveryServer) handleAgentHeartbeat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		d.srv.writeErr(r.Context(), w, http.StatusMethodNotAllowed, "method not allowed", "")
		return
	}

	var req domain.AgentHeartbeatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		d.srv.writeErr(r.Context(), w, http.StatusBadRequest, "invalid request body", err.Error())
		return
	}

	if req.AgentID == uuid.Nil || req.Name == "" || req.AccountID < 1 {
		d.srv.writeErr(r.Context(), w, http.StatusBadRequest, "agent_id, name, and account_id are required", "")
		return
	}

	// Get API key ID from context
	apiKeyID, ok := getAPIKeyIDFromContext(r.Context())
	if !ok {
		d.srv.writeErr(r.Context(), w, http.StatusUnauthorized, "api key required", "")
		return
	}

	// Upsert agent
	lastSeenTime := time.Now().UTC()
	agent := domain.DiscoveryAgent{
		ID:         req.AgentID,
		Name:       req.Name,
		AccountID:  req.AccountID,
		APIKeyID:   apiKeyID,
		Version:    req.Version,
		Hostname:   req.Hostname,
		LastSeenAt: lastSeenTime,
	}

	if err := d.store.UpsertAgent(r.Context(), agent); err != nil {
		d.srv.writeErr(r.Context(), w, http.StatusInternalServerError, "upsert agent failed", err.Error())
		return
	}

	resp := domain.AgentHeartbeatResponse{Status: "ok"}
	job, err := d.store.ClaimPendingAgentSync(r.Context(), req.AgentID)
	if err != nil && !errors.Is(err, storage.ErrNotFound) {
		d.srv.writeErr(r.Context(), w, http.StatusInternalServerError, "claim sync job failed", err.Error())
		return
	}
	if job != nil {
		resp.SyncJobID = &job.ID
		resp.AccountID = job.AccountID
	}

	writeJSON(w, http.StatusOK, resp)
}

// handleListAgents handles GET /api/v1/discovery/agents
func (d *DiscoveryServer) handleListAgents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		d.srv.writeErr(r.Context(), w, http.StatusMethodNotAllowed, "method not allowed", "")
		return
	}

	q := r.URL.Query()
	accountID, _ := strconv.ParseInt(q.Get("account_id"), 10, 64)

	agents, err := d.store.ListAgents(r.Context(), accountID)
	if err != nil {
		d.srv.writeErr(r.Context(), w, http.StatusInternalServerError, "list agents failed", err.Error())
		return
	}

	// Compute status for each agent
	now := time.Now().UTC()
	for i := range agents {
		agents[i].Status = computeAgentStatus(agents[i].LastSeenAt, now)
	}

	if agents == nil {
		agents = []domain.DiscoveryAgent{}
	}
	writeJSON(w, http.StatusOK, domain.DiscoveryAgentsResponse{Items: agents})
}

// handleGetAgent handles GET /api/v1/discovery/agents/{id}
func (d *DiscoveryServer) handleGetAgent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		d.srv.writeErr(r.Context(), w, http.StatusMethodNotAllowed, "method not allowed", "")
		return
	}

	idStr := strings.TrimPrefix(r.URL.Path, "/api/v1/discovery/agents/")
	id, err := uuid.Parse(idStr)
	if err != nil {
		d.srv.writeErr(r.Context(), w, http.StatusBadRequest, "invalid agent id", "")
		return
	}

	agent, err := d.store.GetAgent(r.Context(), id)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			d.srv.writeErr(r.Context(), w, http.StatusNotFound, "agent not found", "")
			return
		}
		d.srv.writeErr(r.Context(), w, http.StatusInternalServerError, "get agent failed", err.Error())
		return
	}

	// Compute status
	now := time.Now().UTC()
	agent.Status = computeAgentStatus(agent.LastSeenAt, now)

	writeJSON(w, http.StatusOK, agent)
}

// computeAgentStatus determines agent health based on last_seen_at.
func computeAgentStatus(lastSeen time.Time, now time.Time) domain.AgentStatus {
	elapsed := now.Sub(lastSeen)
	if elapsed < 5*time.Minute {
		return domain.AgentStatusHealthy
	} else if elapsed < 15*time.Minute {
		return domain.AgentStatusStale
	}
	return domain.AgentStatusOffline
}

// getAPIKeyIDFromContext extracts the API key ID from the context.
func getAPIKeyIDFromContext(ctx context.Context) (string, bool) {
	key := auth.APIKeyFromContext(ctx)
	if key == nil {
		return "", false
	}
	return key.ID, true
}

// handleOrgIngest handles POST /api/v1/discovery/ingest/org
// Accepts discovered resources from multiple AWS accounts (via Organizations) and
// auto-creates CloudPAM Account records for new AWS accounts.
func (d *DiscoveryServer) handleOrgIngest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		d.srv.writeErr(r.Context(), w, http.StatusMethodNotAllowed, "method not allowed", "")
		return
	}

	var req domain.BulkIngestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		d.srv.writeErr(r.Context(), w, http.StatusBadRequest, "invalid request body", err.Error())
		return
	}

	if len(req.Accounts) == 0 {
		d.srv.writeErr(r.Context(), w, http.StatusBadRequest, "accounts list is required", "")
		return
	}

	resp := domain.BulkIngestResponse{}
	syncTime := time.Now().UTC()
	var syncJob *domain.SyncJob

	if req.SyncJobID != nil {
		job, err := d.store.GetSyncJob(r.Context(), *req.SyncJobID)
		if err != nil {
			d.srv.writeErr(r.Context(), w, http.StatusBadRequest, "sync job not found", err.Error())
			return
		}
		job.Status = domain.SyncJobStatusRunning
		job.Source = "agent"
		if job.StartedAt == nil {
			job.StartedAt = &syncTime
		}
		syncJob = job
	}

	if req.AgentID != "" {
		agentID, err := uuid.Parse(req.AgentID)
		if err != nil {
			d.srv.writeErr(r.Context(), w, http.StatusBadRequest, "invalid agent_id", "")
			return
		}
		if err := d.refreshAgentLastSeen(r.Context(), agentID, syncTime); err != nil {
			d.srv.writeErr(r.Context(), w, http.StatusInternalServerError, "update agent heartbeat failed", err.Error())
			return
		}
	}

	for _, orgAcct := range req.Accounts {
		if orgAcct.AWSAccountID == "" {
			resp.Errors = append(resp.Errors, "skipped account with empty aws_account_id")
			continue
		}

		// Build key: "aws:<account_id>"
		key := "aws:" + orgAcct.AWSAccountID
		provider := orgAcct.Provider
		if provider == "" {
			provider = "aws"
		}

		// Look up or auto-create the CloudPAM account
		account, err := d.srv.store.GetAccountByKey(r.Context(), key)
		if err != nil {
			if !errors.Is(err, storage.ErrNotFound) {
				resp.Errors = append(resp.Errors, "lookup "+key+": "+err.Error())
				continue
			}
			// Auto-create
			name := orgAcct.AccountName
			if name == "" {
				name = orgAcct.AWSAccountID
			}
			created, createErr := d.srv.store.CreateAccount(r.Context(), domain.CreateAccount{
				Key:        key,
				Name:       name,
				Provider:   provider,
				ExternalID: orgAcct.AWSAccountID,
				Regions:    orgAcct.Regions,
			})
			if createErr != nil {
				resp.Errors = append(resp.Errors, "create account "+key+": "+createErr.Error())
				continue
			}
			account = &created
			resp.AccountsCreated++
		}

		// Set account_id on all resources
		for i := range orgAcct.Resources {
			orgAcct.Resources[i].AccountID = account.ID
		}

		// Process resources
		_, _, _, processErr := d.syncService.ProcessResources(r.Context(), account.ID, orgAcct.Resources, syncTime)
		if processErr != nil {
			resp.Errors = append(resp.Errors, "process "+key+": "+processErr.Error())
		}

		resp.AccountsProcessed++
		resp.TotalResources += len(orgAcct.Resources)
	}

	if syncJob != nil {
		completedAt := time.Now().UTC()
		syncJob.CompletedAt = &completedAt
		syncJob.ResourcesFound = resp.TotalResources
		syncJob.Status = domain.SyncJobStatusCompleted
		if len(resp.Errors) > 0 {
			syncJob.Status = domain.SyncJobStatusFailed
			syncJob.ErrorMessage = strings.Join(resp.Errors, "; ")
		}
		_ = d.store.UpdateSyncJob(r.Context(), *syncJob)
	}

	writeJSON(w, http.StatusOK, resp)
}

func (d *DiscoveryServer) refreshAgentLastSeen(ctx context.Context, agentID uuid.UUID, seenAt time.Time) error {
	agent, err := d.store.GetAgent(ctx, agentID)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return nil
		}
		return err
	}

	agent.LastSeenAt = seenAt
	return d.store.UpsertAgent(ctx, *agent)
}
