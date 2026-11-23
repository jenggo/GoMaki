// Package web embeds the built browser frontend.
package web

import "embed"

// Dist holds the committed Svelte build output.
//
//go:embed all:dist
var Dist embed.FS
