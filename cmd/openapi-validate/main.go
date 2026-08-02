package main

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const specURLTimeout = 15 * time.Second

// MaxSpecBytes caps how much of a spec is read into memory. The generated
// CloudPAM spec is well under a megabyte; 32MB leaves ample headroom while
// keeping a piped or fetched stream from exhausting memory.
const MaxSpecBytes int64 = 32 << 20

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

// readLimited reads at most MaxSpecBytes and reports an error rather than
// silently truncating, so a malformed or hostile source cannot be validated as
// a shorter document than it really is.
func readLimited(r io.Reader, what string) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, MaxSpecBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > MaxSpecBytes {
		return nil, fmt.Errorf("%s exceeds the %d byte spec limit", what, MaxSpecBytes)
	}
	return data, nil
}

func readSpec(source string) ([]byte, error) {
	switch {
	case source == "-":
		return readLimited(os.Stdin, "stdin")
	case strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "https://"):
		client := http.Client{Timeout: specURLTimeout}
		resp, err := client.Get(source)
		if err != nil {
			return nil, err
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode < 200 || resp.StatusCode > 299 {
			return nil, fmt.Errorf("GET %s returned %s", source, resp.Status)
		}
		return readLimited(resp.Body, "response body")
	default:
		// Reject an oversized file by its stat size before reading it in.
		if info, err := os.Stat(source); err == nil && info.Mode().IsRegular() && info.Size() > MaxSpecBytes {
			return nil, fmt.Errorf("%s is %d bytes, above the %d byte spec limit", source, info.Size(), MaxSpecBytes)
		}
		f, err := os.Open(source)
		if err != nil {
			return nil, err
		}
		defer func() { _ = f.Close() }()
		return readLimited(f, source)
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
	// components is optional: a spec may define every schema inline.
	components := map[string]any{}
	if raw, present := spec["components"]; present {
		components, ok = raw.(map[string]any)
		if !ok {
			return 0, 0, errors.New("components must be a mapping")
		}
	}

	for path, rawPathItem := range paths {
		pathItem, ok := rawPathItem.(map[string]any)
		if !ok {
			return 0, 0, fmt.Errorf("path %s must be a mapping", path)
		}
		for method, rawOperation := range pathItem {
			switch method {
			case "get", "post", "put", "patch", "delete", "options", "head", "trace":
			default:
				continue
			}
			operation, ok := rawOperation.(map[string]any)
			if !ok {
				return 0, 0, fmt.Errorf("%s %s must be a mapping", method, path)
			}
			summary, ok := operation["summary"].(string)
			if !ok || strings.TrimSpace(summary) == "" {
				return 0, 0, fmt.Errorf("%s %s missing summary", method, path)
			}
			if _, ok := operation["responses"].(map[string]any); !ok {
				return 0, 0, fmt.Errorf("%s %s missing responses", method, path)
			}
			for _, ref := range collectRefs(operation) {
				if err := checkComponentRef(components, method, path, ref); err != nil {
					return 0, 0, err
				}
			}
		}
	}
	return len(paths), len(components), nil
}

// componentNamespaces maps each supported components subsection to the noun used
// when reporting a reference to something it does not define.
var componentNamespaces = []struct {
	name string
	noun string
}{
	{name: "schemas", noun: "schema"},
	{name: "responses", noun: "response"},
	{name: "parameters", noun: "parameter"},
	{name: "requestBodies", noun: "request body"},
	{name: "headers", noun: "header"},
	{name: "securitySchemes", noun: "security scheme"},
}

// checkComponentRef resolves a local component reference against the spec's own
// components section. External and unknown-namespace refs are rejected, since
// this validator only reasons about a single self-contained document.
func checkComponentRef(components map[string]any, method, path, ref string) error {
	for _, ns := range componentNamespaces {
		name, ok := strings.CutPrefix(ref, "#/components/"+ns.name+"/")
		if !ok {
			continue
		}
		defined, _ := components[ns.name].(map[string]any)
		if _, exists := defined[name]; !exists {
			return fmt.Errorf("%s %s references missing %s %q", method, path, ns.noun, name)
		}
		return nil
	}
	return fmt.Errorf("%s %s has unsupported ref %q", method, path, ref)
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
