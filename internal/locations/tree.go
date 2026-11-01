package locations

import (
	"context"
	"database/sql"
	"slices"
	"strings"

	"github.com/radruler/personal-inventory/internal/db"
	"github.com/radruler/personal-inventory/internal/media"
	"github.com/radruler/personal-inventory/internal/model"
)

// tree is the whole location forest in memory, archived nodes included.
// Locations number in the tens or hundreds, so every read that needs counts
// or paths loads it in a handful of queries.
type tree struct {
	byID    map[string]*LocationNode
	roots   []*LocationNode
	unknown int // owned items with no location
}

// loadTree reads every location plus item counts and covers.
func loadTree(ctx context.Context, q db.Querier) (*tree, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT id, code, name, kind, parent_id, cover_media_id, notes, layout,
		       archived_at, created_at, updated_at
		  FROM location`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	t := &tree{byID: map[string]*LocationNode{}}
	var all []*LocationNode
	for rows.Next() {
		n := &LocationNode{Children: []*LocationNode{}}
		var code, parent, cover, notes, layout, archived sql.NullString
		if err := rows.Scan(&n.ID, &code, &n.Name, &n.Kind, &parent, &cover, &notes, &layout,
			&archived, &n.CreatedAt, &n.UpdatedAt); err != nil {
			return nil, err
		}
		n.Code, n.ParentID, n.CoverMediaID = nullStr(code), nullStr(parent), nullStr(cover)
		n.Notes, n.ArchivedAt = nullStr(notes), nullStr(archived)
		if layout.Valid {
			n.Layout = []byte(layout.String)
		}
		t.byID[n.ID] = n
		all = append(all, n)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()

	for _, n := range all {
		if p, ok := t.byID[deref(n.ParentID)]; ok {
			p.Children = append(p.Children, n)
		} else {
			t.roots = append(t.roots, n)
		}
	}
	sortNodes(t.roots)
	for _, n := range all {
		sortNodes(n.Children)
	}
	for _, r := range t.roots {
		setPaths(r, nil)
	}

	// Counts. Gone items never count.
	if err := countInto(ctx, q, `SELECT location_id, count(*) FROM item
		WHERE lifecycle = 'owned' AND location_id IS NOT NULL GROUP BY location_id`,
		func(id string, c int) {
			if n, ok := t.byID[id]; ok {
				n.ItemCount = c
			}
		}); err != nil {
		return nil, err
	}
	if err := countInto(ctx, q, `SELECT target_location_id, count(*) FROM item
		WHERE lifecycle = 'owned' AND target_location_id IS NOT NULL GROUP BY target_location_id`,
		func(id string, c int) {
			if n, ok := t.byID[id]; ok {
				n.StagedCount = c
			}
		}); err != nil {
		return nil, err
	}
	if err := q.QueryRowContext(ctx, `SELECT count(*) FROM item
		WHERE lifecycle = 'owned' AND location_id IS NULL`).Scan(&t.unknown); err != nil {
		return nil, err
	}
	for _, r := range t.roots {
		sumTotals(r)
	}

	// Covers: explicit cover_media_id, else the best linked photo.
	var coverIDs, need []string
	for _, n := range all {
		if n.CoverMediaID != nil {
			coverIDs = append(coverIDs, *n.CoverMediaID)
		} else {
			need = append(need, n.ID)
		}
	}
	auto, err := media.CoverFor(ctx, q, model.SubjectLocation, need)
	if err != nil {
		return nil, err
	}
	for _, mid := range auto {
		coverIDs = append(coverIDs, mid)
	}
	refs, err := media.Refs(ctx, q, coverIDs)
	if err != nil {
		return nil, err
	}
	for _, n := range all {
		mid := deref(n.CoverMediaID)
		if mid == "" {
			mid = auto[n.ID]
		}
		if m, ok := refs[mid]; ok {
			n.Cover = &m
		}
	}
	return t, nil
}

func countInto(ctx context.Context, q db.Querier, query string, set func(string, int)) error {
	rows, err := q.QueryContext(ctx, query)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var c int
		if err := rows.Scan(&id, &c); err != nil {
			return err
		}
		set(id, c)
	}
	return rows.Err()
}

func setPaths(n *LocationNode, parent []model.LocationCrumb) {
	n.path = append(slices.Clip(parent), n.crumb())
	n.Label = PathLabel(n.path)
	for _, c := range n.Children {
		setPaths(c, n.path)
	}
}

func sumTotals(n *LocationNode) int {
	n.TotalCount = n.ItemCount
	for _, c := range n.Children {
		n.TotalCount += sumTotals(c)
	}
	return n.TotalCount
}

func (n *LocationNode) crumb() model.LocationCrumb {
	return model.LocationCrumb{ID: n.ID, Code: n.Code, Name: n.Name, Kind: n.Kind}
}

// ref renders the node as a LocationRef.
func (n *LocationNode) ref() model.LocationRef {
	return model.LocationRef{LocationCrumb: n.crumb(), Path: n.path, Label: n.Label, Archived: n.ArchivedAt != nil}
}

// subtreeIDs lists n and all its descendants (archived ones included).
func (n *LocationNode) subtreeIDs() []string {
	ids := []string{n.ID}
	for _, c := range n.Children {
		ids = append(ids, c.subtreeIDs()...)
	}
	return ids
}

// pruned returns a copy of n whose children omit archived nodes (unless
// keepArchived). Counts are unchanged: they cover the whole subtree.
func (n *LocationNode) pruned(keepArchived bool) *LocationNode {
	cp := *n
	cp.Children = []*LocationNode{}
	for _, c := range n.Children {
		if keepArchived || c.ArchivedAt == nil {
			cp.Children = append(cp.Children, c.pruned(keepArchived))
		}
	}
	return &cp
}

// kindRank orders kinds outermost → innermost (model.LocationKinds).
func kindRank(k string) int {
	if i := slices.Index(model.LocationKinds, k); i >= 0 {
		return i
	}
	return len(model.LocationKinds)
}

// sortNodes orders siblings by kind (outer → inner), then code (or name when
// there is no code) in natural order (B-2 < B-10), then name, then id.
func sortNodes(ns []*LocationNode) {
	slices.SortFunc(ns, func(a, b *LocationNode) int {
		if c := kindRank(a.Kind) - kindRank(b.Kind); c != 0 {
			return c
		}
		ka, kb := a.Name, b.Name
		if a.Code != nil && *a.Code != "" {
			ka = *a.Code
		}
		if b.Code != nil && *b.Code != "" {
			kb = *b.Code
		}
		if c := naturalCompare(strings.ToLower(ka), strings.ToLower(kb)); c != 0 {
			return c
		}
		if c := strings.Compare(a.Name, b.Name); c != 0 {
			return c
		}
		return strings.Compare(a.ID, b.ID)
	})
}

// naturalCompare compares strings treating runs of ASCII digits as numbers.
func naturalCompare(a, b string) int {
	for a != "" && b != "" {
		da, dbn := digitRun(a), digitRun(b)
		if da > 0 && dbn > 0 {
			na, nb := strings.TrimLeft(a[:da], "0"), strings.TrimLeft(b[:dbn], "0")
			if len(na) != len(nb) {
				return len(na) - len(nb)
			}
			if c := strings.Compare(na, nb); c != 0 {
				return c
			}
			a, b = a[da:], b[dbn:]
			continue
		}
		if a[0] != b[0] {
			return int(a[0]) - int(b[0])
		}
		a, b = a[1:], b[1:]
	}
	return len(a) - len(b)
}

func digitRun(s string) int {
	i := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	return i
}

func nullStr(n sql.NullString) *string {
	if !n.Valid {
		return nil
	}
	s := n.String
	return &s
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
