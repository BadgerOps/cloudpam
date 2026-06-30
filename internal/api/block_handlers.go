package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/netip"
	"sort"
	"strconv"
	"strings"
	"time"

	"cloudpam/internal/domain"
)

type blockInfo struct {
	CIDR                string `json:"cidr"`
	PrefixLen           int    `json:"prefix_len"`
	Hosts               uint64 `json:"hosts"`
	Used                bool   `json:"used"`
	AssignedID          int64  `json:"assigned_id,omitempty"`
	AssignedName        string `json:"assigned_name,omitempty"`
	AssignedAccountID   int64  `json:"assigned_account_id,omitempty"`
	AssignedAccountName string `json:"assigned_account_name,omitempty"`
	ExistsElsewhere     bool   `json:"exists_elsewhere,omitempty"`
	ExistsElsewhereID   int64  `json:"exists_elsewhere_id,omitempty"`
	ExistsElsewhereName string `json:"exists_elsewhere_name,omitempty"`
}

type blockAccountMeta struct {
	Name        string
	Platform    string
	Tier        string
	Environment string
	Regions     []string
}

type blockAccountResolver struct {
	load func(context.Context) ([]domain.Account, error)

	loaded   bool
	accounts map[int64]blockAccountMeta
}

func newBlockAccountResolver(load func(context.Context) ([]domain.Account, error)) *blockAccountResolver {
	return &blockAccountResolver{load: load}
}

// metadata returns account metadata from a request-local cache. The resolver is
// created inside each blocks request and discarded when the response is written,
// so account creates, updates, and deletes are automatically visible on the next
// request without explicit cross-request invalidation.
func (r *blockAccountResolver) metadata(ctx context.Context, accountID int64) (blockAccountMeta, bool, error) {
	if err := r.ensureLoaded(ctx); err != nil {
		return blockAccountMeta{}, false, err
	}
	meta, ok := r.accounts[accountID]
	if ok {
		meta.Regions = append([]string(nil), meta.Regions...)
	}
	return meta, ok, nil
}

func (r *blockAccountResolver) ensureLoaded(ctx context.Context) error {
	if r.loaded {
		return nil
	}
	accs, err := r.load(ctx)
	if err != nil {
		return err
	}
	r.accounts = make(map[int64]blockAccountMeta, len(accs))
	for _, a := range accs {
		r.accounts[a.ID] = blockAccountMeta{
			Name:        a.Name,
			Platform:    a.Platform,
			Tier:        a.Tier,
			Environment: a.Environment,
			Regions:     append([]string(nil), a.Regions...),
		}
	}
	r.loaded = true
	return nil
}

// GET /api/v1/blocks?accounts=1,2&pools=10,11
// Returns all assigned blocks (sub-pools), optionally filtered by account IDs and parent pool IDs.
func (s *Server) handleBlocksList(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	ps, err := s.store.ListPools(ctx)
	if err != nil {
		s.writeErr(r.Context(), w, http.StatusInternalServerError, "internal error", err.Error())
		return
	}
	accountResolver := newBlockAccountResolver(s.store.ListAccounts)
	poolName := map[int64]string{}
	for _, p := range ps {
		poolName[p.ID] = p.Name
	}
	// Parse filters
	parseIDs := func(s string) map[int64]struct{} {
		set := map[int64]struct{}{}
		if s == "" {
			return set
		}
		for _, part := range strings.Split(s, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			if id, err := strconv.ParseInt(part, 10, 64); err == nil {
				set[id] = struct{}{}
			}
		}
		return set
	}
	accFilter := parseIDs(r.URL.Query().Get("accounts"))
	poolFilter := parseIDs(r.URL.Query().Get("pools"))
	// Pagination params
	pageSizeStr := r.URL.Query().Get("page_size")
	pageStr := r.URL.Query().Get("page")
	// Defaults
	pageSize := 50
	if strings.ToLower(pageSizeStr) == "all" {
		pageSize = 0
	} else if pageSizeStr != "" {
		psn, err := strconv.Atoi(pageSizeStr)
		if err != nil || psn < 0 {
			s.writeErr(r.Context(), w, http.StatusBadRequest, "invalid page_size", "")
			return
		}
		pageSize = psn
	}
	// Cap page size
	if pageSize > 0 && pageSize > 500 {
		pageSize = 500
	}
	page := 1
	if pageStr != "" {
		p, err := strconv.Atoi(pageStr)
		if err != nil || p <= 0 {
			s.writeErr(r.Context(), w, http.StatusBadRequest, "invalid page", "")
			return
		}
		page = p
	}
	// Collect assigned blocks (sub-pools)
	type row struct {
		ID                 int64     `json:"id"`
		Name               string    `json:"name"`
		CIDR               string    `json:"cidr"`
		ParentID           *int64    `json:"parent_id,omitempty"`
		ParentName         string    `json:"parent_name,omitempty"`
		AccountID          *int64    `json:"account_id,omitempty"`
		AccountName        string    `json:"account_name,omitempty"`
		AccountPlatform    string    `json:"account_platform,omitempty"`
		AccountTier        string    `json:"account_tier,omitempty"`
		AccountEnvironment string    `json:"account_environment,omitempty"`
		AccountRegions     []string  `json:"account_regions,omitempty"`
		Type               string    `json:"type"`
		Status             string    `json:"status"`
		Source             string    `json:"source"`
		CreatedAt          time.Time `json:"created_at"`
	}
	var items []row
	for _, p := range ps {
		if p.ParentID == nil && p.Type != "vpc" && p.Type != "subnet" {
			continue
		}
		// Filters
		if len(accFilter) > 0 {
			if p.AccountID == nil {
				continue
			}
			if _, ok := accFilter[*p.AccountID]; !ok {
				continue
			}
		}
		if len(poolFilter) > 0 {
			if p.ParentID == nil {
				continue
			}
			if _, ok := poolFilter[*p.ParentID]; !ok {
				continue
			}
		}
		r := row{
			ID:        p.ID,
			Name:      p.Name,
			CIDR:      p.CIDR,
			ParentID:  p.ParentID,
			AccountID: p.AccountID,
			Type:      string(p.Type),
			Status:    string(p.Status),
			Source:    string(p.Source),
			CreatedAt: p.CreatedAt,
		}
		if p.ParentID != nil {
			r.ParentName = poolName[*p.ParentID]
		}
		if p.AccountID != nil {
			m, ok, err := accountResolver.metadata(ctx, *p.AccountID)
			if err != nil {
				s.writeErr(ctx, w, http.StatusInternalServerError, "internal error", err.Error())
				return
			}
			if ok {
				r.AccountName = m.Name
				r.AccountPlatform = m.Platform
				r.AccountTier = m.Tier
				r.AccountEnvironment = m.Environment
				r.AccountRegions = m.Regions
			}
		}
		items = append(items, r)
	}
	// Sort by CreatedAt then ID for deterministic order
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].ID < items[j].ID
		}
		return items[i].CreatedAt.Before(items[j].CreatedAt)
	})
	total := len(items)
	// Paginate
	if pageSize > 0 {
		start := (page - 1) * pageSize
		if start > total {
			start = total
		}
		end := start + pageSize
		if end > total {
			end = total
		}
		items = items[start:end]
	}
	type resp struct {
		Items    []row `json:"items"`
		Total    int   `json:"total"`
		Page     int   `json:"page"`
		PageSize int   `json:"page_size"`
	}
	writeJSON(w, http.StatusOK, resp{Items: items, Total: total, Page: page, PageSize: pageSize})
}

func (s *Server) blocksForPool(w http.ResponseWriter, r *http.Request, id int64) {
	ctx := r.Context()
	pool, ok, err := s.store.GetPool(ctx, id)
	if err != nil {
		s.writeErr(r.Context(), w, http.StatusInternalServerError, "internal error", err.Error())
		return
	}
	if !ok {
		s.writeErr(r.Context(), w, http.StatusNotFound, "not found", "pool")
		return
	}
	nplStr := r.URL.Query().Get("new_prefix_len")
	if nplStr == "" {
		s.writeErr(r.Context(), w, http.StatusBadRequest, "new_prefix_len is required", "")
		return
	}
	npl, err := strconv.Atoi(nplStr)
	if err != nil || npl <= 0 || npl > 32 {
		s.writeErr(r.Context(), w, http.StatusBadRequest, "invalid new_prefix_len", "")
		return
	}
	// Pagination params
	pageSizeStr := r.URL.Query().Get("page_size")
	pageStr := r.URL.Query().Get("page")
	pageSize := 0 // 0 => all
	if strings.ToLower(pageSizeStr) == "all" {
		pageSize = 0
	} else if pageSizeStr != "" {
		ps, err := strconv.Atoi(pageSizeStr)
		if err != nil || ps < 0 {
			s.writeErr(r.Context(), w, http.StatusBadRequest, "invalid page_size", "")
			return
		}
		pageSize = ps
	}
	page := 1
	if pageStr != "" {
		p, err := strconv.Atoi(pageStr)
		if err != nil || p <= 0 {
			s.writeErr(r.Context(), w, http.StatusBadRequest, "invalid page", "")
			return
		}
		page = p
	}
	// Compute blocks (IPv4 only), returning a page window if requested.
	offset := 0
	limit := 0
	if pageSize > 0 {
		limit = pageSize
		offset = (page - 1) * pageSize
	}
	blocks, hosts, total, err := computeSubnetsIPv4Window(pool.CIDR, npl, offset, limit)
	if err != nil {
		s.writeErr(r.Context(), w, http.StatusBadRequest, err.Error(), "")
		return
	}
	// Determine used blocks: exists child pool with exact CIDR match.
	all, err := s.store.ListPools(ctx)
	if err != nil {
		s.writeErr(r.Context(), w, http.StatusInternalServerError, "internal error", err.Error())
		return
	}
	type usedInfo struct {
		id        int64
		name      string
		accountID *int64
	}
	used := map[string]usedInfo{}
	// Collect direct children of the current pool to evaluate overlaps
	type childPrefix struct {
		id        int64
		name      string
		pfx       netip.Prefix
		cidr      string
		accountID *int64
	}
	var children []childPrefix
	for _, p := range all {
		if p.ParentID != nil && *p.ParentID == pool.ID {
			used[p.CIDR] = usedInfo{id: p.ID, name: p.Name, accountID: p.AccountID}
			if pf, err := netip.ParsePrefix(p.CIDR); err == nil && pf.Addr().Is4() {
				children = append(children, childPrefix{id: p.ID, name: p.Name, pfx: pf, cidr: p.CIDR, accountID: p.AccountID})
			}
		}
	}
	accountResolver := newBlockAccountResolver(s.store.ListAccounts)
	out := make([]blockInfo, 0, len(blocks))
	for _, b := range blocks {
		bi := blockInfo{CIDR: b, PrefixLen: npl, Hosts: hosts}
		if info, ok := used[b]; ok {
			bi.Used = true
			bi.AssignedID = info.id
			bi.AssignedName = info.name
			if info.accountID != nil {
				bi.AssignedAccountID = *info.accountID
				meta, ok, err := accountResolver.metadata(ctx, *info.accountID)
				if err != nil {
					s.writeErr(r.Context(), w, http.StatusInternalServerError, "internal error", err.Error())
					return
				}
				if ok {
					bi.AssignedAccountName = meta.Name
				}
			}
		} else {
			// mark as unavailable if overlaps any existing direct child with a different CIDR
			bp, err := netip.ParsePrefix(b)
			if err == nil && bp.Addr().Is4() {
				for _, ch := range children {
					if ch.cidr == b {
						continue
					}
					if prefixesOverlapIPv4(ch.pfx, bp) {
						bi.ExistsElsewhere = true
						bi.ExistsElsewhereID = ch.id
						bi.ExistsElsewhereName = ch.name
						break
					}
				}
			}
		}
		out = append(out, bi)
	}
	type resp struct {
		Items    []blockInfo `json:"items"`
		Total    int         `json:"total"`
		Page     int         `json:"page"`
		PageSize int         `json:"page_size"`
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp{Items: out, Total: total, Page: page, PageSize: pageSize})
}
