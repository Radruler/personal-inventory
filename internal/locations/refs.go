package locations

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/radruler/personal-inventory/internal/db"
	"github.com/radruler/personal-inventory/internal/httpapi"
	"github.com/radruler/personal-inventory/internal/model"
)

// ResolveRef turns a location reference into an id. A reference is either a
// location id or "code:<CODE>" (prefix and code case-insensitive). Empty
// input returns "". An unknown reference is a 422 validation error, because
// references usually arrive in request bodies; path parameters use lookup and
// answer 404 instead. Frozen signature (used by items and ingest).
func ResolveRef(ctx context.Context, q db.Querier, ref string) (string, error) {
	id, ok, err := lookup(ctx, q, ref)
	if err != nil || ok || strings.TrimSpace(ref) == "" {
		return id, err
	}
	return "", httpapi.Validation("unknown location %q", ref)
}

// lookup resolves ref like ResolveRef but reports "not found" as ok=false.
func lookup(ctx context.Context, q db.Querier, ref string) (string, bool, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", false, nil
	}
	var id string
	var err error
	if code, ok := cutCodePrefix(ref); ok {
		// location.code is COLLATE NOCASE, so = is case-insensitive.
		err = q.QueryRowContext(ctx, `SELECT id FROM location WHERE code = ?`, strings.TrimSpace(code)).Scan(&id)
	} else {
		err = q.QueryRowContext(ctx, `SELECT id FROM location WHERE id = ?`, ref).Scan(&id)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return id, true, nil
}

func cutCodePrefix(ref string) (string, bool) {
	if len(ref) >= 5 && strings.EqualFold(ref[:5], "code:") {
		return ref[5:], true
	}
	return "", false
}

// Refs loads LocationRefs (with full paths) for ids. Missing ids are absent.
// Frozen signature (used by items, media and ingest).
func Refs(ctx context.Context, q db.Querier, ids []string) (map[string]model.LocationRef, error) {
	ids = uniqueNonEmpty(ids)
	out := make(map[string]model.LocationRef, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	// Walk up from each requested id; locations form a shallow tree.
	rows, err := q.QueryContext(ctx, `
		WITH RECURSIVE up(start_id, id, code, name, kind, parent_id, archived_at, depth) AS (
		  SELECT id, id, code, name, kind, parent_id, archived_at, 0 FROM location
		   WHERE id IN (`+db.Placeholders(len(ids))+`)
		  UNION ALL
		  SELECT up.start_id, l.id, l.code, l.name, l.kind, l.parent_id, l.archived_at, up.depth + 1
		    FROM location l JOIN up ON l.id = up.parent_id
		   WHERE up.depth < 32
		)
		SELECT start_id, id, code, name, kind, archived_at, depth FROM up ORDER BY start_id, depth DESC`,
		db.Args(ids)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	paths := map[string][]model.LocationCrumb{}
	archived := map[string]bool{}
	for rows.Next() {
		var start, id, name, kind string
		var code, arch sql.NullString
		var depth int
		if err := rows.Scan(&start, &id, &code, &name, &kind, &arch, &depth); err != nil {
			return nil, err
		}
		c := model.LocationCrumb{ID: id, Name: name, Kind: kind}
		if code.Valid {
			s := code.String
			c.Code = &s
		}
		paths[start] = append(paths[start], c)
		if depth == 0 && arch.Valid {
			archived[start] = true
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for id, p := range paths {
		out[id] = model.LocationRef{
			LocationCrumb: p[len(p)-1],
			Path:          p,
			Label:         PathLabel(p),
			Archived:      archived[id],
		}
	}
	return out, nil
}

// CrumbLabel is how one node is written in a path: containers and shelves by
// code when they have one ("B-014"), everything else by name ("Unit 2").
func CrumbLabel(c model.LocationCrumb) string {
	if (c.Kind == model.LocContainer || c.Kind == model.LocShelf) && c.Code != nil && *c.Code != "" {
		return *c.Code
	}
	return c.Name
}

// PathLabel renders a root→leaf path as "Unit 2 › B-014". The site is
// dropped when something more specific follows it.
func PathLabel(p []model.LocationCrumb) string {
	if len(p) > 1 && p[0].Kind == model.LocSite {
		p = p[1:]
	}
	parts := make([]string, len(p))
	for i, c := range p {
		parts[i] = CrumbLabel(c)
	}
	return strings.Join(parts, " › ")
}

// DerivePlacement computes the "where is it" status (PLAN.md §4.3):
//
//	gone        lifecycle = gone                       "Sold · 2026-03"
//	staged      target set and not yet reached         "Bedroom → going to Unit 2"
//	unknown     no location                            "Location unknown"
//	approximate location is a site or unit             "Somewhere in Unit 2 (box unknown)"
//	placed      location is a room, shelf or container "Unit 2 › B-014"
//
// Rooms count as placed: an item in the bedroom is where it belongs, while an
// item "in Unit 2" is expected to be in some box there. A target counts as
// reached when the location is the target or inside it; the item is then
// shown by where it is, not as staged.
// Frozen signature (used by items); rules belong to thread C.
func DerivePlacement(lifecycle string, goneReason, goneAt *string, loc, target *model.LocationRef) model.Placement {
	p := model.Placement{Location: loc, Target: target, GoneReason: goneReason, GoneAt: goneAt}
	switch {
	case lifecycle == model.LifecycleGone:
		p.Status = model.PlacementGone
		label := "Gone"
		if goneReason != nil && strings.TrimSpace(*goneReason) != "" {
			label = capitalize(strings.TrimSpace(*goneReason))
		}
		if goneAt != nil && len(*goneAt) >= 7 {
			label += " · " + (*goneAt)[:7]
		}
		p.Label = label
	case target != nil && !Within(loc, target.ID):
		p.Status = model.PlacementStaged
		from := "Location unknown"
		if loc != nil {
			from = loc.Label
		}
		p.Label = from + " → going to " + target.Label
	case loc == nil:
		p.Status = model.PlacementUnknown
		p.Label = "Location unknown"
	case loc.Kind == model.LocUnit:
		p.Status = model.PlacementApproximate
		p.Label = "Somewhere in " + loc.Label + " (box unknown)"
	case loc.Kind == model.LocSite:
		p.Status = model.PlacementApproximate
		p.Label = "Somewhere at " + loc.Label
	default:
		p.Status = model.PlacementPlaced
		p.Label = loc.Label
	}
	return p
}

// capitalize upper-cases the first rune ("sold" → "Sold"), UTF-8 safe.
func capitalize(s string) string {
	r, n := utf8.DecodeRuneInString(s)
	if r == utf8.RuneError {
		return s
	}
	return string(unicode.ToUpper(r)) + s[n:]
}

// uniqueNonEmpty drops empty strings and duplicates, keeping order.
func uniqueNonEmpty(ids []string) []string {
	seen := make(map[string]bool, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

// Within reports whether loc is the location id or one of its descendants.
func Within(loc *model.LocationRef, id string) bool {
	if loc == nil {
		return false
	}
	for _, c := range loc.Path {
		if c.ID == id {
			return true
		}
	}
	return loc.ID == id
}
