package api

import (
	"encoding/json"
	stdhttp "net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

type csvImportResult struct {
	Created int      `json:"created"`
	Skipped int      `json:"skipped"`
	Errors  []string `json:"errors"`
}

func importCSV(t *testing.T, srv *Server, path, body string) csvImportResult {
	t.Helper()
	req := httptest.NewRequest(stdhttp.MethodPost, path, strings.NewReader(body))
	rr := httptest.NewRecorder()
	srv.mux.ServeHTTP(rr, req)
	if rr.Code != stdhttp.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var res csvImportResult
	if err := json.Unmarshal(rr.Body.Bytes(), &res); err != nil {
		t.Fatalf("unmarshal import result: %v", err)
	}
	return res
}

func requireRowError(t *testing.T, errs []string, row int, fragment string) {
	t.Helper()
	prefix := "row " + strconv.Itoa(row) + ":"
	for _, e := range errs {
		if strings.HasPrefix(e, prefix) {
			if !strings.Contains(e, fragment) {
				t.Errorf("row %d error %q does not mention %q", row, e, fragment)
			}
			return
		}
	}
	t.Errorf("no error reported for row %d (want one mentioning %q); got %q", row, fragment, errs)
}

// Issue #350 repro: an unparsable CIDR must be rejected per row, not stored.
func TestImportPools_RejectsUnparsableCIDR(t *testing.T) {
	srv, st := setupTestServer()
	res := importCSV(t, srv, "/api/v1/import/pools", "name,cidr\nbad,not-a-cidr\n")
	if res.Created != 0 {
		t.Errorf("expected 0 created, got %d", res.Created)
	}
	if len(res.Errors) != 1 {
		t.Fatalf("expected 1 error, got %d: %q", len(res.Errors), res.Errors)
	}
	requireRowError(t, res.Errors, 2, "invalid cidr \"not-a-cidr\"")
	pools, _ := st.ListPools(t.Context())
	if len(pools) != 0 {
		t.Errorf("expected no pools stored, got %d", len(pools))
	}
}

// Every row goes through the same field validation as POST /api/v1/pools.
func TestImportPools_RowsFailingCreateValidationAreReported(t *testing.T) {
	srv, st := setupTestServer()
	longName := strings.Repeat("n", 256)
	body := "name,cidr,type,status,source\n" +
		"ok,10.0.0.0/16,,,\n" + // row 2: valid
		"v6,2001:db8::/32,,,\n" + // row 3: ipv6
		",10.1.0.0/16,,,\n" + // row 4: empty name
		longName + ",10.2.0.0/16,,,\n" + // row 5: name too long
		"badtype,10.3.0.0/16,wormhole,,\n" + // row 6: invalid type
		"badstatus,10.4.0.0/16,,maybe,\n" + // row 7: invalid status
		"badsource,10.5.0.0/16,,,psychic\n" + // row 8: invalid source
		"loop,127.0.0.0/8,,,\n" + // row 9: reserved range
		"tiny,10.6.0.0/31,,,\n" // row 10: prefix too long
	res := importCSV(t, srv, "/api/v1/import/pools", body)
	if res.Created != 1 {
		t.Errorf("expected 1 created, got %d", res.Created)
	}
	if res.Skipped != 0 {
		t.Errorf("expected 0 skipped, got %d", res.Skipped)
	}
	if len(res.Errors) != 8 {
		t.Errorf("expected 8 errors, got %d: %q", len(res.Errors), res.Errors)
	}
	requireRowError(t, res.Errors, 3, "only ipv4 is supported")
	requireRowError(t, res.Errors, 4, "name")
	requireRowError(t, res.Errors, 5, "exceeds maximum length")
	requireRowError(t, res.Errors, 6, "invalid pool type")
	requireRowError(t, res.Errors, 7, "invalid pool status")
	requireRowError(t, res.Errors, 8, "invalid pool source")
	requireRowError(t, res.Errors, 9, "reserved range")
	requireRowError(t, res.Errors, 10, "too large")

	pools, _ := st.ListPools(t.Context())
	if len(pools) != 1 || pools[0].Name != "ok" {
		t.Fatalf("expected only the valid pool stored, got %+v", pools)
	}
}

// Rows with blank required fields are reported against their row, not dropped.
func TestImportPools_BlankRequiredFieldsAreReported(t *testing.T) {
	srv, _ := setupTestServer()
	res := importCSV(t, srv, "/api/v1/import/pools", "name,cidr\nok,10.0.0.0/16\n,\nnocidr,\n")
	if res.Created != 1 {
		t.Errorf("expected 1 created, got %d", res.Created)
	}
	if len(res.Errors) != 2 {
		t.Fatalf("expected 2 errors, got %d: %q", len(res.Errors), res.Errors)
	}
	requireRowError(t, res.Errors, 3, "name and cidr required")
	requireRowError(t, res.Errors, 4, "name and cidr required")
}

// POST /api/v1/pools and CSV import must agree on what a valid row is.
func TestImportPools_MatchesCreatePoolValidation(t *testing.T) {
	srv, _ := setupTestServer()
	cases := []struct {
		name string
		json string
		csv  string
	}{
		{"unparsable cidr", `{"name":"a","cidr":"not-a-cidr"}`, "name,cidr\na,not-a-cidr\n"},
		{"ipv6", `{"name":"a","cidr":"2001:db8::/32"}`, "name,cidr\na,2001:db8::/32\n"},
		{"non-canonical", `{"name":"a","cidr":"10.0.0.1/24"}`, "name,cidr\na,10.0.0.1/24\n"},
		{"bad type", `{"name":"a","cidr":"10.9.0.0/24","type":"wormhole"}`, "name,cidr,type\na,10.9.0.0/24,wormhole\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rr := doJSON(t, srv.mux, stdhttp.MethodPost, "/api/v1/pools", tc.json, stdhttp.StatusBadRequest)
			var apiErr struct {
				Error string `json:"error"`
			}
			if err := json.Unmarshal(rr.Body.Bytes(), &apiErr); err != nil {
				t.Fatalf("unmarshal POST /pools error body %q: %v", rr.Body.String(), err)
			}
			if apiErr.Error == "" {
				t.Fatalf("POST /pools returned no error message: %s", rr.Body.String())
			}
			res := importCSV(t, srv, "/api/v1/import/pools", tc.csv)
			if res.Created != 0 || len(res.Errors) != 1 {
				t.Fatalf("import: want 0 created and 1 error, got %+v", res)
			}
			if !strings.Contains(res.Errors[0], apiErr.Error) {
				t.Errorf("import error %q does not carry the POST /pools message %q", res.Errors[0], apiErr.Error)
			}
		})
	}
}

// Accounts import has the same gap: it must run createAccount's key and name checks.
func TestImportAccounts_RowsFailingCreateValidationAreReported(t *testing.T) {
	srv, st := setupTestServer()
	body := "key,name\n" +
		"aws:123456789012,Prod\n" + // row 2: valid
		"nope,NoProvider\n" + // row 3: key without provider
		"aws:12,Short\n" + // row 4: aws key must be 12 digits
		"gcp:my-project,bad\tname\n" // row 5: control character in name
	res := importCSV(t, srv, "/api/v1/import/accounts", body)
	if res.Created != 1 {
		t.Errorf("expected 1 created, got %d", res.Created)
	}
	if len(res.Errors) != 3 {
		t.Errorf("expected 3 errors, got %d: %q", len(res.Errors), res.Errors)
	}
	requireRowError(t, res.Errors, 3, "invalid account key")
	requireRowError(t, res.Errors, 4, "invalid account key")
	requireRowError(t, res.Errors, 5, "control characters")
	accs, _ := st.ListAccounts(t.Context())
	if len(accs) != 1 || accs[0].Key != "aws:123456789012" {
		t.Fatalf("expected only the valid account stored, got %+v", accs)
	}
}

// Parse-time and validation errors are reported in CSV row order, not
// grouped by which loop found them.
func TestImportPools_ErrorsKeepRowOrder(t *testing.T) {
	srv, _ := setupTestServer()
	body := "name,cidr,parent_id\n" +
		"ok,10.0.0.0/16,\n" + // row 2: valid
		",,\n" + // row 3: blank (parse-time error)
		"bad,not-a-cidr,\n" + // row 4: validation error
		",,\n" + // row 5: blank (parse-time error)
		"v6,2001:db8::/32,1\n" // row 6: validation error, with a parent_id so sorting would move it
	res := importCSV(t, srv, "/api/v1/import/pools", body)
	if res.Created != 1 {
		t.Errorf("expected 1 created, got %d", res.Created)
	}
	want := []string{"row 3:", "row 4:", "row 5:", "row 6:"}
	if len(res.Errors) != len(want) {
		t.Fatalf("expected %d errors, got %d: %q", len(want), len(res.Errors), res.Errors)
	}
	for i, prefix := range want {
		if !strings.HasPrefix(res.Errors[i], prefix) {
			t.Errorf("errors[%d] = %q, want prefix %q (all: %q)", i, res.Errors[i], prefix, res.Errors)
		}
	}
}
