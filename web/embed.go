// Package web embeds the built Svelte frontend so the Go server can serve it as
// a single self-contained binary. Run `bun run build` in this directory to
// (re)generate the dist/ assets before building the server.
package web

import "embed"

// Dist holds the production build output (web/dist).
//
//go:embed all:dist
var Dist embed.FS
