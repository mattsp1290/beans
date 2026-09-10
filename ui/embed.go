// Package ui embeds the built Svelte application so bn serve ships it inside
// the binary. Run `make ui-build` to populate dist/; without it the placeholder
// index.html is embedded so `go build` never needs Node.
package ui

import "embed"

//go:embed all:dist
var Dist embed.FS
