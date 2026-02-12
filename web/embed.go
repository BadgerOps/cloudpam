package web

import (
	"embed"
	_ "embed"
)

// Index holds the single‑page UI served at the root route.
//
//go:embed index.html
var Index []byte

// DistFS holds the Vite-built wizard SPA assets.
//
//go:embed all:dist
var DistFS embed.FS
