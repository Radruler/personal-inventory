// Package locations owns the location tree, contents, moves (items and whole
// boxes), staging, the movement log and placement-status derivation (thread C).
package locations

import (
	"net/http"

	"github.com/radruler/personal-inventory/internal/httpapi"
)

// Register mounts the locations routes. The route list matches api/openapi.yaml.
func Register(mux *http.ServeMux, d *httpapi.Deps) {
	ni := httpapi.NotImplemented
	mux.HandleFunc("GET /api/locations", ni)
	mux.HandleFunc("POST /api/locations", ni)
	mux.HandleFunc("GET /api/locations/{id}", ni)
	mux.HandleFunc("PATCH /api/locations/{id}", ni)
	mux.HandleFunc("DELETE /api/locations/{id}", ni)
	mux.HandleFunc("POST /api/moves", ni)
	mux.HandleFunc("GET /api/staging", ni)
}
