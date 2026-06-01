package api

import (
	"io"
	"net/http"
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
