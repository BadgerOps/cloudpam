package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestListAccounts(t *testing.T) {
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/accounts" {
			t.Errorf("path = %q", r.URL.Path)
		}
		writeRaw(t, w, http.StatusOK, `[
			{"id":1,"key":"aws:111111111111","name":"prod","provider":"aws","regions":["us-east-1","us-west-2"]},
			{"id":2,"key":"gcp:demo","name":"demo"}
		]`)
	}))

	accounts, err := c.ListAccounts(context.Background())
	if err != nil {
		t.Fatalf("ListAccounts() error = %v", err)
	}
	if len(accounts) != 2 {
		t.Fatalf("len = %d, want 2", len(accounts))
	}
	if !reflect.DeepEqual(accounts[0].Regions, []string{"us-east-1", "us-west-2"}) {
		t.Errorf("regions = %v", accounts[0].Regions)
	}
	if accounts[1].Regions != nil {
		t.Errorf("regions = %v, want nil for omitted key", accounts[1].Regions)
	}
}

func TestCreateAccountOmitsEmptyOptionalFields(t *testing.T) {
	var got map[string]any
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		got = readBody(t, r)
		writeJSON(t, w, http.StatusCreated, Account{ID: 9, Key: "aws:1", Name: "prod"})
	}))

	acct, err := c.CreateAccount(context.Background(), AccountCreate{Key: "aws:1", Name: "prod"})
	if err != nil {
		t.Fatalf("CreateAccount() error = %v", err)
	}
	if acct.ID != 9 {
		t.Errorf("acct.ID = %d, want 9", acct.ID)
	}
	want := map[string]any{"key": "aws:1", "name": "prod"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("body = %#v, want %#v", got, want)
	}
}

func TestCreateAccountSendsAllFields(t *testing.T) {
	var got map[string]any
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = readBody(t, r)
		writeJSON(t, w, http.StatusCreated, Account{ID: 1})
	}))

	in := AccountCreate{
		Key: "aws:222222222222", Name: "staging", Provider: "aws",
		ExternalID: "222222222222", Description: "staging account",
		Platform: "ec2", Tier: "nonprod", Environment: "staging",
		Regions: []string{"eu-west-1"},
	}
	if _, err := c.CreateAccount(context.Background(), in); err != nil {
		t.Fatalf("CreateAccount() error = %v", err)
	}

	want := map[string]any{
		"key": "aws:222222222222", "name": "staging", "provider": "aws",
		"external_id": "222222222222", "description": "staging account",
		"platform": "ec2", "tier": "nonprod", "environment": "staging",
		"regions": []any{"eu-west-1"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("body = %#v\nwant   %#v", got, want)
	}
}

func TestCreateAccountValidationError(t *testing.T) {
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeRaw(t, w, http.StatusBadRequest, `{"error":"invalid account key"}`)
	}))
	_, err := c.CreateAccount(context.Background(), AccountCreate{Key: "!!", Name: "x"})
	if err == nil {
		t.Fatal("expected error")
	}
	want := "POST /api/v1/accounts: 400 Bad Request: invalid account key"
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
}

// TestUpdateAccountAlwaysSendsClearableFields covers the API's replace-in-place
// semantics: the server overwrites every optional field with whatever the body
// contains, so omitting a field would silently keep a stale value.
func TestUpdateAccountAlwaysSendsClearableFields(t *testing.T) {
	var got map[string]any
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Errorf("method = %s, want PATCH", r.Method)
		}
		if r.URL.Path != "/api/v1/accounts/4" {
			t.Errorf("path = %q", r.URL.Path)
		}
		got = readBody(t, r)
		writeJSON(t, w, http.StatusOK, Account{ID: 4, Key: "aws:1", Name: "prod"})
	}))

	if _, err := c.UpdateAccount(context.Background(), 4, AccountUpdate{Name: "prod"}); err != nil {
		t.Fatalf("UpdateAccount() error = %v", err)
	}

	want := map[string]any{
		"name": "prod", "provider": "", "external_id": "", "description": "",
		"platform": "", "tier": "", "environment": "", "regions": []any{},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("body = %#v\nwant   %#v", got, want)
	}
	// `key` must never be patched: the storage layer ignores it, so the
	// provider marks the attribute RequiresReplace instead.
	if _, present := got["key"]; present {
		t.Error("update body must not contain key")
	}
}

// TestUpdateAccountNilRegionsBecomesEmptySlice pins the nullable-collection
// handling: a nil slice would marshal to null, which the server reads as "leave
// regions alone"; an empty slice is what actually clears them.
func TestUpdateAccountNilRegionsBecomesEmptySlice(t *testing.T) {
	var raw map[string]json.RawMessage
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
			t.Fatalf("decode: %v", err)
		}
		writeJSON(t, w, http.StatusOK, Account{ID: 4})
	}))

	if _, err := c.UpdateAccount(context.Background(), 4, AccountUpdate{Name: "n", Regions: nil}); err != nil {
		t.Fatalf("UpdateAccount() error = %v", err)
	}
	if string(raw["regions"]) != "[]" {
		t.Fatalf("regions = %q, want []", string(raw["regions"]))
	}
}

func TestDeleteAccountForce(t *testing.T) {
	for _, tc := range []struct {
		name  string
		force bool
		want  string
	}{
		{"no force", false, ""},
		{"force", true, "force=true"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var gotQuery string
			c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodDelete {
					t.Errorf("method = %s", r.Method)
				}
				gotQuery = r.URL.RawQuery
				w.WriteHeader(http.StatusNoContent)
			}))
			if err := c.DeleteAccount(context.Background(), 4, tc.force); err != nil {
				t.Fatalf("DeleteAccount() error = %v", err)
			}
			if gotQuery != tc.want {
				t.Fatalf("query = %q, want %q", gotQuery, tc.want)
			}
		})
	}
}

func TestAccountCRUDLifecycle(t *testing.T) {
	store := map[int64]*Account{}
	var nextID int64 = 1

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/accounts":
			var in AccountCreate
			if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
				t.Fatalf("decode: %v", err)
			}
			a := &Account{
				ID: nextID, Key: in.Key, Name: in.Name, Provider: in.Provider,
				ExternalID: in.ExternalID, Description: in.Description,
				Platform: in.Platform, Tier: in.Tier, Environment: in.Environment,
				Regions: in.Regions, CreatedAt: "2026-01-01T00:00:00Z",
			}
			store[nextID] = a
			nextID++
			writeJSON(t, w, http.StatusCreated, a)

		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/accounts/1":
			a, ok := store[1]
			if !ok {
				writeRaw(t, w, http.StatusNotFound, `{"error":"not found"}`)
				return
			}
			writeJSON(t, w, http.StatusOK, a)

		case r.Method == http.MethodPatch && r.URL.Path == "/api/v1/accounts/1":
			a, ok := store[1]
			if !ok {
				writeRaw(t, w, http.StatusNotFound, `{"error":"not found"}`)
				return
			}
			var in AccountUpdate
			if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
				t.Fatalf("decode: %v", err)
			}
			// Mirror the server: name only when non-empty, optional fields
			// replaced wholesale, regions replaced when non-nil.
			if in.Name != "" {
				a.Name = in.Name
			}
			a.Provider = in.Provider
			a.ExternalID = in.ExternalID
			a.Description = in.Description
			a.Platform = in.Platform
			a.Tier = in.Tier
			a.Environment = in.Environment
			if in.Regions != nil {
				a.Regions = in.Regions
			}
			a.UpdatedAt = "2026-01-02T00:00:00Z"
			writeJSON(t, w, http.StatusOK, a)

		case r.Method == http.MethodDelete && r.URL.Path == "/api/v1/accounts/1":
			if _, ok := store[1]; !ok {
				writeRaw(t, w, http.StatusNotFound, `{"error":"not found"}`)
				return
			}
			delete(store, 1)
			w.WriteHeader(http.StatusNoContent)

		default:
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()

	c, err := New(srv.URL, "cpam_x")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	ctx := context.Background()

	created, err := c.CreateAccount(ctx, AccountCreate{
		Key: "aws:333333333333", Name: "prod", Provider: "aws",
		Description: "initial", Regions: []string{"us-east-1"},
	})
	if err != nil {
		t.Fatalf("CreateAccount() error = %v", err)
	}
	if created.ID != 1 {
		t.Fatalf("created.ID = %d", created.ID)
	}

	if _, err := c.GetAccount(ctx, 1); err != nil {
		t.Fatalf("GetAccount() error = %v", err)
	}

	updated, err := c.UpdateAccount(ctx, 1, AccountUpdate{Name: "prod-2", Provider: "aws"})
	if err != nil {
		t.Fatalf("UpdateAccount() error = %v", err)
	}
	if updated.Name != "prod-2" {
		t.Fatalf("name = %q", updated.Name)
	}
	if updated.Description != "" {
		t.Fatalf("description = %q, want cleared", updated.Description)
	}
	if len(updated.Regions) != 0 {
		t.Fatalf("regions = %v, want cleared", updated.Regions)
	}

	if err := c.DeleteAccount(ctx, 1, false); err != nil {
		t.Fatalf("DeleteAccount() error = %v", err)
	}
	if _, err := c.GetAccount(ctx, 1); !IsNotFound(err) {
		t.Fatalf("GetAccount() after delete: err = %v, want not found", err)
	}
}
