// Package pgmigrations embeds PostgreSQL migration files.
package pgmigrations

import "embed"

//go:embed *.sql
var Files embed.FS
