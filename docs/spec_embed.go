package docs

import _ "embed"

// OpenAPISpec contains the OpenAPI document embedded for runtime serving.
//
//go:embed openapi.yaml
var OpenAPISpec []byte

// Changelog contains the project changelog for in-app release notes.
//
//go:embed CHANGELOG.md
var Changelog []byte
