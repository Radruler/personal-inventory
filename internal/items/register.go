// Package items owns items, products, observations, identifiers, facts, the
// attrs projection, tags, search, facets and the /api/schema registries
// (thread A).
package items

import (
	"net/http"

	"github.com/radruler/personal-inventory/internal/httpapi"
)

// Register mounts the items routes. The route list matches api/openapi.yaml.
func Register(mux *http.ServeMux, d *httpapi.Deps) {
	ni := httpapi.NotImplemented
	mux.HandleFunc("GET /api/schema", ni)
	mux.HandleFunc("PUT /api/schema/fact-keys/{key}", ni)
	mux.HandleFunc("PUT /api/schema/identifier-schemes/{scheme}", ni)

	mux.HandleFunc("GET /api/items", ni)
	mux.HandleFunc("POST /api/items", ni)
	mux.HandleFunc("GET /api/items/{id}", ni)
	mux.HandleFunc("PATCH /api/items/{id}", ni)
	mux.HandleFunc("DELETE /api/items/{id}", ni)
	mux.HandleFunc("DELETE /api/admin/items/{id}", ni)
	mux.HandleFunc("POST /api/items/{id}/observations", ni)
	mux.HandleFunc("POST /api/items/{id}/identifiers", ni)
	mux.HandleFunc("POST /api/items/{id}/facts", ni)
	mux.HandleFunc("POST /api/items/{id}/references", ni)
	mux.HandleFunc("POST /api/items/{id}/tags", ni)

	mux.HandleFunc("POST /api/facts/{id}/supersede", ni)
	mux.HandleFunc("POST /api/facts/{id}/reject", ni)

	mux.HandleFunc("GET /api/facets", ni)
	mux.HandleFunc("GET /api/identifiers/lookup", ni)

	mux.HandleFunc("GET /api/products", ni)
	mux.HandleFunc("POST /api/products", ni)
	mux.HandleFunc("GET /api/products/{id}", ni)
	mux.HandleFunc("PATCH /api/products/{id}", ni)
	mux.HandleFunc("DELETE /api/products/{id}", ni)
	mux.HandleFunc("POST /api/products/{id}/identifiers", ni)
	mux.HandleFunc("POST /api/products/{id}/facts", ni)
	mux.HandleFunc("POST /api/products/{id}/references", ni)
}
