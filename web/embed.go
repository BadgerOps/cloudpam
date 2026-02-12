package web

import "embed"

// DistFS holds the Vite-built React SPA assets.
//
//go:embed all:dist
var DistFS embed.FS
