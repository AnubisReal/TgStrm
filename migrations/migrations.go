// Package migrations embeds append-only schema migrations in the executable.
package migrations

import "embed"

//go:embed *.sql
var Files embed.FS
