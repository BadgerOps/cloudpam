package http

import (
	"archive/zip"
	"encoding/csv"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"cloudpam/internal/domain"
)

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
	s.logger.InfoContext(ctx, "export requested", fields...)

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

// POST /api/v1/import/accounts
// Accepts CSV data with columns: key,name,provider,external_id,description,platform,tier,environment,regions
func (s *Server) handleImportAccounts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeErr(r.Context(), w, http.StatusMethodNotAllowed, "method not allowed", "")
		return
	}

	reader := csv.NewReader(r.Body)
	records, err := reader.ReadAll()
	if err != nil {
		s.writeErr(r.Context(), w, http.StatusBadRequest, "invalid csv", err.Error())
		return
	}

	if len(records) < 2 {
		s.writeErr(r.Context(), w, http.StatusBadRequest, "csv must have header and at least one data row", "")
		return
	}

	// Parse header to find column indices
	header := records[0]
	colIdx := make(map[string]int)
	for i, h := range header {
		colIdx[strings.ToLower(strings.TrimSpace(h))] = i
	}

	// Required columns
	keyIdx, hasKey := colIdx["key"]
	nameIdx, hasName := colIdx["name"]
	if !hasKey || !hasName {
		s.writeErr(r.Context(), w, http.StatusBadRequest, "csv must have 'key' and 'name' columns", "")
		return
	}

	var created, skipped int
	var errors []string

	for i, row := range records[1:] {
		if len(row) <= keyIdx || len(row) <= nameIdx {
			errors = append(errors, fmt.Sprintf("row %d: insufficient columns", i+2))
			continue
		}

		key := strings.TrimSpace(row[keyIdx])
		name := strings.TrimSpace(row[nameIdx])
		if key == "" || name == "" {
			errors = append(errors, fmt.Sprintf("row %d: key and name required", i+2))
			continue
		}

		acc := domain.CreateAccount{
			Key:  key,
			Name: name,
		}

		// Optional columns
		if idx, ok := colIdx["provider"]; ok && idx < len(row) {
			acc.Provider = strings.TrimSpace(row[idx])
		}
		if idx, ok := colIdx["external_id"]; ok && idx < len(row) {
			acc.ExternalID = strings.TrimSpace(row[idx])
		}
		if idx, ok := colIdx["description"]; ok && idx < len(row) {
			acc.Description = strings.TrimSpace(row[idx])
		}
		if idx, ok := colIdx["platform"]; ok && idx < len(row) {
			acc.Platform = strings.TrimSpace(row[idx])
		}
		if idx, ok := colIdx["tier"]; ok && idx < len(row) {
			acc.Tier = strings.TrimSpace(row[idx])
		}
		if idx, ok := colIdx["environment"]; ok && idx < len(row) {
			acc.Environment = strings.TrimSpace(row[idx])
		}
		if idx, ok := colIdx["regions"]; ok && idx < len(row) {
			regionsStr := strings.TrimSpace(row[idx])
			if regionsStr != "" {
				acc.Regions = strings.Split(regionsStr, ";")
			}
		}

		_, err := s.store.CreateAccount(r.Context(), acc)
		if err != nil {
			if strings.Contains(err.Error(), "duplicate") || strings.Contains(err.Error(), "UNIQUE") {
				skipped++
			} else {
				errors = append(errors, fmt.Sprintf("row %d: %v", i+2, err))
			}
			continue
		}
		created++
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"created": created,
		"skipped": skipped,
		"errors":  errors,
	})
}

// POST /api/v1/import/pools
// Accepts CSV data with columns: name,cidr,parent_id,account_id,type,status,source,description
func (s *Server) handleImportPools(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeErr(r.Context(), w, http.StatusMethodNotAllowed, "method not allowed", "")
		return
	}

	reader := csv.NewReader(r.Body)
	records, err := reader.ReadAll()
	if err != nil {
		s.writeErr(r.Context(), w, http.StatusBadRequest, "invalid csv", err.Error())
		return
	}

	if len(records) < 2 {
		s.writeErr(r.Context(), w, http.StatusBadRequest, "csv must have header and at least one data row", "")
		return
	}

	// Parse header to find column indices
	header := records[0]
	colIdx := make(map[string]int)
	for i, h := range header {
		colIdx[strings.ToLower(strings.TrimSpace(h))] = i
	}

	// Required columns
	nameIdx, hasName := colIdx["name"]
	cidrIdx, hasCIDR := colIdx["cidr"]
	if !hasName || !hasCIDR {
		s.writeErr(r.Context(), w, http.StatusBadRequest, "csv must have 'name' and 'cidr' columns", "")
		return
	}

	// Build account lookup map (key -> ID) for resolving account references
	accounts, _ := s.store.ListAccounts(r.Context())
	accountKeyToID := make(map[string]int64)
	for _, a := range accounts {
		accountKeyToID[a.Key] = a.ID
	}

	s.logger.InfoContext(r.Context(), "pools:import starting",
		"total_rows", len(records)-1,
		"known_accounts", len(accountKeyToID))

	// Parse all rows first to enable hierarchical import
	type poolRow struct {
		rowNum       int
		name         string
		cidr         string
		oldID        int64  // original ID from CSV (if present)
		oldParentID  int64  // original parent_id from CSV
		oldAccountID int64  // original account_id from CSV
		accountKey   string // account_key for lookup
		poolType     domain.PoolType
		status       domain.PoolStatus
		source       domain.PoolSource
		description  string
	}

	var rows []poolRow
	idIdx, hasID := colIdx["id"]

	for i, row := range records[1:] {
		if len(row) <= nameIdx || len(row) <= cidrIdx {
			continue
		}

		name := strings.TrimSpace(row[nameIdx])
		cidr := strings.TrimSpace(row[cidrIdx])
		if name == "" || cidr == "" {
			continue
		}

		pr := poolRow{
			rowNum: i + 2,
			name:   name,
			cidr:   cidr,
		}

		// Get original ID if present
		if hasID && idIdx < len(row) {
			if v := strings.TrimSpace(row[idIdx]); v != "" {
				pr.oldID, _ = strconv.ParseInt(v, 10, 64)
			}
		}

		// Get parent_id
		if idx, ok := colIdx["parent_id"]; ok && idx < len(row) {
			if v := strings.TrimSpace(row[idx]); v != "" {
				pr.oldParentID, _ = strconv.ParseInt(v, 10, 64)
			}
		}

		// Get account_id (we'll resolve this later)
		if idx, ok := colIdx["account_id"]; ok && idx < len(row) {
			if v := strings.TrimSpace(row[idx]); v != "" {
				pr.oldAccountID, _ = strconv.ParseInt(v, 10, 64)
			}
		}

		// Get account_key for direct lookup (preferred over account_id)
		if idx, ok := colIdx["account_key"]; ok && idx < len(row) {
			pr.accountKey = strings.TrimSpace(row[idx])
		}

		if idx, ok := colIdx["type"]; ok && idx < len(row) {
			pr.poolType = domain.PoolType(strings.TrimSpace(row[idx]))
		}
		if idx, ok := colIdx["status"]; ok && idx < len(row) {
			pr.status = domain.PoolStatus(strings.TrimSpace(row[idx]))
		}
		if idx, ok := colIdx["source"]; ok && idx < len(row) {
			pr.source = domain.PoolSource(strings.TrimSpace(row[idx]))
		}
		if idx, ok := colIdx["description"]; ok && idx < len(row) {
			pr.description = strings.TrimSpace(row[idx])
		}

		rows = append(rows, pr)
	}

	// Sort rows: no parent first, then by oldParentID to process parents before children
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].oldParentID == 0 && rows[j].oldParentID != 0 {
			return true
		}
		if rows[i].oldParentID != 0 && rows[j].oldParentID == 0 {
			return false
		}
		return rows[i].oldParentID < rows[j].oldParentID
	})

	var created, skipped int
	var errors []string

	// Map old IDs to new IDs for parent resolution
	oldToNewID := make(map[int64]int64)

	// Also build a CIDR to new ID map for fallback parent resolution
	cidrToNewID := make(map[string]int64)

	for _, pr := range rows {
		pool := domain.CreatePool{
			Name:        pr.name,
			CIDR:        pr.cidr,
			Type:        pr.poolType,
			Status:      pr.status,
			Source:      pr.source,
			Description: pr.description,
		}

		// Resolve account_id: prefer account_key lookup, fall back to direct ID
		if pr.accountKey != "" {
			if accountID, ok := accountKeyToID[pr.accountKey]; ok {
				pool.AccountID = &accountID
				s.logger.DebugContext(r.Context(), "pools:import resolved account by key",
					"row", pr.rowNum, "account_key", pr.accountKey, "account_id", accountID)
			} else {
				s.logger.WarnContext(r.Context(), "pools:import account key not found",
					"row", pr.rowNum, "account_key", pr.accountKey)
				errors = append(errors, fmt.Sprintf("row %d: account_key '%s' not found", pr.rowNum, pr.accountKey))
				continue
			}
		} else if pr.oldAccountID != 0 {
			// Try to find account by checking if this ID exists
			if _, ok, _ := s.store.GetAccount(r.Context(), pr.oldAccountID); ok {
				pool.AccountID = &pr.oldAccountID
			} else {
				// Account ID doesn't exist - this is a stale reference from exported data
				s.logger.WarnContext(r.Context(), "pools:import account_id not found (stale reference)",
					"row", pr.rowNum, "account_id", pr.oldAccountID,
					"hint", "use account_key column instead of account_id for reliable imports")
				errors = append(errors, fmt.Sprintf("row %d: account_id %d not found (use account_key column for imports)", pr.rowNum, pr.oldAccountID))
				continue
			}
		}

		// Resolve parent_id using old-to-new mapping
		if pr.oldParentID != 0 {
			if newParentID, ok := oldToNewID[pr.oldParentID]; ok {
				pool.ParentID = &newParentID
				s.logger.DebugContext(r.Context(), "pools:import resolved parent",
					"row", pr.rowNum, "old_parent_id", pr.oldParentID, "new_parent_id", newParentID)
			} else {
				// Check if parent exists directly (for imports into existing data)
				if _, ok, _ := s.store.GetPool(r.Context(), pr.oldParentID); ok {
					pool.ParentID = &pr.oldParentID
				} else {
					s.logger.WarnContext(r.Context(), "pools:import parent_id not found",
						"row", pr.rowNum, "parent_id", pr.oldParentID)
					errors = append(errors, fmt.Sprintf("row %d: parent_id %d not found", pr.rowNum, pr.oldParentID))
					continue
				}
			}
		}

		// Set defaults
		if pool.Type == "" {
			pool.Type = domain.PoolTypeSubnet
		}
		if pool.Status == "" {
			pool.Status = domain.PoolStatusActive
		}
		if pool.Source == "" {
			pool.Source = domain.PoolSourceManual
		}

		s.logger.DebugContext(r.Context(), "pools:import creating pool",
			"row", pr.rowNum, "name", pool.Name, "cidr", pool.CIDR,
			"parent_id", valueOrNil(pool.ParentID), "account_id", valueOrNil(pool.AccountID))

		createdPool, err := s.store.CreatePool(r.Context(), pool)
		if err != nil {
			s.logger.WarnContext(r.Context(), "pools:import create failed",
				"row", pr.rowNum, "name", pool.Name, "error", err.Error())
			if strings.Contains(err.Error(), "duplicate") || strings.Contains(err.Error(), "UNIQUE") {
				skipped++
			} else {
				errors = append(errors, fmt.Sprintf("row %d: %v", pr.rowNum, err))
			}
			continue
		}

		// Record the ID mapping for child pool resolution
		if pr.oldID != 0 {
			oldToNewID[pr.oldID] = createdPool.ID
		}
		cidrToNewID[pr.cidr] = createdPool.ID

		s.logger.InfoContext(r.Context(), "pools:import created pool",
			"row", pr.rowNum, "id", createdPool.ID, "name", createdPool.Name)
		created++
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"created": created,
		"skipped": skipped,
		"errors":  errors,
	})
}
