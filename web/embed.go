package web

import _ "embed"

// Index holds the single‑page UI served at the root route.
//
//go:embed index.html
var Index []byte
