package items

import (
	"context"
	"database/sql"
	"encoding/json"

	"github.com/radruler/personal-inventory/internal/db"
	"github.com/radruler/personal-inventory/internal/locations"
	"github.com/radruler/personal-inventory/internal/media"
	"github.com/radruler/personal-inventory/internal/model"
)

// Summaries loads ItemSummary cards for ids in the given order, skipping ids
// that do not exist. It implements model.ItemSummaryLoader and is injected
// into httpapi.Deps. Frozen signature; internals belong to thread A.
func Summaries(ctx context.Context, q db.Querier, ids []string) ([]model.ItemSummary, error) {
	if len(ids) == 0 {
		return []model.ItemSummary{}, nil
	}
	rows, err := q.QueryContext(ctx, `
		SELECT id, kind, title, category, quantity, lifecycle, gone_reason, gone_at, review_state,
		       cover_media_id, location_id, target_location_id, attrs, product_id, ext_key,
		       created_at, updated_at,
		       (SELECT json_group_array(tag) FROM (SELECT tag FROM tag WHERE tag.item_id = item.id ORDER BY tag))
		  FROM item WHERE id IN (`+db.Placeholders(len(ids))+`)`, db.Args(ids)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type row struct {
		s                  model.ItemSummary
		goneReason, goneAt *string
		cover, loc, target sql.NullString
	}
	byID := map[string]*row{}
	var locIDs, coverIDs, needCover []string
	for rows.Next() {
		r := &row{}
		var category, goneReason, goneAt, productID, extKey sql.NullString
		var attrs, tags string
		if err := rows.Scan(&r.s.ID, &r.s.Kind, &r.s.Title, &category, &r.s.Quantity, &r.s.Lifecycle,
			&goneReason, &goneAt, &r.s.ReviewState, &r.cover, &r.loc, &r.target, &attrs, &productID,
			&extKey, &r.s.CreatedAt, &r.s.UpdatedAt, &tags); err != nil {
			return nil, err
		}
		r.s.Category = nullStr(category)
		r.goneReason, r.goneAt = nullStr(goneReason), nullStr(goneAt)
		r.s.ProductID, r.s.ExtKey = nullStr(productID), nullStr(extKey)
		r.s.Attrs = map[string]any{}
		_ = json.Unmarshal([]byte(attrs), &r.s.Attrs)
		r.s.Tags = []string{}
		_ = json.Unmarshal([]byte(tags), &r.s.Tags)
		if r.loc.Valid {
			locIDs = append(locIDs, r.loc.String)
		}
		if r.target.Valid {
			locIDs = append(locIDs, r.target.String)
		}
		if r.cover.Valid {
			coverIDs = append(coverIDs, r.cover.String)
		} else {
			needCover = append(needCover, r.s.ID)
		}
		byID[r.s.ID] = r
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()

	// Items without an explicit cover use the best linked photo.
	auto, err := media.CoverFor(ctx, q, model.SubjectItem, needCover)
	if err != nil {
		return nil, err
	}
	for id, mid := range auto {
		byID[id].cover = sql.NullString{String: mid, Valid: true}
		coverIDs = append(coverIDs, mid)
	}
	refs, err := media.Refs(ctx, q, coverIDs)
	if err != nil {
		return nil, err
	}
	locs, err := locations.Refs(ctx, q, locIDs)
	if err != nil {
		return nil, err
	}

	out := make([]model.ItemSummary, 0, len(byID))
	for _, id := range ids {
		r, ok := byID[id]
		if !ok {
			continue
		}
		if r.cover.Valid {
			if m, ok := refs[r.cover.String]; ok {
				r.s.Cover = &m
			}
		}
		var loc, target *model.LocationRef
		if l, ok := locs[r.loc.String]; ok && r.loc.Valid {
			loc = &l
		}
		if l, ok := locs[r.target.String]; ok && r.target.Valid {
			target = &l
		}
		r.s.Placement = locations.DerivePlacement(r.s.Lifecycle, r.goneReason, r.goneAt, loc, target)
		out = append(out, r.s)
	}
	return out, nil
}

func nullStr(n sql.NullString) *string {
	if !n.Valid {
		return nil
	}
	s := n.String
	return &s
}
