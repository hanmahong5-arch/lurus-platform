// Package web exposes the compiled frontend as an embedded filesystem.
package web

import "embed"

//go:embed all:dist
var Dist embed.FS
