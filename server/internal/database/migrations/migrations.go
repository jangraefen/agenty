// Package migrations embeds the goose SQL migrations of the agenty schema
// (ARCHITECTURE §11.1). Files are named <version>_<name>.sql with increasing
// versions; they contain DDL and data changes only, no triggers, functions,
// or procedures (ARCHITECTURE D5).
package migrations

import "embed"

// FS holds the migration files at its root.
//
//go:embed *.sql
var FS embed.FS
