package http

import (
	"net/http"
	"strconv"
	"strings"

	"cloudpam/internal/domain"
)

// handleSearch handles GET /api/v1/search
func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeErr(r.Context(), w, http.StatusMethodNotAllowed, "method not allowed", "")
		return
	}

	q := r.URL.Query()

	// Parse types filter
	var types []string
	if t := q.Get("type"); t != "" {
		types = strings.Split(t, ",")
	}

	// Parse pagination
	page, _ := strconv.Atoi(q.Get("page"))
	pageSize, _ := strconv.Atoi(q.Get("page_size"))

	req := domain.SearchRequest{
		Query:        q.Get("q"),
		CIDRContains: q.Get("cidr_contains"),
		CIDRWithin:   q.Get("cidr_within"),
		Types:        types,
		Page:         page,
		PageSize:     pageSize,
	}

	// Require at least one search criterion
	if req.Query == "" && req.CIDRContains == "" && req.CIDRWithin == "" {
		s.writeErr(r.Context(), w, http.StatusBadRequest, "at least one of q, cidr_contains, or cidr_within is required", "")
		return
	}

	resp, err := s.store.Search(r.Context(), req)
	if err != nil {
		// Check for validation errors (invalid CIDR input)
		if strings.Contains(err.Error(), "invalid cidr_contains") || strings.Contains(err.Error(), "invalid cidr_within") {
			s.writeErr(r.Context(), w, http.StatusBadRequest, err.Error(), "")
			return
		}
		s.writeErr(r.Context(), w, http.StatusInternalServerError, "search failed", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, resp)
}
