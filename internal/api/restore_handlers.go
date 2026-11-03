package api

import (
	"fmt"
	"net/http"

	"cloudpam/internal/audit"
	"cloudpam/internal/auth"
	"cloudpam/internal/domain"
)

// Listing and restoring soft-deleted records takes the resource's delete permission:
// whoever can delete a pool or account can undo that delete.

// GET /api/v1/pools/deleted
func (s *Server) listDeletedPools(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if !s.allowRestore(w, r, auth.ResourcePools, http.MethodGet) {
		return
	}
	pools, err := s.store.ListDeletedPools(ctx)
	if err != nil {
		s.writeStoreErr(ctx, w, err)
		return
	}
	writeJSON(w, http.StatusOK, pools)
}

// POST /api/v1/pools/{id}/restore
func (s *Server) restorePoolHandler(w http.ResponseWriter, r *http.Request, id int64) {
	ctx := r.Context()
	if !s.allowRestore(w, r, auth.ResourcePools, http.MethodPost) {
		return
	}
	deleted, err := s.store.ListDeletedPools(ctx)
	if err != nil {
		s.writeStoreErr(ctx, w, err)
		return
	}
	var target *domain.Pool
	for i := range deleted {
		if deleted[i].ID == id {
			target = &deleted[i]
			break
		}
	}
	if target == nil {
		s.writeErr(ctx, w, http.StatusNotFound, "no deleted pool with this id", "")
		return
	}
	p, ok, err := s.restorePool(ctx, *target)
	if err != nil {
		s.writeStoreErr(ctx, w, err)
		return
	}
	if !ok {
		s.writeErr(ctx, w, http.StatusNotFound, "no deleted pool with this id", "")
		return
	}
	s.logAudit(ctx, audit.ActionRestore, audit.ResourcePool, fmt.Sprintf("%d", p.ID), p.Name, http.StatusOK)
	writeJSON(w, http.StatusOK, p)
}

// GET /api/v1/accounts/deleted
func (s *Server) listDeletedAccounts(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if !s.allowRestore(w, r, auth.ResourceAccounts, http.MethodGet) {
		return
	}
	accounts, err := s.store.ListDeletedAccounts(ctx)
	if err != nil {
		s.writeStoreErr(ctx, w, err)
		return
	}
	writeJSON(w, http.StatusOK, accounts)
}

// POST /api/v1/accounts/{id}/restore
func (s *Server) restoreAccountHandler(w http.ResponseWriter, r *http.Request, id int64) {
	ctx := r.Context()
	if !s.allowRestore(w, r, auth.ResourceAccounts, http.MethodPost) {
		return
	}
	a, ok, err := s.store.RestoreAccount(ctx, id)
	if err != nil {
		s.writeStoreErr(ctx, w, err)
		return
	}
	if !ok {
		s.writeErr(ctx, w, http.StatusNotFound, "no deleted account with this id", "")
		return
	}
	s.logAudit(ctx, audit.ActionRestore, audit.ResourceAccount, fmt.Sprintf("%d", a.ID), a.Name, http.StatusOK)
	writeJSON(w, http.StatusOK, a)
}

func (s *Server) allowRestore(w http.ResponseWriter, r *http.Request, resource, method string) bool {
	ctx := r.Context()
	if r.Method != method {
		w.Header().Set("Allow", method)
		s.writeErr(ctx, w, http.StatusMethodNotAllowed, "method not allowed", "")
		return false
	}
	if !auth.HasPermissionContext(ctx, auth.GetEffectiveRole(ctx), resource, auth.ActionDelete) {
		writeJSON(w, http.StatusForbidden, apiError{Error: "forbidden"})
		return false
	}
	return true
}
