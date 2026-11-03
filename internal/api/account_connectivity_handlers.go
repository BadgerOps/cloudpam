package api

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"

	"cloudpam/internal/audit"
	"cloudpam/internal/auth"
	"cloudpam/internal/discovery"
)

const accountConnectivityTestPath = "POST /api/v1/accounts/{accountId}/test"

// registerAccountConnectivityRoute needs discovery:create, the permission that triggers a sync.
func (d *DiscoveryServer) registerAccountConnectivityRoute(dualMW Middleware, logger *slog.Logger) {
	createMW := RequirePermissionMiddleware(auth.ResourceDiscovery, auth.ActionCreate, logger)
	d.srv.handleOpenAPIRoute(accountConnectivityTestPath, dualMW(createMW(http.HandlerFunc(d.handleAccountConnectivityTest))))
}

// A probe that fails is a 200 with status failed, not an error.
func (d *DiscoveryServer) handleAccountConnectivityTest(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := strconv.ParseInt(r.PathValue("accountId"), 10, 64)
	if err != nil {
		d.srv.writeErr(ctx, w, http.StatusBadRequest, "invalid account id", "")
		return
	}
	account, ok, err := d.srv.store.GetAccount(ctx, id)
	if err != nil {
		d.srv.logger.ErrorContext(ctx, "failed to load account", "error", err)
		d.srv.writeErr(ctx, w, http.StatusInternalServerError, "failed to load account", "")
		return
	}
	if !ok {
		d.srv.writeErr(ctx, w, http.StatusNotFound, "account not found", "")
		return
	}
	if d.syncService == nil {
		d.srv.writeErr(ctx, w, http.StatusServiceUnavailable, "sync service not available", "")
		return
	}

	result, err := d.syncService.TestConnectivity(ctx, account)
	switch {
	case errors.Is(err, discovery.ErrConnectivityUnsupported):
		d.srv.writeErr(ctx, w, http.StatusUnprocessableEntity, err.Error(), "")
	case err != nil:
		d.srv.logger.ErrorContext(ctx, "failed to test account connectivity", "error", err)
		d.srv.writeErr(ctx, w, http.StatusInternalServerError, "failed to test account connectivity", "")
	default:
		d.srv.logAuditWithChanges(ctx, audit.ActionTest, audit.ResourceAccount, fmt.Sprintf("%d", account.ID), account.Name,
			&audit.Changes{After: map[string]any{"status": result.Status, "source": result.Source, "latency_ms": result.LatencyMS}}, http.StatusOK)
		writeJSON(w, http.StatusOK, result)
	}
}
