// Package migrations embeds the SQL migration files so binaries and tests can
// apply them without depending on the working directory.
package migrations

import "embed"

// FS contains all goose migration files.
//
//go:embed *.sql
var FS embed.FS
