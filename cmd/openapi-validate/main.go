package main

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

func main() {
	source := "-"
	if len(os.Args) > 1 {
		source = os.Args[1]
	}

	data, err := readSpec(source)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read spec: %v\n", err)
		os.Exit(1)
	}
	paths, componentGroups, err := validateSpec(data)
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid OpenAPI spec: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("OpenAPI spec OK (%d paths, %d component groups).\n", paths, componentGroups)
}

func readSpec(source string) ([]byte, error) {
	switch {
	case source == "-":
		return io.ReadAll(os.Stdin)
	case strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "https://"):
		resp, err := http.Get(source)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode > 299 {
			return nil, fmt.Errorf("GET %s returned %s", source, resp.Status)
		}
		return io.ReadAll(resp.Body)
	default:
		return os.ReadFile(source)
	}
}

func validateSpec(data []byte) (int, int, error) {
	var spec map[string]any
	if err := yaml.Unmarshal(data, &spec); err != nil {
		return 0, 0, err
	}
	if version, ok := spec["openapi"].(string); !ok || !strings.HasPrefix(version, "3.") {
		return 0, 0, fmt.Errorf("unsupported or missing openapi version: %v", spec["openapi"])
	}
	paths, ok := spec["paths"].(map[string]any)
	if !ok || len(paths) == 0 {
		return 0, 0, errors.New("paths must be a non-empty mapping")
	}
	components, ok := spec["components"].(map[string]any)
	if !ok {
		return 0, 0, errors.New("components must be a mapping")
	}
	schemas, ok := components["schemas"].(map[string]any)
	if !ok || len(schemas) == 0 {
		return 0, 0, errors.New("components.schemas must be a non-empty mapping")
	}

	for path, rawPathItem := range paths {
		pathItem, ok := rawPathItem.(map[string]any)
		if !ok {
			return 0, 0, fmt.Errorf("path %s must be a mapping", path)
		}
		for method, rawOperation := range pathItem {
			switch method {
			case "get", "post", "put", "patch", "delete":
			default:
				continue
			}
			operation, ok := rawOperation.(map[string]any)
			if !ok {
				return 0, 0, fmt.Errorf("%s %s must be a mapping", method, path)
			}
			if operation["summary"] == "" {
				return 0, 0, fmt.Errorf("%s %s missing summary", method, path)
			}
			if _, ok := operation["responses"].(map[string]any); !ok {
				return 0, 0, fmt.Errorf("%s %s missing responses", method, path)
			}
			for _, ref := range collectRefs(operation) {
				name, ok := strings.CutPrefix(ref, "#/components/schemas/")
				if ok {
					if _, exists := schemas[name]; !exists {
						return 0, 0, fmt.Errorf("%s %s references missing schema %q", method, path, name)
					}
					continue
				}
				if _, ok := strings.CutPrefix(ref, "#/components/responses/"); ok {
					continue
				}
				return 0, 0, fmt.Errorf("%s %s has unsupported ref %q", method, path, ref)
			}
		}
	}
	return len(paths), len(components), nil
}

func collectRefs(raw any) []string {
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
			refs = append(refs, collectRefs(value)...)
		}
		return refs
	case []any:
		var refs []string
		for _, item := range v {
			refs = append(refs, collectRefs(item)...)
		}
		return refs
	default:
		return nil
	}
}
