package http

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"cloudpam/internal/domain"
	"cloudpam/internal/storage"
)

type apiError struct {
	Error  string `json:"error"`
	Detail string `json:"detail,omitempty"`
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string, detail string) {
	if detail != "" {
		log.Printf("http %d error: %s detail=%s", code, msg, detail)
	} else {
		log.Printf("http %d error: %s", code, msg)
	}
	writeJSON(w, code, apiError{Error: msg, Detail: detail})
}

type Server struct {
	mux   *http.ServeMux
	store storage.Store
}

func NewServer(mux *http.ServeMux, store storage.Store) *Server {
	return &Server{mux: mux, store: store}
}

func (s *Server) RegisterRoutes() {
	s.mux.HandleFunc("/healthz", s.handleHealth)
	s.mux.HandleFunc("/api/v1/pools", s.handlePools)
	s.mux.HandleFunc("/api/v1/pools/", s.handlePoolsSubroutes)
	s.mux.HandleFunc("/api/v1/accounts", s.handleAccounts)
	s.mux.HandleFunc("/api/v1/accounts/", s.handleAccountsSubroutes)
	s.mux.HandleFunc("/api/v1/blocks", s.handleBlocksList)
	// Static index
	s.mux.HandleFunc("/", s.handleIndex)
}

// /api/v1/accounts/{id}
func (s *Server) handleAccountsSubroutes(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/accounts/")
	idStr := strings.Trim(path, "/")
	if idStr == "" {
		writeErr(w, http.StatusNotFound, "not found", "")
		return
	}
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid id", "")
		return
	}
	switch r.Method {
	case http.MethodGet:
		a, ok, err := s.store.GetAccount(r.Context(), id)
		if err != nil {
			writeErr(w, http.StatusBadRequest, err.Error(), "")
			return
		}
		if !ok {
			writeErr(w, http.StatusNotFound, "not found", "")
			return
		}
		writeJSON(w, http.StatusOK, a)
	case http.MethodPatch:
		var in domain.Account
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid json", "")
			return
		}
		a, ok, err := s.store.UpdateAccount(r.Context(), id, in)
		if err != nil {
			writeErr(w, http.StatusBadRequest, err.Error(), "")
			return
		}
		if !ok {
			writeErr(w, http.StatusNotFound, "not found", "")
			return
		}
		writeJSON(w, http.StatusOK, a)
	case http.MethodDelete:
		var ok bool
		force := strings.ToLower(r.URL.Query().Get("force"))
		if force == "1" || force == "true" || force == "yes" {
			ok, err = s.store.DeleteAccountCascade(r.Context(), id)
		} else {
			ok, err = s.store.DeleteAccount(r.Context(), id)
		}
		if err != nil {
			writeErr(w, http.StatusConflict, err.Error(), "")
			return
		}
		if !ok {
			writeErr(w, http.StatusNotFound, "not found", "")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed", "")
	}
}

// LoggingMiddleware wraps an http.Handler to log basic request info.
func LoggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sr := &statusRecorder{ResponseWriter: w, status: 200}
		next.ServeHTTP(sr, r)
		log.Printf("%s %s -> %d in %s", r.Method, r.URL.Path, sr.status, time.Since(start))
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) { s.status = code; s.ResponseWriter.WriteHeader(code) }

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		writeErr(w, http.StatusNotFound, "not found", "")
		return
	}
	// Serve web/index.html from the repo directory.
	path := filepath.Join("web", "index.html")
	f, err := os.Open(path)
	if err != nil {
		writeErr(w, http.StatusNotFound, "not found", "index")
		return
	}
	defer func() { _ = f.Close() }()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if _, err := io.Copy(w, f); err != nil {
		log.Printf("serve index error: %v", err)
	}
}

func (s *Server) handlePools(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.listPools(w, r)
	case http.MethodPost:
		s.createPool(w, r)
	default:
		w.Header().Set("Allow", strings.Join([]string{http.MethodGet, http.MethodPost}, ", "))
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed", "")
	}
}

// RESTful: PATCH /api/v1/pools/{id} with {"name": "...", "account_id": <int|null>}
// Legacy query-param endpoints are not supported; use /api/v1/pools/{id}.

// Accounts: GET list, POST create
func (s *Server) handleAccounts(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.listAccounts(w, r)
	case http.MethodPost:
		s.createAccount(w, r)
	default:
		w.Header().Set("Allow", strings.Join([]string{http.MethodGet, http.MethodPost}, ", "))
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed", "")
	}
}

func (s *Server) listAccounts(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	accs, err := s.store.ListAccounts(ctx)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error", err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(accs)
}

func (s *Server) createAccount(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var in domain.CreateAccount
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json", "")
		return
	}
	in.Key = strings.TrimSpace(in.Key)
	in.Name = strings.TrimSpace(in.Name)
	if in.Key == "" || in.Name == "" {
		writeErr(w, http.StatusBadRequest, "key and name are required", "")
		return
	}
	a, err := s.store.CreateAccount(ctx, in)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error(), "")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(a)
}

// Legacy account query-param endpoints are not supported; use /api/v1/accounts/{id}.

// GET /api/v1/blocks?accounts=1,2&pools=10,11
// Returns all assigned blocks (sub-pools), optionally filtered by account IDs and parent pool IDs.
func (s *Server) handleBlocksList(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	ps, err := s.store.ListPools(ctx)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error", err.Error())
		return
	}
	accs, err := s.store.ListAccounts(ctx)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error", err.Error())
		return
	}
	// Build lookups
	accName := map[int64]string{}
	accMeta := map[int64]struct {
		Platform, Tier, Environment string
		Regions                     []string
	}{}
	for _, a := range accs {
		accName[a.ID] = a.Name
		accMeta[a.ID] = struct {
			Platform, Tier, Environment string
			Regions                     []string
		}{Platform: a.Platform, Tier: a.Tier, Environment: a.Environment, Regions: a.Regions}
	}
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
			writeErr(w, http.StatusBadRequest, "invalid page_size", "")
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
			writeErr(w, http.StatusBadRequest, "invalid page", "")
			return
		}
		page = p
	}
	// Collect assigned blocks (sub-pools)
	type row struct {
		ID                 int64     `json:"id"`
		Name               string    `json:"name"`
		CIDR               string    `json:"cidr"`
		ParentID           int64     `json:"parent_id"`
		ParentName         string    `json:"parent_name"`
		AccountID          *int64    `json:"account_id,omitempty"`
		AccountName        string    `json:"account_name,omitempty"`
		AccountPlatform    string    `json:"account_platform,omitempty"`
		AccountTier        string    `json:"account_tier,omitempty"`
		AccountEnvironment string    `json:"account_environment,omitempty"`
		AccountRegions     []string  `json:"account_regions,omitempty"`
		CreatedAt          time.Time `json:"created_at"`
	}
	var items []row
	for _, p := range ps {
		if p.ParentID == nil {
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
			if _, ok := poolFilter[*p.ParentID]; !ok {
				continue
			}
		}
		r := row{
			ID:         p.ID,
			Name:       p.Name,
			CIDR:       p.CIDR,
			ParentID:   *p.ParentID,
			ParentName: poolName[*p.ParentID],
			AccountID:  p.AccountID,
			CreatedAt:  p.CreatedAt,
		}
		if p.AccountID != nil {
			if n, ok := accName[*p.AccountID]; ok {
				r.AccountName = n
			}
			if m, ok := accMeta[*p.AccountID]; ok {
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

// /api/v1/pools/{id}/blocks?new_prefix_len=24
func (s *Server) handlePoolsSubroutes(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/pools/")
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		writeErr(w, http.StatusNotFound, "not found", "")
		return
	}
	id64, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid pool id", "")
		return
	}
	if len(parts) >= 2 && parts[1] == "blocks" {
		if r.Method != http.MethodGet {
			writeErr(w, http.StatusMethodNotAllowed, "method not allowed", "")
			return
		}
		s.blocksForPool(w, r, id64)
		return
	}
	// Pool detail
	switch r.Method {
	case http.MethodGet:
		p, ok, err := s.store.GetPool(r.Context(), id64)
		if err != nil {
			writeErr(w, http.StatusBadRequest, err.Error(), "")
			return
		}
		if !ok {
			writeErr(w, http.StatusNotFound, "not found", "")
			return
		}
		writeJSON(w, http.StatusOK, p)
	case http.MethodPatch:
		var payload struct {
			AccountID *int64  `json:"account_id"`
			Name      *string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid json", "")
			return
		}
		p, ok, err := s.store.UpdatePoolMeta(r.Context(), id64, payload.Name, payload.AccountID)
		if err != nil {
			writeErr(w, http.StatusBadRequest, err.Error(), "")
			return
		}
		if !ok {
			writeErr(w, http.StatusNotFound, "not found", "")
			return
		}
		writeJSON(w, http.StatusOK, p)
	case http.MethodDelete:
		var ok bool
		force := strings.ToLower(r.URL.Query().Get("force"))
		if force == "1" || force == "true" || force == "yes" {
			ok, err = s.store.DeletePoolCascade(r.Context(), id64)
		} else {
			ok, err = s.store.DeletePool(r.Context(), id64)
		}
		if err != nil {
			writeErr(w, http.StatusConflict, err.Error(), "")
			return
		}
		if !ok {
			writeErr(w, http.StatusNotFound, "not found", "")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed", "")
	}
}

func (s *Server) listPools(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	pools, err := s.store.ListPools(ctx)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, pools)
}

func (s *Server) createPool(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var in domain.CreatePool
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json", "")
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	in.CIDR = strings.TrimSpace(in.CIDR)
	if in.Name == "" || in.CIDR == "" {
		writeErr(w, http.StatusBadRequest, "name and cidr are required", "")
		return
	}
	// Validate CIDR format and IPv4
	if !strings.Contains(in.CIDR, "/") {
		writeErr(w, http.StatusBadRequest, "cidr must be in a.b.c.d/x form", "")
		return
	}
	if pfx, err := netip.ParsePrefix(in.CIDR); err != nil || !pfx.Addr().Is4() {
		writeErr(w, http.StatusBadRequest, "invalid cidr", "")
		return
	}
	// If ParentID provided, ensure child CIDR is subset of parent CIDR (IPv4 only for now).
	if in.ParentID != nil {
		parent, ok, err := s.store.GetPool(ctx, *in.ParentID)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "internal error", err.Error())
			return
		}
		if !ok {
			writeErr(w, http.StatusBadRequest, "parent not found", "")
			return
		}
		if err := validateChildCIDR(parent.CIDR, in.CIDR); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid sub-pool cidr", err.Error())
			return
		}
	}

	// Overlap protection: disallow any overlapping CIDRs within the same parent scope
	// (i.e., among pools sharing the same parent_id, or among top-level pools).
	{
		pfxNew, _ := netip.ParsePrefix(in.CIDR)
		if !pfxNew.Addr().Is4() {
			writeErr(w, http.StatusBadRequest, "only ipv4 supported for now", "")
			return
		}
		all, err := s.store.ListPools(ctx)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, "internal error", err.Error())
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
				writeErr(w, http.StatusBadRequest, "cidr overlaps with existing block", fmt.Sprintf("conflicts with pool #%d (%s)", p.ID, p.CIDR))
				return
			}
		}
	}
	p, err := s.store.CreatePool(ctx, in)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error(), "")
		return
	}
	writeJSON(w, http.StatusCreated, p)
}

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

func (s *Server) blocksForPool(w http.ResponseWriter, r *http.Request, id int64) {
	ctx := r.Context()
	pool, ok, err := s.store.GetPool(ctx, id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error", err.Error())
		return
	}
	if !ok {
		writeErr(w, http.StatusNotFound, "not found", "pool")
		return
	}
	nplStr := r.URL.Query().Get("new_prefix_len")
	if nplStr == "" {
		writeErr(w, http.StatusBadRequest, "new_prefix_len is required", "")
		return
	}
	npl, err := strconv.Atoi(nplStr)
	if err != nil || npl <= 0 || npl > 32 {
		writeErr(w, http.StatusBadRequest, "invalid new_prefix_len", "")
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
			writeErr(w, http.StatusBadRequest, "invalid page_size", "")
			return
		}
		pageSize = ps
	}
	page := 1
	if pageStr != "" {
		p, err := strconv.Atoi(pageStr)
		if err != nil || p <= 0 {
			writeErr(w, http.StatusBadRequest, "invalid page", "")
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
		writeErr(w, http.StatusBadRequest, err.Error(), "")
		return
	}
	// Determine used blocks: exists child pool with exact CIDR match.
	all, err := s.store.ListPools(ctx)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error", err.Error())
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
	// Account id -> name map
	accs, err := s.store.ListAccounts(ctx)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal error", err.Error())
		return
	}
	accName := map[int64]string{}
	for _, a := range accs {
		accName[a.ID] = a.Name
	}
	out := make([]blockInfo, 0, len(blocks))
	for _, b := range blocks {
		bi := blockInfo{CIDR: b, PrefixLen: npl, Hosts: hosts}
		if info, ok := used[b]; ok {
			bi.Used = true
			bi.AssignedID = info.id
			bi.AssignedName = info.name
			if info.accountID != nil {
				bi.AssignedAccountID = *info.accountID
				if n, ok := accName[*info.accountID]; ok {
					bi.AssignedAccountName = n
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

func validateChildCIDR(parentCIDR, childCIDR string) error {
	pp, err := netip.ParsePrefix(parentCIDR)
	if err != nil {
		return fmt.Errorf("invalid parent: %w", err)
	}
	cp, err := netip.ParsePrefix(childCIDR)
	if err != nil {
		return fmt.Errorf("invalid child: %w", err)
	}
	if !pp.Addr().Is4() || !cp.Addr().Is4() {
		return fmt.Errorf("only ipv4 supported")
	}
	if cp.Bits() <= pp.Bits() {
		return fmt.Errorf("child prefix len must be greater than parent")
	}
	// Check both start and end addresses within parent.
	cstart := cp.Masked().Addr()
	cend, err := lastAddr(cp)
	if err != nil {
		return err
	}
	if !pp.Contains(cstart) || !pp.Contains(cend) {
		return fmt.Errorf("child not within parent")
	}
	return nil
}

// prefixesOverlapIPv4 returns true if two IPv4 prefixes overlap in address space.
func prefixesOverlapIPv4(a, b netip.Prefix) bool {
	if !a.Addr().Is4() || !b.Addr().Is4() {
		return false
	}
	aStart := ipv4ToUint32(a.Masked().Addr())
	aEnd, _ := lastAddr(a)
	bStart := ipv4ToUint32(b.Masked().Addr())
	bEnd, _ := lastAddr(b)
	aEndU := ipv4ToUint32(aEnd)
	bEndU := ipv4ToUint32(bEnd)
	// Overlap if intervals intersect
	return aEndU >= bStart && bEndU >= aStart
}

func computeSubnetsIPv4Window(parentCIDR string, newPrefixLen int, offset, limit int) ([]string, uint64, int, error) {
	pp, err := netip.ParsePrefix(parentCIDR)
	if err != nil {
		return nil, 0, 0, fmt.Errorf("invalid parent cidr: %w", err)
	}
	if !pp.Addr().Is4() {
		return nil, 0, 0, fmt.Errorf("only ipv4 supported")
	}
	if newPrefixLen < pp.Bits() || newPrefixLen > 32 {
		return nil, 0, 0, fmt.Errorf("new_prefix_len must be between %d and 32", pp.Bits())
	}
	// number of blocks = 2^(new - old)
	count := 1 << (newPrefixLen - pp.Bits())
	base := ipv4ToUint32(pp.Masked().Addr())
	step := uint32(1) << uint32(32-newPrefixLen)
	start := 0
	end := count
	if limit > 0 {
		if offset < 0 {
			offset = 0
		}
		if offset > count {
			offset = count
		}
		start = offset
		end = offset + limit
		if end > count {
			end = count
		}
	}
	res := make([]string, 0, end-start)
	for i := start; i < end; i++ {
		addr := uint32ToIPv4(base + uint32(i)*step)
		res = append(res, netip.PrefixFrom(addr, newPrefixLen).String())
	}
	hosts := usableHostsIPv4(newPrefixLen)
	return res, hosts, count, nil
}

func lastAddr(p netip.Prefix) (netip.Addr, error) {
	if !p.Addr().Is4() {
		return netip.Addr{}, fmt.Errorf("only ipv4 supported")
	}
	base := ipv4ToUint32(p.Masked().Addr())
	size := uint32(1) << uint32(32-p.Bits())
	return uint32ToIPv4(base + size - 1), nil
}

func ipv4ToUint32(a netip.Addr) uint32 {
	b := a.As4()
	return binary.BigEndian.Uint32(b[:])
}

func uint32ToIPv4(u uint32) netip.Addr {
	var b [4]byte
	binary.BigEndian.PutUint32(b[:], u)
	ip := net.IPv4(b[0], b[1], b[2], b[3])
	addr, _ := netip.ParseAddr(ip.String())
	return addr
}

func usableHostsIPv4(prefixLen int) uint64 {
	if prefixLen < 0 || prefixLen > 32 {
		return 0
	}
	total := uint64(1) << uint64(32-prefixLen)
	if prefixLen <= 30 {
		if total >= 2 {
			return total - 2 // exclude network and broadcast
		}
	}
	return total
}
