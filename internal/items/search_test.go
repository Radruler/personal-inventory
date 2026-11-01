package items

import (
	"fmt"
	"net/url"
	"sort"
	"strings"
	"testing"

	"github.com/radruler/personal-inventory/internal/db"
	"github.com/radruler/personal-inventory/internal/model"
)

// fixture builds a small inventory covering every placement status.
type fixture struct {
	*env
	home, bed, ss, u2, b1, b2, u1, sh string
	ids                               map[string]string // title → id
}

func newFixture(t *testing.T) *fixture {
	e := newEnv(t)
	f := &fixture{env: e, ids: map[string]string{}}
	f.home = e.loc("HOME", "Home", "site", "")
	f.bed = e.loc("BEDROOM", "Bedroom", "room", f.home)
	f.sh = e.loc("SH-1", "Shelf", "shelf", f.bed)
	f.ss = e.loc("SS", "StoreSafe", "site", "")
	f.u2 = e.loc("U2", "Unit 2", "unit", f.ss)
	f.u1 = e.loc("U1", "Unit 1", "unit", f.ss)
	f.b1 = e.loc("B-001", "Winter coats", "container", f.u2)
	f.b2 = e.loc("B-002", "Sweaters", "container", f.u2)
	add := func(body map[string]any) {
		d := e.item(body)
		f.ids[d.Title] = d.ID
	}
	add(map[string]any{"kind": "clothing", "title": "Navy shell jacket", "category": "outerwear/shell", "location": f.b1, "tags": []string{"rain"},
		"observations": []any{map[string]any{"text": "SHELL: 100% NYLON\nMADE IN VIETNAM"}},
		"identifiers":  []any{map[string]any{"scheme": "uniqlo.product_code", "value": "341-456789", "observation": 0}},
		"facts": []any{map[string]any{"key": "brand", "value": "Uniqlo", "basis": "observed"}, map[string]any{"key": "color", "value": "Navy", "basis": "user"},
			map[string]any{"key": "size", "value": "M", "basis": "user"}}})
	add(map[string]any{"kind": "clothing", "title": "Black wool overcoat", "category": "outerwear/coat", "location": f.b1,
		"facts": []any{map[string]any{"key": "brand", "value": "COS", "basis": "user"}, map[string]any{"key": "color", "value": "black", "basis": "user"}}})
	add(map[string]any{"kind": "clothing", "title": "Striped breton tee", "category": "tops/tee", "location": f.b2, "tags": []string{"summer", "rain"},
		"facts": []any{map[string]any{"key": "color", "value": []string{"navy", "white"}, "basis": "user"}}})
	add(map[string]any{"kind": "clothing", "title": "Grey wool trousers", "category": "bottoms/trousers", "location": f.u2, // approximate
		"facts": []any{map[string]any{"key": "color", "value": "grey", "basis": "user"}, map[string]any{"key": "size", "value": 32, "basis": "user"}}})
	add(map[string]any{"kind": "clothing", "title": "Blue jeans", "category": "bottoms/jeans", "location": f.bed, "target_location": f.u2, // staged
		"facts": []any{map[string]any{"key": "color", "value": "blue", "basis": "user"}}})
	add(map[string]any{"kind": "clothing", "title": "Green flannel", "category": "outerwear", "review_state": "needs_review", // unknown
		"notes": "found in the jacket pile"})
	add(map[string]any{"kind": "object", "title": "Polaroid camera", "category": "electronics/camera", "location": f.sh, "notes": "navy strap"})
	add(map[string]any{"kind": "utility", "title": "USB-C cable", "category": "cable/usb-c", "location": f.u1, "target_location": f.b1}) // staged
	add(map[string]any{"kind": "object", "title": "Trench coat", "category": "outerwear/coat", "location": f.bed})
	// At its target already (via a later SQL change): not staged.
	add(map[string]any{"kind": "object", "title": "Packed lamp", "location": f.b2})
	e.exec(`UPDATE item SET target_location_id = ? WHERE id = ?`, f.u2, f.ids["Packed lamp"])
	e.do("DELETE", "/api/items/"+f.ids["Trench coat"]+"?reason=sold", nil, 200, nil)
	return f
}

func (f *fixture) titlesFor(query string) []string {
	f.t.Helper()
	l := f.list(query)
	if l.Total != len(l.Items) {
		f.t.Fatalf("%s: total %d but %d items", query, l.Total, len(l.Items))
	}
	return titles(l)
}

func (f *fixture) expect(query string, want ...string) {
	f.t.Helper()
	if got := f.titlesFor(query); !sameSet(got, want) {
		f.t.Errorf("GET /api/items?%s\n got %q\nwant %q", query, got, want)
	}
}

func TestSearch(t *testing.T) {
	f := newFixture(t)
	f.expect("q=jack", "Navy shell jacket", "Green flannel")                         // prefix
	f.expect("q=jacket", "Navy shell jacket", "Green flannel")                       // title + notes
	f.expect("q=navy+jack", "Navy shell jacket")                                     // words ANDed
	f.expect("q=navy", "Navy shell jacket", "Polaroid camera", "Striped breton tee") // title, notes, attrs
	f.expect("q=navy+boots")
	f.expect("q=vietnam", "Navy shell jacket")                                        // observation text
	f.expect("q=uniqlo", "Navy shell jacket")                                         // projected brand attr
	f.expect("q=summer", "Striped breton tee")                                        // tags
	f.expect("q=outerw", "Navy shell jacket", "Black wool overcoat", "Green flannel") // category
	f.expect("q=341456789", "Navy shell jacket")                                      // identifier value_norm token
	f.expect("q=341.456.789", "Navy shell jacket")                                    // exact normalized identifier only
	f.expect("q=341-456", "Navy shell jacket")                                        // prefix on identifier tokens
	f.expect("q=%22jack%2A%22%20%28", "Navy shell jacket", "Green flannel")           // FTS syntax is neutralized
	if got := f.titlesFor("q=---"); len(got) != 9 {
		t.Errorf("q without words should not filter: %v", got)
	}

	// Relevance: title match beats a notes-only match; identifier match first.
	l := f.list("q=jacket&sort=relevance")
	if len(l.Items) != 2 || l.Items[0].Title != "Navy shell jacket" {
		t.Fatalf("relevance: %v", titles(l))
	}
	f.item(map[string]any{"kind": "object", "title": "Box 341456789 label", "notes": "x"})
	l = f.list("q=341-456789&sort=relevance")
	if len(l.Items) < 1 || l.Items[0].Title != "Navy shell jacket" {
		t.Fatalf("identifier match should rank first: %v", titles(l))
	}

	// Product identifiers find the product's items.
	var p ProductDetail
	f.do("POST", "/api/products", map[string]any{"name": "Breton"}, 201, &p)
	f.do("POST", "/api/products/"+p.ID+"/identifiers", map[string]any{"scheme": "upc", "value": "0123-4567-8905"}, 201, nil)
	f.do("PATCH", "/api/items/"+f.ids["Striped breton tee"], map[string]any{"product_id": p.ID}, 200, nil)
	f.expect("q=012345678905", "Striped breton tee")
	f.expect("product="+p.ID, "Striped breton tee")
}

func TestFilters(t *testing.T) {
	f := newFixture(t)
	f.expect("kind=object", "Polaroid camera", "Packed lamp")
	f.expect("kind=object&kind=utility", "Polaroid camera", "Packed lamp", "USB-C cable")
	f.expect("kind=object,utility", "Polaroid camera", "Packed lamp", "USB-C cable")
	f.expect("category=outerwear", "Navy shell jacket", "Black wool overcoat", "Green flannel")
	f.expect("category=outer")
	f.expect("category=OUTERWEAR/coat", "Black wool overcoat")
	f.expect("category=bottoms,tops", "Striped breton tee", "Grey wool trousers", "Blue jeans")
	f.expect("location=code:SS", "Navy shell jacket", "Black wool overcoat", "Striped breton tee", "Grey wool trousers", "USB-C cable", "Packed lamp")
	f.expect("location=code:u2", "Navy shell jacket", "Black wool overcoat", "Striped breton tee", "Grey wool trousers", "Packed lamp")
	f.expect("location="+f.b1, "Navy shell jacket", "Black wool overcoat")
	f.expect("location=unknown", "Green flannel")
	f.expect("location=code:HOME", "Blue jeans", "Polaroid camera")
	f.expect("status=staged", "Blue jeans", "USB-C cable")
	f.expect("status=approximate", "Grey wool trousers")
	f.expect("status=unknown", "Green flannel")
	f.expect("status=placed", "Navy shell jacket", "Black wool overcoat", "Striped breton tee", "Polaroid camera", "Packed lamp")
	f.expect("status=gone&lifecycle=all", "Trench coat")
	f.expect("status=gone") // gone items are hidden by default
	f.expect("tag=rain", "Navy shell jacket", "Striped breton tee")
	f.expect("tag=RAIN&tag=summer", "Navy shell jacket", "Striped breton tee")
	f.expect("review=needs_review", "Green flannel")
	f.expect("lifecycle=gone", "Trench coat")
	f.expect("attr.color=navy", "Navy shell jacket", "Striped breton tee") // case-insensitive, array element
	f.expect("attr.color=WHITE", "Striped breton tee")
	f.expect("attr.color=navy,black", "Navy shell jacket", "Black wool overcoat", "Striped breton tee")
	f.expect("attr.color=navy&attr.size=m", "Navy shell jacket")
	f.expect("attr.size=32", "Grey wool trousers") // numbers compare as text
	f.expect("kind=clothing&location=code:U2&attr.color=navy&tag=summer", "Striped breton tee")
	for _, bad := range []string{"kind=car", "status=lost", "review=maybe", "lifecycle=sometimes", "location=code:NOPE", "sort=price", "limit=0", "cursor=notbase64!", "attr.bad%20key=x"} {
		if rec := f.do("GET", "/api/items?"+bad, nil, 400, nil); rec.Code != 400 {
			t.Errorf("%s: %d", bad, rec.Code)
		}
	}

	// The SQL status must agree with locations.DerivePlacement for every item.
	l := f.list("lifecycle=all&limit=200")
	for _, it := range l.Items {
		got := f.titlesFor("lifecycle=all&status=" + it.Placement.Status)
		found := false
		for _, title := range got {
			found = found || title == it.Title
		}
		if !found {
			t.Errorf("%s: placement %s but status filter disagrees (%v)", it.Title, it.Placement.Status, got)
		}
	}
	// Moving the box that holds the target makes no difference to staging.
	var d ItemDetail
	f.do("GET", "/api/items/"+f.ids["Packed lamp"], nil, 200, &d)
	if d.Placement.Status != model.PlacementPlaced {
		t.Fatalf("item inside its target should be placed, got %s", d.Placement.Status)
	}
}

func TestFacets(t *testing.T) {
	f := newFixture(t)
	get := func(q string) Facets {
		var fc Facets
		f.do("GET", "/api/facets?"+q, nil, 200, &fc)
		return fc
	}
	group := func(fc Facets, field string) map[string]int {
		for _, g := range fc.Groups {
			if g.Field == field {
				m := map[string]int{}
				for _, v := range g.Values {
					m[v.Value] = v.Count
				}
				return m
			}
		}
		return nil
	}
	fc := get("")
	if fc.Total != 9 {
		t.Fatalf("total: %d", fc.Total)
	}
	var fields []string
	for _, g := range fc.Groups {
		fields = append(fields, g.Field)
		if g.Values == nil {
			t.Errorf("group %s values must be []", g.Field)
		}
		sorted := sort.SliceIsSorted(g.Values, func(i, j int) bool {
			if g.Values[i].Count != g.Values[j].Count {
				return g.Values[i].Count > g.Values[j].Count
			}
			return strings.ToLower(g.Values[i].Label) < strings.ToLower(g.Values[j].Label)
		})
		if !sorted {
			t.Errorf("group %s not ordered by count desc, label: %+v", g.Field, g.Values)
		}
	}
	// Filterable attrs in fact_key sort order (brand 1, color 2, size 3); empty ones omitted.
	if strings.Join(fields, ",") != "kind,status,category,location,attr.brand,attr.color,attr.size,tag,review" {
		t.Fatalf("group order: %v", fields)
	}
	if k := group(fc, "kind"); k["clothing"] != 6 || k["object"] != 2 || k["utility"] != 1 {
		t.Errorf("kind: %v", k)
	}
	if s := group(fc, "status"); s["placed"] != 5 || s["staged"] != 2 || s["approximate"] != 1 || s["unknown"] != 1 || s["gone"] != 0 {
		t.Errorf("status: %v", s)
	}
	if c := group(fc, "category"); c["outerwear"] != 3 || c["bottoms"] != 2 || c["tops"] != 1 {
		t.Errorf("category top segment: %v", c)
	}
	// Location: top two levels, descendant counts, plus unknown.
	loc := group(fc, "location")
	if loc[f.ss] != 6 || loc[f.u2] != 5 || loc[f.u1] != 1 || loc[f.home] != 2 || loc[f.bed] != 2 || loc["unknown"] != 1 || loc[f.b1] != 0 || loc[f.sh] != 0 {
		t.Errorf("location: %v", loc)
	}
	for _, g := range fc.Groups {
		if g.Field == "location" {
			for _, v := range g.Values {
				if v.Value == f.u2 && v.Label != "Unit 2" {
					t.Errorf("location label: %+v", v)
				}
			}
		}
	}
	if c := group(fc, "attr.color"); c["Navy"]+c["navy"] != 2 || c["black"] != 1 || c["white"] != 1 {
		t.Errorf("color (case-folded, array elements): %v", c)
	}
	if tg := group(fc, "tag"); tg["rain"] != 2 || tg["summer"] != 1 {
		t.Errorf("tag: %v", tg)
	}
	if r := group(fc, "review"); r["ok"] != 8 || r["needs_review"] != 1 {
		t.Errorf("review: %v", r)
	}

	// A group's own filter is excluded from its counts; others apply.
	fc = get("kind=object")
	if fc.Total != 2 {
		t.Errorf("filtered total: %d", fc.Total)
	}
	if k := group(fc, "kind"); k["clothing"] != 6 || k["object"] != 2 {
		t.Errorf("kind ignores its own filter: %v", k)
	}
	if s := group(fc, "status"); s["placed"] != 2 || s["staged"] != 0 {
		t.Errorf("status honours kind filter: %v", s)
	}
	if group(fc, "attr.size") != nil {
		t.Errorf("empty attr group should be omitted")
	}
	fc = get("attr.color=navy")
	if c := group(fc, "attr.color"); c["black"] != 1 || c["grey"] != 1 {
		t.Errorf("color ignores its own filter: %v", c)
	}
	if b := group(fc, "attr.brand"); len(b) != 1 || b["Uniqlo"] != 1 {
		t.Errorf("brand honours color filter: %v", b)
	}
	fc = get("status=staged&location=code:U2")
	if s := group(fc, "status"); s["placed"] != 4 || s["approximate"] != 1 || s["staged"] != 0 {
		t.Errorf("status ignores own filter, honours location: %v", s)
	}
	if loc := group(fc, "location"); loc[f.ss] != 1 || loc[f.u1] != 1 || loc[f.home] != 1 {
		t.Errorf("location ignores own filter, honours status: %v", loc)
	}
	fc = get("tag=summer&q=breton")
	if tg := group(fc, "tag"); tg["rain"] != 1 || tg["summer"] != 1 || fc.Total != 1 {
		t.Errorf("tag with q: %v (total %d)", tg, fc.Total)
	}
	fc = get("attr.size=XXL")
	if group(fc, "attr.size") == nil {
		t.Errorf("an active attr filter keeps its group")
	}
	f.do("GET", "/api/facets?kind=car", nil, 400, nil)
}

func TestCursorPaging(t *testing.T) {
	e := newEnv(t)
	// 23 items; many share a title and timestamps to exercise tie-breaks.
	for i := 0; i < 23; i++ {
		title := fmt.Sprintf("Item %02d", i%5)
		e.item(map[string]any{"kind": "object", "title": title, "notes": "widget"})
	}
	e.exec(`UPDATE item SET updated_at = '2026-01-01T00:00:00.000Z' WHERE rowid % 3 = 0`)
	for _, sortBy := range []string{"updated", "created", "title", "relevance"} {
		base := "sort=" + sortBy + "&q=widget"
		all := e.list(base + "&limit=200")
		if all.Total != 23 || len(all.Items) != 23 || all.NextCursor != nil {
			t.Fatalf("%s: full list %d/%d", sortBy, all.Total, len(all.Items))
		}
		var paged []string
		var cursor string
		pages := 0
		for {
			q := base + "&limit=4"
			if cursor != "" {
				q += "&cursor=" + url.QueryEscape(cursor)
			}
			l := e.list(q)
			if l.Total != 23 && l.Total != 24 {
				t.Fatalf("%s: total per page %d", sortBy, l.Total)
			}
			for _, it := range l.Items {
				paged = append(paged, it.ID)
			}
			pages++
			if pages == 2 && sortBy != "relevance" {
				// A write between pages must not duplicate or skip existing items.
				e.item(map[string]any{"kind": "object", "title": "AAA new", "notes": "widget"})
			}
			if l.NextCursor == nil {
				break
			}
			cursor = *l.NextCursor
		}
		var want []string
		for _, it := range all.Items {
			want = append(want, it.ID)
		}
		// The new item sorts first for updated/created/title, so it never shows up on later pages.
		if strings.Join(paged, ",") != strings.Join(want, ",") {
			t.Errorf("%s: paged order differs\n got %v\nwant %v", sortBy, paged, want)
		}
		seen := map[string]bool{}
		for _, id := range paged {
			if seen[id] {
				t.Errorf("%s: duplicate %s", sortBy, id)
			}
			seen[id] = true
		}
		for _, id := range want {
			if !seen[id] {
				t.Errorf("%s: skipped %s", sortBy, id)
			}
		}
		e.exec(`DELETE FROM item_fts WHERE item_id IN (SELECT id FROM item WHERE title = 'AAA new')`)
		e.exec(`DELETE FROM item WHERE title = 'AAA new'`)
	}
	// Title sort is A→Z, updated is newest first.
	l := e.list("sort=title&limit=200")
	for i := 1; i < len(l.Items); i++ {
		if strings.ToLower(l.Items[i-1].Title) > strings.ToLower(l.Items[i].Title) {
			t.Fatalf("title order: %v", titles(l))
		}
	}
	l = e.list("limit=200")
	for i := 1; i < len(l.Items); i++ {
		if l.Items[i-1].UpdatedAt < l.Items[i].UpdatedAt {
			t.Fatalf("updated order")
		}
	}
	if l := e.list(""); len(l.Items) != 23 || l.NextCursor != nil {
		t.Fatalf("default limit 60: %d", len(l.Items))
	}
	// A cursor from another sort is rejected.
	l = e.list("sort=title&limit=2")
	e.do("GET", "/api/items?sort=created&cursor="+url.QueryEscape(*l.NextCursor), nil, 400, nil)
	// limit is capped at 200.
	for i := 0; i < 180; i++ {
		id := db.NewID()
		now := db.Now()
		e.exec(`INSERT INTO item (id, kind, title, created_at, updated_at) VALUES (?, 'object', 'bulk', ?, ?)`, id, now, now)
	}
	if l := e.list("limit=500"); len(l.Items) != 200 || l.NextCursor == nil || l.Total != 203 {
		t.Fatalf("limit cap: %d %d", len(l.Items), l.Total)
	}
}
