// Package migrations embeds the versioned PostgreSQL schema so the binary
// carries it; internal/postgres applies it. See README.md for conventions.
package migrations

import "embed"

// FS holds every file in this directory; the migrator reads only the files
// named NNNN_description.sql.
//
//go:embed *
var FS embed.FS
