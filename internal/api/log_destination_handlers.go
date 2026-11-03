package api

import (
	"net/http"

	"cloudpam/internal/domain"
)

// SetLogDestinations wires the audit forwarding targets the settings API
// reports. The function is called on every request so the delivery health is
// current. A nil source reports no destinations.
func (ss *SettingsServer) SetLogDestinations(src func() []domain.LogDestination) {
	ss.logDestinations = src
}

func (ss *SettingsServer) handleListLogDestinations(w http.ResponseWriter, r *http.Request) {
	resp := domain.LogDestinationsResponse{Destinations: []domain.LogDestination{}}
	if ss.logDestinations != nil {
		resp.Destinations = append(resp.Destinations, ss.logDestinations()...)
	}
	writeJSON(w, http.StatusOK, resp)
}
