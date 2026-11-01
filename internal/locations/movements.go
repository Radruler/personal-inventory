package locations

import (
	"context"
	"database/sql"

	"github.com/radruler/personal-inventory/internal/db"
	"github.com/radruler/personal-inventory/internal/model"
)

// MovementsForItem returns an item's own movement history, newest first, at
// most limit rows (limit <= 0 means no limit). Moves of a box the item sits
// in are not included; they appear on the box's page. Never returns nil.
func MovementsForItem(ctx context.Context, q db.Querier, itemID string, limit int) ([]Movement, error) {
	return loadMovements(ctx, q, `item_id = ?`, []any{itemID}, limit)
}

// MovementsForLocation returns movements relevant to a location, newest
// first: the location itself being moved, and items or locations moved (or
// staged/unstaged) into or out of it. limit <= 0 means no limit.
func MovementsForLocation(ctx context.Context, q db.Querier, locationID string, limit int) ([]Movement, error) {
	return loadMovements(ctx, q,
		`location_moved_id = ?1 OR to_location_id = ?1 OR from_location_id = ?1`,
		[]any{locationID}, limit)
}

func loadMovements(ctx context.Context, q db.Querier, where string, args []any, limit int) ([]Movement, error) {
	query := `SELECT id, item_id, location_moved_id, kind, from_location_id, to_location_id, at, note, actor
		FROM movement WHERE ` + where + ` ORDER BY at DESC, id DESC`
	if limit > 0 {
		query += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type row struct {
		m        Movement
		from, to sql.NullString
	}
	var rs []row
	var locIDs []string
	for rows.Next() {
		var r row
		var item, moved, note sql.NullString
		if err := rows.Scan(&r.m.ID, &item, &moved, &r.m.Kind, &r.from, &r.to, &r.m.At, &note, &r.m.Actor); err != nil {
			return nil, err
		}
		r.m.ItemID, r.m.LocationMovedID, r.m.Note = nullStr(item), nullStr(moved), nullStr(note)
		if r.from.Valid {
			locIDs = append(locIDs, r.from.String)
		}
		if r.to.Valid {
			locIDs = append(locIDs, r.to.String)
		}
		rs = append(rs, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	refs, err := Refs(ctx, q, locIDs)
	if err != nil {
		return nil, err
	}
	out := make([]Movement, len(rs))
	for i, r := range rs {
		r.m.From, r.m.To = refPtr(refs, r.from), refPtr(refs, r.to)
		out[i] = r.m
	}
	return out, nil
}

func refPtr(refs map[string]model.LocationRef, id sql.NullString) *model.LocationRef {
	if !id.Valid {
		return nil
	}
	if r, ok := refs[id.String]; ok {
		return &r
	}
	return nil
}
