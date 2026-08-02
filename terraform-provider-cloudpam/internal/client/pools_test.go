package client

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func writeJSON(t *testing.T, w http.ResponseWriter, status int, v any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		t.Fatalf("encode test response: %v", err)
	}
}

func writeRaw(t *testing.T, w http.ResponseWriter, status int, body string) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if _, err := io.WriteString(w, body); err != nil {
		t.Fatalf("write test response: %v", err)
	}
}

func readBody(t *testing.T, r *http.Request) map[string]any {
	t.Helper()
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatalf("read request body: %v", err)
	}
	if len(raw) == 0 {
		return nil
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal request body %q: %v", string(raw), err)
	}
	return out
}

func int64Ptr(v int64) *int64 { return &v }
func strPtr(v string) *string { return &v }

func TestListPools(t *testing.T) {
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/pools" {
			t.Errorf("got %s %s", r.Method, r.URL.Path)
		}
		writeRaw(t, w, http.StatusOK, `[
			{"id":1,"name":"root","cidr":"10.0.0.0/8","type":"supernet","status":"active","source":"manual","created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z"},
			{"id":2,"name":"child","cidr":"10.1.0.0/16","parent_id":1,"account_id":5,"type":"vpc","status":"active","source":"manual","tags":{"env":"prod"}}
		]`)
	}))

	pools, err := c.ListPools(context.Background())
	if err != nil {
		t.Fatalf("ListPools() error = %v", err)
	}
	if len(pools) != 2 {
		t.Fatalf("len(pools) = %d, want 2", len(pools))
	}
	if pools[0].ParentID != nil || pools[0].AccountID != nil {
		t.Errorf("pool 0: expected nil parent_id/account_id, got %v/%v", pools[0].ParentID, pools[0].AccountID)
	}
	if pools[1].ParentID == nil || *pools[1].ParentID != 1 {
		t.Errorf("pool 1 parent_id = %v, want 1", pools[1].ParentID)
	}
	if pools[1].AccountID == nil || *pools[1].AccountID != 5 {
		t.Errorf("pool 1 account_id = %v, want 5", pools[1].AccountID)
	}
	if !reflect.DeepEqual(pools[1].Tags, map[string]string{"env": "prod"}) {
		t.Errorf("pool 1 tags = %v", pools[1].Tags)
	}
}

func TestGetPool(t *testing.T) {
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/pools/42" {
			t.Errorf("path = %q, want /api/v1/pools/42", r.URL.Path)
		}
		writeRaw(t, w, http.StatusOK, `{"id":42,"name":"prod","cidr":"10.2.0.0/16","account_id":3,"type":"vpc","status":"active","source":"manual"}`)
	}))

	pool, err := c.GetPool(context.Background(), 42)
	if err != nil {
		t.Fatalf("GetPool() error = %v", err)
	}
	if pool.ID != 42 || pool.Name != "prod" || pool.CIDR != "10.2.0.0/16" {
		t.Fatalf("unexpected pool %+v", pool)
	}
	if pool.AccountID == nil || *pool.AccountID != 3 {
		t.Fatalf("account_id = %v, want 3", pool.AccountID)
	}
}

// TestCreatePoolOmitsNullableFields covers the classic provider bug: nil
// parent_id / account_id must be *omitted*, not sent as explicit null.
func TestCreatePoolOmitsNullableFields(t *testing.T) {
	tests := []struct {
		name      string
		in        PoolCreate
		wantKeys  map[string]any
		absentKey []string
	}{
		{
			name: "top level pool omits parent_id and account_id",
			in:   PoolCreate{Name: "root", CIDR: "10.0.0.0/8"},
			wantKeys: map[string]any{
				"name": "root",
				"cidr": "10.0.0.0/8",
			},
			absentKey: []string{"parent_id", "account_id", "type", "status", "source", "description", "tags"},
		},
		{
			name: "child pool sends parent_id and account_id",
			in: PoolCreate{
				Name:      "child",
				CIDR:      "10.1.0.0/16",
				ParentID:  int64Ptr(1),
				AccountID: int64Ptr(9),
				Type:      "vpc",
				Status:    "planned",
				Source:    "imported",
			},
			wantKeys: map[string]any{
				"name":       "child",
				"cidr":       "10.1.0.0/16",
				"parent_id":  float64(1),
				"account_id": float64(9),
				"type":       "vpc",
				"status":     "planned",
				"source":     "imported",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var got map[string]any
			c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					t.Errorf("method = %s, want POST", r.Method)
				}
				got = readBody(t, r)
				writeJSON(t, w, http.StatusCreated, Pool{ID: 100, Name: tc.in.Name, CIDR: tc.in.CIDR})
			}))

			pool, err := c.CreatePool(context.Background(), tc.in)
			if err != nil {
				t.Fatalf("CreatePool() error = %v", err)
			}
			if pool.ID != 100 {
				t.Errorf("pool.ID = %d, want 100", pool.ID)
			}
			for k, want := range tc.wantKeys {
				if got[k] != want {
					t.Errorf("body[%q] = %v (%T), want %v (%T)", k, got[k], got[k], want, want)
				}
			}
			for _, k := range tc.absentKey {
				if _, present := got[k]; present {
					t.Errorf("body should not contain key %q, got %v", k, got[k])
				}
			}
		})
	}
}

func TestCreatePoolPropagatesAPIError(t *testing.T) {
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeRaw(t, w, http.StatusBadRequest, `{"error":"cidr overlaps with existing block","detail":"conflicts with pool #3 (10.1.0.0/16)"}`)
	}))
	_, err := c.CreatePool(context.Background(), PoolCreate{Name: "x", CIDR: "10.1.0.0/16"})
	if err == nil {
		t.Fatal("expected error")
	}
	want := "POST /api/v1/pools: 400 Bad Request: cidr overlaps with existing block (conflicts with pool #3 (10.1.0.0/16))"
	if err.Error() != want {
		t.Fatalf("error = %q\nwant     %q", err.Error(), want)
	}
}

// TestUpdatePoolAccountIDSemantics pins the three-state account_id contract of
// PATCH /api/v1/pools/{id}:
//
//	absent -> server keeps the current assignment
//	null   -> server clears the assignment
//	number -> server assigns that account
func TestUpdatePoolAccountIDSemantics(t *testing.T) {
	tests := []struct {
		name          string
		update        PoolUpdate
		wantPresent   bool
		wantNullValue bool
		wantValue     float64
	}{
		{
			name:        "unset leaves account_id out of the body",
			update:      PoolUpdate{Name: strPtr("renamed")},
			wantPresent: false,
		},
		{
			name:          "explicit clear sends json null",
			update:        PoolUpdate{SetAccountID: true, AccountID: nil},
			wantPresent:   true,
			wantNullValue: true,
		},
		{
			name:        "assignment sends the id",
			update:      PoolUpdate{SetAccountID: true, AccountID: int64Ptr(12)},
			wantPresent: true,
			wantValue:   12,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var raw []byte
			c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPatch {
					t.Errorf("method = %s, want PATCH", r.Method)
				}
				if r.URL.Path != "/api/v1/pools/7" {
					t.Errorf("path = %q, want /api/v1/pools/7", r.URL.Path)
				}
				b, err := io.ReadAll(r.Body)
				if err != nil {
					t.Fatalf("read body: %v", err)
				}
				raw = b
				writeJSON(t, w, http.StatusOK, Pool{ID: 7, Name: "p", CIDR: "10.0.0.0/16"})
			}))

			if _, err := c.UpdatePool(context.Background(), 7, tc.update); err != nil {
				t.Fatalf("UpdatePool() error = %v", err)
			}

			var decoded map[string]json.RawMessage
			if err := json.Unmarshal(raw, &decoded); err != nil {
				t.Fatalf("unmarshal %q: %v", string(raw), err)
			}
			v, present := decoded["account_id"]
			if present != tc.wantPresent {
				t.Fatalf("account_id present = %v, want %v (body %q)", present, tc.wantPresent, string(raw))
			}
			if !tc.wantPresent {
				return
			}
			if tc.wantNullValue {
				if string(v) != "null" {
					t.Fatalf("account_id = %q, want null", string(v))
				}
				return
			}
			var n float64
			if err := json.Unmarshal(v, &n); err != nil {
				t.Fatalf("account_id not a number: %q", string(v))
			}
			if n != tc.wantValue {
				t.Fatalf("account_id = %v, want %v", n, tc.wantValue)
			}
		})
	}
}

func TestUpdatePoolBodyOnlyIncludesSetFields(t *testing.T) {
	var got map[string]any
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = readBody(t, r)
		writeJSON(t, w, http.StatusOK, Pool{ID: 3})
	}))

	tags := map[string]string{"team": "net"}
	update := PoolUpdate{
		Name:        strPtr("edge"),
		Type:        strPtr("vpc"),
		Status:      strPtr("deprecated"),
		Description: strPtr(""),
		Tags:        &tags,
	}
	if _, err := c.UpdatePool(context.Background(), 3, update); err != nil {
		t.Fatalf("UpdatePool() error = %v", err)
	}

	want := map[string]any{
		"name":        "edge",
		"type":        "vpc",
		"status":      "deprecated",
		"description": "",
		"tags":        map[string]any{"team": "net"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("body = %#v\nwant   %#v", got, want)
	}
}

func TestUpdatePoolEmptyBodyIsEmptyObject(t *testing.T) {
	var raw string
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		raw = string(b)
		writeJSON(t, w, http.StatusOK, Pool{ID: 3})
	}))
	if _, err := c.UpdatePool(context.Background(), 3, PoolUpdate{}); err != nil {
		t.Fatalf("UpdatePool() error = %v", err)
	}
	if raw != "{}" {
		t.Fatalf("body = %q, want {}", raw)
	}
}

func TestDeletePool(t *testing.T) {
	tests := []struct {
		name      string
		force     bool
		wantQuery string
	}{
		{name: "without force", force: false, wantQuery: ""},
		{name: "with force", force: true, wantQuery: "force=true"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var gotQuery, gotMethod, gotPath string
			c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotQuery = r.URL.RawQuery
				gotMethod = r.Method
				gotPath = r.URL.Path
				w.WriteHeader(http.StatusNoContent)
			}))
			if err := c.DeletePool(context.Background(), 11, tc.force); err != nil {
				t.Fatalf("DeletePool() error = %v", err)
			}
			if gotMethod != http.MethodDelete {
				t.Errorf("method = %s, want DELETE", gotMethod)
			}
			if gotPath != "/api/v1/pools/11" {
				t.Errorf("path = %q, want /api/v1/pools/11", gotPath)
			}
			if gotQuery != tc.wantQuery {
				t.Errorf("query = %q, want %q", gotQuery, tc.wantQuery)
			}
		})
	}
}

func TestDeletePoolConflict(t *testing.T) {
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeRaw(t, w, http.StatusConflict, `{"error":"pool has child pools"}`)
	}))
	err := c.DeletePool(context.Background(), 11, false)
	if err == nil {
		t.Fatal("expected conflict error")
	}
	if IsNotFound(err) {
		t.Fatal("409 must not be treated as not found")
	}
}

// TestPoolCRUDLifecycle exercises create -> read -> update -> delete against a
// stub that behaves like the real API, including 404 after deletion.
func TestPoolCRUDLifecycle(t *testing.T) {
	store := map[int64]*Pool{}
	var nextID int64 = 1

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/pools":
			var in PoolCreate
			if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
				t.Fatalf("decode create: %v", err)
			}
			p := &Pool{
				ID: nextID, Name: in.Name, CIDR: in.CIDR,
				ParentID: in.ParentID, AccountID: in.AccountID,
				Type: "subnet", Status: "active", Source: "manual",
				Description: in.Description, Tags: in.Tags,
				CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z",
			}
			store[nextID] = p
			nextID++
			writeJSON(t, w, http.StatusCreated, p)

		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/pools/1":
			p, ok := store[1]
			if !ok {
				writeRaw(t, w, http.StatusNotFound, `{"error":"not found"}`)
				return
			}
			writeJSON(t, w, http.StatusOK, p)

		case r.Method == http.MethodPatch && r.URL.Path == "/api/v1/pools/1":
			p, ok := store[1]
			if !ok {
				writeRaw(t, w, http.StatusNotFound, `{"error":"not found"}`)
				return
			}
			var body map[string]json.RawMessage
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode patch: %v", err)
			}
			if v, present := body["name"]; present {
				var s string
				_ = json.Unmarshal(v, &s)
				p.Name = s
			}
			// Mirror the server: absent key keeps the value, null clears it.
			if v, present := body["account_id"]; present {
				if string(v) == "null" {
					p.AccountID = nil
				} else {
					var n int64
					_ = json.Unmarshal(v, &n)
					p.AccountID = &n
				}
			}
			p.UpdatedAt = "2026-01-02T00:00:00Z"
			writeJSON(t, w, http.StatusOK, p)

		case r.Method == http.MethodDelete && r.URL.Path == "/api/v1/pools/1":
			if _, ok := store[1]; !ok {
				writeRaw(t, w, http.StatusNotFound, `{"error":"not found"}`)
				return
			}
			delete(store, 1)
			w.WriteHeader(http.StatusNoContent)

		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	})

	srv := httptest.NewServer(handler)
	defer srv.Close()
	c, err := New(srv.URL, "cpam_x")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	ctx := context.Background()

	created, err := c.CreatePool(ctx, PoolCreate{Name: "prod", CIDR: "10.0.0.0/16", AccountID: int64Ptr(4)})
	if err != nil {
		t.Fatalf("CreatePool() error = %v", err)
	}
	if created.ID != 1 || created.AccountID == nil || *created.AccountID != 4 {
		t.Fatalf("created = %+v", created)
	}

	got, err := c.GetPool(ctx, 1)
	if err != nil {
		t.Fatalf("GetPool() error = %v", err)
	}
	if got.Name != "prod" {
		t.Fatalf("got.Name = %q", got.Name)
	}

	// Rename without touching the account assignment.
	renamed, err := c.UpdatePool(ctx, 1, PoolUpdate{Name: strPtr("prod-renamed")})
	if err != nil {
		t.Fatalf("UpdatePool() error = %v", err)
	}
	if renamed.Name != "prod-renamed" {
		t.Fatalf("renamed.Name = %q", renamed.Name)
	}
	if renamed.AccountID == nil || *renamed.AccountID != 4 {
		t.Fatalf("account assignment was lost: %v", renamed.AccountID)
	}

	// Explicitly clear the account assignment.
	cleared, err := c.UpdatePool(ctx, 1, PoolUpdate{SetAccountID: true})
	if err != nil {
		t.Fatalf("UpdatePool() error = %v", err)
	}
	if cleared.AccountID != nil {
		t.Fatalf("account_id = %v, want nil", cleared.AccountID)
	}

	if err := c.DeletePool(ctx, 1, false); err != nil {
		t.Fatalf("DeletePool() error = %v", err)
	}

	// Reading a deleted pool must surface ErrNotFound so the provider can drop
	// it from state instead of erroring.
	if _, err := c.GetPool(ctx, 1); !IsNotFound(err) {
		t.Fatalf("GetPool() after delete: err = %v, want not found", err)
	}
}
