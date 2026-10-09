// Package webui embeds the built Angular application.
package webui

import (
	"embed"
	"io/fs"
)

// The Angular build writes to dist/browser (see web/angular.json).
// A placeholder index.html is committed so that the Go code builds without Node.js.
//
//go:embed all:dist/browser
var dist embed.FS

// FS returns the static files of the web application.
func FS() fs.FS {
	sub, err := fs.Sub(dist, "dist/browser")
	if err != nil {
		panic(err)
	}
	return sub
}
