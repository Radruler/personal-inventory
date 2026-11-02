// Package ingest owns the agent ingest bundle and export/import (thread G).
package ingest

import (
	"net/http"

	"github.com/radruler/personal-inventory/internal/httpapi"
)

// Register mounts the ingest routes. The route list matches api/openapi.yaml.
func Register(mux *http.ServeMux, d *httpapi.Deps) {
	h := &handlers{d: d}
	mux.HandleFunc("POST /api/ingest/bundle", d.Handler(h.applyBundle))
	mux.HandleFunc("GET /api/export", d.Handler(h.export))
}
