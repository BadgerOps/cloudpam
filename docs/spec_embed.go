package docs

import _ "embed"

// OpenAPISpec contains the OpenAPI document embedded for runtime serving.
//
//go:embed openapi.yaml
var OpenAPISpec []byte
