// Package locations owns the location tree, contents, moves (items and whole
// boxes), staging, the movement log and placement-status derivation (thread C).
package locations

import (
	"net/http"

	"github.com/radruler/personal-inventory/internal/httpapi"
)

type handlers struct{ d *httpapi.Deps }

// Register mounts the locations routes. The route list matches api/openapi.yaml.
func Register(mux *http.ServeMux, d *httpapi.Deps) {
	h := &handlers{d: d}
	mux.HandleFunc("GET /api/locations", d.Handler(h.list))
	mux.HandleFunc("POST /api/locations", d.Handler(h.create))
	mux.HandleFunc("GET /api/locations/{id}", d.Handler(h.get))
	mux.HandleFunc("PATCH /api/locations/{id}", d.Handler(h.patch))
	mux.HandleFunc("DELETE /api/locations/{id}", d.Handler(h.archive))
	mux.HandleFunc("POST /api/moves", d.Handler(h.move))
	mux.HandleFunc("GET /api/staging", d.Handler(h.staging))
}
