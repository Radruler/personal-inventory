package locations

import (
	"database/sql"
	"net/http"
	"slices"
	"strings"

	"github.com/radruler/personal-inventory/internal/httpapi"
	"github.com/radruler/personal-inventory/internal/model"
)

// staging handles GET /api/staging: owned items with a target, grouped by
// target (groups ordered by target label, items newest first).
func (h *handlers) staging(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	q := h.d.DB
	rows, err := q.QueryContext(ctx, `SELECT id, target_location_id FROM item
		WHERE lifecycle = 'owned' AND target_location_id IS NOT NULL
		ORDER BY created_at DESC, id DESC`)
	if err != nil {
		return err
	}
	defer rows.Close()
	var ids, targets []string
	targetOf := map[string]string{}
	for rows.Next() {
		var id string
		var target sql.NullString
		if err := rows.Scan(&id, &target); err != nil {
			return err
		}
		ids = append(ids, id)
		targetOf[id] = target.String
		targets = append(targets, target.String)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	rows.Close()

	out := Staging{Groups: []StagingGroup{}}
	if err := q.QueryRowContext(ctx, `SELECT count(*) FROM item
		WHERE lifecycle = 'owned' AND location_id IS NULL`).Scan(&out.UnknownCount); err != nil {
		return err
	}
	refs, err := Refs(ctx, q, targets)
	if err != nil {
		return err
	}
	cards, err := h.summaries(ctx, q, ids)
	if err != nil {
		return err
	}
	groups := map[string]*StagingGroup{}
	for _, c := range cards {
		tid := targetOf[c.ID]
		g, ok := groups[tid]
		if !ok {
			g = &StagingGroup{Target: refs[tid], Items: []model.ItemSummary{}}
			groups[tid] = g
		}
		g.Items = append(g.Items, c)
	}
	for _, g := range groups {
		out.Groups = append(out.Groups, *g)
	}
	slices.SortFunc(out.Groups, func(a, b StagingGroup) int {
		if c := naturalCompare(strings.ToLower(a.Target.Label), strings.ToLower(b.Target.Label)); c != 0 {
			return c
		}
		return strings.Compare(a.Target.ID, b.Target.ID)
	})
	httpapi.WriteJSON(w, http.StatusOK, out)
	return nil
}
