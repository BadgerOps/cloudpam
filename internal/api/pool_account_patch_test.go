package api

import (
	"encoding/json"
	stdhttp "net/http"
	"strconv"
	"testing"

	"cloudpam/internal/domain"
)

// TestPatchPoolAccountIDTriState locks the three wire states that
// PATCH /api/v1/pools/{id} distinguishes for account_id:
//
//	key absent        -> keep the current assignment
//	key present, null -> clear the assignment
//	key present, N    -> assign account N
//
// The handler resolves these by inspecting key presence in the raw body, so
// domain.UpdatePool only ever receives an already-resolved value. Adding
// `omitempty` to domain.UpdatePool.AccountID would suggest nil means "absent",
// which contradicts the store contract that nil clears the column.
func TestPatchPoolAccountIDTriState(t *testing.T) {
	srv, _ := setupTestServer()

	rr := doJSON(t, srv.mux, stdhttp.MethodPost, "/api/v1/accounts",
		`{"key":"aws:111111111111","name":"acct-one"}`, stdhttp.StatusCreated)
	var acct struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &acct); err != nil {
		t.Fatalf("unmarshal account: %v", err)
	}

	rr = doJSON(t, srv.mux, stdhttp.MethodPost, "/api/v1/pools",
		`{"name":"root","cidr":"10.0.0.0/16","account_id":`+strconv.FormatInt(acct.ID, 10)+`}`,
		stdhttp.StatusCreated)
	var pool poolDTO
	if err := json.Unmarshal(rr.Body.Bytes(), &pool); err != nil {
		t.Fatalf("unmarshal pool: %v", err)
	}
	if pool.AccountID == nil || *pool.AccountID != acct.ID {
		t.Fatalf("expected pool to start assigned to account %d, got %v", acct.ID, pool.AccountID)
	}

	path := "/api/v1/pools/" + strconv.FormatInt(pool.ID, 10)

	patch := func(body string) poolDTO {
		t.Helper()
		res := doJSON(t, srv.mux, stdhttp.MethodPatch, path, body, stdhttp.StatusOK)
		var got poolDTO
		if err := json.Unmarshal(res.Body.Bytes(), &got); err != nil {
			t.Fatalf("unmarshal patched pool: %v", err)
		}
		return got
	}

	t.Run("absent key keeps the current account", func(t *testing.T) {
		got := patch(`{"name":"renamed"}`)
		if got.Name != "renamed" {
			t.Errorf("expected name renamed, got %q", got.Name)
		}
		if got.AccountID == nil || *got.AccountID != acct.ID {
			t.Fatalf("expected account %d to be preserved, got %v", acct.ID, got.AccountID)
		}
	})

	t.Run("explicit null clears the account", func(t *testing.T) {
		got := patch(`{"account_id":null}`)
		if got.AccountID != nil {
			t.Fatalf("expected account to be cleared, got %v", *got.AccountID)
		}
	})

	t.Run("absent key keeps the cleared state", func(t *testing.T) {
		got := patch(`{"name":"renamed-again"}`)
		if got.AccountID != nil {
			t.Fatalf("expected account to stay cleared, got %v", *got.AccountID)
		}
	})

	t.Run("numeric value assigns the account", func(t *testing.T) {
		got := patch(`{"account_id":` + strconv.FormatInt(acct.ID, 10) + `}`)
		if got.AccountID == nil || *got.AccountID != acct.ID {
			t.Fatalf("expected account %d to be assigned, got %v", acct.ID, got.AccountID)
		}
	})
}

// TestUpdatePoolAccountIDAlwaysSerialized documents why
// domain.UpdatePool.AccountID deliberately has no `omitempty`.
//
// The type is an internal, already-resolved command: nil means "clear the
// account", which every store implements as writing NULL. `omitempty` would
// erase that meaning from the marshaled form, making a clear indistinguishable
// from an untouched field for any Go client that used this type as a request
// body.
func TestUpdatePoolAccountIDAlwaysSerialized(t *testing.T) {
	name := "example"

	b, err := json.Marshal(domain.UpdatePool{Name: &name})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var got map[string]json.RawMessage
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	raw, ok := got["account_id"]
	if !ok {
		t.Fatal("account_id must always be serialized; a nil AccountID means 'clear', not 'unset'")
	}
	if string(raw) != "null" {
		t.Errorf("expected account_id null for a nil AccountID, got %s", raw)
	}

	// Fields that genuinely are optional stay omitted.
	for _, optional := range []string{"type", "status", "description", "tags"} {
		if _, present := got[optional]; present {
			t.Errorf("expected %q to be omitted when unset", optional)
		}
	}
}

// TestUpdatePoolSchemaDoesNotRequireAccountID guards the only place the JSON
// tag is externally visible: the generated OpenAPI component. Pointer fields
// are never marked required, so the emitted schema is identical with or
// without `omitempty` — the tag change the tracker proposes would not alter
// the published contract at all.
func TestUpdatePoolSchemaDoesNotRequireAccountID(t *testing.T) {
	var schema *openAPISchema
	for _, def := range openAPIComponentTypes() {
		if def.name == "UpdatePool" {
			schema = schemaFromType(def.typ, def.name)
			break
		}
	}
	if schema == nil {
		t.Fatal("UpdatePool component not registered")
	}

	if _, ok := schema.Properties["account_id"]; !ok {
		t.Fatal("expected account_id property in the UpdatePool schema")
	}
	for _, req := range schema.Required {
		if req == "account_id" {
			t.Fatal("account_id must not be a required request property")
		}
	}
}
