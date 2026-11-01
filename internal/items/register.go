// Package items owns items, products, observations, identifiers, facts, the
// attrs projection, tags, search, facets and the /api/schema registries
// (thread A).
package items

import (
	"net/http"

	"github.com/radruler/personal-inventory/internal/httpapi"
	"github.com/radruler/personal-inventory/internal/model"
)

type api struct{ d *httpapi.Deps }

// Register mounts the items routes. The route list matches api/openapi.yaml.
func Register(mux *http.ServeMux, d *httpapi.Deps) {
	a := &api{d: d}
	h := d.Handler
	mux.HandleFunc("GET /api/schema", h(a.getSchema))
	mux.HandleFunc("PUT /api/schema/fact-keys/{key}", h(a.putFactKey))
	mux.HandleFunc("PUT /api/schema/identifier-schemes/{scheme}", h(a.putIdentifierScheme))

	mux.HandleFunc("GET /api/items", h(a.listItems))
	mux.HandleFunc("POST /api/items", h(a.createItem))
	mux.HandleFunc("GET /api/items/{id}", h(a.getItem))
	mux.HandleFunc("PATCH /api/items/{id}", h(a.patchItem))
	mux.HandleFunc("DELETE /api/items/{id}", h(a.retireItem))
	mux.HandleFunc("DELETE /api/admin/items/{id}", h(a.hardDeleteItem))
	mux.HandleFunc("POST /api/items/{id}/observations", h(a.addObservation))
	mux.HandleFunc("POST /api/items/{id}/identifiers", h(a.addIdentifier(model.SubjectItem)))
	mux.HandleFunc("POST /api/items/{id}/facts", h(a.addFact(model.SubjectItem)))
	mux.HandleFunc("POST /api/items/{id}/references", h(a.addReference(model.SubjectItem)))
	mux.HandleFunc("POST /api/items/{id}/tags", h(a.editTags))

	mux.HandleFunc("POST /api/facts/{id}/supersede", h(a.supersedeFact))
	mux.HandleFunc("POST /api/facts/{id}/reject", h(a.rejectFact))

	mux.HandleFunc("GET /api/facets", h(a.getFacets))
	mux.HandleFunc("GET /api/identifiers/lookup", h(a.lookupIdentifier))

	mux.HandleFunc("GET /api/products", h(a.listProducts))
	mux.HandleFunc("POST /api/products", h(a.createProduct))
	mux.HandleFunc("GET /api/products/{id}", h(a.getProduct))
	mux.HandleFunc("PATCH /api/products/{id}", h(a.patchProduct))
	mux.HandleFunc("DELETE /api/products/{id}", h(a.deleteProduct))
	mux.HandleFunc("POST /api/products/{id}/identifiers", h(a.addIdentifier(model.SubjectProduct)))
	mux.HandleFunc("POST /api/products/{id}/facts", h(a.addFact(model.SubjectProduct)))
	mux.HandleFunc("POST /api/products/{id}/references", h(a.addReference(model.SubjectProduct)))
}
