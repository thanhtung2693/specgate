package postgres

import "embed"

//go:embed *.migration
var FS embed.FS
