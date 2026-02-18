package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/netip"

	"cloudpam/internal/domain"
	"cloudpam/internal/validation"
)

// --- Request/Response types ---

type schemaPoolEntry struct {
	Ref       string `json:"ref"`
	Name      string `json:"name"`
	CIDR      string `json:"cidr"`
	Type      string `json:"type"`
	ParentRef string `json:"parent_ref"`
}

type schemaCheckRequest struct {
	Pools []schemaPoolEntry `json:"pools"`
}

type schemaConflict struct {
	PlannedCIDR      string `json:"planned_cidr"`
	PlannedName      string `json:"planned_name"`
	ExistingPoolID   int64  `json:"existing_pool_id"`
	ExistingPoolName string `json:"existing_pool_name"`
	ExistingCIDR     string `json:"existing_cidr"`
	OverlapType      string `json:"overlap_type"`
}

type schemaCheckResponse struct {
	Conflicts     []schemaConflict `json:"conflicts"`
	TotalPools    int              `json:"total_pools"`
	ConflictCount int              `json:"conflict_count"`
}

type schemaApplyRequest struct {
	Pools         []schemaPoolEntry `json:"pools"`
	Status        string            `json:"status"`
	Tags          map[string]string `json:"tags"`
	SkipConflicts bool              `json:"skip_conflicts"`
}

type schemaApplyResponse struct {
	Created    int              `json:"created"`
	Skipped    int              `json:"skipped"`
	Errors     []string         `json:"errors"`
	RootPoolID int64            `json:"root_pool_id"`
	PoolMap    map[string]int64 `json:"pool_map"`
}

// POST /api/v1/schema/check — detect conflicts between proposed pools and existing pools.
func (s *Server) handleSchemaCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeErr(r.Context(), w, http.StatusMethodNotAllowed, "method not allowed", "")
		return
	}
	ctx := r.Context()

	var req schemaCheckRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeErr(ctx, w, http.StatusBadRequest, "invalid json", err.Error())
		return
	}
	if len(req.Pools) == 0 {
		s.writeErr(ctx, w, http.StatusBadRequest, "pools array is required", "")
		return
	}

	// Validate all proposed CIDRs
	for i, p := range req.Pools {
		if err := validation.ValidateCIDRWithOptions(p.CIDR, validation.CIDROptions{
			MinPrefix: 8,
			MaxPrefix: 30,
		}); err != nil {
			s.writeErr(ctx, w, http.StatusBadRequest, fmt.Sprintf("pool %d (%s): %v", i, p.Name, err), "")
			return
		}
	}

	// Get all existing pools
	existing, err := s.store.ListPools(ctx)
	if err != nil {
		s.writeErr(ctx, w, http.StatusInternalServerError, "failed to list existing pools", err.Error())
		return
	}

	var conflicts []schemaConflict
	for _, proposed := range req.Pools {
		pp, err := netip.ParsePrefix(proposed.CIDR)
		if err != nil {
			continue // already validated above
		}
		for _, ex := range existing {
			ep, err := netip.ParsePrefix(ex.CIDR)
			if err != nil {
				continue
			}
			if prefixesOverlapIPv4(pp, ep) {
				overlapType := "overlap"
				if pp.Bits() <= ep.Bits() && pp.Contains(ep.Addr()) {
					overlapType = "contains"
				} else if ep.Bits() <= pp.Bits() && ep.Contains(pp.Addr()) {
					overlapType = "contained_by"
				}
				conflicts = append(conflicts, schemaConflict{
					PlannedCIDR:      proposed.CIDR,
					PlannedName:      proposed.Name,
					ExistingPoolID:   ex.ID,
					ExistingPoolName: ex.Name,
					ExistingCIDR:     ex.CIDR,
					OverlapType:      overlapType,
				})
			}
		}
	}

	resp := schemaCheckResponse{
		Conflicts:     conflicts,
		TotalPools:    len(req.Pools),
		ConflictCount: len(conflicts),
	}
	if resp.Conflicts == nil {
		resp.Conflicts = []schemaConflict{}
	}
	writeJSON(w, http.StatusOK, resp)
}

// POST /api/v1/schema/apply — bulk-create pools from a schema tree.
func (s *Server) handleSchemaApply(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeErr(r.Context(), w, http.StatusMethodNotAllowed, "method not allowed", "")
		return
	}
	ctx := r.Context()

	var req schemaApplyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeErr(ctx, w, http.StatusBadRequest, "invalid json", err.Error())
		return
	}
	if len(req.Pools) == 0 {
		s.writeErr(ctx, w, http.StatusBadRequest, "pools array is required", "")
		return
	}

	// Set defaults
	status := domain.PoolStatusPlanned
	if req.Status != "" {
		status = domain.PoolStatus(req.Status)
		if !domain.IsValidPoolStatus(status) {
			s.writeErr(ctx, w, http.StatusBadRequest, fmt.Sprintf("invalid status: %s", req.Status), "")
			return
		}
	}
	tags := req.Tags
	if tags == nil {
		tags = map[string]string{}
	}
	tags["schema_planner"] = "true"

	// Validate all entries upfront
	refSet := make(map[string]bool)
	for i, p := range req.Pools {
		if p.Ref == "" {
			s.writeErr(ctx, w, http.StatusBadRequest, fmt.Sprintf("pool %d: ref is required", i), "")
			return
		}
		if refSet[p.Ref] {
			s.writeErr(ctx, w, http.StatusBadRequest, fmt.Sprintf("pool %d: duplicate ref %q", i, p.Ref), "")
			return
		}
		refSet[p.Ref] = true

		if err := validation.ValidateName(p.Name); err != nil {
			s.writeErr(ctx, w, http.StatusBadRequest, fmt.Sprintf("pool %d (%s): %v", i, p.Ref, err), "")
			return
		}
		if err := validation.ValidateCIDRWithOptions(p.CIDR, validation.CIDROptions{
			MinPrefix: 8,
			MaxPrefix: 30,
		}); err != nil {
			s.writeErr(ctx, w, http.StatusBadRequest, fmt.Sprintf("pool %d (%s): %v", i, p.Ref, err), "")
			return
		}
		if p.ParentRef != "" && !refSet[p.ParentRef] {
			// parent_ref must reference an earlier entry in the array (topological order)
			found := false
			for _, prev := range req.Pools[:i] {
				if prev.Ref == p.ParentRef {
					found = true
					break
				}
			}
			if !found {
				s.writeErr(ctx, w, http.StatusBadRequest,
					fmt.Sprintf("pool %d (%s): parent_ref %q not found in preceding entries", i, p.Ref, p.ParentRef), "")
				return
			}
		}
	}

	// If skip_conflicts is false, run conflict check first
	if !req.SkipConflicts {
		existing, err := s.store.ListPools(ctx)
		if err != nil {
			s.writeErr(ctx, w, http.StatusInternalServerError, "failed to list existing pools", err.Error())
			return
		}
		for _, proposed := range req.Pools {
			pp, err := netip.ParsePrefix(proposed.CIDR)
			if err != nil {
				continue
			}
			for _, ex := range existing {
				ep, err := netip.ParsePrefix(ex.CIDR)
				if err != nil {
					continue
				}
				if prefixesOverlapIPv4(pp, ep) {
					s.writeErr(ctx, w, http.StatusConflict,
						fmt.Sprintf("pool %q (%s) overlaps with existing pool %q (%s)", proposed.Name, proposed.CIDR, ex.Name, ex.CIDR),
						"set skip_conflicts to true to bypass this check")
					return
				}
			}
		}
	}

	// Create pools in order (the request must be topologically sorted)
	refToID := make(map[string]int64)
	var created, skipped int
	var errs []string
	var rootPoolID int64

	for _, p := range req.Pools {
		poolType := domain.PoolType(p.Type)
		if poolType == "" {
			poolType = domain.PoolTypeSubnet
		}
		if !domain.IsValidPoolType(poolType) {
			errs = append(errs, fmt.Sprintf("pool %q: invalid type %q, using subnet", p.Ref, p.Type))
			poolType = domain.PoolTypeSubnet
		}

		cp := domain.CreatePool{
			Name:        p.Name,
			CIDR:        p.CIDR,
			Type:        poolType,
			Status:      status,
			Source:      domain.PoolSourceManual,
			Description: "Created by Schema Planner",
			Tags:        tags,
		}

		// Resolve parent ref
		if p.ParentRef != "" {
			parentID, ok := refToID[p.ParentRef]
			if !ok {
				errs = append(errs, fmt.Sprintf("pool %q: parent_ref %q not yet created", p.Ref, p.ParentRef))
				skipped++
				continue
			}
			cp.ParentID = &parentID
		}

		pool, err := s.store.CreatePool(ctx, cp)
		if err != nil {
			errs = append(errs, fmt.Sprintf("pool %q: %v", p.Ref, err))
			skipped++
			continue
		}

		refToID[p.Ref] = pool.ID
		if p.ParentRef == "" {
			rootPoolID = pool.ID
		}
		created++

		s.logAudit(ctx, "create", "pool", fmt.Sprintf("%d", pool.ID), pool.Name, http.StatusCreated)
	}

	resp := schemaApplyResponse{
		Created:    created,
		Skipped:    skipped,
		Errors:     errs,
		RootPoolID: rootPoolID,
		PoolMap:    refToID,
	}
	if resp.Errors == nil {
		resp.Errors = []string{}
	}
	writeJSON(w, http.StatusOK, resp)
}
