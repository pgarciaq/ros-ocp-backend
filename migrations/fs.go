package migrations

import "embed"

// FS is the product golang-migrate tree (*.sql). robne embeds this for use case (c).
//
//go:embed *.sql
var FS embed.FS
