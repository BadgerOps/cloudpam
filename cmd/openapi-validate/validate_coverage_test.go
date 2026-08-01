package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

const covValidSpec = `
openapi: 3.1.0
info:
  title: CloudPAM
  version: 1.0.0
paths:
  /widgets:
    parameters:
      - name: trace
        in: query
    get:
      summary: List widgets
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema:
                $ref: "#/components/schemas/Widget"
        "500":
          $ref: "#/components/responses/ServerError"
    post:
      summary: Create widget
      responses:
        "201":
          description: created
components:
  schemas:
    Widget:
      type: object
    Error:
      type: object
  responses:
    ServerError:
      description: boom
`

func covWriteSpec(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "openapi.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write spec: %v", err)
	}
	return path
}

func TestCovValidateSpecAcceptsValidDocument(t *testing.T) {
	paths, componentGroups, err := validateSpec([]byte(covValidSpec))
	if err != nil {
		t.Fatalf("validateSpec() error = %v", err)
	}
	if paths != 1 {
		t.Errorf("paths = %d, want 1", paths)
	}
	if componentGroups != 2 {
		t.Errorf("component groups = %d, want 2 (schemas and responses)", componentGroups)
	}
}

func TestCovValidateSpecAcceptsOpenAPI30(t *testing.T) {
	spec := strings.Replace(covValidSpec, "openapi: 3.1.0", "openapi: 3.0.3", 1)
	if _, _, err := validateSpec([]byte(spec)); err != nil {
		t.Fatalf("validateSpec() error = %v, want 3.0.x to be accepted", err)
	}
}

func TestCovValidateSpecRejections(t *testing.T) {
	tests := []struct {
		name    string
		spec    string
		wantErr string
	}{
		{
			name:    "malformed yaml",
			spec:    "openapi: [3.1.0\n",
			wantErr: "",
		},
		{
			name: "missing openapi version",
			spec: `
paths:
  /w:
    get:
      summary: s
      responses: {}
components:
  schemas:
    E: {}
`,
			wantErr: "unsupported or missing openapi version",
		},
		{
			name: "openapi 2.0",
			spec: `
openapi: "2.0"
paths:
  /w:
    get:
      summary: s
      responses: {}
components:
  schemas:
    E: {}
`,
			wantErr: "unsupported or missing openapi version",
		},
		{
			name: "non-string openapi version",
			spec: `
openapi: 3
paths:
  /w:
    get:
      summary: s
      responses: {}
components:
  schemas:
    E: {}
`,
			wantErr: "unsupported or missing openapi version",
		},
		{
			name: "missing paths",
			spec: `
openapi: 3.1.0
components:
  schemas:
    E: {}
`,
			wantErr: "paths must be a non-empty mapping",
		},
		{
			name: "empty paths",
			spec: `
openapi: 3.1.0
paths: {}
components:
  schemas:
    E: {}
`,
			wantErr: "paths must be a non-empty mapping",
		},
		{
			name: "missing components",
			spec: `
openapi: 3.1.0
paths:
  /w:
    get:
      summary: s
      responses: {}
`,
			wantErr: "components must be a mapping",
		},
		{
			name: "missing schemas",
			spec: `
openapi: 3.1.0
paths:
  /w:
    get:
      summary: s
      responses: {}
components:
  responses:
    E: {}
`,
			wantErr: "components.schemas must be a non-empty mapping",
		},
		{
			name: "empty schemas",
			spec: `
openapi: 3.1.0
paths:
  /w:
    get:
      summary: s
      responses: {}
components:
  schemas: {}
`,
			wantErr: "components.schemas must be a non-empty mapping",
		},
		{
			name: "path item is not a mapping",
			spec: `
openapi: 3.1.0
paths:
  /w: "just a string"
components:
  schemas:
    E: {}
`,
			wantErr: "path /w must be a mapping",
		},
		{
			name: "operation is not a mapping",
			spec: `
openapi: 3.1.0
paths:
  /w:
    get: "not an operation"
components:
  schemas:
    E: {}
`,
			wantErr: "get /w must be a mapping",
		},
		{
			name: "blank summary",
			spec: `
openapi: 3.1.0
paths:
  /w:
    get:
      summary: "   "
      responses: {}
components:
  schemas:
    E: {}
`,
			wantErr: "missing summary",
		},
		{
			name: "responses is not a mapping",
			spec: `
openapi: 3.1.0
paths:
  /w:
    get:
      summary: s
      responses: "none"
components:
  schemas:
    E: {}
`,
			wantErr: "missing responses",
		},
		{
			name: "ref to unknown schema",
			spec: `
openapi: 3.1.0
paths:
  /w:
    get:
      summary: s
      responses:
        "200":
          content:
            application/json:
              schema:
                $ref: "#/components/schemas/Missing"
components:
  schemas:
    E: {}
`,
			wantErr: `references missing schema "Missing"`,
		},
		{
			name: "unsupported ref target",
			spec: `
openapi: 3.1.0
paths:
  /w:
    get:
      summary: s
      responses:
        "200":
          content:
            application/json:
              schema:
                $ref: "#/components/parameters/Page"
components:
  schemas:
    E: {}
`,
			wantErr: `unsupported ref "#/components/parameters/Page"`,
		},
		{
			name: "external ref",
			spec: `
openapi: 3.1.0
paths:
  /w:
    get:
      summary: s
      responses:
        "200":
          content:
            application/json:
              schema:
                $ref: "other.yaml#/Widget"
components:
  schemas:
    E: {}
`,
			wantErr: `unsupported ref "other.yaml#/Widget"`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			paths, groups, err := validateSpec([]byte(tc.spec))
			if err == nil {
				t.Fatal("validateSpec() error = nil, want failure")
			}
			if tc.wantErr != "" && !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error = %q, want it to contain %q", err.Error(), tc.wantErr)
			}
			if paths != 0 || groups != 0 {
				t.Errorf("counts = %d/%d, want 0/0 on error", paths, groups)
			}
		})
	}
}

func TestCovValidateSpecIgnoresNonOperationKeys(t *testing.T) {
	// summary/description/servers/parameters at path level are not operations
	// and must not be validated as such.
	spec := `
openapi: 3.1.0
paths:
  /w:
    summary: a path-level summary
    description: some text
    servers:
      - url: https://example.com
    parameters:
      - name: page
        in: query
    get:
      summary: List
      responses:
        "200":
          description: ok
components:
  schemas:
    E: {}
`
	paths, groups, err := validateSpec([]byte(spec))
	if err != nil {
		t.Fatalf("validateSpec() error = %v", err)
	}
	if paths != 1 || groups != 1 {
		t.Fatalf("counts = %d/%d, want 1/1", paths, groups)
	}
}

func TestCovValidateSpecAcceptsAllHTTPMethods(t *testing.T) {
	for _, method := range []string{"get", "post", "put", "patch", "delete", "options", "head", "trace"} {
		t.Run(method, func(t *testing.T) {
			spec := `
openapi: 3.1.0
paths:
  /w:
    ` + method + `:
      summary: Do it
      responses:
        "200":
          description: ok
components:
  schemas:
    E: {}
`
			if _, _, err := validateSpec([]byte(spec)); err != nil {
				t.Fatalf("validateSpec() error = %v for method %s", err, method)
			}
		})
	}
}

func TestCovCollectRefs(t *testing.T) {
	tests := []struct {
		name string
		raw  any
		want []string
	}{
		{"nil", nil, nil},
		{"scalar string", "hello", nil},
		{"scalar int", 42, nil},
		{"empty map", map[string]any{}, nil},
		{"empty slice", []any{}, nil},
		{
			"top level ref",
			map[string]any{"$ref": "#/components/schemas/A"},
			[]string{"#/components/schemas/A"},
		},
		{
			"non-string ref value is skipped",
			map[string]any{"$ref": 123},
			nil,
		},
		{
			"nested ref",
			map[string]any{"content": map[string]any{"schema": map[string]any{"$ref": "#/components/schemas/B"}}},
			[]string{"#/components/schemas/B"},
		},
		{
			"refs inside a slice",
			map[string]any{"allOf": []any{
				map[string]any{"$ref": "#/components/schemas/C"},
				map[string]any{"$ref": "#/components/schemas/D"},
				"literal",
			}},
			[]string{"#/components/schemas/C", "#/components/schemas/D"},
		},
		{
			"slice at the top level",
			[]any{map[string]any{"$ref": "#/components/schemas/E"}},
			[]string{"#/components/schemas/E"},
		},
		{
			"sibling keys of a ref are not descended into",
			map[string]any{"$ref": "#/components/schemas/F"},
			[]string{"#/components/schemas/F"},
		},
		{
			"deeply nested mixed structure",
			map[string]any{
				"responses": map[string]any{
					"200": map[string]any{
						"content": map[string]any{
							"application/json": map[string]any{
								"schema": map[string]any{
									"items": []any{map[string]any{"$ref": "#/components/schemas/G"}},
								},
							},
						},
					},
					"500": map[string]any{"$ref": "#/components/responses/Err"},
				},
			},
			[]string{"#/components/responses/Err", "#/components/schemas/G"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := collectRefs(tc.raw)
			sort.Strings(got)
			want := append([]string(nil), tc.want...)
			sort.Strings(want)
			if len(got) == 0 && len(want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("collectRefs() = %v, want %v", got, want)
			}
		})
	}
}

func TestCovReadSpecFromFile(t *testing.T) {
	path := covWriteSpec(t, covValidSpec)

	data, err := readSpec(path)
	if err != nil {
		t.Fatalf("readSpec() error = %v", err)
	}
	if string(data) != covValidSpec {
		t.Fatalf("readSpec() returned %d bytes, want the file contents verbatim", len(data))
	}
}

func TestCovReadSpecFromMissingFile(t *testing.T) {
	if _, err := readSpec(filepath.Join(t.TempDir(), "nope.yaml")); err == nil {
		t.Fatal("readSpec() error = nil, want a file read failure")
	}
}

func TestCovReadSpecFromHTTP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		_, _ = w.Write([]byte(covValidSpec))
	}))
	defer srv.Close()

	data, err := readSpec(srv.URL + "/openapi.yaml")
	if err != nil {
		t.Fatalf("readSpec() error = %v", err)
	}
	if string(data) != covValidSpec {
		t.Fatalf("readSpec() body mismatch (%d bytes)", len(data))
	}
}

func TestCovReadSpecRejectsNon2xxHTTPStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	url := srv.URL + "/openapi.yaml"
	_, err := readSpec(url)
	if err == nil {
		t.Fatal("readSpec() error = nil, want a status failure")
	}
	if !strings.Contains(err.Error(), "GET "+url+" returned") {
		t.Errorf("error = %q, want it to name the URL", err.Error())
	}
	if !strings.Contains(err.Error(), "404") {
		t.Errorf("error = %q, want the 404 status", err.Error())
	}
}

func TestCovReadSpecRejectsRedirectToError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	if _, err := readSpec(srv.URL); err == nil || !strings.Contains(err.Error(), "500") {
		t.Fatalf("readSpec() error = %v, want a 500 failure", err)
	}
}

func TestCovReadSpecReportsHTTPTransportFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close()

	if _, err := readSpec(url + "/openapi.yaml"); err == nil {
		t.Fatal("readSpec() error = nil, want a transport failure")
	}
}

func TestCovReadSpecUsesHTTPSPrefix(t *testing.T) {
	// An https URL takes the HTTP branch; with nothing listening it fails at
	// the transport layer rather than being treated as a file path.
	_, err := readSpec("https://127.0.0.1:1/openapi.yaml")
	if err == nil {
		t.Fatal("readSpec() error = nil, want a transport failure")
	}
	if strings.Contains(err.Error(), "no such file") {
		t.Fatalf("error = %q, want the https URL to take the HTTP branch", err.Error())
	}
}

func TestCovReadSpecFromStdin(t *testing.T) {
	path := covWriteSpec(t, covValidSpec)
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = f.Close() }()

	original := os.Stdin
	os.Stdin = f
	defer func() { os.Stdin = original }()

	data, err := readSpec("-")
	if err != nil {
		t.Fatalf("readSpec() error = %v", err)
	}
	if string(data) != covValidSpec {
		t.Fatalf("readSpec() from stdin returned %d bytes, want the piped spec", len(data))
	}
}

func TestCovReadSpecThenValidateRoundTrip(t *testing.T) {
	path := covWriteSpec(t, covValidSpec)

	data, err := readSpec(path)
	if err != nil {
		t.Fatalf("readSpec() error = %v", err)
	}
	paths, groups, err := validateSpec(data)
	if err != nil {
		t.Fatalf("validateSpec() error = %v", err)
	}
	if paths != 1 || groups != 2 {
		t.Fatalf("counts = %d/%d, want 1/2", paths, groups)
	}
}
