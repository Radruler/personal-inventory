package locations_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/radruler/personal-inventory/internal/locations"
	"github.com/radruler/personal-inventory/internal/testutil"
)

// world is a small tree:
//
//	HOME (site) › BEDROOM (room), CLOSET (room)
//	SS (site) › U1 (unit), U2 (unit) › B-014 (container) › B-015 (container)
type world struct {
	*env
	home, bedroom, closet, ss, u1, u2, b14, b15 string
}

func newWorld(t *testing.T) *world {
	e := newEnv(t)
	w := &world{env: e}
	w.home = e.loc("HOME", "Home", "site", "")
	w.ss = e.loc("SS", "StoreSafe", "site", "")
	w.bedroom = e.loc("BEDROOM", "Bedroom", "room", "code:home")
	w.closet = e.loc("CLOSET", "Hall closet", "room", w.home)
	w.u2 = e.loc("U2", "Unit 2", "unit", "code:SS")
	w.u1 = e.loc("U1", "Unit 1", "unit", "CODE:ss")
	w.b14 = e.loc("B-014", "Winter coats", "container", "code:U2")
	w.b15 = e.loc("B-015", "Hats", "container", "code:b-014")
	return w
}

func (w *world) move(body map[string]any) locations.MoveResult {
	w.t.Helper()
	var res locations.MoveResult
	w.want(w.do("POST", "/api/moves", body, &res), 200)
	return res
}

func TestTreeCounts(t *testing.T) {
	w := newWorld(t)
	w.item("coat 1", w.b14, "", "owned")
	w.item("coat 2", w.b14, "", "owned")
	w.item("sold coat", w.b14, "", "gone")
	w.item("hat", w.b15, "", "owned")
	w.item("loose in unit", w.u2, "", "owned")
	w.item("lamp", w.bedroom, w.u2, "owned") // staged to U2
	w.item("sold lamp", w.bedroom, w.u2, "gone")
	w.item("lost thing", "", "", "owned")
	w.item("lost and gone", "", "", "gone")
	garage := w.loc("GARAGE", "Garage", "room", "")
	empty := w.loc("B-099", "Empty", "container", "code:U1")
	w.want(w.do("DELETE", "/api/locations/code:B-099", nil, nil), 204)

	// A photo linked to the box becomes its cover.
	w.exec(`INSERT INTO media (id, sha256, mime, ext, bytes, variant_status) VALUES ('m1', 'aabbccdd', 'image/jpeg', 'jpg', 10, 'ready')`)
	w.exec(`INSERT INTO media_link (media_id, subject_type, subject_id, role) VALUES ('m1', 'location', ?, 'box_exterior')`, w.b14)

	var tree locations.LocationTree
	rec := w.do("GET", "/api/locations", nil, &tree)
	w.want(rec, 200)
	if tree.UnknownCount != 1 {
		t.Errorf("unknown_count = %d, want 1", tree.UnknownCount)
	}
	var rootIDs []string
	for _, r := range tree.Roots {
		rootIDs = append(rootIDs, r.ID)
	}
	if !slices.Equal(rootIDs, []string{w.home, w.ss, garage}) {
		t.Errorf("roots order = %v, want HOME, SS (sites) then GARAGE (room)", rootIDs)
	}
	check := func(id string, item, total, staged int) {
		t.Helper()
		n := findNode(tree.Roots, id)
		if n == nil {
			t.Fatalf("node %s missing", id)
		}
		if n.ItemCount != item || n.TotalCount != total || n.StagedCount != staged {
			t.Errorf("%s (%s): item/total/staged = %d/%d/%d, want %d/%d/%d",
				n.Label, id, n.ItemCount, n.TotalCount, n.StagedCount, item, total, staged)
		}
	}
	check(w.b15, 1, 1, 0)
	check(w.b14, 2, 3, 0)
	check(w.u2, 1, 4, 1)
	check(w.ss, 0, 4, 0)
	check(w.bedroom, 1, 1, 0)
	check(w.home, 0, 1, 0)
	if findNode(tree.Roots, empty) != nil {
		t.Errorf("archived location listed without include_archived")
	}
	ss := findNode(tree.Roots, w.ss)
	if len(ss.Children) != 2 || ss.Children[0].ID != w.u1 || ss.Children[1].ID != w.u2 {
		t.Errorf("SS children should be U1, U2")
	}
	b14 := findNode(tree.Roots, w.b14)
	if b14.Label != "Unit 2 › B-014" || findNode(tree.Roots, w.b15).Label != "Unit 2 › B-014 › B-015" {
		t.Errorf("labels = %q, %q", b14.Label, findNode(tree.Roots, w.b15).Label)
	}
	if b14.Cover == nil || b14.Cover.ID != "m1" || b14.Cover.ThumbURL == nil {
		t.Errorf("box cover = %+v", b14.Cover)
	}
	if !strings.Contains(rec.Body.String(), `"children":[]`) || !strings.Contains(rec.Body.String(), `"layout":null`) {
		t.Errorf("leaf children must be [] and layout null: %s", rec.Body.String())
	}

	var all locations.LocationTree
	w.want(w.do("GET", "/api/locations?include_archived=true", nil, &all), 200)
	if n := findNode(all.Roots, empty); n == nil || n.ArchivedAt == nil {
		t.Errorf("include_archived should list the archived box")
	}
	w.want(w.do("GET", "/api/locations?include_archived=maybe", nil, nil), 400)
}

func TestDetail(t *testing.T) {
	w := newWorld(t)
	a := w.item("coat a", w.b14, "", "owned")
	b := w.item("coat b", w.b14, "", "owned")
	w.item("gone coat", w.b14, "", "gone")
	hat := w.item("hat", w.b15, "", "owned")
	lamp := w.item("lamp", w.bedroom, w.u2, "owned")
	w.exec(`INSERT INTO media (id, sha256, mime, ext, bytes, variant_status) VALUES ('m1', 'aabbccdd', 'image/heic', 'heic', 10, 'pending')`)
	w.exec(`INSERT INTO media_link (media_id, subject_type, subject_id, role, caption) VALUES ('m1', 'location', ?, 'box_contents', 'top layer')`, w.b14)
	w.exec(`INSERT INTO media_link (media_id, subject_type, subject_id, role) VALUES ('m1', 'item', ?, 'detail')`, a)

	var det locations.LocationDetail
	rec := w.do("GET", "/api/locations/code:b-014", nil, &det)
	w.want(rec, 200)
	if det.ID != w.b14 || det.Label != "Unit 2 › B-014" {
		t.Fatalf("detail = %s %q", det.ID, det.Label)
	}
	if got := ids(det.Items); !slices.Equal(got, []string{b, a}) {
		t.Errorf("items (newest first, owned, direct) = %v, want [%s %s]", got, b, a)
	}
	if len(det.Path) != 3 || det.Path[0].ID != w.ss || det.Path[2].ID != w.b14 {
		t.Errorf("path = %+v", det.Path)
	}
	if len(det.Children) != 1 || det.Children[0].ID != w.b15 || det.Children[0].TotalCount != 1 {
		t.Errorf("children = %+v", det.Children)
	}
	if len(det.Media) != 1 || det.Media[0].Role != "box_contents" || det.Media[0].Caption == nil ||
		det.Media[0].Media.ID != "m1" || len(det.Media[0].Media.Links) != 2 || det.Media[0].Media.ThumbURL != nil {
		t.Errorf("media = %+v", det.Media)
	}
	for _, key := range []string{`"staged_items":[]`, `"movements":[]`, `"exif":null`, `"thumb_url":null`} {
		if !strings.Contains(rec.Body.String(), key) {
			t.Errorf("detail JSON lacks %s", key)
		}
	}

	var deep locations.LocationDetail
	w.want(w.do("GET", "/api/locations/"+w.b14+"?deep=true", nil, &deep), 200)
	if got := ids(deep.Items); !slices.Equal(got, []string{hat, b, a}) {
		t.Errorf("deep items = %v", got)
	}

	var u2 locations.LocationDetail
	w.want(w.do("GET", "/api/locations/code:U2", nil, &u2), 200)
	if got := ids(u2.StagedItems); !slices.Equal(got, []string{lamp}) {
		t.Errorf("staged_items = %v", got)
	}
	if u2.StagedItems[0].Placement.Label != "Bedroom → going to Unit 2" {
		t.Errorf("staged placement = %q", u2.StagedItems[0].Placement.Label)
	}

	// Movements on the page: the box moving in, items moved in.
	w.move(map[string]any{"action": "move", "location_id": "code:B-014", "to": "code:U1"})
	w.move(map[string]any{"action": "move", "item_ids": []string{lamp}, "to": "code:U1"})
	var u1 locations.LocationDetail
	w.want(w.do("GET", "/api/locations/code:U1", nil, &u1), 200)
	if len(u1.Movements) != 2 || u1.Movements[0].ItemID == nil || *u1.Movements[0].ItemID != lamp ||
		u1.Movements[1].LocationMovedID == nil || u1.Movements[1].From.ID != w.u2 {
		t.Errorf("U1 movements = %+v", u1.Movements)
	}
	var u2b locations.LocationDetail
	w.want(w.do("GET", "/api/locations/code:U2", nil, &u2b), 200)
	if len(u2b.Movements) != 1 || u2b.Movements[0].To.ID != w.u1 {
		t.Errorf("U2 movements (box moved out) = %+v", u2b.Movements)
	}

	w.want(w.do("GET", "/api/locations/code:NOPE", nil, nil), 404)
	w.want(w.do("GET", "/api/locations/01JNOSUCHID000000000000000", nil, nil), 404)
}

func TestCreateAndPatch(t *testing.T) {
	w := newWorld(t)
	var det locations.LocationDetail
	rec := w.do("POST", "/api/locations", map[string]any{
		"code": "  B-020 ", "name": " Packing box ", "kind": "container", "parent": "code:closet",
		"notes": "tape it", "layout": map[string]any{"x": 1},
	}, &det)
	w.want(rec, 201)
	if det.Code == nil || *det.Code != "B-020" || det.Name != "Packing box" || det.ParentID == nil ||
		*det.ParentID != w.closet || det.Label != "Hall closet › B-020" || string(det.Layout) != `{"x":1}` {
		t.Errorf("created = %+v", det.Location)
	}
	for _, key := range []string{`"items":[]`, `"staged_items":[]`, `"media":[]`, `"children":[]`, `"cover":null`, `"archived_at":null`} {
		if !strings.Contains(rec.Body.String(), key) {
			t.Errorf("create JSON lacks %s: %s", key, rec.Body.String())
		}
	}

	var e errBody
	w.want(w.do("POST", "/api/locations", map[string]any{"code": "b-020", "name": "dup", "kind": "container"}, &e), 409)
	if e.Error.Code != "conflict" || e.Error.Details["id"] != det.ID {
		t.Errorf("dup code error = %+v", e.Error)
	}
	w.want(w.do("POST", "/api/locations", map[string]any{"name": "Unit in box", "kind": "unit", "parent": "code:B-014"}, nil), 422)
	w.want(w.do("POST", "/api/locations", map[string]any{"name": "Room on shelf", "kind": "room", "parent": "code:B-014"}, nil), 422)
	w.want(w.do("POST", "/api/locations", map[string]any{"name": "x", "kind": "attic"}, nil), 422)
	w.want(w.do("POST", "/api/locations", map[string]any{"name": " ", "kind": "room"}, nil), 422)
	w.want(w.do("POST", "/api/locations", map[string]any{"name": "x", "kind": "room", "parent": "code:NOPE"}, nil), 422)
	w.want(w.do("POST", "/api/locations", map[string]any{"name": "x", "kind": "room", "colour": "red"}, nil), 400)
	w.want(w.do("POST", "/api/locations", map[string]any{"name": "x", "kind": "room", "cover_media_id": "nope"}, nil), 422)
	// Box in box, shelf in unit, unit in room are fine; no code is fine.
	w.loc("", "Inner box", "container", "code:B-015")
	w.loc("SH-1", "Shelf", "shelf", "code:U1")
	w.loc("", "Closet unit", "unit", "code:BEDROOM")

	// PATCH
	var p locations.LocationDetail
	w.want(w.do("PATCH", "/api/locations/code:B-020", map[string]any{"name": "Garage box", "code": "B-021", "notes": nil}, &p), 200)
	if p.Name != "Garage box" || *p.Code != "B-021" || p.Notes != nil {
		t.Errorf("patched = %+v", p.Location)
	}
	w.want(w.do("PATCH", "/api/locations/code:B-021", map[string]any{"code": "u2"}, &e), 409)
	if e.Error.Details["id"] != w.u2 {
		t.Errorf("code conflict details = %+v", e.Error.Details)
	}
	w.want(w.do("PATCH", "/api/locations/code:B-021", map[string]any{"parent": "code:U1"}, nil), 400)
	w.want(w.do("PATCH", "/api/locations/code:B-021", map[string]any{}, nil), 422)
	w.want(w.do("PATCH", "/api/locations/code:B-021", map[string]any{"name": nil}, nil), 422)
	w.want(w.do("PATCH", "/api/locations/code:NOPE", map[string]any{"name": "x"}, nil), 404)
	w.want(w.do("PATCH", "/api/locations/code:B-021", map[string]any{"layout": "flat"}, nil), 422)
	w.want(w.do("PATCH", "/api/locations/code:B-021", map[string]any{"code": nil, "layout": nil}, &p), 200)
	if p.Code != nil || (p.Layout != nil && string(p.Layout) != "null") {
		t.Errorf("code/layout should be cleared: %+v", p.Location)
	}
	// Re-kinding: a unit holding a container may become a room, but not a box.
	w.want(w.do("PATCH", "/api/locations/code:SS", map[string]any{"kind": "container"}, nil), 422)
	w.want(w.do("PATCH", "/api/locations/code:B-015", map[string]any{"kind": "unit"}, nil), 422)
	w.want(w.do("PATCH", "/api/locations/code:U1", map[string]any{"kind": "room"}, &p), 200)
	if p.Kind != "room" {
		t.Errorf("kind = %s", p.Kind)
	}
}

func TestArchiveRules(t *testing.T) {
	w := newWorld(t)
	coat := w.item("coat", w.b15, "", "owned")
	w.item("sold", w.b15, "", "gone") // gone items never block
	var e errBody
	w.want(w.do("DELETE", "/api/locations/code:B-015", nil, &e), 409)
	if e.Error.Details["items"] != float64(1) {
		t.Errorf("details = %+v", e.Error.Details)
	}
	w.want(w.do("DELETE", "/api/locations/code:B-014", nil, &e), 409) // non-archived child
	if e.Error.Details["children"] != float64(1) {
		t.Errorf("details = %+v", e.Error.Details)
	}
	w.move(map[string]any{"action": "move", "item_ids": []string{coat}, "to": nil})
	w.move(map[string]any{"action": "stage", "item_ids": []string{coat}, "to": "code:B-015"})
	w.want(w.do("DELETE", "/api/locations/code:B-015", nil, &e), 409) // staged here
	if e.Error.Details["staged"] != float64(1) {
		t.Errorf("details = %+v", e.Error.Details)
	}
	w.want(w.do("PATCH", "/api/locations/code:B-015", map[string]any{"archived": true}, nil), 409)
	w.move(map[string]any{"action": "unstage", "item_ids": []string{coat}})

	w.want(w.do("DELETE", "/api/locations/code:B-015", nil, nil), 204)
	at := w.str(`SELECT archived_at FROM location WHERE id = ?`, w.b15)
	if at == "" {
		t.Fatal("archived_at not set")
	}
	w.want(w.do("DELETE", "/api/locations/code:B-015", nil, nil), 204) // idempotent
	if w.str(`SELECT archived_at FROM location WHERE id = ?`, w.b15) != at {
		t.Errorf("re-archiving rewrote archived_at")
	}
	// Archived locations refuse new contents.
	w.want(w.do("POST", "/api/moves", map[string]any{"action": "move", "item_ids": []string{coat}, "to": "code:B-015"}, nil), 422)
	w.want(w.do("POST", "/api/moves", map[string]any{"action": "stage", "item_ids": []string{coat}, "to": "code:B-015"}, nil), 422)
	w.want(w.do("POST", "/api/locations", map[string]any{"name": "x", "kind": "container", "parent": "code:B-015"}, nil), 422)
	w.want(w.do("POST", "/api/moves", map[string]any{"action": "move", "location_id": "code:B-015", "to": "code:U1"}, nil), 422)

	// Now B-014 can be archived; its child cannot be un-archived under it.
	w.want(w.do("DELETE", "/api/locations/code:B-014", nil, nil), 204)
	w.want(w.do("PATCH", "/api/locations/code:B-015", map[string]any{"archived": false}, nil), 409)
	var p locations.LocationDetail
	w.want(w.do("PATCH", "/api/locations/code:B-014", map[string]any{"archived": false}, &p), 200)
	if p.ArchivedAt != nil {
		t.Errorf("un-archive failed")
	}
	w.want(w.do("PATCH", "/api/locations/code:B-015", map[string]any{"archived": false}, nil), 200)
	w.want(w.do("DELETE", "/api/locations/code:NOPE", nil, nil), 404)
}

func TestBoxMoveMovesContentsWithoutItemWrites(t *testing.T) {
	w := newWorld(t)
	a := w.item("coat a", w.b14, "", "owned")
	b := w.item("hat", w.b15, "", "owned")
	itemsBefore := w.str(`SELECT group_concat(id || location_id || updated_at, ',') FROM item`)

	var res locations.MoveResult
	w.want(w.do("POST", "/api/moves", map[string]any{
		"action": "move", "location_id": "code:B-014", "to": "code:U1", "note": "moved Sunday", "actor": "agent:test",
	}, &res), 200)
	if res.Moved != 1 || len(res.Movements) != 1 || len(res.Items) != 0 {
		t.Fatalf("result = %+v", res)
	}
	mv := res.Movements[0]
	if mv.LocationMovedID == nil || *mv.LocationMovedID != w.b14 || mv.ItemID != nil || mv.Kind != "move" ||
		mv.From == nil || mv.From.ID != w.u2 || mv.To == nil || mv.To.ID != w.u1 ||
		mv.Note == nil || *mv.Note != "moved Sunday" || mv.Actor != "agent:test" {
		t.Errorf("movement = %+v", mv)
	}
	if res.Location == nil || res.Location.ID != w.b14 || res.Location.Label != "Unit 1 › B-014" ||
		res.Location.TotalCount != 2 || len(res.Location.Children) != 1 {
		t.Errorf("location = %+v", res.Location)
	}
	if got := w.str(`SELECT group_concat(id || location_id || updated_at, ',') FROM item`); got != itemsBefore {
		t.Errorf("box move wrote item rows:\n%s\n%s", itemsBefore, got)
	}
	if n := w.count(`SELECT count(*) FROM movement`); n != 1 {
		t.Errorf("movement rows = %d, want 1", n)
	}
	// Contents' derived paths follow the box.
	var u1 locations.LocationDetail
	w.want(w.do("GET", "/api/locations/code:U1?deep=1", nil, &u1), 200)
	labels := map[string]string{}
	for _, it := range u1.Items {
		labels[it.ID] = it.Placement.Label
	}
	if labels[a] != "Unit 1 › B-014" || labels[b] != "Unit 1 › B-014 › B-015" {
		t.Errorf("contents labels = %v", labels)
	}
	var tree locations.LocationTree
	w.want(w.do("GET", "/api/locations", nil, &tree), 200)
	if findNode(tree.Roots, w.u1).TotalCount != 2 || findNode(tree.Roots, w.u2).TotalCount != 0 {
		t.Errorf("unit totals did not follow the box")
	}

	// Same move again is a no-op: nothing written.
	res = w.move(map[string]any{"action": "move", "location_id": w.b14, "to": w.u1})
	if res.Moved != 0 || len(res.Movements) != 0 || res.Location == nil {
		t.Errorf("no-op box move = %+v", res)
	}
	if n := w.count(`SELECT count(*) FROM movement`); n != 1 {
		t.Errorf("no-op box move added a movement")
	}
	// Top level, with the actor from X-Actor-less default.
	res = w.move(map[string]any{"action": "move", "location_id": "code:B-015", "to": nil})
	if res.Location.ParentID != nil || res.Location.Label != "B-015" || res.Movements[0].To != nil || res.Movements[0].Actor != "user" {
		t.Errorf("to top level = %+v / %+v", res.Location, res.Movements)
	}
}

func TestBoxMoveRejectsCycles(t *testing.T) {
	w := newWorld(t)
	w.want(w.do("POST", "/api/moves", map[string]any{"action": "move", "location_id": "code:B-014", "to": "code:B-014"}, nil), 409)
	w.want(w.do("POST", "/api/moves", map[string]any{"action": "move", "location_id": "code:B-014", "to": "code:B-015"}, nil), 409)
	w.want(w.do("POST", "/api/moves", map[string]any{"action": "move", "location_id": "code:SS", "to": "code:U2"}, nil), 409)
	// Not a cycle, but a unit cannot go into a box.
	w.want(w.do("POST", "/api/moves", map[string]any{"action": "move", "location_id": "code:U1", "to": "code:B-014"}, nil), 422)
	w.want(w.do("POST", "/api/moves", map[string]any{"action": "move", "location_id": "code:NOPE", "to": "code:U1"}, nil), 404)
	w.want(w.do("POST", "/api/moves", map[string]any{"action": "move", "location_id": "code:B-014", "to": "code:NOPE"}, nil), 422)
	if n := w.count(`SELECT count(*) FROM movement`); n != 0 {
		t.Errorf("rejected moves wrote %d movements", n)
	}
	if w.str(`SELECT parent_id FROM location WHERE id = ?`, w.b14) != w.u2 {
		t.Errorf("rejected move changed the parent")
	}
}

func TestPackingFlow(t *testing.T) {
	w := newWorld(t)
	coat := w.item("coat", w.bedroom, "", "owned")
	scarf := w.item("scarf", w.bedroom, "", "owned")
	boots := w.item("boots", w.closet, "", "owned")
	gloves := w.item("gloves", w.u2, "", "owned")

	res := w.move(map[string]any{"action": "stage", "item_ids": []string{coat, scarf, boots, gloves}, "to": "code:U2"})
	if res.Moved != 4 || len(res.Movements) != 4 || res.Movements[0].Kind != "stage" || res.Movements[0].From != nil ||
		res.Movements[0].To.ID != w.u2 {
		t.Fatalf("stage = %+v", res)
	}
	if res.Items[0].Placement.Status != "staged" || res.Items[0].Placement.Label != "Bedroom → going to Unit 2" {
		t.Errorf("staged card = %+v", res.Items[0].Placement)
	}
	if w.str(`SELECT updated_at FROM item WHERE id = ?`, coat) == oldStamp {
		t.Errorf("staging should bump the item's updated_at")
	}

	// Pack into a box inside the target: target cleared, one move row.
	res = w.move(map[string]any{"action": "move", "item_ids": []string{coat}, "to": "code:B-015", "note": "packed"})
	if res.Moved != 1 || len(res.Movements) != 1 || res.Movements[0].Kind != "move" ||
		res.Movements[0].From.ID != w.bedroom || res.Movements[0].To.ID != w.b15 {
		t.Errorf("pack = %+v", res.Movements)
	}
	if w.str(`SELECT target_location_id FROM item WHERE id = ?`, coat) != "" || res.Items[0].Placement.Label != "Unit 2 › B-014 › B-015" {
		t.Errorf("packing did not clear the target: %+v", res.Items[0].Placement)
	}

	// Moving elsewhere keeps the target.
	res = w.move(map[string]any{"action": "move", "item_ids": []string{scarf}, "to": "code:CLOSET"})
	if w.str(`SELECT target_location_id FROM item WHERE id = ?`, scarf) != w.u2 || res.Items[0].Placement.Label != "Hall closet → going to Unit 2" {
		t.Errorf("move elsewhere cleared the target")
	}
	// ...unless clear_target.
	w.move(map[string]any{"action": "move", "item_ids": []string{boots}, "to": "code:U1", "clear_target": true})
	if w.str(`SELECT target_location_id FROM item WHERE id = ?`, boots) != "" {
		t.Errorf("clear_target ignored")
	}
	// Already at the target: only the target changes → one unstage row.
	res = w.move(map[string]any{"action": "move", "item_ids": []string{gloves}, "to": "code:U2"})
	if res.Moved != 1 || res.Movements[0].Kind != "unstage" || res.Movements[0].From.ID != w.u2 || res.Movements[0].To != nil {
		t.Errorf("arrive at target = %+v", res.Movements)
	}
	if res.Items[0].Placement.Status != "approximate" {
		t.Errorf("gloves placement = %+v", res.Items[0].Placement)
	}
	if n := w.count(`SELECT count(*) FROM movement WHERE item_id = ?`, coat); n != 2 {
		t.Errorf("coat movements = %d, want stage + move", n)
	}
	hist, err := locations.MovementsForItem(t.Context(), w.d.DB, coat, 10)
	if err != nil || len(hist) != 2 || hist[0].Kind != "move" || hist[1].Kind != "stage" {
		t.Errorf("MovementsForItem = %+v, %v", hist, err)
	}
}

func TestUnstageAndNoOps(t *testing.T) {
	w := newWorld(t)
	coat := w.item("coat", w.bedroom, w.u2, "owned")
	hat := w.item("hat", w.bedroom, "", "owned")

	res := w.move(map[string]any{"action": "unstage", "item_ids": []string{coat, hat}})
	if res.Moved != 1 || len(res.Movements) != 1 || res.Movements[0].Kind != "unstage" ||
		res.Movements[0].From.ID != w.u2 || res.Movements[0].To != nil || len(res.Items) != 2 {
		t.Errorf("unstage = %+v", res)
	}
	if w.str(`SELECT updated_at FROM item WHERE id = ?`, hat) != oldStamp {
		t.Errorf("no-op item was written")
	}
	before := w.count(`SELECT count(*) FROM movement`)
	for _, body := range []map[string]any{
		{"action": "unstage", "item_ids": []string{coat}},
		{"action": "move", "item_ids": []string{coat, hat, coat}, "to": "code:BEDROOM"},
		{"action": "stage", "item_ids": []string{hat}, "to": "code:U1"},
		{"action": "stage", "item_ids": []string{hat}, "to": "code:u1"},
	} {
		w.move(body)
	}
	if n := w.count(`SELECT count(*) FROM movement`) - before; n != 1 {
		t.Errorf("no-ops wrote movements: %d new rows, want 1 (the first stage of hat)", n)
	}
	res = w.move(map[string]any{"action": "move", "item_ids": []string{coat}, "to": nil})
	if res.Moved != 1 || res.Movements[0].To != nil || res.Items[0].Placement.Status != "unknown" {
		t.Errorf("move to unknown = %+v", res)
	}
}

func TestMoveValidation(t *testing.T) {
	w := newWorld(t)
	coat := w.item("coat", w.bedroom, "", "owned")
	sold := w.item("sold", w.bedroom, "", "gone")
	post := func(body map[string]any, code int) errBody {
		t.Helper()
		var e errBody
		w.want(w.do("POST", "/api/moves", body, &e), code)
		return e
	}
	e := post(map[string]any{"action": "move", "item_ids": []string{coat, "NOPE1", "NOPE2"}, "to": "code:U1"}, 404)
	if m, _ := e.Error.Details["missing"].([]any); len(m) != 2 || m[0] != "NOPE1" {
		t.Errorf("missing details = %+v", e.Error.Details)
	}
	e = post(map[string]any{"action": "stage", "item_ids": []string{coat, sold}, "to": "code:U1"}, 409)
	if g, _ := e.Error.Details["gone"].([]any); len(g) != 1 || g[0] != sold {
		t.Errorf("gone details = %+v", e.Error.Details)
	}
	if w.str(`SELECT location_id FROM item WHERE id = ?`, coat) != w.bedroom || w.count(`SELECT count(*) FROM movement`) != 0 {
		t.Errorf("failed request changed state")
	}
	post(map[string]any{"action": "stage", "item_ids": []string{coat}}, 422)
	post(map[string]any{"action": "stage", "item_ids": []string{coat}, "to": nil}, 422)
	post(map[string]any{"action": "move", "item_ids": []string{coat}}, 422)
	post(map[string]any{"action": "unstage", "item_ids": []string{coat}, "to": "code:U1"}, 422)
	post(map[string]any{"action": "fly", "item_ids": []string{coat}, "to": "code:U1"}, 422)
	post(map[string]any{"action": "move", "to": "code:U1"}, 422)
	post(map[string]any{"action": "move", "item_ids": []string{}, "to": "code:U1"}, 422)
	post(map[string]any{"action": "move", "item_ids": []string{coat}, "location_id": "code:B-014", "to": "code:U1"}, 422)
	post(map[string]any{"action": "stage", "location_id": "code:B-014", "to": "code:U1"}, 422)
	post(map[string]any{"action": "move", "location_id": "code:B-014", "to": "code:U1", "clear_target": true}, 422)
	post(map[string]any{"action": "move", "item_ids": []string{coat}, "to": "code:NOPE"}, 422)
	post(map[string]any{"action": "move", "item_ids": []string{coat}, "to": " "}, 422)
	post(map[string]any{"action": "move", "item_ids": []string{coat}, "to": "code:U1", "speed": 3}, 400)
	many := make([]string, 1001)
	for i := range many {
		many[i] = coat
	}
	post(map[string]any{"action": "move", "item_ids": many, "to": "code:U1"}, 422)
}

func TestStagingGroups(t *testing.T) {
	w := newWorld(t)
	a := w.item("a", w.bedroom, w.u2, "owned")
	b := w.item("b", "", w.u2, "owned")
	c := w.item("c", w.closet, w.u1, "owned")
	w.item("gone", w.closet, w.u1, "gone")
	w.item("lost", "", "", "owned")
	w.item("placed", w.b14, "", "owned")

	var st locations.Staging
	rec := w.do("GET", "/api/staging", nil, &st)
	w.want(rec, 200)
	if st.UnknownCount != 2 { // b (staged from unknown) and lost
		t.Errorf("unknown_count = %d", st.UnknownCount)
	}
	if len(st.Groups) != 2 || st.Groups[0].Target.ID != w.u1 || st.Groups[1].Target.ID != w.u2 {
		t.Fatalf("groups = %+v", st.Groups)
	}
	if got := ids(st.Groups[0].Items); !slices.Equal(got, []string{c}) {
		t.Errorf("U1 group = %v", got)
	}
	if got := ids(st.Groups[1].Items); !slices.Equal(got, []string{b, a}) {
		t.Errorf("U2 group = %v (newest first)", got)
	}
	if st.Groups[1].Target.Label != "Unit 2" || len(st.Groups[1].Target.Path) != 2 {
		t.Errorf("target ref = %+v", st.Groups[1].Target)
	}

	e := newEnv(t)
	var empty map[string]any
	e.want(e.do("GET", "/api/staging", nil, &empty), 200)
	if g, ok := empty["groups"].([]any); !ok || len(g) != 0 {
		t.Errorf("empty staging groups = %v, want []", empty["groups"])
	}
}

// TestGetsWriteNothing checks PLAN.md §2: read-only browsing never touches
// the disk. The -shm file is shared memory (mmap'd WAL index) and is excluded;
// the DB and WAL files must keep their size and mtime.
func TestGetsWriteNothing(t *testing.T) {
	w := newWorld(t)
	coat := w.item("coat", w.b14, w.u1, "owned")
	w.item("lost", "", "", "owned")
	w.move(map[string]any{"action": "move", "location_id": "code:B-015", "to": "code:U1"})
	w.move(map[string]any{"action": "move", "item_ids": []string{coat}, "to": "code:B-015"})
	w.exec(`INSERT INTO media (id, sha256, mime, ext, bytes, variant_status) VALUES ('m1', 'aabbccdd', 'image/jpeg', 'jpg', 10, 'ready')`)
	w.exec(`INSERT INTO media_link (media_id, subject_type, subject_id, role) VALUES ('m1', 'location', ?, 'box_exterior')`, w.b14)
	wal := w.d.Cfg.DBPath + "-wal"
	if fi, err := os.Stat(wal); err != nil || fi.Size() == 0 {
		t.Fatalf("expected a non-empty WAL to watch: %v", err)
	}

	snap := func() map[string]string {
		m := testutil.Mtimes(t, w.d.Cfg.DataDir, w.d.Cfg.MediaDir)
		for p := range m {
			if strings.HasSuffix(p, "-shm") {
				delete(m, p)
			}
		}
		return m
	}
	before := snap()
	if _, ok := before[w.d.Cfg.DBPath]; !ok {
		t.Fatalf("snapshot misses the DB: %v", before)
	}
	for _, path := range []string{
		"/api/locations",
		"/api/locations?include_archived=true",
		"/api/locations/code:B-014",
		"/api/locations/code:U1?deep=true",
		"/api/locations/" + w.ss + "?deep=true",
		"/api/staging",
	} {
		var out json.RawMessage
		w.want(w.do("GET", path, nil, &out), 200)
	}
	after := snap()
	for p, v := range before {
		if after[p] != v {
			t.Errorf("GET changed %s: %s → %s", filepath.Base(p), v, after[p])
		}
	}
	for p := range after {
		if _, ok := before[p]; !ok {
			t.Errorf("GET created %s", p)
		}
	}
}
