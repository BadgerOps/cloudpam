package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"

	"cloudpam/internal/audit"
	"cloudpam/internal/auth"
	"cloudpam/internal/discovery"
	"cloudpam/internal/domain"
	"cloudpam/internal/storage"
)

const (
	accountCredentialPath = "/api/v1/accounts/{accountId}/credentials"

	errNoCredentialStored = "no credential stored for this account"
)

// AccountCredentialServer handles the per-account stored credential endpoints.
type AccountCredentialServer struct {
	srv         *Server
	credentials *discovery.AccountCredentials
	sync        *discovery.SyncService
}

// NewAccountCredentialServer creates an AccountCredentialServer over the vault and the sync service.
func NewAccountCredentialServer(srv *Server, credentials *discovery.AccountCredentials, sync *discovery.SyncService) *AccountCredentialServer {
	return &AccountCredentialServer{srv: srv, credentials: credentials, sync: sync}
}

// RegisterProtectedAccountCredentialRoutes registers the credential routes with RBAC.
func (cs *AccountCredentialServer) RegisterProtectedAccountCredentialRoutes(dualMW Middleware, logger *slog.Logger) {
	readMW := RequirePermissionMiddleware(auth.ResourceAccounts, auth.ActionRead, logger)
	updateMW := RequirePermissionMiddleware(auth.ResourceAccounts, auth.ActionUpdate, logger)

	cs.srv.handleOpenAPIRoute("GET "+accountCredentialPath, dualMW(readMW(http.HandlerFunc(cs.handleGet))))
	cs.srv.handleOpenAPIRoute("PUT "+accountCredentialPath, dualMW(updateMW(http.HandlerFunc(cs.handlePut))))
	cs.srv.handleOpenAPIRoute("DELETE "+accountCredentialPath, dualMW(updateMW(http.HandlerFunc(cs.handleDelete))))
	cs.srv.handleOpenAPIRoute("POST "+accountCredentialPath+"/test", dualMW(updateMW(http.HandlerFunc(cs.handleTest))))
}

// RegisterAccountCredentialRoutes registers the credential routes without RBAC (for tests).
func (cs *AccountCredentialServer) RegisterAccountCredentialRoutes() {
	cs.srv.handleOpenAPIRouteFunc("GET "+accountCredentialPath, cs.handleGet)
	cs.srv.handleOpenAPIRouteFunc("PUT "+accountCredentialPath, cs.handlePut)
	cs.srv.handleOpenAPIRouteFunc("DELETE "+accountCredentialPath, cs.handleDelete)
	cs.srv.handleOpenAPIRouteFunc("POST "+accountCredentialPath+"/test", cs.handleTest)
}

// loadAccount resolves the live account named by the path, writing 400 or 404 otherwise.
func (cs *AccountCredentialServer) loadAccount(w http.ResponseWriter, r *http.Request) (domain.Account, bool) {
	ctx := r.Context()
	id, err := strconv.ParseInt(r.PathValue("accountId"), 10, 64)
	if err != nil {
		cs.srv.writeErr(ctx, w, http.StatusBadRequest, "invalid account id", "")
		return domain.Account{}, false
	}
	account, ok, err := cs.srv.store.GetAccount(ctx, id)
	if err != nil {
		cs.writeInternal(ctx, w, "failed to load account", err)
		return domain.Account{}, false
	}
	if !ok {
		cs.srv.writeErr(ctx, w, http.StatusNotFound, "account not found", "")
		return domain.Account{}, false
	}
	return account, true
}

// writeInternal answers 500 without the error text, which may name storage
// internals, and keeps the cause in the server log.
func (cs *AccountCredentialServer) writeInternal(ctx context.Context, w http.ResponseWriter, msg string, err error) {
	cs.srv.logger.ErrorContext(ctx, msg, "error", err)
	cs.srv.writeErr(ctx, w, http.StatusInternalServerError, msg, "")
}

func (cs *AccountCredentialServer) audit(ctx context.Context, action string, account domain.Account, after map[string]any, status int) {
	var changes *audit.Changes
	if after != nil {
		changes = &audit.Changes{After: after}
	}
	cs.srv.logAuditWithChanges(ctx, action, audit.ResourceAccountCredential, fmt.Sprintf("%d", account.ID), account.Name, changes, status)
}

// handleGet returns the stored credential's summary.
// GET /api/v1/accounts/{accountId}/credentials
func (cs *AccountCredentialServer) handleGet(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	account, ok := cs.loadAccount(w, r)
	if !ok {
		return
	}
	cred, err := cs.credentials.Get(ctx, account.ID)
	switch {
	case errors.Is(err, storage.ErrNotFound):
		cs.srv.writeErr(ctx, w, http.StatusNotFound, errNoCredentialStored, "")
	case err != nil:
		cs.writeInternal(ctx, w, "failed to load credential", err)
	default:
		writeJSON(w, http.StatusOK, cred)
	}
}

// handlePut stores or replaces the account's credential.
// PUT /api/v1/accounts/{accountId}/credentials
func (cs *AccountCredentialServer) handlePut(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	account, ok := cs.loadAccount(w, r)
	if !ok {
		return
	}
	var req domain.PutAccountCredentialRequest
	if !cs.srv.decodeStrictJSON(w, r, &req) {
		return
	}
	cred, err := cs.credentials.Put(ctx, account, req)
	var inputErr *discovery.CredentialInputError
	switch {
	case errors.As(err, &inputErr):
		cs.srv.writeErr(ctx, w, http.StatusBadRequest, "invalid "+inputErr.Field, inputErr.Detail)
	case errors.Is(err, discovery.ErrCredentialProviderMismatch):
		cs.srv.writeErr(ctx, w, http.StatusBadRequest, err.Error(), "")
	case errors.Is(err, discovery.ErrCredentialKeyEphemeral):
		cs.srv.writeErr(ctx, w, http.StatusConflict, err.Error(), "")
	case errors.Is(err, storage.ErrNotFound):
		cs.srv.writeErr(ctx, w, http.StatusNotFound, "account not found", "")
	case err != nil:
		cs.writeInternal(ctx, w, "failed to store credential", err)
	default:
		cs.audit(ctx, audit.ActionUpdate, account, map[string]any{"type": cred.Type}, http.StatusOK)
		writeJSON(w, http.StatusOK, cred)
	}
}

// handleDelete removes the account's credential.
// DELETE /api/v1/accounts/{accountId}/credentials
func (cs *AccountCredentialServer) handleDelete(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	account, ok := cs.loadAccount(w, r)
	if !ok {
		return
	}
	existed, err := cs.credentials.Delete(ctx, account.ID)
	switch {
	case err != nil:
		cs.writeInternal(ctx, w, "failed to delete credential", err)
	case !existed:
		cs.srv.writeErr(ctx, w, http.StatusNotFound, errNoCredentialStored, "")
	default:
		cs.audit(ctx, audit.ActionDelete, account, nil, http.StatusNoContent)
		w.WriteHeader(http.StatusNoContent)
	}
}

// handleTest runs one read-only provider call as the stored credential.
// A failing credential is a 200 with status failed.
// POST /api/v1/accounts/{accountId}/credentials/test
func (cs *AccountCredentialServer) handleTest(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	account, ok := cs.loadAccount(w, r)
	if !ok {
		return
	}
	result, err := cs.sync.TestCredential(ctx, account)
	switch {
	case errors.Is(err, storage.ErrNotFound):
		cs.srv.writeErr(ctx, w, http.StatusNotFound, errNoCredentialStored, "")
	case err != nil:
		cs.writeInternal(ctx, w, "failed to test credential", err)
	default:
		cs.audit(ctx, audit.ActionTest, account, map[string]any{"status": result.Status}, http.StatusOK)
		writeJSON(w, http.StatusOK, result)
	}
}
