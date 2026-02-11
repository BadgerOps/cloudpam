package http

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/netip"
	"strconv"
	"strings"

	"cloudpam/internal/audit"
	"cloudpam/internal/auth"
	"cloudpam/internal/domain"
	"cloudpam/internal/storage"
	"cloudpam/internal/validation"
)

// protectedPoolsHandler returns a handler for /api/v1/pools with RBAC.
func (s *Server) protectedPoolsHandler(logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		role := auth.GetEffectiveRole(ctx)

		switch r.Method {
		case http.MethodGet:
			// List pools requires pools:list or pools:read
			if !auth.HasPermission(role, auth.ResourcePools, auth.ActionList) &&
				!auth.HasPermission(role, auth.ResourcePools, auth.ActionRead) {
				writeJSON(w, http.StatusForbidden, apiError{Error: "forbidden"})
				return
			}
			s.listPools(w, r)
		case http.MethodPost:
			// Create pool requires pools:create
			if !auth.HasPermission(role, auth.ResourcePools, auth.ActionCreate) {
				writeJSON(w, http.StatusForbidden, apiError{Error: "forbidden"})
				return
			}
			s.createPool(w, r)
		default:
			w.Header().Set("Allow", strings.Join([]string{http.MethodGet, http.MethodPost}, ", "))
			s.writeErr(ctx, w, http.StatusMethodNotAllowed, "method not allowed", "")
		}
	})
}

// protectedPoolsSubroutesHandler returns a handler for /api/v1/pools/{id}/* with RBAC.
func (s *Server) protectedPoolsSubroutesHandler(logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		role := auth.GetEffectiveRole(ctx)

		path := strings.TrimPrefix(r.URL.Path, "/api/v1/pools/")
		parts := strings.Split(strings.Trim(path, "/"), "/")
		if len(parts) == 0 || parts[0] == "" {
			s.writeErr(ctx, w, http.StatusNotFound, "not found", "")
			return
		}

		// Handle /pools/hierarchy (no ID)
		if parts[0] == "hierarchy" {
			if r.Method != http.MethodGet {
				s.writeErr(ctx, w, http.StatusMethodNotAllowed, "method not allowed", "")
				return
			}
			// Requires pools:read
			if !auth.HasPermission(role, auth.ResourcePools, auth.ActionRead) {
				writeJSON(w, http.StatusForbidden, apiError{Error: "forbidden"})
				return
			}
			s.handlePoolsHierarchy(w, r)
			return
		}

		id64, err := strconv.ParseInt(parts[0], 10, 64)
		if err != nil {
			s.writeErr(ctx, w, http.StatusBadRequest, "invalid pool id", "")
			return
		}

		// Handle /pools/{id}/blocks
		if len(parts) >= 2 && parts[1] == "blocks" {
			if r.Method != http.MethodGet {
				s.writeErr(ctx, w, http.StatusMethodNotAllowed, "method not allowed", "")
				return
			}
			// Requires pools:read
			if !auth.HasPermission(role, auth.ResourcePools, auth.ActionRead) {
				writeJSON(w, http.StatusForbidden, apiError{Error: "forbidden"})
				return
			}
			s.blocksForPool(w, r, id64)
			return
		}

		// Handle /pools/{id}/stats
		if len(parts) >= 2 && parts[1] == "stats" {
			if r.Method != http.MethodGet {
				s.writeErr(ctx, w, http.StatusMethodNotAllowed, "method not allowed", "")
				return
			}
			// Requires pools:read
			if !auth.HasPermission(role, auth.ResourcePools, auth.ActionRead) {
				writeJSON(w, http.StatusForbidden, apiError{Error: "forbidden"})
				return
			}
			s.handlePoolStats(w, r, id64)
			return
		}

		// Handle /pools/{id}
		switch r.Method {
		case http.MethodGet:
			if !auth.HasPermission(role, auth.ResourcePools, auth.ActionRead) {
				writeJSON(w, http.StatusForbidden, apiError{Error: "forbidden"})
				return
			}
			p, ok, err := s.store.GetPool(ctx, id64)
			if err != nil {
				s.writeErr(ctx, w, http.StatusBadRequest, err.Error(), "")
				return
			}
			if !ok {
				s.writeErr(ctx, w, http.StatusNotFound, "not found", "")
				return
			}
			writeJSON(w, http.StatusOK, p)

		case http.MethodPatch:
			if !auth.HasPermission(role, auth.ResourcePools, auth.ActionUpdate) {
				writeJSON(w, http.StatusForbidden, apiError{Error: "forbidden"})
				return
			}
			s.updatePool(w, r, id64)

		case http.MethodDelete:
			if !auth.HasPermission(role, auth.ResourcePools, auth.ActionDelete) {
				writeJSON(w, http.StatusForbidden, apiError{Error: "forbidden"})
				return
			}
			var ok bool
			force := strings.ToLower(r.URL.Query().Get("force"))
			if force == "1" || force == "true" || force == "yes" {
				ok, err = s.store.DeletePoolCascade(ctx, id64)
			} else {
				ok, err = s.store.DeletePool(ctx, id64)
			}
			if err != nil {
				s.writeErr(ctx, w, http.StatusConflict, err.Error(), "")
				return
			}
			if !ok {
				s.writeErr(ctx, w, http.StatusNotFound, "not found", "")
				return
			}
			w.WriteHeader(http.StatusNoContent)

		default:
			s.writeErr(ctx, w, http.StatusMethodNotAllowed, "method not allowed", "")
		}
	})
}

func (s *Server) handlePools(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.listPools(w, r)
	case http.MethodPost:
		s.createPool(w, r)
	default:
		w.Header().Set("Allow", strings.Join([]string{http.MethodGet, http.MethodPost}, ", "))
		s.writeErr(r.Context(), w, http.StatusMethodNotAllowed, "method not allowed", "")
	}
}

// /api/v1/pools/{id}/blocks?new_prefix_len=24
// /api/v1/pools/hierarchy - returns pool hierarchy tree
// /api/v1/pools/{id}/stats - returns pool statistics
func (s *Server) handlePoolsSubroutes(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/pools/")
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		s.writeErr(r.Context(), w, http.StatusNotFound, "not found", "")
		return
	}

	// Handle /api/v1/pools/hierarchy (no ID)
	if parts[0] == "hierarchy" {
		if r.Method != http.MethodGet {
			s.writeErr(r.Context(), w, http.StatusMethodNotAllowed, "method not allowed", "")
			return
		}
		s.handlePoolsHierarchy(w, r)
		return
	}

	id64, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		s.writeErr(r.Context(), w, http.StatusBadRequest, "invalid pool id", "")
		return
	}
	if len(parts) >= 2 && parts[1] == "blocks" {
		if r.Method != http.MethodGet {
			s.writeErr(r.Context(), w, http.StatusMethodNotAllowed, "method not allowed", "")
			return
		}
		s.blocksForPool(w, r, id64)
		return
	}
	// Handle /api/v1/pools/{id}/stats
	if len(parts) >= 2 && parts[1] == "stats" {
		if r.Method != http.MethodGet {
			s.writeErr(r.Context(), w, http.StatusMethodNotAllowed, "method not allowed", "")
			return
		}
		s.handlePoolStats(w, r, id64)
		return
	}
	// Pool detail
	switch r.Method {
	case http.MethodGet:
		p, ok, err := s.store.GetPool(r.Context(), id64)
		if err != nil {
			s.writeErr(r.Context(), w, http.StatusBadRequest, err.Error(), "")
			return
		}
		if !ok {
			s.writeErr(r.Context(), w, http.StatusNotFound, "not found", "")
			return
		}
		writeJSON(w, http.StatusOK, p)
	case http.MethodPatch:
		s.updatePool(w, r, id64)
	case http.MethodDelete:
		// Get pool info before delete for audit logging
		pool, poolFound, _ := s.store.GetPool(r.Context(), id64)
		var ok bool
		force := strings.ToLower(r.URL.Query().Get("force"))
		if force == "1" || force == "true" || force == "yes" {
			ok, err = s.store.DeletePoolCascade(r.Context(), id64)
		} else {
			ok, err = s.store.DeletePool(r.Context(), id64)
		}
		if err != nil {
			s.writeErr(r.Context(), w, http.StatusConflict, err.Error(), "")
			return
		}
		if !ok {
			s.writeErr(r.Context(), w, http.StatusNotFound, "not found", "")
			return
		}
		poolName := ""
		if poolFound {
			poolName = pool.Name
		}
		s.logAudit(r.Context(), audit.ActionDelete, audit.ResourcePool, fmt.Sprintf("%d", id64), poolName, http.StatusNoContent)
		w.WriteHeader(http.StatusNoContent)
	default:
		s.writeErr(r.Context(), w, http.StatusMethodNotAllowed, "method not allowed", "")
	}
}

// handlePoolsHierarchy returns the pool hierarchy tree.
// GET /api/v1/pools/hierarchy?root_id=1
func (s *Server) handlePoolsHierarchy(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Optional root_id query param
	var rootID *int64
	if rootIDStr := r.URL.Query().Get("root_id"); rootIDStr != "" {
		id, err := strconv.ParseInt(rootIDStr, 10, 64)
		if err != nil {
			s.writeErr(ctx, w, http.StatusBadRequest, "invalid root_id", "")
			return
		}
		rootID = &id
	}

	hierarchy, err := s.store.GetPoolHierarchy(ctx, rootID)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			s.writeErr(ctx, w, http.StatusNotFound, err.Error(), "")
			return
		}
		s.writeErr(ctx, w, http.StatusInternalServerError, "internal error", err.Error())
		return
	}

	type response struct {
		Pools []domain.PoolWithStats `json:"pools"`
	}
	writeJSON(w, http.StatusOK, response{Pools: hierarchy})
}

// handlePoolStats returns statistics for a specific pool.
// GET /api/v1/pools/{id}/stats
func (s *Server) handlePoolStats(w http.ResponseWriter, r *http.Request, id int64) {
	ctx := r.Context()

	stats, err := s.store.CalculatePoolUtilization(ctx, id)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			s.writeErr(ctx, w, http.StatusNotFound, err.Error(), "")
			return
		}
		s.writeErr(ctx, w, http.StatusInternalServerError, "internal error", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, stats)
}

// updatePool handles PATCH /api/v1/pools/{id}
func (s *Server) updatePool(w http.ResponseWriter, r *http.Request, id int64) {
	ctx := r.Context()

	var payload struct {
		AccountID   *int64             `json:"account_id"`
		Name        *string            `json:"name"`
		Type        *domain.PoolType   `json:"type"`
		Status      *domain.PoolStatus `json:"status"`
		Description *string            `json:"description"`
		Tags        *map[string]string `json:"tags"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		s.writeErr(ctx, w, http.StatusBadRequest, "invalid json", "")
		return
	}

	// Validate name if provided
	if payload.Name != nil {
		trimmed := strings.TrimSpace(*payload.Name)
		payload.Name = &trimmed
		if err := validation.ValidateName(*payload.Name); err != nil {
			s.writeErr(ctx, w, http.StatusBadRequest, err.Error(), "")
			return
		}
	}

	// Validate type if provided
	if payload.Type != nil && !domain.IsValidPoolType(*payload.Type) {
		s.writeErr(ctx, w, http.StatusBadRequest, "invalid pool type", fmt.Sprintf("valid types: %v", domain.ValidPoolTypes))
		return
	}

	// Validate status if provided
	if payload.Status != nil && !domain.IsValidPoolStatus(*payload.Status) {
		s.writeErr(ctx, w, http.StatusBadRequest, "invalid pool status", fmt.Sprintf("valid statuses: %v", domain.ValidPoolStatuses))
		return
	}

	update := domain.UpdatePool{
		Name:        payload.Name,
		AccountID:   payload.AccountID,
		Type:        payload.Type,
		Status:      payload.Status,
		Description: payload.Description,
		Tags:        payload.Tags,
	}

	p, ok, err := s.store.UpdatePool(ctx, id, update)
	if err != nil {
		s.writeErr(ctx, w, http.StatusBadRequest, err.Error(), "")
		return
	}
	if !ok {
		s.writeErr(ctx, w, http.StatusNotFound, "not found", "")
		return
	}
	s.logAudit(ctx, audit.ActionUpdate, audit.ResourcePool, fmt.Sprintf("%d", p.ID), p.Name, http.StatusOK)
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) listPools(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Check for include_stats query param
	includeStats := strings.ToLower(r.URL.Query().Get("include_stats"))
	if includeStats == "true" || includeStats == "1" || includeStats == "yes" {
		// Return all pools with stats (flat list, not hierarchy)
		pools, err := s.store.ListPools(ctx)
		if err != nil {
			s.writeErr(ctx, w, http.StatusInternalServerError, "internal error", err.Error())
			return
		}

		type poolWithStats struct {
			domain.Pool
			Stats domain.PoolStats `json:"stats"`
		}

		result := make([]poolWithStats, 0, len(pools))
		for _, p := range pools {
			stats, err := s.store.CalculatePoolUtilization(ctx, p.ID)
			if err != nil {
				s.writeErr(ctx, w, http.StatusInternalServerError, "internal error", err.Error())
				return
			}
			result = append(result, poolWithStats{Pool: p, Stats: *stats})
		}
		writeJSON(w, http.StatusOK, result)
		return
	}

	pools, err := s.store.ListPools(ctx)
	if err != nil {
		s.writeErr(ctx, w, http.StatusInternalServerError, "internal error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, pools)
}

func (s *Server) createPool(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := s.logger
	var in domain.CreatePool
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		logger.WarnContext(ctx, "pools:create invalid json", appendRequestID(ctx, []any{"reason", err.Error()})...)
		s.writeErr(r.Context(), w, http.StatusBadRequest, "invalid json", "")
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	in.CIDR = strings.TrimSpace(in.CIDR)

	// Validate pool name
	if err := validation.ValidateName(in.Name); err != nil {
		logger.WarnContext(ctx, "pools:create invalid name", appendRequestID(ctx, []any{
			"name", in.Name,
			"reason", err.Error(),
		})...)
		s.writeErr(r.Context(), w, http.StatusBadRequest, err.Error(), "")
		return
	}

	// Validate CIDR format, IPv4, reserved ranges, and prefix bounds
	if err := validation.ValidateCIDR(in.CIDR); err != nil {
		logger.WarnContext(ctx, "pools:create invalid cidr", appendRequestID(ctx, []any{
			"cidr", in.CIDR,
			"reason", err.Error(),
		})...)
		s.writeErr(r.Context(), w, http.StatusBadRequest, err.Error(), "")
		return
	}

	// Validate pool type if provided
	if in.Type != "" && !domain.IsValidPoolType(in.Type) {
		logger.WarnContext(ctx, "pools:create invalid type", appendRequestID(ctx, []any{
			"type", in.Type,
		})...)
		s.writeErr(r.Context(), w, http.StatusBadRequest, "invalid pool type", fmt.Sprintf("valid types: %v", domain.ValidPoolTypes))
		return
	}

	// Validate pool status if provided
	if in.Status != "" && !domain.IsValidPoolStatus(in.Status) {
		logger.WarnContext(ctx, "pools:create invalid status", appendRequestID(ctx, []any{
			"status", in.Status,
		})...)
		s.writeErr(r.Context(), w, http.StatusBadRequest, "invalid pool status", fmt.Sprintf("valid statuses: %v", domain.ValidPoolStatuses))
		return
	}

	// Validate pool source if provided
	if in.Source != "" && !domain.IsValidPoolSource(in.Source) {
		logger.WarnContext(ctx, "pools:create invalid source", appendRequestID(ctx, []any{
			"source", in.Source,
		})...)
		s.writeErr(r.Context(), w, http.StatusBadRequest, "invalid pool source", fmt.Sprintf("valid sources: %v", domain.ValidPoolSources))
		return
	}

	// If ParentID provided, ensure child CIDR is subset of parent CIDR (IPv4 only for now).
	if in.ParentID != nil {
		parent, ok, err := s.store.GetPool(ctx, *in.ParentID)
		if err != nil {
			s.writeErr(r.Context(), w, http.StatusInternalServerError, "internal error", err.Error())
			return
		}
		if !ok {
			logger.WarnContext(ctx, "pools:create parent not found", appendRequestID(ctx, []any{
				"parent_id", *in.ParentID,
				"cidr", in.CIDR,
			})...)
			s.writeErr(r.Context(), w, http.StatusBadRequest, "parent not found", "")
			return
		}
		if err := validateChildCIDR(parent.CIDR, in.CIDR); err != nil {
			logger.WarnContext(ctx, "pools:create invalid sub-pool cidr", appendRequestID(ctx, []any{
				"child_cidr", in.CIDR,
				"parent_cidr", parent.CIDR,
				"reason", err.Error(),
			})...)
			s.writeErr(r.Context(), w, http.StatusBadRequest, "invalid sub-pool cidr", err.Error())
			return
		}
	}

	// Overlap protection: disallow any overlapping CIDRs within the same parent scope
	// (i.e., among pools sharing the same parent_id, or among top-level pools).
	{
		pfxNew, _ := netip.ParsePrefix(in.CIDR)
		if !pfxNew.Addr().Is4() {
			s.writeErr(r.Context(), w, http.StatusBadRequest, "only ipv4 supported for now", "")
			return
		}
		all, err := s.store.ListPools(ctx)
		if err != nil {
			s.writeErr(r.Context(), w, http.StatusInternalServerError, "internal error", err.Error())
			return
		}
		for _, p := range all {
			if in.ParentID == nil {
				if p.ParentID != nil {
					continue
				}
			} else {
				if p.ParentID == nil || *p.ParentID != *in.ParentID {
					continue
				}
			}
			// Skip comparing with an exact duplicate; DB uniqueness should also catch
			if strings.EqualFold(strings.TrimSpace(p.CIDR), in.CIDR) {
				continue
			}
			old, err := netip.ParsePrefix(p.CIDR)
			if err != nil || !old.Addr().Is4() {
				continue
			}
			if prefixesOverlapIPv4(old, pfxNew) {
				logger.WarnContext(ctx, "pools:create cidr overlap", appendRequestID(ctx, []any{
					"candidate_cidr", in.CIDR,
					"existing_pool_id", p.ID,
					"existing_cidr", p.CIDR,
				})...)
				s.writeErr(r.Context(), w, http.StatusBadRequest, "cidr overlaps with existing block", fmt.Sprintf("conflicts with pool #%d (%s)", p.ID, p.CIDR))
				return
			}
		}
	}
	p, err := s.store.CreatePool(ctx, in)
	if err != nil {
		logger.WarnContext(ctx, "pools:create storage error", appendRequestID(ctx, []any{
			"name", in.Name,
			"cidr", in.CIDR,
			"parent_id", valueOrNil(in.ParentID),
			"account_id", valueOrNil(in.AccountID),
			"error", err.Error(),
		})...)
		s.writeErr(r.Context(), w, http.StatusBadRequest, err.Error(), "")
		return
	}
	logger.InfoContext(ctx, "pools:create success", appendRequestID(ctx, []any{
		"id", p.ID,
		"name", p.Name,
		"cidr", p.CIDR,
		"parent_id", valueOrNil(p.ParentID),
		"account_id", valueOrNil(p.AccountID),
	})...)
	s.logAudit(ctx, audit.ActionCreate, audit.ResourcePool, fmt.Sprintf("%d", p.ID), p.Name, http.StatusCreated)
	writeJSON(w, http.StatusCreated, p)
}
