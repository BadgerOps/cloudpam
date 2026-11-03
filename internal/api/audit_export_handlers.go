package api

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"cloudpam/internal/audit"
)

// auditExportPageSize is the largest page every audit backend serves, so the
// export walks the log in pages of this size.
const auditExportPageSize = 1000

// auditFilters reads the filter query parameters shared by the audit list and
// export endpoints. Pagination is left to the caller.
func auditFilters(q url.Values) audit.ListOptions {
	opts := audit.ListOptions{
		Actor:        q.Get("actor"),
		Action:       q.Get("action"),
		ResourceType: q.Get("resource_type"),
	}
	if t, err := time.Parse(time.RFC3339, q.Get("since")); err == nil {
		opts.Since = &t
	}
	if t, err := time.Parse(time.RFC3339, q.Get("until")); err == nil {
		opts.Until = &t
	}
	return opts
}

// auditExporter streams audit events in one output format.
type auditExporter interface {
	contentType() string
	extension() string
	begin(w io.Writer) error
	write(e *audit.AuditEvent) error
	end(total int) error
}

// auditExporters maps the format query parameter to its exporter.
var auditExporters = map[string]func() auditExporter{
	"csv":  func() auditExporter { return &auditCSVExporter{} },
	"json": func() auditExporter { return &auditJSONExporter{} },
}

// handleAuditExport handles GET /api/v1/audit/export?format=csv|json. It
// honours the list endpoint's filters and streams every matching event.
func (as *AuthServer) handleAuditExport(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if r.Method != http.MethodGet {
		as.writeErr(ctx, w, http.StatusMethodNotAllowed, "method not allowed", "")
		return
	}
	q := r.URL.Query()
	format := q.Get("format")
	if format == "" {
		format = "csv"
	}
	newExporter, ok := auditExporters[format]
	if !ok {
		as.writeErr(ctx, w, http.StatusBadRequest, "invalid format", "must be csv or json")
		return
	}

	opts := auditFilters(q)
	now := time.Now().UTC()
	if opts.Until == nil {
		// Pin the window so events logged during the export do not shift
		// later pages and duplicate rows.
		opts.Until = &now
	}
	opts.Limit = auditExportPageSize

	// The first page is fetched before any header goes out so a failing
	// backend still yields a proper error response.
	events, total, err := as.auditLogger.List(ctx, opts)
	if err != nil {
		as.writeErr(ctx, w, http.StatusInternalServerError, "failed to list audit events", err.Error())
		return
	}

	exporter := newExporter()
	w.Header().Set("Content-Type", exporter.contentType())
	w.Header().Set("Content-Disposition", "attachment; filename=cloudpam-audit-"+now.Format("20060102-150405")+"."+exporter.extension())
	if err := exporter.begin(w); err != nil {
		return
	}
	written := 0
	for {
		for _, e := range events {
			if err := exporter.write(e); err != nil {
				return
			}
		}
		written += len(events)
		if len(events) == 0 || written >= total {
			break
		}
		opts.Offset = written
		if events, _, err = as.auditLogger.List(ctx, opts); err != nil {
			as.logger.WarnContext(ctx, "audit export aborted", "error", err, "written", written, "total", total)
			return
		}
	}
	_ = exporter.end(written)
}

var auditCSVHeader = []string{"id", "timestamp", "actor", "actor_type", "action", "resource_type", "resource_id", "resource_name", "request_id", "ip_address", "status_code", "changes"}

type auditCSVExporter struct {
	cw *csv.Writer
}

func (x *auditCSVExporter) contentType() string { return "text/csv; charset=utf-8" }
func (x *auditCSVExporter) extension() string   { return "csv" }

func (x *auditCSVExporter) begin(w io.Writer) error {
	x.cw = csv.NewWriter(w)
	return x.cw.Write(auditCSVHeader)
}

func (x *auditCSVExporter) write(e *audit.AuditEvent) error {
	changes := ""
	if e.Changes != nil {
		if b, err := json.Marshal(e.Changes); err == nil {
			changes = string(b)
		}
	}
	row := []string{
		e.ID,
		e.Timestamp.UTC().Format(time.RFC3339Nano),
		e.Actor,
		e.ActorType,
		e.Action,
		e.ResourceType,
		e.ResourceID,
		e.ResourceName,
		e.RequestID,
		e.IPAddress,
		strconv.Itoa(e.StatusCode),
		changes,
	}
	for i, cell := range row {
		row[i] = csvSafeCell(cell)
	}
	return x.cw.Write(row)
}

func (x *auditCSVExporter) end(int) error {
	x.cw.Flush()
	return x.cw.Error()
}

// csvSafeCell neutralises a cell that a spreadsheet would run as a formula.
// It mirrors escapeCsvCell in ui/src/utils/csv.ts; encoding/csv does the
// RFC 4180 quoting.
func csvSafeCell(cell string) string {
	if cell != "" && strings.ContainsRune("=+-@\t\r", rune(cell[0])) {
		return "'" + cell
	}
	return cell
}

type auditJSONExporter struct {
	w     io.Writer
	count int
}

func (x *auditJSONExporter) contentType() string { return "application/json" }
func (x *auditJSONExporter) extension() string   { return "json" }

func (x *auditJSONExporter) begin(w io.Writer) error {
	x.w = w
	_, err := io.WriteString(w, `{"events":[`)
	return err
}

func (x *auditJSONExporter) write(e *audit.AuditEvent) error {
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	if x.count > 0 {
		b = append([]byte{','}, b...)
	}
	x.count++
	_, err = x.w.Write(b)
	return err
}

func (x *auditJSONExporter) end(total int) error {
	_, err := fmt.Fprintf(x.w, `],"total":%d}`, total)
	return err
}
