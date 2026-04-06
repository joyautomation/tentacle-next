//go:build !(web || all)

// Package web provides a stub when web assets are not embedded.
package web

import "net/http"

// Handler returns a handler that returns 404 when web is not compiled in.
func Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "web UI not included in this build", http.StatusNotFound)
	})
}
