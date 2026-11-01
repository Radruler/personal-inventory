// Package migrations embeds the numbered SQL migrations. New schema changes
// are added as NNN_name.sql and go through the coordinator (PLAN.md §10).
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
