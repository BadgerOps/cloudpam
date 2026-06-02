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
	"cloudpam/internal/domain"
	"cloudpam/internal/storage"
)

// NetworkServer handles merged network view endpoints.
type NetworkServer struct {
	srv        *Server
	store      storage.Store
	discStore  storage.DiscoveryStore
	driftStore storage.DriftStore
}

// NewNetworkServer creates a merged network view server.
func NewNetworkServer(srv *Server, store storage.Store, discStore storage.DiscoveryStore, driftStore storage.DriftStore) *NetworkServer {
	return &NetworkServer{srv: srv, store: store, discStore: discStore, driftStore: driftStore}
}

// RegisterNetworkRoutes registers network routes without RBAC.
func (ns *NetworkServer) RegisterNetworkRoutes() {
	ns.srv.handleOpenAPIRouteFunc("/api/v1/network/flat", ns.handleFlat)
	ns.srv.handleOpenAPIRouteFunc("/api/v1/network/hierarchy", ns.handleHierarchy)
	ns.srv.handleOpenAPIRouteFunc("/api/v1/network/merged", ns.handleMerged)
	ns.srv.handleOpenAPIRouteFunc("/api/v1/network/conflicts", ns.handleConflicts)
	ns.srv.handleOpenAPIRouteFunc("/api/v1/network/conflicts/", ns.handleConflictSubroutes)
}

// RegisterProtectedNetworkRoutes registers network routes with RBAC.
func (ns *NetworkServer) RegisterProtectedNetworkRoutes(dualMW Middleware, logger *slog.Logger) {
	readMW := RequirePermissionMiddleware(auth.ResourceDiscovery, auth.ActionRead, logger)
	updateMW := RequirePermissionMiddleware(auth.ResourceDiscovery, auth.ActionUpdate, logger)
	ns.srv.handleOpenAPIRoute("/api/v1/network/flat", dualMW(readMW(http.HandlerFunc(ns.handleFlat))))
	ns.srv.handleOpenAPIRoute("/api/v1/network/hierarchy", dualMW(readMW(http.HandlerFunc(ns.handleHierarchy))))
	ns.srv.handleOpenAPIRoute("/api/v1/network/merged", dualMW(readMW(http.HandlerFunc(ns.handleMerged))))
	ns.srv.handleOpenAPIRoute("/api/v1/network/conflicts", dualMW(readMW(http.HandlerFunc(ns.handleConflicts))))
	ns.srv.handleOpenAPIRoute("/api/v1/network/conflicts/", dualMW(updateMW(http.HandlerFunc(ns.handleConflictSubroutes))))
}

func (ns *NetworkServer) handleFlat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		ns.srv.writeErr(r.Context(), w, http.StatusMethodNotAllowed, "method not allowed", "")
		return
	}
	view, err := ns.buildNetworkView(r.Context(), networkFiltersFromRequest(r))
	if err != nil {
		ns.srv.writeErr(r.Context(), w, http.StatusInternalServerError, "network view failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, domain.NetworkViewResponse{
		Items:         view.flat,
		Total:         len(view.flat),
		ConflictCount: len(view.conflicts),
		Conflicts:     view.conflicts,
	})
}

func (ns *NetworkServer) handleHierarchy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		ns.srv.writeErr(r.Context(), w, http.StatusMethodNotAllowed, "method not allowed", "")
		return
	}
	view, err := ns.buildNetworkView(r.Context(), networkFiltersFromRequest(r))
	if err != nil {
		ns.srv.writeErr(r.Context(), w, http.StatusInternalServerError, "network hierarchy failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, domain.NetworkViewResponse{
		Items:         view.hierarchy,
		Total:         len(view.flat),
		ConflictCount: len(view.conflicts),
		Conflicts:     view.conflicts,
	})
}

func (ns *NetworkServer) handleMerged(w http.ResponseWriter, r *http.Request) {
	ns.handleHierarchy(w, r)
}

func (ns *NetworkServer) handleConflicts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		ns.srv.writeErr(r.Context(), w, http.StatusMethodNotAllowed, "method not allowed", "")
		return
	}
	view, err := ns.buildNetworkView(r.Context(), networkFiltersFromRequest(r))
	if err != nil {
		ns.srv.writeErr(r.Context(), w, http.StatusInternalServerError, "network conflicts failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, domain.NetworkConflictListResponse{Items: view.conflicts, Total: len(view.conflicts)})
}

func (ns *NetworkServer) handleConflictSubroutes(w http.ResponseWriter, r *http.Request) {
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/network/conflicts/"), "/")
	parts := strings.Split(path, "/")
	if len(parts) < 2 || parts[0] == "" {
		ns.srv.writeErr(r.Context(), w, http.StatusNotFound, "not found", "")
		return
	}
	if parts[1] == "resolve" && len(parts) == 2 {
		ns.handleResolveNetworkConflict(w, r, parts[0])
		return
	}
	if parts[1] == "actions" && len(parts) == 3 {
		switch parts[2] {
		case "link":
			ns.handleNetworkConflictLinkAction(w, r, parts[0])
		case "import":
			ns.handleNetworkConflictImportAction(w, r, parts[0])
		default:
			ns.srv.writeErr(r.Context(), w, http.StatusNotFound, "not found", "")
		}
		return
	}
	ns.srv.writeErr(r.Context(), w, http.StatusNotFound, "not found", "")
}

func (ns *NetworkServer) handleResolveNetworkConflict(w http.ResponseWriter, r *http.Request, conflictID string) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		ns.srv.writeErr(r.Context(), w, http.StatusMethodNotAllowed, "method not allowed", "")
		return
	}
	var req domain.ResolveNetworkConflictRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		ns.srv.writeErr(r.Context(), w, http.StatusBadRequest, "invalid request body", err.Error())
		return
	}
	if strings.TrimSpace(req.Decision) == "" {
		ns.srv.writeErr(r.Context(), w, http.StatusBadRequest, "decision is required", "")
		return
	}
	if !isValidNetworkDecision(req.Decision) {
		ns.srv.writeErr(r.Context(), w, http.StatusBadRequest, "decision must be one of skip, ignore, or defer", "")
		return
	}
	view, err := ns.buildNetworkView(r.Context(), networkViewFilters{})
	if err != nil {
		ns.srv.writeErr(r.Context(), w, http.StatusInternalServerError, "network conflicts failed", err.Error())
		return
	}
	for _, conflict := range view.conflicts {
		if conflict.ID == conflictID {
			if err := ns.persistNetworkConflictResolution(r.Context(), conflict, req); err != nil {
				ns.srv.writeErr(r.Context(), w, http.StatusInternalServerError, "resolve conflict failed", err.Error())
				return
			}
			conflict.Status = string(networkDecisionStatus(req.Decision))
			conflict.ResolutionState = conflict.Status
			conflict.ResolutionRequested = req.Decision
			writeJSON(w, http.StatusOK, conflict)
			return
		}
	}
	ns.srv.writeErr(r.Context(), w, http.StatusNotFound, "conflict not found", "")
}

func (ns *NetworkServer) handleNetworkConflictLinkAction(w http.ResponseWriter, r *http.Request, conflictID string) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		ns.srv.writeErr(r.Context(), w, http.StatusMethodNotAllowed, "method not allowed", "")
		return
	}
	var req domain.NetworkConflictLinkActionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		ns.srv.writeErr(r.Context(), w, http.StatusBadRequest, "invalid request body", err.Error())
		return
	}
	if req.DiscoveredID == uuid.Nil {
		ns.srv.writeErr(r.Context(), w, http.StatusBadRequest, "discovered_id is required", "")
		return
	}
	if req.PoolID < 1 {
		ns.srv.writeErr(r.Context(), w, http.StatusBadRequest, "pool_id is required", "")
		return
	}

	conflict, err := ns.findNetworkConflict(r.Context(), conflictID)
	if err != nil {
		ns.srv.writeErr(r.Context(), w, http.StatusInternalServerError, "network conflicts failed", err.Error())
		return
	}
	if conflict == nil {
		ns.srv.writeErr(r.Context(), w, http.StatusNotFound, "conflict not found", "")
		return
	}
	res, err := ns.discStore.GetDiscoveredResource(r.Context(), req.DiscoveredID)
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, storage.ErrNotFound) {
			status = http.StatusBadRequest
		}
		ns.srv.writeErr(r.Context(), w, status, "discovered resource not found", err.Error())
		return
	}
	pool, found, err := ns.store.GetPool(r.Context(), req.PoolID)
	if err != nil {
		ns.srv.writeErr(r.Context(), w, http.StatusInternalServerError, "pool lookup failed", err.Error())
		return
	}
	if !found {
		ns.srv.writeErr(r.Context(), w, http.StatusBadRequest, "pool not found", "")
		return
	}
	if err := validateNetworkConflictLinkAction(*conflict, *res, pool, req); err != nil {
		ns.srv.writeErr(r.Context(), w, http.StatusBadRequest, err.Error(), "")
		return
	}

	actionDetails := map[string]string{
		"network_conflict_action": "link",
		"discovered_id":           req.DiscoveredID.String(),
		"pool_id":                 fmt.Sprintf("%d", req.PoolID),
	}
	resolveReq := domain.ResolveNetworkConflictRequest{
		Decision: "link",
		Reason:   networkActionReason("link", req.Reason, actionDetails),
	}
	if err := ns.ensureNetworkConflictResolutionRecord(r.Context(), *conflict, networkActionReason("link_pending", req.Reason, actionDetails), actionDetails); err != nil {
		ns.srv.writeErr(r.Context(), w, http.StatusInternalServerError, "prepare conflict action failed", err.Error())
		return
	}
	previousPoolID := res.PoolID
	if err := ns.discStore.LinkResourceToPool(r.Context(), req.DiscoveredID, req.PoolID); err != nil {
		ns.srv.writeErr(r.Context(), w, http.StatusInternalServerError, "link resource to pool failed", err.Error())
		return
	}
	if err := ns.persistNetworkConflictActionResolution(r.Context(), *conflict, resolveReq, actionDetails); err != nil {
		rollbackErr := ns.rollbackNetworkConflictLink(r.Context(), req.DiscoveredID, previousPoolID)
		if rollbackErr != nil {
			ns.srv.writeErr(r.Context(), w, http.StatusInternalServerError, "record conflict action failed and rollback failed", err.Error()+"; rollback: "+rollbackErr.Error())
			return
		}
		ns.srv.writeErr(r.Context(), w, http.StatusInternalServerError, "record conflict action failed", err.Error())
		return
	}

	updated := ns.conflictAfterAction(r.Context(), *conflict, "link")
	writeJSON(w, http.StatusOK, domain.NetworkConflictActionResponse{
		Conflict:       updated,
		Action:         "link",
		ResourceLinked: true,
		DiscoveredID:   &req.DiscoveredID,
		PoolID:         &req.PoolID,
	})
}

func (ns *NetworkServer) handleNetworkConflictImportAction(w http.ResponseWriter, r *http.Request, conflictID string) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		ns.srv.writeErr(r.Context(), w, http.StatusMethodNotAllowed, "method not allowed", "")
		return
	}
	var req domain.NetworkConflictImportActionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		ns.srv.writeErr(r.Context(), w, http.StatusBadRequest, "invalid request body", err.Error())
		return
	}
	if len(req.ResourceIDs) == 0 {
		ns.srv.writeErr(r.Context(), w, http.StatusBadRequest, "resource_ids must include at least one discovered resource", "")
		return
	}

	conflict, err := ns.findNetworkConflict(r.Context(), conflictID)
	if err != nil {
		ns.srv.writeErr(r.Context(), w, http.StatusInternalServerError, "network conflicts failed", err.Error())
		return
	}
	if conflict == nil {
		ns.srv.writeErr(r.Context(), w, http.StatusNotFound, "conflict not found", "")
		return
	}
	if err := validateNetworkConflictImportSelection(*conflict, req); err != nil {
		ns.srv.writeErr(r.Context(), w, http.StatusBadRequest, err.Error(), "")
		return
	}
	accountID, err := ns.importActionAccountID(r.Context(), *conflict, req.ResourceIDs)
	if err != nil {
		ns.srv.writeErr(r.Context(), w, http.StatusBadRequest, err.Error(), "")
		return
	}
	if req.PoolID != nil {
		pool, found, err := ns.store.GetPool(r.Context(), *req.PoolID)
		if err != nil {
			ns.srv.writeErr(r.Context(), w, http.StatusInternalServerError, "pool lookup failed", err.Error())
			return
		}
		if !found {
			ns.srv.writeErr(r.Context(), w, http.StatusBadRequest, "selected pool not found", "")
			return
		}
		if pool.AccountID != nil && *pool.AccountID != accountID {
			ns.srv.writeErr(r.Context(), w, http.StatusBadRequest, "selected pool account does not match import account", "")
			return
		}
	}
	importReq := domain.DiscoveryImportApplyRequest{
		AccountID:   accountID,
		ResourceIDs: req.ResourceIDs,
		PoolID:      req.PoolID,
	}
	discoveryServer := &DiscoveryServer{srv: ns.srv, store: ns.discStore}
	if !req.Override {
		preview, err := discoveryServer.previewDiscoveryImport(r.Context(), domain.DiscoveryImportPreviewRequest(importReq))
		if err != nil {
			ns.srv.writeErr(r.Context(), w, http.StatusInternalServerError, "preview import failed", err.Error())
			return
		}
		if preview.Importable != len(req.ResourceIDs) {
			ns.srv.writeErr(r.Context(), w, http.StatusBadRequest, "selected resources are not all importable; set override to import conflict rows", "")
			return
		}
	}
	actionDetails := map[string]string{
		"network_conflict_action": "import",
		"resource_ids":            joinUUIDs(req.ResourceIDs),
	}
	if req.PoolID != nil {
		actionDetails["pool_id"] = fmt.Sprintf("%d", *req.PoolID)
	}
	if err := ns.ensureNetworkConflictResolutionRecord(r.Context(), *conflict, networkActionReason("import_pending", req.Reason, actionDetails), actionDetails); err != nil {
		ns.srv.writeErr(r.Context(), w, http.StatusInternalServerError, "prepare conflict action failed", err.Error())
		return
	}
	importResp, err := discoveryServer.applyDiscoveryImport(r.Context(), importReq, discoveryImportApplyOptions{AllowBlocked: req.Override})
	if err != nil {
		ns.srv.writeErr(r.Context(), w, http.StatusInternalServerError, "apply import failed", err.Error())
		return
	}
	if importResp.PoolsCreated+importResp.ResourcesLinked == 0 || importResp.Skipped > 0 || len(importResp.Errors) > 0 {
		rollbackErr := ns.rollbackNetworkConflictImport(r.Context(), importResp)
		detail := strings.Join(importResp.Errors, "; ")
		if detail == "" && importResp.Skipped > 0 {
			detail = fmt.Sprintf("%d selected resources were skipped", importResp.Skipped)
		}
		if rollbackErr != nil {
			ns.srv.writeErr(r.Context(), w, http.StatusInternalServerError, "import was incomplete and rollback failed", detail+"; rollback: "+rollbackErr.Error())
			return
		}
		ns.srv.writeErr(r.Context(), w, http.StatusBadRequest, "import did not complete for all selected resources", detail)
		return
	}
	actionDetails["pools_created"] = fmt.Sprintf("%d", importResp.PoolsCreated)
	actionDetails["resources_linked"] = fmt.Sprintf("%d", importResp.ResourcesLinked)
	actionDetails["skipped"] = fmt.Sprintf("%d", importResp.Skipped)
	actionDetails["created_pool_ids"] = joinInt64s(importResp.CreatedPoolIDs)
	actionDetails["linked_resource_ids"] = joinUUIDs(importResp.LinkedResourceIDs)
	resolveReq := domain.ResolveNetworkConflictRequest{
		Decision: "import",
		Reason:   networkActionReason("import", req.Reason, actionDetails),
	}
	if err := ns.persistNetworkConflictActionResolution(r.Context(), *conflict, resolveReq, actionDetails); err != nil {
		rollbackErr := ns.rollbackNetworkConflictImport(r.Context(), importResp)
		if rollbackErr != nil {
			ns.srv.writeErr(r.Context(), w, http.StatusInternalServerError, "record conflict action failed and rollback failed", err.Error()+"; rollback: "+rollbackErr.Error())
			return
		}
		ns.srv.writeErr(r.Context(), w, http.StatusInternalServerError, "record conflict action failed", err.Error())
		return
	}

	updated := ns.conflictAfterAction(r.Context(), *conflict, "import")
	writeJSON(w, http.StatusOK, domain.NetworkConflictActionResponse{
		Conflict: updated,
		Action:   "import",
		PoolID:   req.PoolID,
		Import:   &importResp,
	})
}

type networkViewFilters struct {
	accountID    int64
	provider     string
	region       string
	objectType   string
	status       string
	conflictType string
	query        string
}

type builtNetworkView struct {
	flat      []domain.NetworkNode
	hierarchy []domain.NetworkNode
	conflicts []domain.NetworkConflict
}

func networkFiltersFromRequest(r *http.Request) networkViewFilters {
	q := r.URL.Query()
	filters := networkViewFilters{
		provider:     q.Get("provider"),
		region:       q.Get("region"),
		objectType:   q.Get("object_type"),
		status:       q.Get("status"),
		conflictType: q.Get("conflict_type"),
		query:        strings.ToLower(strings.TrimSpace(q.Get("q"))),
	}
	if idText := q.Get("account_id"); idText != "" {
		if id, err := strconv.ParseInt(idText, 10, 64); err == nil {
			filters.accountID = id
		}
	}
	return filters
}

func (ns *NetworkServer) buildNetworkView(ctx context.Context, filters networkViewFilters) (builtNetworkView, error) {
	accounts, err := ns.store.ListAccounts(ctx)
	if err != nil {
		return builtNetworkView{}, err
	}
	pools, err := ns.store.ListPools(ctx)
	if err != nil {
		return builtNetworkView{}, err
	}

	accountByID := make(map[int64]domain.Account, len(accounts))
	for _, account := range accounts {
		accountByID[account.ID] = account
	}
	poolByID := make(map[int64]domain.Pool, len(pools))
	for _, pool := range pools {
		poolByID[pool.ID] = pool
	}

	resources, err := ns.listAllDiscoveredResources(ctx, accounts)
	if err != nil {
		return builtNetworkView{}, err
	}
	resourceByProvider := make(map[string]domain.DiscoveredResource, len(resources))
	for _, res := range resources {
		resourceByProvider[resourceKey(res.AccountID, res.ResourceID)] = res
	}

	issuesByNode, conflicts := ns.computeNetworkConflicts(resources, pools, accountByID, poolByID, resourceByProvider)
	conflicts = ns.applyStoredConflictResolutions(ctx, conflicts)
	nodes := make([]domain.NetworkNode, 0, len(pools)+len(resources)+len(accounts))
	for _, pool := range pools {
		node := networkNodeFromPool(pool, accountByID)
		if nodeIssues := issuesByNode[node.ID]; len(nodeIssues) > 0 {
			node.Issues = append(node.Issues, nodeIssues...)
			node.State = "conflict"
		}
		nodes = append(nodes, node)
	}
	for _, res := range resources {
		node := ns.networkNodeFromResource(res, accountByID, poolByID, resourceByProvider)
		if nodeIssues := issuesByNode[node.ID]; len(nodeIssues) > 0 {
			node.Issues = append(node.Issues, nodeIssues...)
			node.State = worstNodeState(node.State, nodeIssues)
		}
		nodes = append(nodes, node)
	}

	filtered := make([]domain.NetworkNode, 0, len(nodes))
	for _, node := range nodes {
		if matchesNetworkFilters(node, filters) {
			filtered = append(filtered, node)
		}
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		if filtered[i].AccountName == filtered[j].AccountName {
			if filtered[i].Region == filtered[j].Region {
				if filtered[i].ObjectType == filtered[j].ObjectType {
					return filtered[i].Name < filtered[j].Name
				}
				return filtered[i].ObjectType < filtered[j].ObjectType
			}
			return filtered[i].Region < filtered[j].Region
		}
		return filtered[i].AccountName < filtered[j].AccountName
	})

	filteredConflicts := filterNetworkConflicts(conflicts, filters)
	return builtNetworkView{
		flat:      filtered,
		hierarchy: buildNetworkHierarchy(filtered),
		conflicts: filteredConflicts,
	}, nil
}

func (ns *NetworkServer) listAllDiscoveredResources(ctx context.Context, accounts []domain.Account) ([]domain.DiscoveredResource, error) {
	const pageSize = 1000
	var out []domain.DiscoveredResource
	for _, account := range accounts {
		accountCount := 0
		for page := 1; ; page++ {
			resources, total, err := ns.discStore.ListDiscoveredResources(ctx, account.ID, domain.DiscoveryFilters{Page: page, PageSize: pageSize})
			if err != nil {
				return nil, err
			}
			out = append(out, resources...)
			accountCount += len(resources)
			if len(resources) == 0 || accountCount >= total {
				break
			}
		}
	}
	return out, nil
}

func networkNodeFromPool(pool domain.Pool, accounts map[int64]domain.Account) domain.NetworkNode {
	nodeID := poolNodeID(pool.ID)
	node := domain.NetworkNode{
		ID:         nodeID,
		Kind:       "pool",
		ObjectType: string(pool.Type),
		Name:       pool.Name,
		CIDR:       pool.CIDR,
		Source:     string(pool.Source),
		State:      "managed",
	}
	if pool.ParentID != nil {
		parentID := poolNodeID(*pool.ParentID)
		node.ParentID = &parentID
	}
	if pool.AccountID != nil {
		node.AccountID = pool.AccountID
		if account, ok := accounts[*pool.AccountID]; ok {
			node.AccountName = account.Name
			node.Provider = account.Provider
		}
	}
	return node
}

func (ns *NetworkServer) networkNodeFromResource(res domain.DiscoveredResource, accounts map[int64]domain.Account, pools map[int64]domain.Pool, resources map[string]domain.DiscoveredResource) domain.NetworkNode {
	nodeID := resourceNodeID(res.ID)
	state := "discovered"
	source := "discovered"
	var parentID *string
	if res.PoolID != nil {
		state = "linked"
		source = "linked"
		id := poolNodeID(*res.PoolID)
		parentID = &id
	} else if res.ParentResourceID != nil {
		if parent, ok := resources[resourceKey(res.AccountID, *res.ParentResourceID)]; ok {
			id := resourceNodeID(parent.ID)
			parentID = &id
		}
	} else if pool := bestContainingPool(res.CIDR, pools); pool != nil {
		id := poolNodeID(pool.ID)
		parentID = &id
	}

	account := accounts[res.AccountID]
	node := domain.NetworkNode{
		ID:                 nodeID,
		ParentID:           parentID,
		Kind:               "discovered",
		ObjectType:         string(res.ResourceType),
		Name:               firstNonEmpty(res.Name, res.ResourceID),
		CIDR:               res.CIDR,
		Provider:           res.Provider,
		AccountID:          &res.AccountID,
		AccountName:        account.Name,
		Region:             res.Region,
		ProviderResourceID: res.ResourceID,
		DiscoveredID:       &res.ID,
		LinkedPoolID:       res.PoolID,
		Source:             source,
		State:              state,
		Evidence:           []string{fmt.Sprintf("%s %s in %s", res.Provider, res.ResourceID, firstNonEmpty(res.Region, "global"))},
	}
	if res.ResourceType == domain.ResourceTypeElasticIP {
		node.IPAddress = res.Metadata["public_ip"]
		if node.IPAddress == "" {
			node.IPAddress = strings.TrimSuffix(res.CIDR, "/32")
		}
	}
	return node
}

func (ns *NetworkServer) computeNetworkConflicts(resources []domain.DiscoveredResource, pools []domain.Pool, accounts map[int64]domain.Account, poolByID map[int64]domain.Pool, resourceByProvider map[string]domain.DiscoveredResource) (map[string][]domain.NetworkIssue, []domain.NetworkConflict) {
	issuesByNode := map[string][]domain.NetworkIssue{}
	var conflicts []domain.NetworkConflict
	add := func(conflict domain.NetworkConflict) {
		conflict.AvailableDecisions = networkConflictDecisions()
		conflicts = append(conflicts, conflict)
		issue := domain.NetworkIssue{ID: conflict.ID, Type: conflict.Type, Severity: conflict.Severity, Message: conflict.Title}
		for _, nodeID := range conflict.NodeIDs {
			issuesByNode[nodeID] = append(issuesByNode[nodeID], issue)
		}
	}

	resourcesByCIDR := map[string][]domain.DiscoveredResource{}
	for _, res := range resources {
		if res.CIDR != "" {
			resourcesByCIDR[res.CIDR] = append(resourcesByCIDR[res.CIDR], res)
		}
	}

	for _, res := range resources {
		nodeID := resourceNodeID(res.ID)
		if res.CIDR != "" {
			if _, err := netip.ParsePrefix(res.CIDR); err != nil {
				add(domain.NetworkConflict{
					ID:                "invalid-cidr:" + res.ID.String(),
					Type:              "invalid_cidr",
					Severity:          "critical",
					Status:            "open",
					Title:             "Invalid discovered CIDR",
					Description:       fmt.Sprintf("%s has invalid CIDR %q", res.ResourceID, res.CIDR),
					RecommendedAction: "Skip this object until discovery sends a valid CIDR.",
					NodeIDs:           []string{nodeID},
					DiscoveredIDs:     []uuid.UUID{res.ID},
					AccountIDs:        []int64{res.AccountID},
					Regions:           []string{res.Region},
					ObjectTypes:       []string{string(res.ResourceType)},
					CIDR:              res.CIDR,
					Evidence:          []string{fmt.Sprintf("resource_id=%s", res.ResourceID)},
				})
			}
		}
		if res.ResourceType == domain.ResourceTypeSubnet && res.ParentResourceID != nil {
			parent, ok := resourceByProvider[resourceKey(res.AccountID, *res.ParentResourceID)]
			if !ok {
				add(domain.NetworkConflict{
					ID:                "missing-parent:" + res.ID.String(),
					Type:              "missing_parent",
					Severity:          "warning",
					Status:            "open",
					Title:             "Missing parent VPC",
					Description:       fmt.Sprintf("%s references parent %s, but that parent was not discovered", res.ResourceID, *res.ParentResourceID),
					RecommendedAction: "Rediscover the account, create a placeholder parent, or skip this object.",
					NodeIDs:           []string{nodeID},
					DiscoveredIDs:     []uuid.UUID{res.ID},
					AccountIDs:        []int64{res.AccountID},
					Regions:           []string{res.Region},
					ObjectTypes:       []string{string(res.ResourceType)},
					CIDR:              res.CIDR,
					Evidence:          []string{"parent_provider_resource_id=" + *res.ParentResourceID},
				})
			} else if res.CIDR != "" && parent.CIDR != "" && !networkCIDRContains(parent.CIDR, res.CIDR) {
				add(domain.NetworkConflict{
					ID:                "invalid-nesting:" + res.ID.String(),
					Type:              "invalid_nesting",
					Severity:          "critical",
					Status:            "open",
					Title:             "Subnet is outside parent VPC",
					Description:       fmt.Sprintf("%s (%s) is not contained by parent %s (%s)", res.ResourceID, res.CIDR, parent.ResourceID, parent.CIDR),
					RecommendedAction: "Check provider data and do not import until topology is corrected.",
					NodeIDs:           []string{nodeID, resourceNodeID(parent.ID)},
					DiscoveredIDs:     []uuid.UUID{res.ID, parent.ID},
					AccountIDs:        []int64{res.AccountID},
					Regions:           []string{res.Region},
					ObjectTypes:       []string{string(res.ResourceType), string(parent.ResourceType)},
					CIDR:              res.CIDR,
					Evidence:          []string{fmt.Sprintf("parent_cidr=%s", parent.CIDR)},
				})
			}
		}
		if res.PoolID != nil {
			if pool, ok := poolByID[*res.PoolID]; ok && res.CIDR != "" && !networkCIDREqual(pool.CIDR, res.CIDR) {
				severity := "warning"
				conflictType := "linked_pool_mismatch"
				if !networkCIDRContains(pool.CIDR, res.CIDR) && !networkCIDRContains(res.CIDR, pool.CIDR) {
					severity = "critical"
					conflictType = "outside_pool"
				}
				add(domain.NetworkConflict{
					ID:                conflictType + ":" + res.ID.String(),
					Type:              conflictType,
					Severity:          severity,
					Status:            "open",
					Title:             "Linked pool CIDR differs from discovered resource",
					Description:       fmt.Sprintf("%s (%s) is linked to pool %s (%s)", res.ResourceID, res.CIDR, pool.Name, pool.CIDR),
					RecommendedAction: "Review whether this is a soft link, import conflict, or drift.",
					NodeIDs:           []string{nodeID, poolNodeID(pool.ID)},
					DiscoveredIDs:     []uuid.UUID{res.ID},
					PoolIDs:           []int64{pool.ID},
					AccountIDs:        []int64{res.AccountID},
					Regions:           []string{res.Region},
					ObjectTypes:       []string{string(res.ResourceType), string(pool.Type)},
					CIDR:              res.CIDR,
					Evidence:          []string{fmt.Sprintf("pool_id=%d", pool.ID), "pool_cidr=" + pool.CIDR},
				})
			}
		}
		for _, pool := range pools {
			if res.PoolID != nil && *res.PoolID == pool.ID {
				continue
			}
			if res.CIDR == "" || pool.CIDR == "" {
				continue
			}
			if networkCIDREqual(res.CIDR, pool.CIDR) {
				if res.PoolID == nil {
					add(domain.NetworkConflict{
						ID:                fmt.Sprintf("unlinked-exact-pool:%s:%d", res.ID.String(), pool.ID),
						Type:              "unlinked_exact_pool",
						Severity:          "warning",
						Status:            "open",
						Title:             "Discovered CIDR matches managed pool",
						Description:       fmt.Sprintf("%s (%s) exactly matches pool %s", res.ResourceID, res.CIDR, pool.Name),
						RecommendedAction: "Link the discovered resource to the matching managed pool, import separately, or mark reviewed.",
						NodeIDs:           []string{nodeID, poolNodeID(pool.ID)},
						DiscoveredIDs:     []uuid.UUID{res.ID},
						PoolIDs:           []int64{pool.ID},
						AccountIDs:        accountIDsForPool(res.AccountID, pool),
						Regions:           []string{res.Region},
						ObjectTypes:       []string{string(res.ResourceType), string(pool.Type)},
						CIDR:              res.CIDR,
						Evidence:          []string{fmt.Sprintf("pool_id=%d", pool.ID), "pool_cidr=" + pool.CIDR},
					})
				}
				continue
			}
			if !networkCIDROverlaps(res.CIDR, pool.CIDR) {
				continue
			}
			if networkCIDRContains(pool.CIDR, res.CIDR) || networkCIDRContains(res.CIDR, pool.CIDR) {
				continue
			}
			add(domain.NetworkConflict{
				ID:                fmt.Sprintf("pool-overlap:%s:%d", res.ID.String(), pool.ID),
				Type:              "managed_overlap",
				Severity:          "warning",
				Status:            "open",
				Title:             "Discovered CIDR overlaps managed pool",
				Description:       fmt.Sprintf("%s (%s) overlaps pool %s (%s)", res.ResourceID, res.CIDR, pool.Name, pool.CIDR),
				RecommendedAction: "Choose whether to link, import separately, or skip this discovered object.",
				NodeIDs:           []string{nodeID, poolNodeID(pool.ID)},
				DiscoveredIDs:     []uuid.UUID{res.ID},
				PoolIDs:           []int64{pool.ID},
				AccountIDs:        accountIDsForPool(res.AccountID, pool),
				Regions:           []string{res.Region},
				ObjectTypes:       []string{string(res.ResourceType), string(pool.Type)},
				CIDR:              res.CIDR,
				Evidence:          []string{fmt.Sprintf("pool_id=%d", pool.ID), "pool_cidr=" + pool.CIDR},
			})
		}
	}

	for cidr, matches := range resourcesByCIDR {
		accountsSeen := map[int64]bool{}
		for _, res := range matches {
			accountsSeen[res.AccountID] = true
		}
		if len(accountsSeen) < 2 {
			continue
		}
		nodeIDs := make([]string, 0, len(matches))
		ids := make([]uuid.UUID, 0, len(matches))
		accountIDs := make([]int64, 0, len(accountsSeen))
		regions := make([]string, 0, len(matches))
		objectTypes := make([]string, 0, len(matches))
		evidence := make([]string, 0, len(matches))
		for id := range accountsSeen {
			accountIDs = append(accountIDs, id)
		}
		sort.Slice(accountIDs, func(i, j int) bool { return accountIDs[i] < accountIDs[j] })
		for _, res := range matches {
			nodeIDs = append(nodeIDs, resourceNodeID(res.ID))
			ids = append(ids, res.ID)
			regions = append(regions, res.Region)
			objectTypes = append(objectTypes, string(res.ResourceType))
			accountName := accounts[res.AccountID].Name
			evidence = append(evidence, fmt.Sprintf("%s in %s (%s)", res.ResourceID, firstNonEmpty(accountName, fmt.Sprintf("account %d", res.AccountID)), res.Region))
		}
		add(domain.NetworkConflict{
			ID:                "duplicate-cidr:" + strings.ReplaceAll(cidr, "/", "_"),
			Type:              "duplicate_cidr",
			Severity:          "critical",
			Status:            "open",
			Title:             "Duplicate CIDR across accounts",
			Description:       fmt.Sprintf("%s is discovered in multiple accounts", cidr),
			RecommendedAction: "Choose the authoritative account or mark the duplicate reviewed.",
			NodeIDs:           nodeIDs,
			DiscoveredIDs:     ids,
			AccountIDs:        accountIDs,
			Regions:           uniqueStrings(regions),
			ObjectTypes:       uniqueStrings(objectTypes),
			CIDR:              cidr,
			Evidence:          evidence,
		})
	}

	sort.SliceStable(conflicts, func(i, j int) bool {
		if conflicts[i].Severity == conflicts[j].Severity {
			return conflicts[i].ID < conflicts[j].ID
		}
		return severityRank(conflicts[i].Severity) > severityRank(conflicts[j].Severity)
	})
	return issuesByNode, conflicts
}

func (ns *NetworkServer) applyStoredConflictResolutions(ctx context.Context, conflicts []domain.NetworkConflict) []domain.NetworkConflict {
	if ns.driftStore == nil {
		return conflicts
	}
	for i := range conflicts {
		item, err := ns.driftStore.GetDriftItem(ctx, conflicts[i].ID)
		if err != nil {
			continue
		}
		conflicts[i].Status = string(item.Status)
		conflicts[i].ResolutionState = string(item.Status)
		if item.IgnoreReason != "" {
			conflicts[i].ResolutionRequested = parseNetworkDecision(item.IgnoreReason)
			conflicts[i].Evidence = append(conflicts[i].Evidence, "resolution="+item.IgnoreReason)
		}
	}
	return conflicts
}

func (ns *NetworkServer) persistNetworkConflictResolution(ctx context.Context, conflict domain.NetworkConflict, req domain.ResolveNetworkConflictRequest) error {
	return ns.persistNetworkConflictActionResolution(ctx, conflict, req, nil)
}

func (ns *NetworkServer) persistNetworkConflictActionResolution(ctx context.Context, conflict domain.NetworkConflict, req domain.ResolveNetworkConflictRequest, details map[string]string) error {
	if ns.driftStore == nil {
		return fmt.Errorf("drift store is not available")
	}
	status := networkDecisionStatus(req.Decision)
	reason := networkResolutionReason(req.Decision, req.Reason)
	if len(details) > 0 {
		if err := ns.driftStore.UpdateDriftDetails(ctx, conflict.ID, details); err != nil && !errors.Is(err, storage.ErrNotFound) {
			return err
		}
	}
	if err := ns.driftStore.UpdateDriftStatus(ctx, conflict.ID, status, reason); err == nil {
		return nil
	} else if !errors.Is(err, storage.ErrNotFound) {
		return err
	}

	now := time.Now().UTC()
	item := domain.DriftItem{
		ID:           conflict.ID,
		AccountID:    firstConflictAccountID(conflict),
		Type:         domain.DriftTypeAccountDrift,
		Severity:     driftSeverityFromNetwork(conflict.Severity),
		Status:       domain.DriftStatusOpen,
		Title:        conflict.Title,
		Description:  conflict.Description,
		ResourceCIDR: conflict.CIDR,
		Details: map[string]string{
			"network_conflict_type": conflict.Type,
			"recommended_action":    conflict.RecommendedAction,
		},
		DetectedAt: now,
		UpdatedAt:  now,
	}
	for key, value := range details {
		item.Details[key] = value
	}
	if len(conflict.DiscoveredIDs) > 0 {
		item.ResourceID = &conflict.DiscoveredIDs[0]
	}
	if len(conflict.PoolIDs) > 0 {
		item.PoolID = &conflict.PoolIDs[0]
	}
	if err := ns.driftStore.CreateDriftItem(ctx, item); err != nil {
		return err
	}
	return ns.driftStore.UpdateDriftStatus(ctx, conflict.ID, status, reason)
}

func (ns *NetworkServer) ensureNetworkConflictResolutionRecord(ctx context.Context, conflict domain.NetworkConflict, reason string, details map[string]string) error {
	if ns.driftStore == nil {
		return fmt.Errorf("drift store is not available")
	}
	if _, err := ns.driftStore.GetDriftItem(ctx, conflict.ID); err == nil {
		if len(details) > 0 {
			if err := ns.driftStore.UpdateDriftDetails(ctx, conflict.ID, details); err != nil {
				return err
			}
		}
		return ns.driftStore.UpdateDriftStatus(ctx, conflict.ID, domain.DriftStatusOpen, reason)
	} else if !errors.Is(err, storage.ErrNotFound) {
		return err
	}

	now := time.Now().UTC()
	item := domain.DriftItem{
		ID:           conflict.ID,
		AccountID:    firstConflictAccountID(conflict),
		Type:         domain.DriftTypeAccountDrift,
		Severity:     driftSeverityFromNetwork(conflict.Severity),
		Status:       domain.DriftStatusOpen,
		Title:        conflict.Title,
		Description:  conflict.Description,
		ResourceCIDR: conflict.CIDR,
		Details: map[string]string{
			"network_conflict_type": conflict.Type,
			"recommended_action":    conflict.RecommendedAction,
		},
		IgnoreReason: reason,
		DetectedAt:   now,
		UpdatedAt:    now,
	}
	for key, value := range details {
		item.Details[key] = value
	}
	if len(conflict.DiscoveredIDs) > 0 {
		item.ResourceID = &conflict.DiscoveredIDs[0]
	}
	if len(conflict.PoolIDs) > 0 {
		item.PoolID = &conflict.PoolIDs[0]
	}
	return ns.driftStore.CreateDriftItem(ctx, item)
}

func (ns *NetworkServer) rollbackNetworkConflictLink(ctx context.Context, discoveredID uuid.UUID, previousPoolID *int64) error {
	if previousPoolID == nil {
		return ns.discStore.UnlinkResource(ctx, discoveredID)
	}
	return ns.discStore.LinkResourceToPool(ctx, discoveredID, *previousPoolID)
}

func (ns *NetworkServer) rollbackNetworkConflictImport(ctx context.Context, resp domain.DiscoveryImportApplyResponse) error {
	var errs []string
	for _, resourceID := range resp.LinkedResourceIDs {
		if err := ns.discStore.UnlinkResource(ctx, resourceID); err != nil {
			errs = append(errs, fmt.Sprintf("unlink %s: %v", resourceID, err))
		}
	}
	for i := len(resp.CreatedPoolIDs) - 1; i >= 0; i-- {
		if ok, err := ns.store.DeletePool(ctx, resp.CreatedPoolIDs[i]); err != nil {
			errs = append(errs, fmt.Sprintf("delete pool %d: %v", resp.CreatedPoolIDs[i], err))
		} else if !ok {
			errs = append(errs, fmt.Sprintf("delete pool %d: not found", resp.CreatedPoolIDs[i]))
		}
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

func (ns *NetworkServer) findNetworkConflict(ctx context.Context, conflictID string) (*domain.NetworkConflict, error) {
	view, err := ns.buildNetworkView(ctx, networkViewFilters{})
	if err != nil {
		return nil, err
	}
	for i := range view.conflicts {
		if view.conflicts[i].ID == conflictID {
			return &view.conflicts[i], nil
		}
	}
	return nil, nil
}

func (ns *NetworkServer) conflictAfterAction(ctx context.Context, fallback domain.NetworkConflict, action string) domain.NetworkConflict {
	conflict, err := ns.findNetworkConflict(ctx, fallback.ID)
	if err == nil && conflict != nil {
		return *conflict
	}
	fallback.Status = string(domain.DriftStatusResolved)
	fallback.ResolutionState = fallback.Status
	fallback.ResolutionRequested = action
	return fallback
}

func validateNetworkConflictLinkAction(conflict domain.NetworkConflict, res domain.DiscoveredResource, pool domain.Pool, req domain.NetworkConflictLinkActionRequest) error {
	if !containsUUID(conflict.DiscoveredIDs, req.DiscoveredID) {
		return fmt.Errorf("discovered resource is not part of this conflict")
	}
	if !containsInt64(conflict.PoolIDs, req.PoolID) {
		return fmt.Errorf("pool is not part of this conflict")
	}
	if res.PoolID != nil {
		return fmt.Errorf("discovered resource is already linked")
	}
	if !req.Override {
		if pool.AccountID != nil && *pool.AccountID != res.AccountID {
			return fmt.Errorf("pool account does not match discovered resource account; set override to force")
		}
		if res.CIDR == "" || pool.CIDR == "" {
			return fmt.Errorf("resource and pool must both have CIDR values")
		}
		if !networkCIDREqual(pool.CIDR, res.CIDR) && !networkCIDRContains(pool.CIDR, res.CIDR) {
			return fmt.Errorf("pool CIDR does not contain discovered resource CIDR; set override to force")
		}
	}
	return nil
}

func validateNetworkConflictImportSelection(conflict domain.NetworkConflict, req domain.NetworkConflictImportActionRequest) error {
	if req.PoolID != nil && *req.PoolID < 1 {
		return fmt.Errorf("pool_id must be a positive integer")
	}
	for _, id := range req.ResourceIDs {
		if !containsUUID(conflict.DiscoveredIDs, id) {
			return fmt.Errorf("resource %s is not part of this conflict", id)
		}
	}
	return nil
}

func (ns *NetworkServer) importActionAccountID(ctx context.Context, conflict domain.NetworkConflict, resourceIDs []uuid.UUID) (int64, error) {
	var accountID int64
	for _, id := range resourceIDs {
		res, err := ns.discStore.GetDiscoveredResource(ctx, id)
		if err != nil {
			if errors.Is(err, storage.ErrNotFound) {
				return 0, fmt.Errorf("resource %s not found", id)
			}
			return 0, err
		}
		if accountID == 0 {
			accountID = res.AccountID
		} else if accountID != res.AccountID {
			return 0, fmt.Errorf("selected resources must belong to one account")
		}
	}
	if accountID == 0 {
		accountID = firstConflictAccountID(conflict)
	}
	if accountID < 1 {
		return 0, fmt.Errorf("could not determine import account")
	}
	return accountID, nil
}

func buildNetworkHierarchy(flat []domain.NetworkNode) []domain.NetworkNode {
	nodes := make(map[string]domain.NetworkNode, len(flat))
	childrenByParent := make(map[string][]string, len(flat))
	for i := range flat {
		node := flat[i]
		node.Children = nil
		nodes[node.ID] = node
	}
	var rootIDs []string
	for _, node := range nodes {
		if node.ParentID != nil {
			if _, ok := nodes[*node.ParentID]; ok {
				childrenByParent[*node.ParentID] = append(childrenByParent[*node.ParentID], node.ID)
				continue
			}
		}
		rootIDs = append(rootIDs, node.ID)
	}
	var build func(id string) domain.NetworkNode
	build = func(id string) domain.NetworkNode {
		node := nodes[id]
		for _, childID := range childrenByParent[id] {
			node.Children = append(node.Children, build(childID))
		}
		sortNetworkNodes(node.Children)
		return node
	}
	roots := make([]domain.NetworkNode, 0, len(rootIDs))
	for _, id := range rootIDs {
		roots = append(roots, build(id))
	}
	sortNetworkNodes(roots)
	return roots
}

func sortNetworkNodes(nodes []domain.NetworkNode) {
	sort.SliceStable(nodes, func(i, j int) bool {
		if nodes[i].Kind == nodes[j].Kind {
			return nodes[i].Name < nodes[j].Name
		}
		return nodes[i].Kind < nodes[j].Kind
	})
	for i := range nodes {
		sortNetworkNodes(nodes[i].Children)
	}
}

func matchesNetworkFilters(node domain.NetworkNode, filters networkViewFilters) bool {
	if filters.accountID > 0 && (node.AccountID == nil || *node.AccountID != filters.accountID) {
		if node.Kind != "pool" || node.AccountID != nil {
			return false
		}
	}
	if filters.provider != "" && node.Provider != filters.provider {
		return false
	}
	if filters.region != "" && node.Region != filters.region {
		return false
	}
	if filters.objectType != "" && node.ObjectType != filters.objectType {
		return false
	}
	if filters.status != "" && node.State != filters.status {
		return false
	}
	if filters.conflictType != "" {
		found := false
		for _, issue := range node.Issues {
			if issue.Type == filters.conflictType {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	if filters.query != "" {
		haystack := strings.ToLower(strings.Join([]string{node.Name, node.CIDR, node.IPAddress, node.ProviderResourceID, node.AccountName, node.Region}, " "))
		if !strings.Contains(haystack, filters.query) {
			return false
		}
	}
	return true
}

func filterNetworkConflicts(conflicts []domain.NetworkConflict, filters networkViewFilters) []domain.NetworkConflict {
	out := make([]domain.NetworkConflict, 0, len(conflicts))
	for _, conflict := range conflicts {
		if filters.conflictType != "" && conflict.Type != filters.conflictType {
			continue
		}
		if filters.accountID > 0 && !containsInt64(conflict.AccountIDs, filters.accountID) {
			continue
		}
		if filters.region != "" && !containsStringValue(conflict.Regions, filters.region) {
			continue
		}
		if filters.objectType != "" && !containsStringValue(conflict.ObjectTypes, filters.objectType) {
			continue
		}
		if filters.query != "" {
			haystack := strings.ToLower(strings.Join(append([]string{conflict.Title, conflict.Description, conflict.CIDR}, conflict.Evidence...), " "))
			if !strings.Contains(haystack, filters.query) {
				continue
			}
		}
		out = append(out, conflict)
	}
	return out
}

func networkDecisionStatus(decision string) domain.DriftStatus {
	switch strings.ToLower(strings.TrimSpace(decision)) {
	case "ignore":
		return domain.DriftStatusIgnored
	case "skip", "link", "import":
		return domain.DriftStatusResolved
	case "defer":
		return domain.DriftStatusOpen
	default:
		return domain.DriftStatusOpen
	}
}

func networkConflictDecisions() []string {
	return []string{"skip", "ignore", "defer"}
}

func isValidNetworkDecision(decision string) bool {
	decision = strings.ToLower(strings.TrimSpace(decision))
	for _, valid := range networkConflictDecisions() {
		if decision == valid {
			return true
		}
	}
	return false
}

func networkResolutionReason(decision string, reason string) string {
	decision = strings.TrimSpace(decision)
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return "decision=" + decision
	}
	return "decision=" + decision + " reason=" + reason
}

func networkActionReason(action string, reason string, details map[string]string) string {
	parts := []string{}
	for key, value := range details {
		if strings.TrimSpace(value) == "" {
			continue
		}
		parts = append(parts, key+"="+strings.TrimSpace(value))
	}
	sort.Strings(parts)
	if reason = strings.TrimSpace(reason); reason != "" {
		parts = append(parts, "operator_reason="+reason)
	}
	if len(parts) == 0 {
		return action
	}
	return action + " " + strings.Join(parts, " ")
}

func parseNetworkDecision(reason string) string {
	for _, part := range strings.Fields(reason) {
		if strings.HasPrefix(part, "decision=") {
			return strings.TrimPrefix(part, "decision=")
		}
	}
	return ""
}

func firstConflictAccountID(conflict domain.NetworkConflict) int64 {
	if len(conflict.AccountIDs) > 0 {
		return conflict.AccountIDs[0]
	}
	return 0
}

func containsUUID(values []uuid.UUID, target uuid.UUID) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func joinUUIDs(values []uuid.UUID) string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, value.String())
	}
	return strings.Join(out, ",")
}

func joinInt64s(values []int64) string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, fmt.Sprintf("%d", value))
	}
	return strings.Join(out, ",")
}

func driftSeverityFromNetwork(severity string) domain.DriftSeverity {
	switch severity {
	case "critical":
		return domain.DriftSeverityCritical
	case "info":
		return domain.DriftSeverityInfo
	default:
		return domain.DriftSeverityWarning
	}
}

func poolNodeID(id int64) string { return fmt.Sprintf("pool:%d", id) }

func resourceNodeID(id uuid.UUID) string { return "discovered:" + id.String() }

func resourceKey(accountID int64, providerResourceID string) string {
	return fmt.Sprintf("%d:%s", accountID, providerResourceID)
}

func bestContainingPool(cidr string, pools map[int64]domain.Pool) *domain.Pool {
	var best *domain.Pool
	bestBits := -1
	for _, pool := range pools {
		if networkCIDRContains(pool.CIDR, cidr) {
			bits := networkPrefixLength(pool.CIDR)
			if bits > bestBits {
				p := pool
				best = &p
				bestBits = bits
			}
		}
	}
	return best
}

func worstNodeState(current string, issues []domain.NetworkIssue) string {
	for _, issue := range issues {
		if issue.Severity == "critical" {
			return "conflict"
		}
	}
	if len(issues) > 0 {
		return "warning"
	}
	return current
}

func networkCIDREqual(a, b string) bool {
	pa, errA := netip.ParsePrefix(a)
	pb, errB := netip.ParsePrefix(b)
	return errA == nil && errB == nil && pa.Masked() == pb.Masked()
}

func networkCIDRContains(parent, child string) bool {
	p, errP := netip.ParsePrefix(parent)
	c, errC := netip.ParsePrefix(child)
	if errP != nil || errC != nil || p.Addr().BitLen() != c.Addr().BitLen() {
		return false
	}
	return p.Bits() <= c.Bits() && p.Masked().Contains(c.Masked().Addr())
}

func networkCIDROverlaps(a, b string) bool {
	pa, errA := netip.ParsePrefix(a)
	pb, errB := netip.ParsePrefix(b)
	if errA != nil || errB != nil || pa.Addr().BitLen() != pb.Addr().BitLen() {
		return false
	}
	return pa.Masked().Contains(pb.Masked().Addr()) || pb.Masked().Contains(pa.Masked().Addr())
}

func networkPrefixLength(cidr string) int {
	prefix, err := netip.ParsePrefix(cidr)
	if err != nil {
		return -1
	}
	return prefix.Bits()
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func accountIDsForPool(discoveredAccountID int64, pool domain.Pool) []int64 {
	if pool.AccountID == nil || *pool.AccountID == discoveredAccountID {
		return []int64{discoveredAccountID}
	}
	return []int64{discoveredAccountID, *pool.AccountID}
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, value := range values {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func severityRank(severity string) int {
	switch severity {
	case "critical":
		return 3
	case "warning":
		return 2
	default:
		return 1
	}
}

func containsInt64(items []int64, want int64) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}

func containsStringValue(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
