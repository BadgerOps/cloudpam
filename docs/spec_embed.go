package docs

import _ "embed"

// Changelog contains the project changelog for in-app release notes.
//
//go:embed CHANGELOG.md
var Changelog []byte
