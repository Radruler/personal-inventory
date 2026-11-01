// Package ingest owns the agent ingest bundle and export/import (thread G).
package ingest

import (
	"net/http"

	"github.com/radruler/personal-inventory/internal/httpapi"
)

// Register mounts the ingest routes. The route list matches api/openapi.yaml.
func Register(mux *http.ServeMux, d *httpapi.Deps) {
	ni := httpapi.NotImplemented
	mux.HandleFunc("POST /api/ingest/bundle", ni)
	mux.HandleFunc("GET /api/export", ni)
}
