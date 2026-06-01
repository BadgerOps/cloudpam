package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/netip"
	"sort"
	"strconv"
	"strings"

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
	if len(parts) != 2 || parts[0] == "" || parts[1] != "resolve" {
		ns.srv.writeErr(r.Context(), w, http.StatusNotFound, "not found", "")
		return
	}
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
	view, err := ns.buildNetworkView(r.Context(), networkViewFilters{})
	if err != nil {
		ns.srv.writeErr(r.Context(), w, http.StatusInternalServerError, "network conflicts failed", err.Error())
		return
	}
	for _, conflict := range view.conflicts {
		if conflict.ID == parts[0] {
			conflict.ResolutionState = "requested"
			conflict.ResolutionRequested = req.Decision
			writeJSON(w, http.StatusOK, conflict)
			return
		}
	}
	ns.srv.writeErr(r.Context(), w, http.StatusNotFound, "conflict not found", "")
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
		conflict.AvailableDecisions = []string{"import", "link", "skip", "ignore", "defer"}
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
			if res.CIDR == "" || pool.CIDR == "" || networkCIDREqual(res.CIDR, pool.CIDR) || !networkCIDROverlaps(res.CIDR, pool.CIDR) {
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
