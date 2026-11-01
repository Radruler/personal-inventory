package items

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/radruler/personal-inventory/internal/model"
	"github.com/radruler/personal-inventory/internal/testutil"
)

func TestSchemaAndRegistries(t *testing.T) {
	e := newEnv(t)
	var s SchemaInfo
	e.do("GET", "/api/schema", nil, 200, &s)
	if len(s.Kinds) != 3 || s.Kinds[0].Value != "clothing" || len(s.MediaRoles) == 0 || len(s.ObservationSources) == 0 {
		t.Fatalf("registries: %+v", s)
	}
	if strings.Join(s.GoneReasons, ",") != "sold,donated,discarded,lost,other" ||
		strings.Join(s.FactBases, ",") != "user,observed,looked_up,inferred" ||
		strings.Join(s.LocationKinds, ",") != "site,room,unit,shelf,container" {
		t.Fatalf("enums: %v %v %v", s.GoneReasons, s.FactBases, s.LocationKinds)
	}
	var brand *FactKey
	for i := range s.FactKeys {
		if s.FactKeys[i].Key == "brand" {
			brand = &s.FactKeys[i]
		}
	}
	if brand == nil || !brand.Filterable || brand.Kinds == nil || len(brand.Kinds) != 0 {
		t.Fatalf("brand fact key: %+v", brand)
	}
	raw := e.raw("GET", "/api/schema", nil, 200)
	fk := raw["fact_keys"].([]any)[0].(map[string]any)
	requireKeys(t, "fact_key", fk, "key", "label", "value_type", "kinds", "filterable", "sort", "description")
	mr := raw["media_roles"].([]any)[3].(map[string]any)
	requireKeys(t, "media_role", mr, "value", "label", "sort", "description", "cover_rank")
	if mr["cover_rank"] != nil {
		t.Errorf("label role should have null cover_rank: %v", mr)
	}

	// Create, then update with merge semantics.
	var k FactKey
	e.do("PUT", "/api/schema/fact-keys/shell.layers", map[string]any{"label": "Layers", "value_type": "number", "kinds": []string{"clothing"}, "filterable": true}, 200, &k)
	if k.Key != "shell.layers" || k.ValueType != "number" || !k.Filterable || k.Sort != 100 || k.Description != nil {
		t.Fatalf("put fact key: %+v", k)
	}
	e.do("PUT", "/api/schema/fact-keys/shell.layers", map[string]any{"label": "Shell layers", "description": "2L, 2.5L, 3L"}, 200, &k)
	if k.Label != "Shell layers" || k.ValueType != "number" || !k.Filterable || *k.Description != "2L, 2.5L, 3L" {
		t.Fatalf("merge update: %+v", k)
	}
	e.do("PUT", "/api/schema/fact-keys/x", map[string]any{"label": "X", "value_type": "colour"}, 422, nil)
	e.do("PUT", "/api/schema/fact-keys/x", map[string]any{"label": "X", "kinds": []string{"car"}}, 422, nil)
	e.do("PUT", "/api/schema/fact-keys/x", map[string]any{"label": ""}, 422, nil)
	e.do("PUT", "/api/schema/fact-keys/x", map[string]any{"label": "X", "filterabel": true}, 400, nil)

	var sc IdentifierScheme
	e.do("PUT", "/api/schema/identifier-schemes/uniqlo.product_code", map[string]any{"label": "Uniqlo code", "notes": "3xx-xxxxxx"}, 200, &sc)
	if sc.Label != "Uniqlo code" || sc.Brand == nil || *sc.Brand != "Uniqlo" || *sc.Notes != "3xx-xxxxxx" {
		t.Fatalf("scheme update kept brand and set notes: %+v", sc)
	}
	e.do("PUT", "/api/schema/identifier-schemes/acme.sku", map[string]any{"label": "Acme SKU", "brand": "Acme"}, 200, &sc)
	if sc.Scheme != "acme.sku" || sc.Pattern != nil {
		t.Fatalf("new scheme: %+v", sc)
	}
}

func TestProjectionPrecedence(t *testing.T) {
	e := newEnv(t)
	it := e.item(map[string]any{"kind": "clothing", "title": "Shell"})
	id := it.ID

	// Basis beats confidence.
	e.fact("item", id, map[string]any{"key": "color", "value": "black", "basis": "inferred", "confidence": 1})
	fw := e.fact("item", id, map[string]any{"key": "color", "value": "navy", "basis": "user", "confidence": 0.3})
	if fw.Attrs["color"] != "navy" {
		t.Fatalf("user basis should beat inferred: %v", fw.Attrs)
	}
	userColor := fw.Fact.ID
	fw = e.fact("item", id, map[string]any{"key": "color", "value": "blue", "basis": "observed", "confidence": 1})
	if fw.Attrs["color"] != "navy" {
		t.Fatalf("observed must not beat user: %v", fw.Attrs)
	}

	// Confidence beats recency.
	e.fact("item", id, map[string]any{"key": "size", "value": "M", "basis": "observed", "confidence": 0.9})
	fw = e.fact("item", id, map[string]any{"key": "size", "value": "L", "basis": "observed", "confidence": 0.5})
	if fw.Attrs["size"] != "M" {
		t.Fatalf("higher confidence should win over newer: %v", fw.Attrs)
	}
	// Recency breaks ties.
	fw = e.fact("item", id, map[string]any{"key": "size", "value": "S", "basis": "observed", "confidence": 0.9})
	if fw.Attrs["size"] != "S" {
		t.Fatalf("newest should win a tie: %v", fw.Attrs)
	}
	sizeS := fw.Fact.ID

	// Rejected facts are ignored; rejecting twice is a conflict.
	var rw FactWrite
	e.do("POST", "/api/facts/"+sizeS+"/reject", map[string]any{"note": "misread"}, 200, &rw)
	if rw.Fact.Status != "rejected" || *rw.Fact.Note != "misread" || rw.Attrs["size"] != "M" {
		t.Fatalf("reject: %+v", rw)
	}
	e.do("POST", "/api/facts/"+sizeS+"/reject", nil, http.StatusConflict, nil)
	e.do("POST", "/api/facts/"+sizeS+"/supersede", map[string]any{"value": "XS", "basis": "user"}, http.StatusConflict, nil)
	e.do("POST", "/api/facts/nope/reject", nil, http.StatusNotFound, nil)
	e.do("POST", "/api/facts/"+userColor+"/reject", map[string]any{"bogus": 1}, http.StatusBadRequest, nil)

	// Supersede chain: superseded facts are ignored, even when the new one
	// has a weaker basis.
	var s1 FactWrite
	e.do("POST", "/api/facts/"+userColor+"/supersede", map[string]any{"value": "midnight", "basis": "inferred", "confidence": 0.2, "note": "rechecked"}, 201, &s1)
	// black (inferred 1.0), blue (observed) and midnight (inferred 0.2) are active: observed wins.
	if s1.Fact.Key != "color" || *s1.Fact.SupersedesID != userColor || s1.Attrs["color"] != "blue" {
		t.Fatalf("supersede 1: %+v", s1)
	}
	var s2 FactWrite
	e.do("POST", "/api/facts/"+s1.Fact.ID+"/supersede", map[string]any{"value": "navy", "basis": "user"}, 201, &s2)
	if s2.Attrs["color"] != "navy" || *s2.Fact.SupersedesID != s1.Fact.ID {
		t.Fatalf("supersede 2: %+v", s2)
	}
	e.do("POST", "/api/facts/"+s1.Fact.ID+"/supersede", map[string]any{"value": "x", "basis": "user"}, http.StatusConflict, nil)
	var d ItemDetail
	e.do("GET", "/api/items/"+id, nil, 200, &d)
	status := map[string]string{}
	for _, f := range d.Facts {
		status[f.ID] = f.Status
	}
	if status[userColor] != "superseded" || status[s1.Fact.ID] != "superseded" || status[s2.Fact.ID] != "active" {
		t.Fatalf("chain statuses: %v", status)
	}
	if d.Facts[0].ID != s2.Fact.ID {
		t.Errorf("facts should be newest first")
	}
	if d.AttrSources["color"].FactID != s2.Fact.ID || d.AttrSources["color"].SubjectType != "item" {
		t.Errorf("attr_sources color: %+v", d.AttrSources)
	}
	// Supersede may move a value to another key.
	var s3 FactWrite
	e.do("POST", "/api/facts/"+s2.Fact.ID+"/supersede", map[string]any{"key": "shell.color", "value": "navy", "basis": "user"}, 201, &s3)
	if _, ok := s3.Attrs["shell.color"]; !ok || s3.Attrs["color"] != "blue" {
		t.Fatalf("supersede to new key: %v", s3.Attrs)
	}

	// Product facts fill keys the item lacks; item facts win.
	var p ProductDetail
	e.do("POST", "/api/products", map[string]any{"brand": "Uniqlo", "name": "Blocktech Parka"}, 201, &p)
	e.fact("product", p.ID, map[string]any{"key": "season", "value": "2023 Spring/Summer", "basis": "inferred", "confidence": 0.8})
	pfw := e.fact("product", p.ID, map[string]any{"key": "size", "value": "XL", "basis": "user"})
	if pfw.Attrs["season"] != "2023 Spring/Summer" || pfw.Attrs["size"] != "XL" {
		t.Fatalf("product attrs: %v", pfw.Attrs)
	}
	e.do("PATCH", "/api/items/"+id, map[string]any{"product_id": p.ID}, 200, &d)
	if d.Attrs["season"] != "2023 Spring/Summer" || d.Attrs["size"] != "M" {
		t.Fatalf("product fill-in after linking: %v", d.Attrs)
	}
	if d.AttrSources["season"].SubjectType != "product" || d.AttrSources["size"].SubjectType != "item" {
		t.Fatalf("attr_sources: %+v", d.AttrSources)
	}
	if d.Product == nil || d.Product.ID != p.ID || d.Product.Items == nil || len(d.Product.Items) != 0 {
		t.Fatalf("nested product: %+v", d.Product)
	}
	// A product fact change recomputes its items.
	e.fact("product", p.ID, map[string]any{"key": "retail_price", "value": map[string]any{"amount": 79.9, "currency": "USD"}, "basis": "looked_up", "confidence": 0.7})
	e.do("GET", "/api/items/"+id, nil, 200, &d)
	if rp, ok := d.Attrs["retail_price"].(map[string]any); !ok || rp["amount"] != 79.9 {
		t.Fatalf("item not recomputed after product fact: %v", d.Attrs)
	}
	// Unlinking the product drops the filled keys.
	d = ItemDetail{} // decoding into a used struct would merge maps
	e.do("PATCH", "/api/items/"+id, map[string]any{"product_id": nil}, 200, &d)
	if _, ok := d.Attrs["season"]; ok || d.Product != nil {
		t.Fatalf("unlink product: %v", d.Attrs)
	}

	// Validation.
	e.do("POST", "/api/items/"+id+"/facts", map[string]any{"key": "color", "value": "x", "basis": "guess"}, 422, nil)
	e.do("POST", "/api/items/"+id+"/facts", map[string]any{"key": "color", "value": "x", "basis": "user", "confidence": 2}, 422, nil)
	e.do("POST", "/api/items/"+id+"/facts", map[string]any{"key": "color", "basis": "user"}, 422, nil)
	e.do("POST", "/api/items/"+id+"/facts", map[string]any{"key": "bad key", "value": 1, "basis": "user"}, 422, nil)
	e.do("POST", "/api/items/"+id+"/facts", map[string]any{"key": "c", "value": 1, "basis": "user", "sources": []any{map[string]any{"observation": 0}}}, 422, nil)
	e.do("POST", "/api/items/"+id+"/facts", map[string]any{"key": "c", "value": 1, "basis": "user", "sources": []any{map[string]any{"obs": "x"}}}, 400, nil)
	e.do("POST", "/api/items/nope/facts", map[string]any{"key": "c", "value": 1, "basis": "user"}, 404, nil)
}

func TestCreateNested(t *testing.T) {
	e := newEnv(t)
	home := e.loc("HOME", "Home", "site", "")
	bed := e.loc("BEDROOM", "Bedroom", "room", home)
	ss := e.loc("SS", "StoreSafe", "site", "")
	u2 := e.loc("U2", "Unit 2", "unit", ss)
	m1, m2 := e.media("image/jpeg"), e.media("image/jpeg")

	raw := e.raw("POST", "/api/items", map[string]any{
		"kind": "clothing", "title": "Navy shell jacket", "category": "outerwear/shell",
		"location": "code:bedroom", "target_location": "code:U2", "tags": []string{"rain", "Rain", " "},
		"ext_key": "batch/jacket-01", "review_state": "needs_review", "recorded_by": "agent:test",
		"media": []any{map[string]any{"media_id": m1, "role": "flat_lay"}, map[string]any{"media_id": m2, "role": "care_label"}},
		"observations": []any{
			map[string]any{"source": "care_label", "media_id": m2, "text": "SHELL: 100% NYLON\nRN 12345\n341-456789(23SS)",
				"data": map[string]any{"codes": []string{"RN 12345"}}, "region": map[string]any{"x": 0.1, "y": 0.2, "w": 0.5, "h": 0.5}},
			map[string]any{"source": "hang_tag", "text": "BLOCKTECH"},
		},
		"identifiers": []any{
			map[string]any{"scheme": "rn", "value": "12345", "observation": 0},
			map[string]any{"scheme": "uniqlo.product_code", "value": "341-456789", "observation": 1},
		},
		"facts": []any{
			map[string]any{"key": "brand", "value": "Uniqlo", "basis": "observed", "confidence": 1,
				"sources": []any{map[string]any{"observation": 0}, map[string]any{"reference": 0}, map[string]any{"note": "label"}}},
		},
		"references": []any{map[string]any{"url": "https://example.com/p", "kind": "product_page", "fetched_at": "2026-11-01T10:00:00Z"}},
	}, 201)
	requireKeys(t, "ItemDetail", raw, "id", "kind", "title", "category", "quantity", "lifecycle", "review_state", "cover",
		"placement", "attrs", "tags", "product_id", "ext_key", "created_at", "updated_at", "notes", "parent_item_id",
		"location_id", "target_location_id", "cover_media_id", "gone_reason", "gone_at", "attr_sources", "media",
		"observations", "identifiers", "facts", "references", "movements", "product", "parent", "children")
	obs0 := raw["observations"].([]any)[0].(map[string]any)
	requireKeys(t, "Observation", obs0, "id", "item_id", "source", "media_id", "media", "region", "text", "data", "recorded_by", "recorded_at", "ext_key")
	ml := raw["media"].([]any)[0].(map[string]any)
	requireKeys(t, "MediaLinkView", ml, "media", "role", "sort", "caption")
	requireKeys(t, "Media", ml["media"].(map[string]any), "id", "sha256", "mime", "width", "height", "variant_status",
		"thumb_url", "display_url", "orig_url", "ext", "bytes", "taken_at", "original_name", "exif", "context", "variant_error", "created_at", "links")
	mv := raw["movements"].([]any)[0].(map[string]any)
	requireKeys(t, "Movement", mv, "id", "item_id", "location_moved_id", "kind", "from", "to", "at", "note", "actor")
	ref := raw["references"].([]any)[0].(map[string]any)
	requireKeys(t, "Reference", ref, "id", "subject_type", "subject_id", "url", "title", "kind", "archive_media_id", "archive",
		"fetched_at", "notes", "recorded_by", "recorded_at", "ext_key")
	if raw["children"] == nil || raw["product"] != nil || raw["parent"] != nil {
		t.Errorf("children must be [], product/parent null: %v %v %v", raw["children"], raw["product"], raw["parent"])
	}

	var d ItemDetail
	e.do("GET", "/api/items/"+raw["id"].(string), nil, 200, &d)
	if d.Placement.Status != model.PlacementStaged || d.Placement.Label != "Bedroom → going to Unit 2" || *d.LocationID != bed || *d.TargetLocationID != u2 {
		t.Fatalf("placement: %+v", d.Placement)
	}
	if strings.Join(d.Tags, ",") != "rain" || d.ReviewState != "needs_review" || *d.ExtKey != "batch/jacket-01" {
		t.Fatalf("tags/review/ext: %v %s", d.Tags, d.ReviewState)
	}
	if len(d.Media) != 2 || d.Media[0].Role != "flat_lay" || d.Media[0].Media.ID != m1 || len(d.Media[0].Media.Links) != 1 {
		t.Fatalf("media: %+v", d.Media)
	}
	if d.Cover == nil || d.Cover.ID != m1 || d.CoverMediaID != nil {
		t.Fatalf("auto cover: %+v %v", d.Cover, d.CoverMediaID)
	}
	if len(d.Observations) != 2 || d.Observations[0].Media == nil || d.Observations[0].Media.ID != m2 || d.Observations[1].Media != nil {
		t.Fatalf("observations: %+v", d.Observations)
	}
	jsonEq(t, d.Observations[0].Region, `{"x":0.1,"y":0.2,"w":0.5,"h":0.5}`)
	jsonEq(t, d.Observations[1].Data, `{}`)
	if d.Observations[0].RecordedBy != "agent:test" {
		t.Errorf("recorded_by: %s", d.Observations[0].RecordedBy)
	}
	if len(d.Identifiers) != 2 || *d.Identifiers[0].ObservationID != d.Observations[0].ID ||
		*d.Identifiers[1].ObservationID != d.Observations[1].ID || d.Identifiers[1].ValueNorm != "341456789" {
		t.Fatalf("identifiers: %+v", d.Identifiers)
	}
	jsonEq(t, d.Facts[0].Sources, `[{"observation_id":"`+d.Observations[0].ID+`"},{"reference_id":"`+d.References[0].ID+`"},{"note":"label"}]`)
	if d.Attrs["brand"] != "Uniqlo" || *d.References[0].FetchedAt != "2026-11-01T10:00:00.000Z" {
		t.Fatalf("attrs/ref: %v %v", d.Attrs, d.References[0].FetchedAt)
	}
	// Movements newest first: stage after move (same timestamp, later id).
	if len(d.Movements) != 2 || d.Movements[0].Kind != "stage" || d.Movements[1].Kind != "move" ||
		d.Movements[1].From != nil || d.Movements[1].To.ID != bed || d.Movements[0].From.ID != bed || d.Movements[0].To.Label != "Unit 2" {
		t.Fatalf("movements: %+v", d.Movements)
	}

	// Bad nested index: 422, nothing written.
	before := e.count(`SELECT count(*) FROM item`)
	e.do("POST", "/api/items", map[string]any{"kind": "object", "title": "X", "observations": []any{map[string]any{"text": "a"}},
		"identifiers": []any{map[string]any{"scheme": "sku", "value": "1", "observation": 1}}}, 422, nil)
	e.do("POST", "/api/items", map[string]any{"kind": "object", "title": "X",
		"facts": []any{map[string]any{"key": "k", "value": 1, "basis": "user", "sources": []any{map[string]any{"reference": 0}}}}}, 422, nil)
	e.do("POST", "/api/items", map[string]any{"kind": "object", "title": "X", "location": "code:NOPE"}, 422, nil)
	e.do("POST", "/api/items", map[string]any{"kind": "object", "title": "X", "media": []any{map[string]any{"media_id": "nope"}}}, 422, nil)
	e.do("POST", "/api/items", map[string]any{"kind": "car", "title": "X"}, 422, nil)
	e.do("POST", "/api/items", map[string]any{"kind": "object", "title": "  "}, 422, nil)
	e.do("POST", "/api/items", map[string]any{"kind": "object", "title": "X", "quantity": 0}, 422, nil)
	e.do("POST", "/api/items", map[string]any{"kind": "object", "title": "X", "observations": []any{map[string]any{"region": map[string]any{"x": 2, "y": 0, "w": 0, "h": 0}}}}, 422, nil)
	if n := e.count(`SELECT count(*) FROM item`); n != before || e.count(`SELECT count(*) FROM observation`) != 2 {
		t.Fatalf("failed creates wrote rows: %d items", n)
	}

	// Created already inside its target: nothing to stage.
	var in ItemDetail
	e.do("POST", "/api/items", map[string]any{"kind": "object", "title": "Boxed", "location": u2, "target_location": "code:SS"}, 201, &in)
	if in.TargetLocationID != nil || len(in.Movements) != 1 || in.Placement.Status != "approximate" {
		t.Fatalf("inside target: %+v %+v", in.TargetLocationID, in.Placement)
	}

	// Minimal item: no location means unknown, no movement.
	var m ItemDetail
	e.do("POST", "/api/items", map[string]any{"kind": "object", "title": "Lamp"}, 201, &m)
	if m.Placement.Status != "unknown" || len(m.Movements) != 0 || m.Quantity != 1 || m.Tags == nil || m.Media == nil {
		t.Fatalf("minimal: %+v", m)
	}
}

func TestExtKeyConflictAndUnknownFields(t *testing.T) {
	e := newEnv(t)
	first := e.item(map[string]any{"kind": "object", "title": "A", "ext_key": "k1"})
	var er struct {
		Error struct {
			Code    string         `json:"code"`
			Details map[string]any `json:"details"`
		} `json:"error"`
	}
	e.do("POST", "/api/items", map[string]any{"kind": "object", "title": "B", "ext_key": "k1"}, http.StatusConflict, &er)
	if er.Error.Code != "conflict" || er.Error.Details["id"] != first.ID {
		t.Fatalf("409 details: %+v", er)
	}
	second := e.item(map[string]any{"kind": "object", "title": "B"})
	e.do("PATCH", "/api/items/"+second.ID, map[string]any{"ext_key": "k1"}, http.StatusConflict, nil)
	e.do("PATCH", "/api/items/"+first.ID, map[string]any{"ext_key": "k1"}, http.StatusOK, nil) // own key is fine

	for name, body := range map[string]any{
		"top-level typo":    map[string]any{"kind": "object", "title": "X", "colour": "red"},
		"nested typo":       map[string]any{"kind": "object", "title": "X", "observations": []any{map[string]any{"txt": "a"}}},
		"identifier typo":   map[string]any{"kind": "object", "title": "X", "identifiers": []any{map[string]any{"scheme": "a", "value": "b", "obs": 0}}},
		"media link typo":   map[string]any{"kind": "object", "title": "X", "media": []any{map[string]any{"id": "x"}}},
		"location_id field": map[string]any{"kind": "object", "title": "X", "location_id": "x"},
	} {
		rec := testutil.Do(t, e.h, "POST", "/api/items", body, nil)
		if rec.Code != 400 || !strings.Contains(rec.Body.String(), "bad_request") {
			t.Errorf("%s: %d %s", name, rec.Code, rec.Body.String())
		}
	}
	e.do("PATCH", "/api/items/"+first.ID, map[string]any{"location": "code:X"}, 400, nil)
	e.do("PATCH", "/api/items/"+first.ID, map[string]any{"target_location": nil}, 400, nil)
	e.do("POST", "/api/items/"+first.ID+"/identifiers", map[string]any{"scheme": "a", "value": "b", "observation": 0}, 400, nil)
	e.do("POST", "/api/items/"+first.ID+"/tags", map[string]any{"added": []string{"x"}}, 400, nil)
	e.do("POST", "/api/items", []byte(`{"kind":"object","title":"X"} {}`), 400, nil)
	e.do("POST", "/api/items", []byte(``), 400, nil)
}

func TestPatchRules(t *testing.T) {
	e := newEnv(t)
	it := e.item(map[string]any{"kind": "clothing", "title": "Coat", "tags": []string{"a", "b"}, "notes": "warm"})
	id := it.ID
	var d ItemDetail
	e.do("PATCH", "/api/items/"+id, map[string]any{"title": "Wool coat", "tags": []string{"winter"}, "notes": nil, "quantity": 2, "category": "outerwear/coat"}, 200, &d)
	if d.Title != "Wool coat" || strings.Join(d.Tags, ",") != "winter" || d.Notes != nil || d.Quantity != 2 || *d.Category != "outerwear/coat" {
		t.Fatalf("patch fields: %+v", d)
	}
	if d.UpdatedAt < it.UpdatedAt {
		t.Errorf("updated_at went backwards")
	}
	if l := e.list("q=wool"); l.Total != 1 {
		t.Fatalf("patched title not reindexed: %v", titles(l))
	}
	if l := e.list("q=warm"); l.Total != 0 {
		t.Fatalf("cleared notes still indexed")
	}

	// lifecycle gone sets gone_at; owned clears gone fields.
	e.do("PATCH", "/api/items/"+id, map[string]any{"lifecycle": "gone", "gone_reason": "sold"}, 200, &d)
	if d.Lifecycle != "gone" || d.GoneAt == nil || *d.GoneReason != "sold" || d.Placement.Status != "gone" || !strings.HasPrefix(d.Placement.Label, "Sold · ") {
		t.Fatalf("gone: %+v", d.Placement)
	}
	e.do("PATCH", "/api/items/"+id, map[string]any{"gone_at": "2026-03-04T05:06:07Z"}, 200, &d)
	if *d.GoneAt != "2026-03-04T05:06:07.000Z" || d.Placement.Label != "Sold · 2026-03" {
		t.Fatalf("gone_at: %v %s", d.GoneAt, d.Placement.Label)
	}
	e.do("PATCH", "/api/items/"+id, map[string]any{"lifecycle": "owned"}, 200, &d)
	if d.Lifecycle != "owned" || d.GoneAt != nil || d.GoneReason != nil {
		t.Fatalf("owned should clear gone fields: %+v", d)
	}
	e.do("PATCH", "/api/items/"+id, map[string]any{"gone_reason": "lost"}, 422, nil)
	e.do("PATCH", "/api/items/"+id, map[string]any{"lifecycle": "gone", "gone_at": "2025-01-01T00:00:00.000Z"}, 200, &d)
	if *d.GoneAt != "2025-01-01T00:00:00.000Z" || d.GoneReason != nil {
		t.Fatalf("given gone_at: %+v", d)
	}
	e.do("PATCH", "/api/items/"+id, map[string]any{"lifecycle": "owned"}, 200, nil)

	// Validation and references.
	for _, body := range []map[string]any{
		{"title": nil}, {"title": ""}, {"kind": "car"}, {"kind": nil}, {"quantity": 0}, {"review_state": "maybe"},
		{"lifecycle": "lost"}, {"gone_at": "yesterday", "lifecycle": "gone"}, {"product_id": "nope"},
		{"parent_item_id": id}, {"parent_item_id": "nope"}, {"cover_media_id": "nope"}, {"tags": nil},
	} {
		rec := testutil.Do(t, e.h, "PATCH", "/api/items/"+id, body, nil)
		if rec.Code != 422 {
			t.Errorf("PATCH %v: %d %s", body, rec.Code, rec.Body.String())
		}
	}
	e.do("PATCH", "/api/items/"+id, map[string]any{}, 400, nil)
	e.do("PATCH", "/api/items/nope", map[string]any{"title": "x"}, 404, nil)

	// Sets: parent/children, no cycles.
	child := e.item(map[string]any{"kind": "clothing", "title": "Trousers"})
	e.do("PATCH", "/api/items/"+child.ID, map[string]any{"parent_item_id": id}, 200, &d)
	if d.Parent == nil || d.Parent.ID != id {
		t.Fatalf("parent: %+v", d.Parent)
	}
	e.do("PATCH", "/api/items/"+id, map[string]any{"parent_item_id": child.ID}, 422, nil)
	e.do("GET", "/api/items/"+id, nil, 200, &d)
	if len(d.Children) != 1 || d.Children[0].ID != child.ID {
		t.Fatalf("children: %+v", d.Children)
	}

	// Explicit cover.
	m := e.media("image/png")
	e.do("PATCH", "/api/items/"+id, map[string]any{"cover_media_id": m, "review_state": "ok"}, 200, &d)
	if d.Cover == nil || d.Cover.ID != m || *d.CoverMediaID != m {
		t.Fatalf("cover: %+v", d.Cover)
	}
}

func TestKnowledgeEndpoints(t *testing.T) {
	e := newEnv(t)
	a := e.item(map[string]any{"kind": "clothing", "title": "Shirt"})
	b := e.item(map[string]any{"kind": "clothing", "title": "Other"})
	mid := e.media("image/heic")

	raw := e.raw("POST", "/api/items/"+a.ID+"/observations", map[string]any{"source": "care_label", "media_id": mid, "text": "100% LINEN\nMADE IN CHINA"}, 201)
	requireKeys(t, "Observation", raw, "id", "item_id", "source", "media_id", "region", "text", "data", "recorded_by", "recorded_at", "ext_key")
	obsID := raw["id"].(string)
	if raw["region"] != nil || raw["recorded_by"] != "user" {
		t.Errorf("observation: %v", raw)
	}
	e.do("POST", "/api/items/"+a.ID+"/observations", map[string]any{"data": []int{1}}, 422, nil)
	e.do("POST", "/api/items/"+a.ID+"/observations", map[string]any{"media_id": "nope"}, 422, nil)
	e.do("POST", "/api/items/nope/observations", map[string]any{"text": "x"}, 404, nil)
	if l := e.list("q=linen"); l.Total != 1 {
		t.Fatalf("observation text not searchable")
	}

	var idf Identifier
	e.do("POST", "/api/items/"+a.ID+"/identifiers", map[string]any{"scheme": "rn", "value": "rn 12-345", "observation_id": obsID}, 201, &idf)
	if idf.ValueNorm != "RN12345" || *idf.ObservationID != obsID || idf.SubjectType != "item" {
		t.Fatalf("identifier: %+v", idf)
	}
	e.do("POST", "/api/items/"+b.ID+"/identifiers", map[string]any{"scheme": "rn", "value": "1", "observation_id": obsID}, 422, nil)
	e.do("POST", "/api/items/"+a.ID+"/identifiers", map[string]any{"scheme": "", "value": "1"}, 422, nil)
	e.do("POST", "/api/items/"+a.ID+"/identifiers", map[string]any{"scheme": "rn", "value": "--"}, 422, nil)

	var ref Reference
	req := httptestReq(e, "POST", "/api/items/"+a.ID+"/references", map[string]any{"url": "https://x.test", "title": "Page"}, "agent:claude")
	if req.Code != 201 {
		t.Fatalf("reference: %d %s", req.Code, req.Body.String())
	}
	decode(t, req.Body.Bytes(), &ref)
	if ref.Kind != "other" || ref.RecordedBy != "agent:claude" || ref.FetchedAt != nil {
		t.Fatalf("reference defaults/X-Actor: %+v", ref)
	}
	e.do("POST", "/api/items/"+a.ID+"/references", map[string]any{"fetched_at": "last week"}, 422, nil)

	var ts TagSet
	e.do("POST", "/api/items/"+a.ID+"/tags", map[string]any{"add": []string{"summer", "Linen", "summer"}}, 200, &ts)
	if strings.Join(ts.Tags, ",") != "Linen,summer" {
		t.Fatalf("tags add: %v", ts.Tags)
	}
	e.do("POST", "/api/items/"+a.ID+"/tags", map[string]any{"add": []string{"beach"}, "remove": []string{"LINEN"}}, 200, &ts)
	if strings.Join(ts.Tags, ",") != "beach,summer" {
		t.Fatalf("tags remove (case-insensitive): %v", ts.Tags)
	}
	e.do("POST", "/api/items/nope/tags", map[string]any{"add": []string{"x"}}, 404, nil)
}

func TestIdentifierLookup(t *testing.T) {
	for in, want := range map[string]string{"341-456789": "341456789", "rn 12345": "RN12345", "a.b/c_d\te": "ABCDE", " 23ss ": "23SS"} {
		if got := NormIdentifier(in); got != want {
			t.Errorf("NormIdentifier(%q) = %q, want %q", in, got, want)
		}
	}
	e := newEnv(t)
	it := e.item(map[string]any{"kind": "clothing", "title": "Jacket", "identifiers": []any{map[string]any{"scheme": "uniqlo.product_code", "value": "341-456789"}}})
	var p ProductDetail
	e.do("POST", "/api/products", map[string]any{"name": "Parka"}, 201, &p)
	e.do("POST", "/api/products/"+p.ID+"/identifiers", map[string]any{"scheme": "upc", "value": "341 456 789"}, 201, nil)

	var lk IdentifierLookup
	e.do("GET", "/api/identifiers/lookup?value=341.456.789", nil, 200, &lk)
	if lk.ValueNorm != "341456789" || len(lk.Matches) != 2 || lk.Matches[0].Item == nil || lk.Matches[0].Item.ID != it.ID ||
		lk.Matches[1].Product == nil || lk.Matches[1].Product.ID != p.ID || lk.Matches[1].Item != nil {
		t.Fatalf("lookup: %+v", lk)
	}
	raw := e.raw("GET", "/api/identifiers/lookup?value=341456789&scheme=upc", nil, 200)
	ms := raw["matches"].([]any)
	if len(ms) != 1 {
		t.Fatalf("scheme filter: %v", ms)
	}
	requireKeys(t, "IdentifierMatch", ms[0].(map[string]any), "identifier", "item", "product")
	e.do("GET", "/api/identifiers/lookup", nil, 400, nil)
	e.do("GET", "/api/identifiers/lookup?value=nothing", nil, 200, &lk)
	if lk.Matches == nil || len(lk.Matches) != 0 {
		t.Fatalf("no matches must be []: %+v", lk)
	}
}

func TestSoftVsHardDelete(t *testing.T) {
	e := newEnv(t)
	u := e.loc("U1", "Unit 1", "unit", "")
	m := e.media("image/jpeg")
	parent := e.item(map[string]any{"kind": "clothing", "title": "Suit jacket", "location": u, "tags": []string{"formal"},
		"media":        []any{map[string]any{"media_id": m, "role": "flat_lay"}},
		"observations": []any{map[string]any{"text": "SUPER 110S"}},
		"identifiers":  []any{map[string]any{"scheme": "style", "value": "SJ-1", "observation": 0}},
		"facts":        []any{map[string]any{"key": "color", "value": "navy", "basis": "user"}},
		"references":   []any{map[string]any{"url": "https://x.test"}}})
	child := e.item(map[string]any{"kind": "clothing", "title": "Suit trousers", "parent_item_id": parent.ID})

	// Soft delete.
	var d ItemDetail
	e.do("DELETE", "/api/items/"+parent.ID+"?reason=donated", nil, 200, &d)
	if d.Lifecycle != "gone" || *d.GoneReason != "donated" || d.GoneAt == nil || d.Placement.Status != "gone" {
		t.Fatalf("soft delete: %+v", d)
	}
	goneAt := *d.GoneAt
	e.do("DELETE", "/api/items/"+parent.ID, nil, 200, &d)
	if *d.GoneAt != goneAt || *d.GoneReason != "donated" {
		t.Fatalf("second soft delete changed gone fields: %v %v", d.GoneAt, d.GoneReason)
	}
	if l := e.list(""); l.Total != 1 || l.Items[0].ID != child.ID {
		t.Fatalf("default list hides gone: %v", titles(l))
	}
	if l := e.list("lifecycle=gone"); l.Total != 1 || l.Items[0].ID != parent.ID {
		t.Fatalf("lifecycle=gone: %v", titles(l))
	}
	if l := e.list("lifecycle=all"); l.Total != 2 {
		t.Fatalf("lifecycle=all: %v", titles(l))
	}
	e.do("DELETE", "/api/items/nope", nil, 404, nil)

	// Hard delete.
	e.do("DELETE", "/api/admin/items/"+parent.ID, nil, 204, nil)
	e.do("GET", "/api/items/"+parent.ID, nil, 404, nil)
	e.do("DELETE", "/api/admin/items/"+parent.ID, nil, 404, nil)
	for table, q := range map[string]string{
		"observation": `SELECT count(*) FROM observation WHERE item_id = ?`,
		"identifier":  `SELECT count(*) FROM identifier WHERE subject_id = ?`,
		"fact":        `SELECT count(*) FROM fact WHERE subject_id = ?`,
		"reference":   `SELECT count(*) FROM reference WHERE subject_id = ?`,
		"tag":         `SELECT count(*) FROM tag WHERE item_id = ?`,
		"media_link":  `SELECT count(*) FROM media_link WHERE subject_id = ?`,
		"movement":    `SELECT count(*) FROM movement WHERE item_id = ?`,
		"item_fts":    `SELECT count(*) FROM item_fts WHERE item_id = ?`,
	} {
		if n := e.count(q, parent.ID); n != 0 {
			t.Errorf("hard delete left %d %s rows", n, table)
		}
	}
	if e.count(`SELECT count(*) FROM media WHERE id = ?`, m) != 1 {
		t.Errorf("media row must be kept")
	}
	e.do("GET", "/api/items/"+child.ID, nil, 200, &d)
	if d.ParentItemID != nil || d.Parent != nil {
		t.Errorf("child kept its parent: %+v", d.ParentItemID)
	}
}

func TestProducts(t *testing.T) {
	e := newEnv(t)
	var p ProductDetail
	raw := e.raw("POST", "/api/products", map[string]any{"brand": "Uniqlo", "name": "Blocktech Parka (2023)", "ext_key": "uniqlo/341-456789/23SS"}, 201)
	requireKeys(t, "ProductDetail", raw, "id", "brand", "name", "attrs", "notes", "ext_key", "created_at", "updated_at", "cover",
		"item_count", "media", "identifiers", "facts", "references", "items")
	pid := raw["id"].(string)
	var er struct {
		Error struct {
			Details map[string]any `json:"details"`
		} `json:"error"`
	}
	e.do("POST", "/api/products", map[string]any{"name": "Dup", "ext_key": "uniqlo/341-456789/23SS"}, 409, &er)
	if er.Error.Details["id"] != pid {
		t.Fatalf("product 409 details: %+v", er)
	}
	e.do("POST", "/api/products", map[string]any{"name": ""}, 422, nil)
	e.do("POST", "/api/products", map[string]any{"name": "X", "attrs": map[string]any{}}, 400, nil)
	for i, n := range []string{"Airism Tee", "Heattech Crew", "Ultra Light Down"} {
		brand := "Uniqlo"
		if i == 2 {
			brand = "Patagonia"
		}
		e.do("POST", "/api/products", map[string]any{"brand": brand, "name": n}, 201, nil)
	}
	e.do("POST", "/api/products", map[string]any{"name": "No brand thing"}, 201, nil)

	// Stock photo cover, references, identifiers.
	stock := e.media("image/jpeg")
	e.link(stock, "product", pid, "stock", 0)
	e.do("POST", "/api/products/"+pid+"/references", map[string]any{"url": "https://uniqlo.test/p", "kind": "product_page"}, 201, nil)
	e.do("POST", "/api/products/"+pid+"/identifiers", map[string]any{"scheme": "uniqlo.product_code", "value": "341-456789"}, 201, nil)
	e.do("POST", "/api/products/nope/identifiers", map[string]any{"scheme": "a", "value": "1"}, 404, nil)

	it := e.item(map[string]any{"kind": "clothing", "title": "Parka", "product_id": pid})
	e.do("GET", "/api/products/"+pid, nil, 200, &p)
	if p.Cover == nil || p.Cover.ID != stock || p.ItemCount != 1 || len(p.Items) != 1 || p.Items[0].ID != it.ID ||
		len(p.Media) != 1 || len(p.References) != 1 || len(p.Identifiers) != 1 {
		t.Fatalf("product detail: %+v", p)
	}

	// List: alphabetical by brand then name, paged, filtered.
	var pl ProductList
	e.do("GET", "/api/products?limit=2", nil, 200, &pl)
	var names []string
	for {
		for _, x := range pl.Products {
			names = append(names, x.Name)
		}
		if pl.NextCursor == nil {
			break
		}
		next := *pl.NextCursor
		pl = ProductList{}
		e.do("GET", "/api/products?limit=2&cursor="+next, nil, 200, &pl)
	}
	if strings.Join(names, "|") != "No brand thing|Ultra Light Down|Airism Tee|Blocktech Parka (2023)|Heattech Crew" {
		t.Fatalf("product order: %v", names)
	}
	e.do("GET", "/api/products?brand=uniqlo", nil, 200, &pl)
	if len(pl.Products) != 3 {
		t.Fatalf("brand filter: %d", len(pl.Products))
	}
	e.do("GET", "/api/products?q=tech", nil, 200, &pl)
	if len(pl.Products) != 2 || pl.NextCursor != nil {
		t.Fatalf("q filter: %+v", pl)
	}

	// Patch and delete rules.
	e.do("PATCH", "/api/products/"+pid, map[string]any{"name": "Blocktech Parka", "brand": nil, "notes": "2023 version"}, 200, &p)
	if p.Name != "Blocktech Parka" || p.Brand != nil || *p.Notes != "2023 version" {
		t.Fatalf("product patch: %+v", p)
	}
	e.do("PATCH", "/api/products/"+pid, map[string]any{"name": nil}, 422, nil)
	e.do("PATCH", "/api/products/"+pid, map[string]any{}, 400, nil)
	e.do("PATCH", "/api/products/nope", map[string]any{"name": "x"}, 404, nil)
	e.do("DELETE", "/api/products/"+pid, nil, 409, nil)
	e.do("PATCH", "/api/items/"+it.ID, map[string]any{"product_id": nil}, 200, nil)
	e.do("DELETE", "/api/products/"+pid, nil, 204, nil)
	e.do("GET", "/api/products/"+pid, nil, 404, nil)
	if e.count(`SELECT count(*) FROM identifier WHERE subject_id = ?`, pid)+e.count(`SELECT count(*) FROM media_link WHERE subject_id = ?`, pid) != 0 {
		t.Fatalf("product knowledge not deleted")
	}
	e.do("DELETE", "/api/products/"+pid, nil, 404, nil)
}

// TestGetWritesNothing runs every read endpoint and checks that no file in
// the data or media directory changed, including the DB file and WAL.
func TestGetWritesNothing(t *testing.T) {
	e := newEnv(t)
	ss := e.loc("SS", "StoreSafe", "site", "")
	u2 := e.loc("U2", "Unit 2", "unit", ss)
	b1 := e.loc("B-1", "Coats", "container", u2)
	m := e.media("image/jpeg")
	var p ProductDetail
	e.do("POST", "/api/products", map[string]any{"brand": "Uniqlo", "name": "Parka"}, 201, &p)
	e.fact("product", p.ID, map[string]any{"key": "season", "value": "23SS", "basis": "inferred"})
	it := e.item(map[string]any{"kind": "clothing", "title": "Navy parka", "location": b1, "product_id": p.ID, "tags": []string{"winter"},
		"media":        []any{map[string]any{"media_id": m, "role": "flat_lay"}},
		"observations": []any{map[string]any{"text": "RN 12345", "media_id": m}},
		"identifiers":  []any{map[string]any{"scheme": "rn", "value": "12345", "observation": 0}},
		"facts":        []any{map[string]any{"key": "color", "value": "navy", "basis": "user"}}})
	e.item(map[string]any{"kind": "object", "title": "Lamp", "target_location": u2})

	dbPath := e.d.Cfg.DBPath
	stat := func(p string) string {
		fi, err := os.Stat(p)
		if err != nil {
			return "absent"
		}
		return fmt.Sprintf("%d/%d", fi.ModTime().UnixNano(), fi.Size())
	}
	before := testutil.Mtimes(t, e.d.Cfg.DataDir, e.d.Cfg.MediaDir)
	dbBefore, walBefore := stat(dbPath), stat(dbPath+"-wal")
	if walBefore == "absent" {
		t.Fatalf("expected a WAL after writes")
	}

	for _, path := range []string{
		"/api/schema", "/api/items", "/api/items?q=navy", "/api/items?q=12345&sort=relevance", "/api/items?q=parka&sort=title&limit=1",
		"/api/items?kind=clothing&category=outerwear&location=code:SS&status=placed&tag=winter&review=ok&attr.color=navy&lifecycle=all",
		"/api/items?location=unknown", "/api/items?product=" + p.ID, "/api/items?sort=created&limit=1",
		"/api/facets", "/api/facets?kind=object&attr.color=navy&q=lamp", "/api/items/" + it.ID, "/api/items/nope",
		"/api/identifiers/lookup?value=12345", "/api/products", "/api/products?q=par", "/api/products/" + p.ID,
	} {
		rec := testutil.Do(t, e.h, "GET", path, nil, nil)
		if rec.Code >= 500 {
			t.Fatalf("GET %s: %d %s", path, rec.Code, rec.Body.String())
		}
	}
	after := testutil.Mtimes(t, e.d.Cfg.DataDir, e.d.Cfg.MediaDir)
	for f, v := range before {
		if after[f] != v {
			t.Errorf("GETs changed %s: %s → %s", f, v, after[f])
		}
	}
	for f := range after {
		if _, ok := before[f]; !ok {
			t.Errorf("GETs created %s", f)
		}
	}
	if stat(dbPath) != dbBefore || stat(dbPath+"-wal") != walBefore {
		t.Errorf("DB or WAL changed: %s→%s, %s→%s", dbBefore, stat(dbPath), walBefore, stat(dbPath+"-wal"))
	}
}

func TestSummariesOrderAndMissing(t *testing.T) {
	e := newEnv(t)
	a := e.item(map[string]any{"kind": "object", "title": "A"})
	b := e.item(map[string]any{"kind": "object", "title": "B"})
	got, err := Summaries(context.Background(), e.d.DB, []string{b.ID, "missing", a.ID})
	if err != nil || len(got) != 2 || got[0].ID != b.ID || got[1].ID != a.ID {
		t.Fatalf("Summaries: %v %+v", err, got)
	}
	got, err = Summaries(context.Background(), e.d.DB, nil)
	if err != nil || got == nil || len(got) != 0 {
		t.Fatalf("empty Summaries: %v %v", got, err)
	}
}
