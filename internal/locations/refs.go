package locations

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/radruler/personal-inventory/internal/db"
	"github.com/radruler/personal-inventory/internal/httpapi"
	"github.com/radruler/personal-inventory/internal/model"
)

// ResolveRef turns a location reference into an id. A reference is either a
// location id or "code:<CODE>" (case-insensitive). Empty input returns "".
// Frozen signature (used by items and ingest).
func ResolveRef(ctx context.Context, q db.Querier, ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", nil
	}
	var id string
	var err error
	if code, ok := strings.CutPrefix(ref, "code:"); ok {
		err = q.QueryRowContext(ctx, `SELECT id FROM location WHERE code = ?`, code).Scan(&id)
	} else {
		err = q.QueryRowContext(ctx, `SELECT id FROM location WHERE id = ?`, ref).Scan(&id)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return "", httpapi.Validation("unknown location %q", ref)
	}
	return id, err
}

// Refs loads LocationRefs (with full paths) for ids. Missing ids are absent.
// Frozen signature (used by items, media and ingest).
func Refs(ctx context.Context, q db.Querier, ids []string) (map[string]model.LocationRef, error) {
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
//	staged      target set                             "Bedroom → going to Unit 2"
//	unknown     no location                            "Location unknown"
//	approximate location is a site or unit             "Somewhere in Unit 2 (box unknown)"
//	placed      location is a room, shelf or container "Unit 2 › B-014"
//
// Rooms count as placed: an item in the bedroom is where it belongs, while an
// item "in Unit 2" is expected to be in some box there.
// Frozen signature (used by items); rules belong to thread C.
func DerivePlacement(lifecycle string, goneReason, goneAt *string, loc, target *model.LocationRef) model.Placement {
	p := model.Placement{Location: loc, Target: target, GoneReason: goneReason, GoneAt: goneAt}
	switch {
	case lifecycle == model.LifecycleGone:
		p.Status = model.PlacementGone
		label := "Gone"
		if goneReason != nil && *goneReason != "" {
			label = strings.ToUpper((*goneReason)[:1]) + (*goneReason)[1:]
		}
		if goneAt != nil && len(*goneAt) >= 7 {
			label += " · " + (*goneAt)[:7]
		}
		p.Label = label
	case target != nil:
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
