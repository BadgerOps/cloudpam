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
	"strconv"
	"strings"

	"cloudpam/internal/domain"
	"cloudpam/internal/storage"
)

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
	// Static index
	s.mux.HandleFunc("/", s.handleIndex)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	// Serve web/index.html from the repo directory.
	path := filepath.Join("web", "index.html")
	f, err := os.Open(path)
	if err != nil {
		http.Error(w, "index not found", http.StatusNotFound)
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
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// /api/v1/pools/{id}/blocks?new_prefix_len=24
func (s *Server) handlePoolsSubroutes(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/pools/")
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		http.NotFound(w, r)
		return
	}
	id64, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		http.Error(w, "invalid pool id", http.StatusBadRequest)
		return
	}
	if len(parts) >= 2 && parts[1] == "blocks" {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		s.blocksForPool(w, r, id64)
		return
	}
	http.NotFound(w, r)
}

func (s *Server) listPools(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	pools, err := s.store.ListPools(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(pools)
}

func (s *Server) createPool(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var in domain.CreatePool
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	in.CIDR = strings.TrimSpace(in.CIDR)
	if in.Name == "" || in.CIDR == "" {
		http.Error(w, "name and cidr are required", http.StatusBadRequest)
		return
	}
	// lightweight validation
	if !strings.Contains(in.CIDR, "/") {
		http.Error(w, "cidr must be in a.b.c.d/x form", http.StatusBadRequest)
		return
	}
	// If ParentID provided, ensure child CIDR is subset of parent CIDR (IPv4 only for now).
	if in.ParentID != nil {
		parent, ok, err := s.store.GetPool(ctx, *in.ParentID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if !ok {
			http.Error(w, "parent not found", http.StatusBadRequest)
			return
		}
		if err := validateChildCIDR(parent.CIDR, in.CIDR); err != nil {
			http.Error(w, fmt.Sprintf("invalid sub-pool cidr: %v", err), http.StatusBadRequest)
			return
		}
	}
	p, err := s.store.CreatePool(ctx, in)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(p)
}

type blockInfo struct {
	CIDR         string `json:"cidr"`
	PrefixLen    int    `json:"prefix_len"`
	Hosts        uint64 `json:"hosts"`
	Used         bool   `json:"used"`
	AssignedName string `json:"assigned_name,omitempty"`
}

func (s *Server) blocksForPool(w http.ResponseWriter, r *http.Request, id int64) {
	ctx := r.Context()
	pool, ok, err := s.store.GetPool(ctx, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !ok {
		http.Error(w, "pool not found", http.StatusNotFound)
		return
	}
	nplStr := r.URL.Query().Get("new_prefix_len")
	if nplStr == "" {
		http.Error(w, "new_prefix_len is required", http.StatusBadRequest)
		return
	}
	npl, err := strconv.Atoi(nplStr)
	if err != nil || npl <= 0 || npl > 32 {
		http.Error(w, "invalid new_prefix_len", http.StatusBadRequest)
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
			http.Error(w, "invalid page_size", http.StatusBadRequest)
			return
		}
		pageSize = ps
	}
	page := 1
	if pageStr != "" {
		p, err := strconv.Atoi(pageStr)
		if err != nil || p <= 0 {
			http.Error(w, "invalid page", http.StatusBadRequest)
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
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// Determine used blocks: exists child pool with exact CIDR match.
	all, err := s.store.ListPools(ctx)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	used := map[string]string{}
	for _, p := range all {
		if p.ParentID != nil && *p.ParentID == pool.ID {
			used[p.CIDR] = p.Name
		}
	}
	out := make([]blockInfo, 0, len(blocks))
	for _, b := range blocks {
		bi := blockInfo{CIDR: b, PrefixLen: npl, Hosts: hosts}
		if name, ok := used[b]; ok {
			bi.Used = true
			bi.AssignedName = name
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
	if cp.Bits() < pp.Bits() {
		return fmt.Errorf("child prefix len must be >= parent")
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
