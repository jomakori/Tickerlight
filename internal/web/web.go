// Package web holds the embedded static assets served by the console.
package web

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed static
var assets embed.FS

// Static returns the embedded static directory as a filesystem.
func Static() http.FileSystem {
	sub, err := fs.Sub(assets, "static")
	if err != nil {
		panic(err)
	}
	return http.FS(sub)
}
