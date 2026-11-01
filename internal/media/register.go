// Package media owns uploads (streamed, sha256-deduplicated), the vips
// variant pipeline, media serving, links and the inbox (thread B).
package media

import (
	"net/http"

	"github.com/radruler/personal-inventory/internal/httpapi"
)

// Register mounts the media routes. The route list matches api/openapi.yaml.
func Register(mux *http.ServeMux, d *httpapi.Deps) {
	ni := httpapi.NotImplemented
	mux.HandleFunc("POST /api/media", ni)
	mux.HandleFunc("GET /api/media/sha256/{sha}", ni) // also answers HEAD
	mux.HandleFunc("GET /api/media/{id}", ni)
	mux.HandleFunc("PATCH /api/media/{id}", ni)
	mux.HandleFunc("DELETE /api/media/{id}", ni)
	mux.HandleFunc("POST /api/media/{id}/links", ni)
	mux.HandleFunc("DELETE /api/media/{id}/links", ni)
	mux.HandleFunc("GET /api/inbox", ni)

	mux.HandleFunc("GET /media/{sha}/{variant}", ni)
}
