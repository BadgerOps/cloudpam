package api

import (
	"fmt"
	"io"
	stdhttp "net/http"
	"strings"
	"testing"

	"cloudpam/internal/domain"
	"cloudpam/internal/observability"
	"cloudpam/internal/storage"
)

func TestImportPools_ParentRows(t *testing.T) {
	testImportPoolParentRows(t, func(t *testing.T) storage.Store {
		return storage.NewMemoryStore()
	})
}

// Run the same HTTP and persisted-hierarchy assertions on each storage backend.
func testImportPoolParentRows(t *testing.T, newStore func(*testing.T) storage.Store) {
	t.Helper()
	cases := []struct {
		name    string
		csv     string
		created int
		skipped int
		errors  []string
		parents map[string]string
	}{
		{
			name: "overlapping parent and independent row",
			csv: "id,name,cidr,parent_id\n%[1]d,parent,10.0.0.0/9,\n" +
				"20,child,10.1.0.0/16,%[1]d\n30,independent,172.16.0.0/12,\n",
			created: 1,
			errors:  []string{"row 2:", "row 3: parent row 2 failed"},
			parents: map[string]string{"independent": ""},
		},
		{
			name:    "duplicate parent is skipped but child fails",
			csv:     "id,name,cidr,parent_id\n%[1]d,parent,10.0.0.0/8,\n20,child,10.1.0.0/16,%[1]d\n",
			skipped: 1,
			errors:  []string{"row 3: parent row 2 failed"},
		},
		{
			name:   "invalid parent CIDR",
			csv:    "id,name,cidr,parent_id\n20,parent,invalid,%[1]d\n30,child,10.1.0.0/16,20\n",
			errors: []string{"row 2:", "row 3: parent row 2 failed"},
		},
		{
			name: "parent account key fails before placement",
			csv: "id,name,cidr,parent_id,account_key\n%[1]d,parent,192.168.0.0/16,,missing\n" +
				"20,child,10.1.0.0/16,%[1]d,\n",
			errors: []string{"row 2: account_key 'missing' not found", "row 3: parent row 2 failed"},
		},
		{
			name: "parent account ID fails before placement",
			csv: "id,name,cidr,parent_id,account_id\n%[1]d,parent,192.168.0.0/16,,999\n" +
				"20,child,10.1.0.0/16,%[1]d,\n",
			errors: []string{"row 2: account_id 999 not found", "row 3: parent row 2 failed"},
		},
		{
			name:   "filtered empty parent still declares its ID",
			csv:    "id,name,cidr,parent_id\n%[1]d,,192.168.0.0/16,\n20,child,10.1.0.0/16,%[1]d\n",
			errors: []string{"row 3: parent row 2 failed"},
		},
		{
			name: "transitive parent failure",
			csv: "id,name,cidr,parent_id\n%[1]d,parent,10.0.0.0/9,\n" +
				"20,child,10.1.0.0/16,%[1]d\n30,grandchild,10.1.1.0/24,20\n",
			errors: []string{"row 2:", "row 3: parent row 2 failed", "row 4: parent row 3 failed"},
		},
		{
			name: "parent containment failure",
			csv: "id,name,cidr,parent_id\n20,parent,192.168.0.0/16,%[1]d\n" +
				"30,child,192.168.1.0/24,20\n",
			errors: []string{"row 2: invalid sub-pool cidr", "row 3: parent row 2 failed"},
		},
		{
			name: "successful hierarchy remaps colliding old ID",
			csv: "id,name,cidr,parent_id\n%[1]d,parent,192.168.0.0/16,\n" +
				"20,child,192.168.1.0/24,%[1]d\n30,grandchild,192.168.1.0/26,20\n",
			created: 3,
			parents: map[string]string{"parent": "", "child": "parent", "grandchild": "child"},
		},
		{
			name: "out of order descending hierarchy",
			csv: "id,name,cidr,parent_id\n10,grandchild,192.168.1.0/26,20\n" +
				"20,child,192.168.1.0/24,30\n30,parent,192.168.0.0/16,\n",
			created: 3,
			parents: map[string]string{"parent": "", "child": "parent", "grandchild": "child"},
		},
		{
			name:    "existing parent absent from CSV",
			csv:     "id,name,cidr,parent_id\n20,child,10.1.0.0/16,%[1]d\n",
			created: 1,
			parents: map[string]string{"child": "root"},
		},
		{
			name:   "cycle cannot fall back to existing parent",
			csv:    "id,name,cidr,parent_id\n%[1]d,parent,10.0.0.0/9,20\n20,child,10.1.0.0/16,%[1]d\n",
			errors: []string{"row 2: parent row 3 failed", "row 3: parent row 2 failed"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			st := newStore(t)
			logger := observability.NewLogger(observability.Config{Level: "info", Format: "json", Output: io.Discard})
			srv := NewServer(stdhttp.NewServeMux(), st, logger, nil, nil)
			srv.registerUnprotectedTestRoutes()
			root := createRootPool(t, srv, "10.0.0.0/8")
			body := tc.csv
			if strings.Contains(body, "%[1]d") {
				body = fmt.Sprintf(body, root.ID)
			}
			res := importPoolsCSV(t, srv, body)
			if res.Created != tc.created || res.Skipped != tc.skipped || len(res.Errors) != len(tc.errors) {
				t.Errorf("import result = %+v, want created=%d skipped=%d errors=%v", res, tc.created, tc.skipped, tc.errors)
			}
			for _, want := range tc.errors {
				if !hasErrorContaining(res.Errors, want) {
					t.Errorf("errors = %v, want %q", res.Errors, want)
				}
			}
			pools, err := st.ListPools(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if len(pools) != 1+tc.created {
				t.Errorf("stored %d pools, want %d", len(pools), 1+tc.created)
			}
			byName := make(map[string]domain.Pool, len(pools))
			for _, p := range pools {
				byName[p.Name] = p
			}
			byName["root"] = byName[root.Name]
			for name, parentName := range tc.parents {
				p, ok := byName[name]
				if !ok {
					t.Errorf("pool %q missing", name)
					continue
				}
				if parentName == "" {
					if p.ParentID != nil {
						t.Errorf("pool %q parent = %d, want top-level", name, *p.ParentID)
					}
					continue
				}
				parent, ok := byName[parentName]
				if !ok || p.ParentID == nil || *p.ParentID != parent.ID {
					t.Errorf("pool %q parent = %v, want %q (%d)", name, p.ParentID, parentName, parent.ID)
				}
			}
		})
	}
}
