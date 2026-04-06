//go:build web || all

// Package web embeds the SvelteKit static build output for serving from Go.
package web

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed static/*
var staticFiles embed.FS

// Handler returns an http.Handler that serves embedded static files.
// It strips the "static/" prefix and serves from the embedded filesystem.
// For SPA routing, unmatched paths fall back to index.html.
func Handler() http.Handler {
	sub, err := fs.Sub(staticFiles, "static")
	if err != nil {
		panic("web: embedded static files not found: " + err.Error())
	}
	fileServer := http.FileServer(http.FS(sub))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Try to serve the file directly
		path := r.URL.Path
		if path == "/" {
			path = "/index.html"
		}

		// Check if the file exists
		f, err := sub.Open(path[1:]) // strip leading /
		if err != nil {
			// SPA fallback: serve index.html for unmatched routes
			r.URL.Path = "/"
			fileServer.ServeHTTP(w, r)
			return
		}
		f.Close()

		fileServer.ServeHTTP(w, r)
	})
}
