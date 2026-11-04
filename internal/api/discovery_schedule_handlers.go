package api

import (
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"cloudpam/internal/audit"
	"cloudpam/internal/auth"
	"cloudpam/internal/storage"
)

const schedulePath = "/api/v1/discovery/schedules/{accountId}"

func (d *DiscoveryServer) registerScheduleRoutes(dualMW Middleware, logger *slog.Logger) {
	read := RequirePermissionMiddleware(auth.ResourceDiscovery, auth.ActionRead, logger)
	write := RequirePermissionMiddleware(auth.ResourceDiscovery, auth.ActionCreate, logger)
	d.srv.handleOpenAPIRoute("GET "+schedulePath, dualMW(read(http.HandlerFunc(d.handleSchedule))))
	d.srv.handleOpenAPIRoute("PUT "+schedulePath, dualMW(write(http.HandlerFunc(d.handleSchedule))))
}
func (d *DiscoveryServer) handleSchedule(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := strconv.ParseInt(r.PathValue("accountId"), 10, 64)
	if err != nil || id < 1 {
		d.srv.writeErr(ctx, w, 400, "invalid account id", "")
		return
	}
	account, found, err := d.srv.store.GetAccount(ctx, id)
	if err != nil {
		d.srv.writeErr(ctx, w, 500, "failed to load account", "")
		return
	}
	if !found {
		d.srv.writeErr(ctx, w, 404, "account not found", "")
		return
	}
	store, ok := d.store.(storage.SyncScheduleStore)
	if !ok {
		d.srv.writeErr(ctx, w, 503, "discovery schedules unavailable", "")
		return
	}
	if r.Method == http.MethodPut {
		// A pointer distinguishes omitted interval_seconds from an explicit disable.
		var body struct {
			IntervalSeconds *int64 `json:"interval_seconds"`
		}
		if !d.srv.decodeStrictJSON(w, r, &body) {
			return
		}
		if body.IntervalSeconds == nil || (*body.IntervalSeconds != 0 && (*body.IntervalSeconds < 60 || *body.IntervalSeconds > 604800)) {
			d.srv.writeErr(ctx, w, 400, "interval_seconds must be 0 or between 60 and 604800", "")
			return
		}
		if *body.IntervalSeconds > 0 && (account.Provider != "aws" && account.Provider != "gcp") {
			d.srv.writeErr(ctx, w, 422, "scheduled discovery supports aws and gcp accounts", "")
			return
		}
		if err := store.PutSyncSchedule(ctx, id, *body.IntervalSeconds, time.Now().UTC()); err != nil {
			d.srv.writeErr(ctx, w, 500, "failed to save schedule", "")
			return
		}
		d.srv.logAuditWithChanges(ctx, audit.ActionUpdate, audit.ResourceAccount, strconv.FormatInt(id, 10), account.Name, &audit.Changes{After: map[string]any{"sync_interval_seconds": *body.IntervalSeconds}}, 200)
	}
	v, err := store.GetSyncSchedule(ctx, id)
	if err != nil {
		d.srv.writeErr(ctx, w, 500, "failed to load schedule", "")
		return
	}
	writeJSON(w, http.StatusOK, v)
}
