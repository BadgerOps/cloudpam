package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"cloudpam/internal/observability"
	"cloudpam/internal/storage"

	"gopkg.in/yaml.v3"
)

func TestOpenAPISpecValidation(t *testing.T) {
	st := storage.NewMemoryStore()
	mux := http.NewServeMux()
	logger := observability.NewLogger(observability.Config{
		Level:  "info",
		Format: "json",
		Output: io.Discard,
	})
	srv := NewServer(mux, st, logger, nil, nil)
	srv.registerUnprotectedTestRoutes()

	var spec map[string]any
	if err := yaml.Unmarshal(srv.openAPISpecYAML(), &spec); err != nil {
		t.Fatalf("generated OpenAPI is not valid YAML: %v", err)
	}
	if got := spec["openapi"]; got != "3.1.0" {
		t.Fatalf("unexpected openapi version: %v", got)
	}
	paths := asMap(t, spec["paths"], "paths")
	if len(paths) == 0 {
		t.Fatal("spec has no paths")
	}
	components := asMap(t, spec["components"], "components")
	schemas := asMap(t, components["schemas"], "components.schemas")
	if len(schemas) == 0 {
		t.Fatal("spec has no component schemas")
	}

	for path, rawPathItem := range paths {
		pathItem := asMap(t, rawPathItem, "paths."+path)
		if strings.HasSuffix(path, "/") {
			t.Fatalf("path should not have trailing slash: %s", path)
		}
		for method, rawOperation := range pathItem {
			switch method {
			case "get", "post", "patch", "delete", "put", "options", "head", "trace":
			default:
				continue
			}
			operation := asMap(t, rawOperation, path+"."+method)
			summary, ok := operation["summary"].(string)
			if !ok || strings.TrimSpace(summary) == "" {
				t.Fatalf("%s %s missing summary", method, path)
			}
			if _, ok := operation["responses"]; !ok {
				t.Fatalf("%s %s missing responses", method, path)
			}
			for _, ref := range collectOpenAPIRefs(operation) {
				name, ok := strings.CutPrefix(ref, "#/components/schemas/")
				if !ok {
					if _, ok := strings.CutPrefix(ref, "#/components/responses/"); ok {
						continue
					}
					t.Fatalf("%s %s has unsupported ref %q", method, path, ref)
				}
				if _, ok := schemas[name]; !ok {
					t.Fatalf("%s %s references missing schema %q", method, path, name)
				}
			}
		}
	}
}

func asMap(t *testing.T, raw any, name string) map[string]any {
	t.Helper()
	value, ok := raw.(map[string]any)
	if !ok {
		t.Fatalf("%s must be a mapping, got %T", name, raw)
	}
	return value
}

func collectOpenAPIRefs(raw any) []string {
	switch v := raw.(type) {
	case map[string]any:
		var refs []string
		for key, value := range v {
			if key == "$ref" {
				if ref, ok := value.(string); ok {
					refs = append(refs, ref)
				}
				continue
			}
			refs = append(refs, collectOpenAPIRefs(value)...)
		}
		return refs
	case []any:
		var refs []string
		for _, item := range v {
			refs = append(refs, collectOpenAPIRefs(item)...)
		}
		return refs
	default:
		return nil
	}
}

func TestOpenAPISchemaRoutesDescribeHandlerRequests(t *testing.T) {
	st := storage.NewMemoryStore()
	mux := http.NewServeMux()
	logger := observability.NewLogger(observability.Config{Level: "info", Format: "json", Output: io.Discard})
	srv := NewServer(mux, st, logger, nil, nil)
	srv.registerUnprotectedTestRoutes()
	ts := httptest.NewServer(mux)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/openapi.yaml")
	if err != nil {
		t.Fatalf("GET /openapi.yaml: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read spec: %v", err)
	}
	var spec map[string]any
	if err := yaml.Unmarshal(body, &spec); err != nil {
		t.Fatalf("spec is not valid YAML: %v", err)
	}
	schemas := asMap(t, asMap(t, spec["components"], "components")["schemas"], "components.schemas")

	component := func(holder map[string]any, name string) map[string]any {
		ref, _ := holder["$ref"].(string)
		return asMap(t, schemas[strings.TrimPrefix(ref, "#/components/schemas/")], name+" -> "+ref)
	}
	requestComponent := func(path string) map[string]any {
		op := asMap(t, asMap(t, asMap(t, spec["paths"], "paths")[path], path)["post"], path+".post")
		content := asMap(t, asMap(t, op["requestBody"], path+".requestBody")["content"], path+".content")
		return component(asMap(t, asMap(t, content["application/json"], path+".json")["schema"], path+".schema"), path)
	}
	requiredOf := func(c map[string]any) []string {
		var out []string
		for _, v := range c["required"].([]any) {
			out = append(out, v.(string))
		}
		return out
	}
	assertRequired := func(name string, c map[string]any, want []string) {
		if got := requiredOf(c); !reflect.DeepEqual(got, want) {
			t.Errorf("%s required = %v, handler requires %v", name, got, want)
		}
	}

	for _, path := range []string{"/api/v1/schema/check", "/api/v1/schema/apply"} {
		request := requestComponent(path)
		assertRequired(path+" request", request, []string{"pools"})
		pools := asMap(t, asMap(t, request["properties"], path+".properties")["pools"], path+" pools")
		entry := component(asMap(t, pools["items"], path+" pools.items"), path+" entry")
		entryProps := asMap(t, entry["properties"], path+" entry.properties")
		for _, want := range []string{"ref", "name", "cidr", "type", "parent_ref"} {
			if _, ok := entryProps[want]; !ok {
				t.Errorf("%s entry schema missing %q, which the handler decodes", path, want)
			}
		}
		for _, ignored := range []string{"parent_id", "account_id", "status", "source", "description", "tags"} {
			if _, ok := entryProps[ignored]; ok {
				t.Errorf("%s entry schema advertises %q, which the handler ignores", path, ignored)
			}
		}
		assertRequired(path+" entry", entry, []string{"cidr", "name", "ref"})
	}

	check := asMap(t, requestComponent("/api/v1/schema/check")["properties"], "check.properties")
	for _, applyOnly := range []string{"status", "tags", "skip_conflicts"} {
		if _, ok := check[applyOnly]; ok {
			t.Errorf("schema/check request advertises %q, which only apply reads", applyOnly)
		}
	}
	apply := asMap(t, requestComponent("/api/v1/schema/apply")["properties"], "apply.properties")
	for _, want := range []string{"pools", "status", "tags", "skip_conflicts"} {
		if _, ok := apply[want]; !ok {
			t.Errorf("schema/apply request schema missing %q, which the handler decodes", want)
		}
	}
}
