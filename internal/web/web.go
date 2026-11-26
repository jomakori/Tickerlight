// Package web holds the embedded static assets served by the console.
package web

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed static
var assets embed.FS

//go:embed static/sw.js
var serviceWorker []byte

// ServiceWorker returns the worker source, served from the app root so its
// scope covers every page rather than only /static/.
func ServiceWorker() []byte {
	return serviceWorker
}

// Static returns the embedded static directory as a filesystem.
func Static() http.FileSystem {
	sub, err := fs.Sub(assets, "static")
	if err != nil {
		panic(err)
	}
	return http.FS(sub)
}
