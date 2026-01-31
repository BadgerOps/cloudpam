package http

import (
	"archive/zip"
	"context"
	"encoding/binary"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/getsentry/sentry-go"

	apidocs "cloudpam/docs"
	"cloudpam/internal/domain"
	"cloudpam/internal/storage"
	"cloudpam/internal/validation"
	webui "cloudpam/web"
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

type Server struct {
	mux    *http.ServeMux
	store  storage.Store
	logger *slog.Logger
}

func NewServer(mux *http.ServeMux, store storage.Store, logger *slog.Logger) *Server {
	return &Server{mux: mux, store: store, logger: logger}
}

func (s *Server) loggerOrDefault() *slog.Logger {
	if s.logger != nil {
		return s.logger
	}
	return slog.Default()
}

func valueOrNil[T any](ptr *T) any {
	if ptr == nil {
		return nil
	}
	return *ptr
}

func (s *Server) writeErr(ctx context.Context, w http.ResponseWriter, code int, msg string, detail string) {
	fields := []any{
		"status", code,
		"error", msg,
	}
	if detail != "" {
		fields = append(fields, "detail", detail)
	}
	fields = appendRequestID(ctx, fields)
	logger := s.loggerOrDefault()
	if code >= 500 {
		logger.ErrorContext(ctx, "request failed", fields...)
		sentry.CaptureMessage(fmt.Sprintf("HTTP %d: %s (detail: %s)", code, msg, detail))
	} else {
		logger.WarnContext(ctx, "request failed", fields...)
	}
	writeJSON(w, code, apiError{Error: msg, Detail: detail})
}

func (s *Server) RegisterRoutes() {
	s.mux.HandleFunc("/openapi.yaml", s.handleOpenAPISpec)
	s.mux.HandleFunc("/healthz", s.handleHealth)
	s.mux.HandleFunc("/readyz", s.handleReady)
	s.mux.HandleFunc("/api/v1/test-sentry", s.handleTestSentry)
	s.mux.HandleFunc("/api/v1/pools", s.handlePools)
	s.mux.HandleFunc("/api/v1/pools/", s.handlePoolsSubroutes)
	s.mux.HandleFunc("/api/v1/accounts", s.handleAccounts)
	s.mux.HandleFunc("/api/v1/accounts/", s.handleAccountsSubroutes)
	s.mux.HandleFunc("/api/v1/blocks", s.handleBlocksList)
	// Data export (CSV in ZIP)
	s.mux.HandleFunc("/api/v1/export", s.handleExport)
	// Static index
	s.mux.HandleFunc("/", s.handleIndex)
}

func (s *Server) handleOpenAPISpec(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeErr(r.Context(), w, http.StatusMethodNotAllowed, "method not allowed", "")
		return
	}
	w.Header().Set("Content-Type", "application/yaml")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(apidocs.OpenAPISpec)
}

// GET /api/v1/export?datasets=accounts,pools,blocks&accounts_fields=...&pools_fields=...&blocks_fields=...&accounts=1,2&pools=3,4
// Returns a ZIP archive containing separate CSV files per selected dataset.
func (s *Server) handleExport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeErr(r.Context(), w, http.StatusMethodNotAllowed, "method not allowed", "")
		return
	}
	ctx := r.Context()

	datasetsQ := strings.TrimSpace(r.URL.Query().Get("datasets"))
	if datasetsQ == "" {
		s.writeErr(r.Context(), w, http.StatusBadRequest, "datasets is required", "")
		return
	}
	want := map[string]bool{}
	for _, d := range strings.Split(datasetsQ, ",") {
		d = strings.TrimSpace(strings.ToLower(d))
		if d == "accounts" || d == "pools" || d == "blocks" {
			want[d] = true
		}
	}
	if len(want) == 0 {
		s.writeErr(r.Context(), w, http.StatusBadRequest, "no valid datasets requested", "")
		return
	}
	fields := []any{
		"datasets", datasetsQ,
		"accounts_fields", r.URL.Query().Get("accounts_fields"),
		"pools_fields", r.URL.Query().Get("pools_fields"),
		"blocks_fields", r.URL.Query().Get("blocks_fields"),
	}
	fields = appendRequestID(ctx, fields)
	s.loggerOrDefault().InfoContext(ctx, "export requested", fields...)

	// Helper to parse field lists with defaults
	parseFields := func(q, def string) []string {
		s := strings.TrimSpace(r.URL.Query().Get(q))
		if s == "" {
			if def == "" {
				return nil
			}
			s = def
		}
		out := []string{}
		for _, f := range strings.Split(s, ",") {
			f = strings.TrimSpace(f)
			if f != "" {
				out = append(out, f)
			}
		}
		return out
	}
	// Defaults
	accDefault := "id,key,name,provider,external_id,description,platform,tier,environment,regions,created_at"
	poolDefault := "id,name,cidr,parent_id,account_id,created_at"
	blkDefault := "id,name,cidr,parent_id,parent_name,account_id,account_name,account_platform,account_tier,account_environment,account_regions,created_at"

	accFields := parseFields("accounts_fields", accDefault)
	poolFields := parseFields("pools_fields", poolDefault)
	blkFields := parseFields("blocks_fields", blkDefault)

	// Preload data
	var (
		accounts []domain.Account
		pools    []domain.Pool
		err      error
	)
	if want["accounts"] || want["blocks"] {
		accounts, err = s.store.ListAccounts(ctx)
		if err != nil {
			s.writeErr(r.Context(), w, http.StatusInternalServerError, "internal error", err.Error())
			return
		}
		sort.Slice(accounts, func(i, j int) bool { return accounts[i].ID < accounts[j].ID })
	}
	if want["pools"] || want["blocks"] {
		pools, err = s.store.ListPools(ctx)
		if err != nil {
			s.writeErr(r.Context(), w, http.StatusInternalServerError, "internal error", err.Error())
			return
		}
		sort.Slice(pools, func(i, j int) bool { return pools[i].ID < pools[j].ID })
	}

	// Prepare ZIP writer
	w.Header().Set("Content-Type", "application/zip")
	ts := time.Now().UTC().Format("20060102-150405")
	w.Header().Set("Content-Disposition", "attachment; filename=cloudpam-export-"+ts+".zip")

	zw := zip.NewWriter(w)
	defer func() { _ = zw.Close() }()

	// CSV helper
	writeCSV := func(name string, header []string, rows [][]string) error {
		f, err := zw.Create(name)
		if err != nil {
			return err
		}
		cw := csv.NewWriter(f)
		if err := cw.Write(header); err != nil {
			return err
		}
		for _, r := range rows {
			if err := cw.Write(r); err != nil {
				return err
			}
		}
		cw.Flush()
		return cw.Error()
	}

	if want["accounts"] {
		// Build header and rows
		hdr := accFields
		rows := make([][]string, 0, len(accounts))
		for _, a := range accounts {
			row := make([]string, len(hdr))
			for i, col := range hdr {
				switch col {
				case "id":
					row[i] = strconv.FormatInt(a.ID, 10)
				case "key":
					row[i] = a.Key
				case "name":
					row[i] = a.Name
				case "provider":
					row[i] = a.Provider
				case "external_id":
					row[i] = a.ExternalID
				case "description":
					row[i] = a.Description
				case "platform":
					row[i] = a.Platform
				case "tier":
					row[i] = a.Tier
				case "environment":
					row[i] = a.Environment
				case "regions":
					row[i] = strings.Join(a.Regions, "|")
				case "created_at":
					row[i] = a.CreatedAt.UTC().Format(time.RFC3339)
				default:
					row[i] = ""
				}
			}
			rows = append(rows, row)
		}
		if err := writeCSV("accounts.csv", hdr, rows); err != nil {
			s.writeErr(r.Context(), w, http.StatusInternalServerError, "failed to write accounts.csv", err.Error())
			return
		}
	}

	if want["pools"] {
		hdr := poolFields
		rows := make([][]string, 0, len(pools))
		for _, p := range pools {
			row := make([]string, len(hdr))
			for i, col := range hdr {
				switch col {
				case "id":
					row[i] = strconv.FormatInt(p.ID, 10)
				case "name":
					row[i] = p.Name
				case "cidr":
					row[i] = p.CIDR
				case "parent_id":
					if p.ParentID != nil {
						row[i] = strconv.FormatInt(*p.ParentID, 10)
					} else {
						row[i] = ""
					}
				case "account_id":
					if p.AccountID != nil {
						row[i] = strconv.FormatInt(*p.AccountID, 10)
					} else {
						row[i] = ""
					}
				case "created_at":
					row[i] = p.CreatedAt.UTC().Format(time.RFC3339)
				default:
					row[i] = ""
				}
			}
			rows = append(rows, row)
		}
		if err := writeCSV("pools.csv", hdr, rows); err != nil {
			s.writeErr(r.Context(), w, http.StatusInternalServerError, "failed to write pools.csv", err.Error())
			return
		}
	}

	if want["blocks"] {
		// Reuse logic similar to handleBlocksList to assemble sub-pools
		accName := map[int64]string{}
		accMeta := map[int64]struct {
			Platform, Tier, Environment string
			Regions                     []string
		}{}
		for _, a := range accounts {
			accName[a.ID] = a.Name
			accMeta[a.ID] = struct {
				Platform, Tier, Environment string
				Regions                     []string
			}{a.Platform, a.Tier, a.Environment, a.Regions}
		}
		poolName := map[int64]string{}
		for _, p := range pools {
			poolName[p.ID] = p.Name
		}

		type row struct {
			ID                                                            int64
			Name, CIDR                                                    string
			ParentID                                                      int64
			ParentName                                                    string
			AccountID                                                     *int64
			AccountName, AccountPlatform, AccountTier, AccountEnvironment string
			AccountRegions                                                []string
			CreatedAt                                                     time.Time
		}
		// Optional filters via query to mirror /api/v1/blocks
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

		items := []row{}
		for _, p := range pools {
			if p.ParentID == nil {
				continue
			}
			if len(poolFilter) > 0 {
				if _, ok := poolFilter[*p.ParentID]; !ok {
					continue
				}
			}
			if len(accFilter) > 0 && p.AccountID != nil {
				if _, ok := accFilter[*p.AccountID]; !ok {
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
				r.AccountName = accName[*p.AccountID]
				meta := accMeta[*p.AccountID]
				r.AccountPlatform = meta.Platform
				r.AccountTier = meta.Tier
				r.AccountEnvironment = meta.Environment
				r.AccountRegions = meta.Regions
			}
			items = append(items, r)
		}
		sort.Slice(items, func(i, j int) bool {
			if items[i].CreatedAt.Equal(items[j].CreatedAt) {
				return items[i].ID < items[j].ID
			}
			return items[i].CreatedAt.Before(items[j].CreatedAt)
		})

		hdr := blkFields
		rows := make([][]string, 0, len(items))
		for _, it := range items {
			rowOut := make([]string, len(hdr))
			for i, col := range hdr {
				switch col {
				case "id":
					rowOut[i] = strconv.FormatInt(it.ID, 10)
				case "name":
					rowOut[i] = it.Name
				case "cidr":
					rowOut[i] = it.CIDR
				case "parent_id":
					rowOut[i] = strconv.FormatInt(it.ParentID, 10)
				case "parent_name":
					rowOut[i] = it.ParentName
				case "account_id":
					if it.AccountID != nil {
						rowOut[i] = strconv.FormatInt(*it.AccountID, 10)
					} else {
						rowOut[i] = ""
					}
				case "account_name":
					rowOut[i] = it.AccountName
				case "account_platform":
					rowOut[i] = it.AccountPlatform
				case "account_tier":
					rowOut[i] = it.AccountTier
				case "account_environment":
					rowOut[i] = it.AccountEnvironment
				case "account_regions":
					rowOut[i] = strings.Join(it.AccountRegions, "|")
				case "created_at":
					rowOut[i] = it.CreatedAt.UTC().Format(time.RFC3339)
				default:
					rowOut[i] = ""
				}
			}
			rows = append(rows, rowOut)
		}
		if err := writeCSV("blocks.csv", hdr, rows); err != nil {
			s.writeErr(r.Context(), w, http.StatusInternalServerError, "failed to write blocks.csv", err.Error())
			return
		}
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
			s.writeErr(r.Context(), w, http.StatusConflict, err.Error(), "")
			return
		}
		if !ok {
			s.writeErr(r.Context(), w, http.StatusNotFound, "not found", "")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		s.writeErr(r.Context(), w, http.StatusMethodNotAllowed, "method not allowed", "")
	}
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) { s.status = code; s.ResponseWriter.WriteHeader(code) }

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ReadinessResponse represents the JSON response for the readiness check endpoint.
type ReadinessResponse struct {
	Status string            `json:"status"`
	Checks map[string]string `json:"checks"`
}

// handleReady checks if the application is ready to accept traffic.
// Unlike /healthz (liveness), this endpoint verifies that dependencies are accessible.
// Returns 200 OK if all checks pass, 503 Service Unavailable otherwise.
func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeErr(r.Context(), w, http.StatusMethodNotAllowed, "method not allowed", "")
		return
	}

	ctx := r.Context()
	checks := make(map[string]string)
	status := "ok"

	// Database check: attempt to list pools to verify database connectivity
	_, err := s.store.ListPools(ctx)
	if err != nil {
		checks["database"] = "error"
		status = "unhealthy"
		s.loggerOrDefault().ErrorContext(ctx, "readiness check failed", appendRequestID(ctx, []any{
			"check", "database",
			"error", err.Error(),
		})...)
	} else {
		checks["database"] = "ok"
	}

	resp := ReadinessResponse{
		Status: status,
		Checks: checks,
	}

	if status == "ok" {
		writeJSON(w, http.StatusOK, resp)
	} else {
		writeJSON(w, http.StatusServiceUnavailable, resp)
	}
}

func (s *Server) handleTestSentry(w http.ResponseWriter, r *http.Request) {
	// Test endpoint to verify Sentry is working
	testType := r.URL.Query().Get("type")

	switch testType {
	case "message":
		// Test message capture
		sentry.CaptureMessage("Sentry test message from CloudPAM")
		sentry.Flush(2 * time.Second)
		writeJSON(w, http.StatusOK, map[string]string{"status": "message sent to Sentry"})
	case "error":
		// Test error capture with 500 status
		s.writeErr(r.Context(), w, http.StatusInternalServerError, "test error for Sentry", "this is a test error to verify Sentry integration")
	case "panic":
		// Test panic recovery
		panic("test panic for Sentry")
	default:
		writeJSON(w, http.StatusOK, map[string]string{
			"message": "Sentry test endpoint",
			"usage":   "?type=message|error|panic",
		})
	}
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		s.writeErr(r.Context(), w, http.StatusNotFound, "not found", "")
		return
	}
	// Serve embedded single‑page UI to ensure release binaries include the frontend.
	if len(webui.Index) == 0 {
		s.writeErr(r.Context(), w, http.StatusNotFound, "not found", "index")
		return
	}

	// Inject Sentry frontend DSN if configured
	html := string(webui.Index)
	if frontendDSN := os.Getenv("SENTRY_FRONTEND_DSN"); frontendDSN != "" {
		// Inject meta tag before the Sentry script so it's available when the script runs
		metaTag := fmt.Sprintf(`<meta name="sentry-dsn" content="%s">
    `, frontendDSN)
		html = strings.Replace(html, "<!-- Sentry Browser SDK -->", metaTag+"<!-- Sentry Browser SDK -->", 1)
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(html))
}

func (s *Server) handlePools(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.listPools(w, r)
	case http.MethodPost:
		s.createPool(w, r)
	default:
		w.Header().Set("Allow", strings.Join([]string{http.MethodGet, http.MethodPost}, ", "))
		s.writeErr(r.Context(), w, http.StatusMethodNotAllowed, "method not allowed", "")
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
		s.loggerOrDefault().WarnContext(ctx, "accounts:create invalid json", fields...)
		s.writeErr(r.Context(), w, http.StatusBadRequest, "invalid json", "")
		return
	}
	in.Key = strings.TrimSpace(in.Key)
	in.Name = strings.TrimSpace(in.Name)

	// Validate account key format
	if err := validation.ValidateAccountKey(in.Key); err != nil {
		fields := appendRequestID(ctx, []any{"key", in.Key, "reason", err.Error()})
		s.loggerOrDefault().WarnContext(ctx, "accounts:create invalid key", fields...)
		s.writeErr(r.Context(), w, http.StatusBadRequest, err.Error(), "")
		return
	}

	// Validate account name
	if err := validation.ValidateName(in.Name); err != nil {
		fields := appendRequestID(ctx, []any{"name", in.Name, "reason", err.Error()})
		s.loggerOrDefault().WarnContext(ctx, "accounts:create invalid name", fields...)
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
		s.loggerOrDefault().WarnContext(ctx, "accounts:create storage error", fields...)
		s.writeErr(r.Context(), w, http.StatusBadRequest, err.Error(), "")
		return
	}
	fields := appendRequestID(ctx, []any{
		"id", a.ID,
		"key", a.Key,
		"name", a.Name,
	})
	s.loggerOrDefault().InfoContext(ctx, "accounts:create success", fields...)
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
		s.writeErr(r.Context(), w, http.StatusInternalServerError, "internal error", err.Error())
		return
	}
	accs, err := s.store.ListAccounts(ctx)
	if err != nil {
		s.writeErr(r.Context(), w, http.StatusInternalServerError, "internal error", err.Error())
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
		s.writeErr(r.Context(), w, http.StatusNotFound, "not found", "")
		return
	}
	id64, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		s.writeErr(r.Context(), w, http.StatusBadRequest, "invalid pool id", "")
		return
	}
	if len(parts) >= 2 && parts[1] == "blocks" {
		if r.Method != http.MethodGet {
			s.writeErr(r.Context(), w, http.StatusMethodNotAllowed, "method not allowed", "")
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
			s.writeErr(r.Context(), w, http.StatusBadRequest, err.Error(), "")
			return
		}
		if !ok {
			s.writeErr(r.Context(), w, http.StatusNotFound, "not found", "")
			return
		}
		writeJSON(w, http.StatusOK, p)
	case http.MethodPatch:
		var payload struct {
			AccountID *int64  `json:"account_id"`
			Name      *string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			s.writeErr(r.Context(), w, http.StatusBadRequest, "invalid json", "")
			return
		}
		// Validate name if provided
		if payload.Name != nil {
			trimmed := strings.TrimSpace(*payload.Name)
			payload.Name = &trimmed
			if err := validation.ValidateName(*payload.Name); err != nil {
				s.writeErr(r.Context(), w, http.StatusBadRequest, err.Error(), "")
				return
			}
		}
		p, ok, err := s.store.UpdatePoolMeta(r.Context(), id64, payload.Name, payload.AccountID)
		if err != nil {
			s.writeErr(r.Context(), w, http.StatusBadRequest, err.Error(), "")
			return
		}
		if !ok {
			s.writeErr(r.Context(), w, http.StatusNotFound, "not found", "")
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
			s.writeErr(r.Context(), w, http.StatusConflict, err.Error(), "")
			return
		}
		if !ok {
			s.writeErr(r.Context(), w, http.StatusNotFound, "not found", "")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		s.writeErr(r.Context(), w, http.StatusMethodNotAllowed, "method not allowed", "")
	}
}

func (s *Server) listPools(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	pools, err := s.store.ListPools(ctx)
	if err != nil {
		s.writeErr(r.Context(), w, http.StatusInternalServerError, "internal error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, pools)
}

func (s *Server) createPool(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := s.loggerOrDefault()
	var in domain.CreatePool
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		logger.WarnContext(ctx, "pools:create invalid json", appendRequestID(ctx, []any{"reason", err.Error()})...)
		s.writeErr(r.Context(), w, http.StatusBadRequest, "invalid json", "")
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	in.CIDR = strings.TrimSpace(in.CIDR)

	// Validate pool name
	if err := validation.ValidateName(in.Name); err != nil {
		logger.WarnContext(ctx, "pools:create invalid name", appendRequestID(ctx, []any{
			"name", in.Name,
			"reason", err.Error(),
		})...)
		s.writeErr(r.Context(), w, http.StatusBadRequest, err.Error(), "")
		return
	}

	// Validate CIDR format, IPv4, reserved ranges, and prefix bounds
	if err := validation.ValidateCIDR(in.CIDR); err != nil {
		logger.WarnContext(ctx, "pools:create invalid cidr", appendRequestID(ctx, []any{
			"cidr", in.CIDR,
			"reason", err.Error(),
		})...)
		s.writeErr(r.Context(), w, http.StatusBadRequest, err.Error(), "")
		return
	}

	// If ParentID provided, ensure child CIDR is subset of parent CIDR (IPv4 only for now).
	if in.ParentID != nil {
		parent, ok, err := s.store.GetPool(ctx, *in.ParentID)
		if err != nil {
			s.writeErr(r.Context(), w, http.StatusInternalServerError, "internal error", err.Error())
			return
		}
		if !ok {
			logger.WarnContext(ctx, "pools:create parent not found", appendRequestID(ctx, []any{
				"parent_id", *in.ParentID,
				"cidr", in.CIDR,
			})...)
			s.writeErr(r.Context(), w, http.StatusBadRequest, "parent not found", "")
			return
		}
		if err := validateChildCIDR(parent.CIDR, in.CIDR); err != nil {
			logger.WarnContext(ctx, "pools:create invalid sub-pool cidr", appendRequestID(ctx, []any{
				"child_cidr", in.CIDR,
				"parent_cidr", parent.CIDR,
				"reason", err.Error(),
			})...)
			s.writeErr(r.Context(), w, http.StatusBadRequest, "invalid sub-pool cidr", err.Error())
			return
		}
	}

	// Overlap protection: disallow any overlapping CIDRs within the same parent scope
	// (i.e., among pools sharing the same parent_id, or among top-level pools).
	{
		pfxNew, _ := netip.ParsePrefix(in.CIDR)
		if !pfxNew.Addr().Is4() {
			s.writeErr(r.Context(), w, http.StatusBadRequest, "only ipv4 supported for now", "")
			return
		}
		all, err := s.store.ListPools(ctx)
		if err != nil {
			s.writeErr(r.Context(), w, http.StatusInternalServerError, "internal error", err.Error())
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
				logger.WarnContext(ctx, "pools:create cidr overlap", appendRequestID(ctx, []any{
					"candidate_cidr", in.CIDR,
					"existing_pool_id", p.ID,
					"existing_cidr", p.CIDR,
				})...)
				s.writeErr(r.Context(), w, http.StatusBadRequest, "cidr overlaps with existing block", fmt.Sprintf("conflicts with pool #%d (%s)", p.ID, p.CIDR))
				return
			}
		}
	}
	p, err := s.store.CreatePool(ctx, in)
	if err != nil {
		logger.WarnContext(ctx, "pools:create storage error", appendRequestID(ctx, []any{
			"name", in.Name,
			"cidr", in.CIDR,
			"parent_id", valueOrNil(in.ParentID),
			"account_id", valueOrNil(in.AccountID),
			"error", err.Error(),
		})...)
		s.writeErr(r.Context(), w, http.StatusBadRequest, err.Error(), "")
		return
	}
	logger.InfoContext(ctx, "pools:create success", appendRequestID(ctx, []any{
		"id", p.ID,
		"name", p.Name,
		"cidr", p.CIDR,
		"parent_id", valueOrNil(p.ParentID),
		"account_id", valueOrNil(p.AccountID),
	})...)
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
	// Account id -> name map
	accs, err := s.store.ListAccounts(ctx)
	if err != nil {
		s.writeErr(r.Context(), w, http.StatusInternalServerError, "internal error", err.Error())
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
