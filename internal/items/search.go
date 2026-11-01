package items

import (
	"context"
	"database/sql"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"unicode"

	"github.com/radruler/personal-inventory/internal/db"
	"github.com/radruler/personal-inventory/internal/httpapi"
	"github.com/radruler/personal-inventory/internal/locations"
	"github.com/radruler/personal-inventory/internal/model"
)

// statusExpr computes the placement status in SQL, in the same order as
// locations.DerivePlacement: gone → staged → unknown → approximate → placed.
// An item already at its target, or inside it, is not staged
// (locations.Within).
const statusExpr = `(CASE
	WHEN i.lifecycle = 'gone' THEN 'gone'
	WHEN i.target_location_id IS NOT NULL AND (i.location_id IS NULL OR i.target_location_id NOT IN (
	  WITH RECURSIVE up(id, depth) AS (
	    SELECT i.location_id, 0
	    UNION SELECT l.parent_id, up.depth + 1 FROM location l JOIN up ON l.id = up.id
	     WHERE l.parent_id IS NOT NULL AND up.depth < 32)
	  SELECT id FROM up)) THEN 'staged'
	WHEN i.location_id IS NULL THEN 'unknown'
	WHEN (SELECT kind FROM location WHERE id = i.location_id) IN ('site', 'unit') THEN 'approximate'
	ELSE 'placed' END)`

// attrValueExpr renders one json_each row as a comparable string.
const attrValueExpr = `(CASE je.type WHEN 'true' THEN 'true' WHEN 'false' THEN 'false' ELSE CAST(je.value AS TEXT) END)`

// bm25 column weights: item_id, title, category, notes, tags, observations, identifiers, attrs.
const bm25Expr = `bm25(item_fts, 0, 10, 4, 1, 4, 2, 5, 4)`

var placementStatuses = []string{model.PlacementGone, model.PlacementStaged, model.PlacementUnknown, model.PlacementApproximate, model.PlacementPlaced}

var statusLabels = map[string]string{
	model.PlacementGone: "Gone", model.PlacementStaged: "Staged", model.PlacementUnknown: "Unknown",
	model.PlacementApproximate: "Approximate", model.PlacementPlaced: "Placed",
}

// filters is the parsed filter set shared by GET /api/items and /api/facets.
type filters struct {
	match      string // FTS5 expression ("" = no full-text part)
	norm       string // normalized identifier for exact matches
	hasQ       bool
	kinds      []string
	categories []string
	locSet     bool
	locUnknown bool
	locID      string
	statuses   []string
	tags       []string
	review     string
	lifecycle  string
	product    string
	attrs      map[string][]string // key → lower-cased values
}

// multi returns all values of a parameter, splitting on commas.
func multi(qv url.Values, name string) []string {
	var out []string
	for _, v := range qv[name] {
		for _, p := range strings.Split(v, ",") {
			if p = strings.TrimSpace(p); p != "" {
				out = append(out, p)
			}
		}
	}
	return out
}

func parseFilters(ctx context.Context, q db.Querier, qv url.Values) (*filters, error) {
	f := &filters{lifecycle: model.LifecycleOwned, attrs: map[string][]string{}}
	if s := strings.TrimSpace(qv.Get("q")); s != "" {
		f.match, f.norm = ftsQuery(s), NormIdentifier(s)
		f.hasQ = f.match != "" || f.norm != ""
	}
	f.kinds = multi(qv, "kind")
	for _, k := range f.kinds {
		if !contains(model.Kinds, k) {
			return nil, httpapi.BadRequest("kind: %q must be one of %s", k, strings.Join(model.Kinds, ", "))
		}
	}
	for _, c := range multi(qv, "category") {
		f.categories = append(f.categories, strings.Trim(c, "/"))
	}
	if l := strings.TrimSpace(qv.Get("location")); l != "" {
		f.locSet = true
		if strings.EqualFold(l, "unknown") {
			f.locUnknown = true
		} else {
			id, err := locations.ResolveRef(ctx, q, l)
			if err != nil {
				return nil, asBadRequest(err)
			}
			f.locID = id
		}
	}
	f.statuses = multi(qv, "status")
	for _, s := range f.statuses {
		if !contains(placementStatuses, s) {
			return nil, httpapi.BadRequest("status: %q must be one of %s", s, strings.Join(placementStatuses, ", "))
		}
	}
	f.tags = multi(qv, "tag")
	if s := strings.TrimSpace(qv.Get("review")); s != "" {
		if s != model.ReviewOK && s != model.ReviewNeedsReview {
			return nil, httpapi.BadRequest("review: must be ok or needs_review")
		}
		f.review = s
	}
	if s := strings.TrimSpace(qv.Get("lifecycle")); s != "" {
		if s != model.LifecycleOwned && s != model.LifecycleGone && s != "all" {
			return nil, httpapi.BadRequest("lifecycle: must be owned, gone or all")
		}
		f.lifecycle = s
	}
	f.product = strings.TrimSpace(qv.Get("product"))
	for name := range qv {
		key, ok := strings.CutPrefix(name, "attr.")
		if !ok {
			continue
		}
		if !validKey(key) {
			return nil, httpapi.BadRequest("%s: not a valid attribute key", name)
		}
		for _, v := range multi(qv, name) {
			f.attrs[key] = append(f.attrs[key], strings.ToLower(v))
		}
	}
	return f, nil
}

// ftsQuery turns free text into an FTS5 expression: every word is
// prefix-matched and the words are ANDed. Words are split the way the
// unicode61 tokenizer splits them, then quoted, so user input can never be
// FTS syntax.
func ftsQuery(s string) string {
	words := strings.FieldsFunc(s, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsNumber(r) })
	parts := make([]string, 0, len(words))
	for _, w := range words {
		parts = append(parts, `"`+strings.ReplaceAll(w, `"`, `""`)+`"*`)
	}
	return strings.Join(parts, " AND ")
}

// identMatch matches items carrying the identifier, or whose product does.
const identMatch = `(i.id IN (SELECT subject_id FROM identifier WHERE subject_type = 'item' AND value_norm = ?)
	OR i.product_id IN (SELECT subject_id FROM identifier WHERE subject_type = 'product' AND value_norm = ?))`

// where renders the filter set as SQL over `item i`, leaving out the group
// named by skip ("kind", "status", "category", "location", "tag", "review"
// or "attr.<key>") for facet counts.
func (f *filters) where(skip string) (string, []any) {
	var cl []string
	var args []any
	switch f.lifecycle {
	case model.LifecycleOwned, model.LifecycleGone:
		cl, args = append(cl, `i.lifecycle = ?`), append(args, f.lifecycle)
	}
	if f.hasQ {
		var or []string
		if f.match != "" {
			or, args = append(or, `i.id IN (SELECT item_id FROM item_fts WHERE item_fts MATCH ?)`), append(args, f.match)
		}
		if f.norm != "" {
			or, args = append(or, identMatch), append(args, f.norm, f.norm)
		}
		cl = append(cl, `(`+strings.Join(or, ` OR `)+`)`)
	}
	if len(f.kinds) > 0 && skip != "kind" {
		cl, args = append(cl, `i.kind IN (`+db.Placeholders(len(f.kinds))+`)`), append(args, db.Args(f.kinds)...)
	}
	if len(f.categories) > 0 && skip != "category" {
		var or []string
		for _, c := range f.categories {
			or = append(or, `(i.category = ? COLLATE NOCASE OR i.category LIKE ? ESCAPE '\')`)
			args = append(args, c, likeEscape(c)+"/%")
		}
		cl = append(cl, `(`+strings.Join(or, ` OR `)+`)`)
	}
	if f.locSet && skip != "location" {
		if f.locUnknown {
			cl = append(cl, `i.location_id IS NULL`)
		} else {
			cl = append(cl, `i.location_id IN (WITH RECURSIVE sub(id) AS (
				SELECT ? UNION SELECT l.id FROM location l JOIN sub ON l.parent_id = sub.id) SELECT id FROM sub)`)
			args = append(args, f.locID)
		}
	}
	if len(f.statuses) > 0 && skip != "status" {
		cl, args = append(cl, statusExpr+` IN (`+db.Placeholders(len(f.statuses))+`)`), append(args, db.Args(f.statuses)...)
	}
	if len(f.tags) > 0 && skip != "tag" {
		cl = append(cl, `i.id IN (SELECT item_id FROM tag WHERE tag IN (`+db.Placeholders(len(f.tags))+`))`)
		args = append(args, db.Args(f.tags)...)
	}
	if f.review != "" && skip != "review" {
		cl, args = append(cl, `i.review_state = ?`), append(args, f.review)
	}
	if f.product != "" {
		cl, args = append(cl, `i.product_id = ?`), append(args, f.product)
	}
	for _, k := range sortedKeys(f.attrs) {
		if skip == "attr."+k {
			continue
		}
		vals := f.attrs[k]
		cl = append(cl, `EXISTS (SELECT 1 FROM json_each(i.attrs, ?) je WHERE lower(`+attrValueExpr+`) IN (`+db.Placeholders(len(vals))+`))`)
		args = append(append(args, attrPath(k)), db.Args(vals)...)
	}
	if len(cl) == 0 {
		return "1", nil
	}
	return strings.Join(cl, ` AND `), args
}

// attrPath is the JSON path of a top-level attrs key (keys may contain dots).
func attrPath(key string) string { return `$."` + key + `"` }

// ---------------------------------------------------------------- list

func (a *api) listItems(w http.ResponseWriter, r *http.Request) error {
	ctx, qv := r.Context(), r.URL.Query()
	f, err := parseFilters(ctx, a.d.DB, qv)
	if err != nil {
		return err
	}
	limit, err := httpapi.Limit(r, 60, 200)
	if err != nil {
		return err
	}
	sortBy := qv.Get("sort")
	switch sortBy {
	case "":
		sortBy = "updated"
	case "relevance", "updated", "created", "title":
	default:
		return httpapi.BadRequest("sort: must be relevance, updated, created or title")
	}
	if sortBy == "relevance" && !f.hasQ {
		sortBy = "updated"
	}
	cur, err := httpapi.DecodeCursor(qv.Get("cursor"))
	if err != nil {
		return err
	}
	where, wargs := f.where("")

	// Inner query: id plus the sort key k.
	var inner string
	var args []any
	dir := "DESC"
	switch sortBy {
	case "updated":
		inner = `SELECT i.id AS id, i.updated_at AS k FROM item i WHERE ` + where
	case "created":
		inner = `SELECT i.id AS id, i.created_at AS k FROM item i WHERE ` + where
	case "title":
		inner, dir = `SELECT i.id AS id, lower(i.title) AS k FROM item i WHERE `+where, "ASC"
	case "relevance":
		// Exact identifier matches rank first, then bm25 (lower is better).
		// Placeholders appear in text order: score, then FROM, then WHERE.
		dir = "ASC"
		score := `0.0`
		from := `item i`
		var fromArgs []any
		if f.match != "" {
			from = `item i LEFT JOIN (SELECT item_id AS fid, ` + bm25Expr + ` AS score FROM item_fts WHERE item_fts MATCH ?) f ON f.fid = i.id`
			score = `COALESCE(f.score, 0.0)`
			fromArgs = append(fromArgs, f.match)
		}
		if f.norm != "" {
			score += ` - CASE WHEN ` + identMatch + ` THEN 1000000.0 ELSE 0.0 END`
			args = append(args, f.norm, f.norm)
		}
		args = append(args, fromArgs...)
		inner = `SELECT i.id AS id, (` + score + `) AS k FROM ` + from + ` WHERE ` + where
	}
	args = append(args, wargs...)
	query := `SELECT id, k FROM (` + inner + `)`
	if cur != nil {
		if s, _ := cur["s"].(string); s != sortBy {
			return httpapi.BadRequest("cursor does not match sort=%s", sortBy)
		}
		cid, ok := cur["id"].(string)
		k, okk := cur["k"]
		if !ok || !okk {
			return httpapi.BadRequest("invalid cursor")
		}
		cmp := "<"
		if dir == "ASC" {
			cmp = ">"
		}
		query += ` WHERE (k ` + cmp + ` ? OR (k = ? AND id ` + cmp + ` ?))`
		args = append(args, k, k, cid)
	}
	query += ` ORDER BY k ` + dir + `, id ` + dir + ` LIMIT ?`
	args = append(args, limit+1)

	rows, err := a.d.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	type key struct {
		id string
		k  any
	}
	var keys []key
	for rows.Next() {
		var k key
		if err := rows.Scan(&k.id, &k.k); err != nil {
			rows.Close()
			return err
		}
		keys = append(keys, k)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	out := ItemList{Items: []model.ItemSummary{}}
	if len(keys) > limit {
		last := keys[limit-1]
		c := httpapi.EncodeCursor(httpapi.Cursor{"s": sortBy, "k": last.k, "id": last.id})
		out.NextCursor = &c
		keys = keys[:limit]
	}
	ids := make([]string, len(keys))
	for i, k := range keys {
		ids[i] = k.id
	}
	if out.Items, err = Summaries(ctx, a.d.DB, ids); err != nil {
		return err
	}
	if err := a.d.DB.QueryRowContext(ctx, `SELECT count(*) FROM item i WHERE `+where, wargs...).Scan(&out.Total); err != nil {
		return err
	}
	httpapi.WriteJSON(w, http.StatusOK, out)
	return nil
}

// ---------------------------------------------------------------- facets

func (a *api) getFacets(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	f, err := parseFilters(ctx, a.d.DB, r.URL.Query())
	if err != nil {
		return err
	}
	fc := facetCtx{ctx: ctx, q: a.d.DB, f: f}
	out := Facets{Groups: []FacetGroup{}}
	where, args := f.where("")
	if err := a.d.DB.QueryRowContext(ctx, `SELECT count(*) FROM item i WHERE `+where, args...).Scan(&out.Total); err != nil {
		return err
	}

	kindLabels, err := labelMap(ctx, a.d.DB, `SELECT kind, label FROM item_kind`)
	if err != nil {
		return err
	}
	g, err := fc.simple("kind", "Kind", `i.kind`, kindLabels)
	if err != nil {
		return err
	}
	out.Groups = append(out.Groups, g)

	if g, err = fc.simple("status", "Where", statusExpr, statusLabels); err != nil {
		return err
	}
	out.Groups = append(out.Groups, g)

	if g, err = fc.simple("category", "Category",
		`(CASE WHEN instr(i.category, '/') > 0 THEN substr(i.category, 1, instr(i.category, '/') - 1) ELSE i.category END)`, nil); err != nil {
		return err
	}
	out.Groups = append(out.Groups, g)

	if g, err = fc.location(); err != nil {
		return err
	}
	out.Groups = append(out.Groups, g)

	keys, err := factKeys(ctx, a.d.DB, `WHERE filterable = 1`)
	if err != nil {
		return err
	}
	for _, k := range keys {
		g, err := fc.attr(k.Key, k.Label)
		if err != nil {
			return err
		}
		if _, active := f.attrs[k.Key]; len(g.Values) > 0 || active {
			out.Groups = append(out.Groups, g)
		}
	}

	if g, err = fc.tags(); err != nil {
		return err
	}
	out.Groups = append(out.Groups, g)

	if g, err = fc.simple("review", "Review", `i.review_state`, map[string]string{model.ReviewOK: "OK", model.ReviewNeedsReview: "Needs review"}); err != nil {
		return err
	}
	out.Groups = append(out.Groups, g)

	httpapi.WriteJSON(w, http.StatusOK, out)
	return nil
}

type facetCtx struct {
	ctx context.Context
	q   db.Querier
	f   *filters
}

// counts runs a value/count query and returns facet values ordered by count
// descending, then label.
func (fc facetCtx) counts(query string, args []any, labels map[string]string) ([]FacetValue, error) {
	rows, err := fc.q.QueryContext(fc.ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []FacetValue{}
	for rows.Next() {
		var v sql.NullString
		var n int
		if err := rows.Scan(&v, &n); err != nil {
			return nil, err
		}
		if !v.Valid || v.String == "" || n == 0 {
			continue
		}
		label := v.String
		if l, ok := labels[v.String]; ok {
			label = l
		}
		out = append(out, FacetValue{Value: v.String, Label: label, Count: n})
	}
	sortFacet(out)
	return out, rows.Err()
}

func sortFacet(vs []FacetValue) {
	sort.SliceStable(vs, func(i, j int) bool {
		if vs[i].Count != vs[j].Count {
			return vs[i].Count > vs[j].Count
		}
		return strings.ToLower(vs[i].Label) < strings.ToLower(vs[j].Label)
	})
}

func (fc facetCtx) simple(field, label, expr string, labels map[string]string) (FacetGroup, error) {
	where, args := fc.f.where(field)
	vals, err := fc.counts(`SELECT `+expr+` AS v, count(*) FROM item i WHERE `+where+` GROUP BY v`, args, labels)
	return FacetGroup{Field: field, Label: label, Values: vals}, err
}

func (fc facetCtx) tags() (FacetGroup, error) {
	where, args := fc.f.where("tag")
	vals, err := fc.counts(`SELECT min(t.tag), count(*) FROM tag t JOIN item i ON i.id = t.item_id WHERE `+where+` GROUP BY t.tag`, args, nil)
	return FacetGroup{Field: "tag", Label: "Tags", Values: vals}, err
}

func (fc facetCtx) attr(key, label string) (FacetGroup, error) {
	where, wargs := fc.f.where("attr." + key)
	args := append([]any{attrPath(key)}, wargs...)
	vals, err := fc.counts(`SELECT min(v), count(DISTINCT id) FROM (
		SELECT i.id AS id, `+attrValueExpr+` AS v FROM item i, json_each(i.attrs, ?) je
		 WHERE je.type NOT IN ('object', 'array', 'null') AND `+where+`) GROUP BY lower(v)`, args, nil)
	return FacetGroup{Field: "attr." + key, Label: label, Values: vals}, err
}

// location counts items per location for the top two levels of the tree,
// each including its descendants, plus the items with no location.
func (fc facetCtx) location() (FacetGroup, error) {
	g := FacetGroup{Field: "location", Label: "Location", Values: []FacetValue{}}
	where, args := fc.f.where("location")
	rows, err := fc.q.QueryContext(fc.ctx, `SELECT i.location_id, count(*) FROM item i WHERE `+where+` GROUP BY i.location_id`, args...)
	if err != nil {
		return g, err
	}
	perLoc := map[string]int{}
	unknown := 0
	for rows.Next() {
		var id sql.NullString
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			rows.Close()
			return g, err
		}
		if id.Valid {
			perLoc[id.String] = n
		} else {
			unknown = n
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return g, err
	}
	type node struct {
		parent string
		crumb  model.LocationCrumb
	}
	nodes := map[string]node{}
	lrows, err := fc.q.QueryContext(fc.ctx, `SELECT id, parent_id, code, name, kind FROM location`)
	if err != nil {
		return g, err
	}
	for lrows.Next() {
		var id, name, kind string
		var parent, code sql.NullString
		if err := lrows.Scan(&id, &parent, &code, &name, &kind); err != nil {
			lrows.Close()
			return g, err
		}
		nodes[id] = node{parent: parent.String, crumb: model.LocationCrumb{ID: id, Code: nullStr(code), Name: name, Kind: kind}}
	}
	lrows.Close()
	if err := lrows.Err(); err != nil {
		return g, err
	}
	counts := map[string]int{}
	for id, n := range perLoc {
		// Path root → id.
		var path []string
		for cur, guard := id, 0; cur != "" && guard < 64; guard++ {
			path = append([]string{cur}, path...)
			cur = nodes[cur].parent
		}
		for d := 0; d < len(path) && d < 2; d++ {
			counts[path[d]] += n
		}
	}
	for id, n := range counts {
		nd := nodes[id]
		crumbs := []model.LocationCrumb{nd.crumb}
		if nd.parent != "" {
			crumbs = []model.LocationCrumb{nodes[nd.parent].crumb, nd.crumb}
		}
		g.Values = append(g.Values, FacetValue{Value: id, Label: locations.PathLabel(crumbs), Count: n})
	}
	if unknown > 0 {
		g.Values = append(g.Values, FacetValue{Value: "unknown", Label: "Location unknown", Count: unknown})
	}
	sortFacet(g.Values)
	return g, nil
}

func labelMap(ctx context.Context, q db.Querier, query string) (map[string]string, error) {
	rows, err := q.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, rows.Err()
}
