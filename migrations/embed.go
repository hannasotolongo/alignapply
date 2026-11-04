// Package migrations embeds the SQL migrations so the server can apply them
// at startup.
package migrations

import "embed"

//go:embed *.sql
var Files embed.FS
