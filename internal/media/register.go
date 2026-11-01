// Package media owns uploads (streamed, sha256-deduplicated), the vips
// variant pipeline, media serving, links and the inbox (thread B).
//
// What the upload accepts, and the iOS note for the SPA's file input, are
// documented at the top of upload.go.
package media

import (
	"net/http"

	"github.com/radruler/personal-inventory/internal/httpapi"
)

// Register mounts the media routes. The route list matches api/openapi.yaml.
func Register(mux *http.ServeMux, d *httpapi.Deps) {
	h := &handlers{d: d}
	mux.HandleFunc("POST /api/media", d.Handler(h.upload))
	mux.HandleFunc("GET /api/media/sha256/{sha}", d.Handler(h.getBySha)) // also answers HEAD
	mux.HandleFunc("GET /api/media/{id}", d.Handler(h.get))
	mux.HandleFunc("PATCH /api/media/{id}", d.Handler(h.patch))
	mux.HandleFunc("DELETE /api/media/{id}", d.Handler(h.delete))
	mux.HandleFunc("POST /api/media/{id}/links", d.Handler(h.link))
	mux.HandleFunc("DELETE /api/media/{id}/links", d.Handler(h.unlink))
	mux.HandleFunc("GET /api/inbox", d.Handler(h.inbox))

	mux.HandleFunc("GET /media/{sha}/{variant}", d.Handler(h.serve))
}
