package http

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"cloudpam/internal/audit"
	"cloudpam/internal/auth"
	"cloudpam/internal/domain"
	"cloudpam/internal/validation"
)

// protectedAccountsHandler returns a handler for /api/v1/accounts with RBAC.
func (s *Server) protectedAccountsHandler(logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		role := auth.GetEffectiveRole(ctx)

		switch r.Method {
		case http.MethodGet:
			if !auth.HasPermission(role, auth.ResourceAccounts, auth.ActionList) &&
				!auth.HasPermission(role, auth.ResourceAccounts, auth.ActionRead) {
				writeJSON(w, http.StatusForbidden, apiError{Error: "forbidden"})
				return
			}
			s.listAccounts(w, r)
		case http.MethodPost:
			if !auth.HasPermission(role, auth.ResourceAccounts, auth.ActionCreate) {
				writeJSON(w, http.StatusForbidden, apiError{Error: "forbidden"})
				return
			}
			s.createAccount(w, r)
		default:
			w.Header().Set("Allow", strings.Join([]string{http.MethodGet, http.MethodPost}, ", "))
			s.writeErr(ctx, w, http.StatusMethodNotAllowed, "method not allowed", "")
		}
	})
}

// protectedAccountsSubroutesHandler returns a handler for /api/v1/accounts/{id} with RBAC.
func (s *Server) protectedAccountsSubroutesHandler(logger *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		role := auth.GetEffectiveRole(ctx)

		path := strings.TrimPrefix(r.URL.Path, "/api/v1/accounts/")
		idStr := strings.Trim(path, "/")
		if idStr == "" {
			s.writeErr(ctx, w, http.StatusNotFound, "not found", "")
			return
		}
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			s.writeErr(ctx, w, http.StatusBadRequest, "invalid id", "")
			return
		}

		switch r.Method {
		case http.MethodGet:
			if !auth.HasPermission(role, auth.ResourceAccounts, auth.ActionRead) {
				writeJSON(w, http.StatusForbidden, apiError{Error: "forbidden"})
				return
			}
			a, ok, err := s.store.GetAccount(ctx, id)
			if err != nil {
				s.writeErr(ctx, w, http.StatusBadRequest, err.Error(), "")
				return
			}
			if !ok {
				s.writeErr(ctx, w, http.StatusNotFound, "not found", "")
				return
			}
			writeJSON(w, http.StatusOK, a)

		case http.MethodPatch:
			if !auth.HasPermission(role, auth.ResourceAccounts, auth.ActionUpdate) {
				writeJSON(w, http.StatusForbidden, apiError{Error: "forbidden"})
				return
			}
			var in domain.Account
			if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
				s.writeErr(ctx, w, http.StatusBadRequest, "invalid json", "")
				return
			}
			in.Name = strings.TrimSpace(in.Name)
			if in.Name != "" {
				if err := validation.ValidateName(in.Name); err != nil {
					s.writeErr(ctx, w, http.StatusBadRequest, err.Error(), "")
					return
				}
			}
			a, ok, err := s.store.UpdateAccount(ctx, id, in)
			if err != nil {
				s.writeErr(ctx, w, http.StatusBadRequest, err.Error(), "")
				return
			}
			if !ok {
				s.writeErr(ctx, w, http.StatusNotFound, "not found", "")
				return
			}
			writeJSON(w, http.StatusOK, a)

		case http.MethodDelete:
			if !auth.HasPermission(role, auth.ResourceAccounts, auth.ActionDelete) {
				writeJSON(w, http.StatusForbidden, apiError{Error: "forbidden"})
				return
			}
			var ok bool
			force := strings.ToLower(r.URL.Query().Get("force"))
			if force == "1" || force == "true" || force == "yes" {
				ok, err = s.store.DeleteAccountCascade(ctx, id)
			} else {
				ok, err = s.store.DeleteAccount(ctx, id)
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

// Accounts: GET list, POST create
func (s *Server) handleAccounts(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.listAccounts(w, r)
	case http.MethodPost:
		s.createAccount(w, r)
	default:
		w.Header().Set("Allow", strings.Join([]string{http.MethodGet, http.MethodPost}, ", "))
		s.writeErr(r.Context(), w, http.StatusMethodNotAllowed, "method not allowed", "")
	}
}

// /api/v1/accounts/{id}
func (s *Server) handleAccountsSubroutes(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/accounts/")
	idStr := strings.Trim(path, "/")
	if idStr == "" {
		s.writeErr(r.Context(), w, http.StatusNotFound, "not found", "")
		return
	}
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		s.writeErr(r.Context(), w, http.StatusBadRequest, "invalid id", "")
		return
	}
	switch r.Method {
	case http.MethodGet:
		a, ok, err := s.store.GetAccount(r.Context(), id)
		if err != nil {
			s.writeErr(r.Context(), w, http.StatusBadRequest, err.Error(), "")
			return
		}
		if !ok {
			s.writeErr(r.Context(), w, http.StatusNotFound, "not found", "")
			return
		}
		writeJSON(w, http.StatusOK, a)
	case http.MethodPatch:
		var in domain.Account
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			s.writeErr(r.Context(), w, http.StatusBadRequest, "invalid json", "")
			return
		}
		// Validate name if provided
		in.Name = strings.TrimSpace(in.Name)
		if in.Name != "" {
			if err := validation.ValidateName(in.Name); err != nil {
				s.writeErr(r.Context(), w, http.StatusBadRequest, err.Error(), "")
				return
			}
		}
		a, ok, err := s.store.UpdateAccount(r.Context(), id, in)
		if err != nil {
			s.writeErr(r.Context(), w, http.StatusBadRequest, err.Error(), "")
			return
		}
		if !ok {
			s.writeErr(r.Context(), w, http.StatusNotFound, "not found", "")
			return
		}
		s.logAudit(r.Context(), audit.ActionUpdate, audit.ResourceAccount, fmt.Sprintf("%d", a.ID), a.Name, http.StatusOK)
		writeJSON(w, http.StatusOK, a)
	case http.MethodDelete:
		// Get account info before delete for audit logging
		acct, acctFound, _ := s.store.GetAccount(r.Context(), id)
		var ok bool
		force := strings.ToLower(r.URL.Query().Get("force"))
		if force == "1" || force == "true" || force == "yes" {
			ok, err = s.store.DeleteAccountCascade(r.Context(), id)
		} else {
			ok, err = s.store.DeleteAccount(r.Context(), id)
		}
		if err != nil {
			s.writeErr(r.Context(), w, http.StatusConflict, err.Error(), "")
			return
		}
		if !ok {
			s.writeErr(r.Context(), w, http.StatusNotFound, "not found", "")
			return
		}
		accountName := ""
		if acctFound {
			accountName = acct.Name
		}
		s.logAudit(r.Context(), audit.ActionDelete, audit.ResourceAccount, fmt.Sprintf("%d", id), accountName, http.StatusNoContent)
		w.WriteHeader(http.StatusNoContent)
	default:
		s.writeErr(r.Context(), w, http.StatusMethodNotAllowed, "method not allowed", "")
	}
}

func (s *Server) listAccounts(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	accs, err := s.store.ListAccounts(ctx)
	if err != nil {
		s.writeErr(r.Context(), w, http.StatusInternalServerError, "internal error", err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(accs)
}

func (s *Server) createAccount(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var in domain.CreateAccount
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		fields := appendRequestID(ctx, []any{"reason", err.Error()})
		s.logger.WarnContext(ctx, "accounts:create invalid json", fields...)
		s.writeErr(r.Context(), w, http.StatusBadRequest, "invalid json", "")
		return
	}
	in.Key = strings.TrimSpace(in.Key)
	in.Name = strings.TrimSpace(in.Name)

	// Validate account key format
	if err := validation.ValidateAccountKey(in.Key); err != nil {
		fields := appendRequestID(ctx, []any{"key", in.Key, "reason", err.Error()})
		s.logger.WarnContext(ctx, "accounts:create invalid key", fields...)
		s.writeErr(r.Context(), w, http.StatusBadRequest, err.Error(), "")
		return
	}

	// Validate account name
	if err := validation.ValidateName(in.Name); err != nil {
		fields := appendRequestID(ctx, []any{"name", in.Name, "reason", err.Error()})
		s.logger.WarnContext(ctx, "accounts:create invalid name", fields...)
		s.writeErr(r.Context(), w, http.StatusBadRequest, err.Error(), "")
		return
	}

	a, err := s.store.CreateAccount(ctx, in)
	if err != nil {
		fields := appendRequestID(ctx, []any{
			"key", in.Key,
			"name", in.Name,
			"error", err.Error(),
		})
		s.logger.WarnContext(ctx, "accounts:create storage error", fields...)
		s.writeErr(r.Context(), w, http.StatusBadRequest, err.Error(), "")
		return
	}
	fields := appendRequestID(ctx, []any{
		"id", a.ID,
		"key", a.Key,
		"name", a.Name,
	})
	s.logger.InfoContext(ctx, "accounts:create success", fields...)
	s.logAudit(ctx, audit.ActionCreate, audit.ResourceAccount, fmt.Sprintf("%d", a.ID), a.Name, http.StatusCreated)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(a)
}
