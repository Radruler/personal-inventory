package items

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/radruler/personal-inventory/internal/db"
	"github.com/radruler/personal-inventory/internal/httpapi"
	"github.com/radruler/personal-inventory/internal/model"
)

// Projection rule (PLAN.md §4.2): for each key, the active fact with the best
// basis (user > observed > looked_up > inferred), then the highest
// confidence, then the newest (recorded_at, then id). Product facts fill in
// keys the item lacks.

// winner is the fact that supplies one projected attrs key.
type winner struct {
	FactID      string
	SubjectType string
	Value       json.RawMessage
}

const basisRank = `CASE basis WHEN 'user' THEN 0 WHEN 'observed' THEN 1 WHEN 'looked_up' THEN 2 ELSE 3 END`

// winners returns the winning active fact per key for one subject.
func winners(ctx context.Context, q db.Querier, subjectType, subjectID string) (map[string]winner, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT key, id, value FROM (
		  SELECT key, id, value, row_number() OVER (PARTITION BY key
		           ORDER BY `+basisRank+`, confidence DESC, recorded_at DESC, id DESC) AS rn
		    FROM fact WHERE subject_type = ? AND subject_id = ? AND status = 'active'
		) WHERE rn = 1`, subjectType, subjectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]winner{}
	for rows.Next() {
		var k, id, v string
		if err := rows.Scan(&k, &id, &v); err != nil {
			return nil, err
		}
		out[k] = winner{FactID: id, SubjectType: subjectType, Value: json.RawMessage(v)}
	}
	return out, rows.Err()
}

// itemWinners is the item's own winners with the product's filling gaps.
func itemWinners(ctx context.Context, q db.Querier, itemID string, productID *string) (map[string]winner, error) {
	w, err := winners(ctx, q, model.SubjectItem, itemID)
	if err != nil {
		return nil, err
	}
	if productID != nil {
		pw, err := winners(ctx, q, model.SubjectProduct, *productID)
		if err != nil {
			return nil, err
		}
		for k, v := range pw {
			if _, ok := w[k]; !ok {
				w[k] = v
			}
		}
	}
	return w, nil
}

func attrsJSON(w map[string]winner) string {
	m := make(map[string]json.RawMessage, len(w))
	for k, v := range w {
		m[k] = v.Value
	}
	b, _ := json.Marshal(m) // sorted keys, compacted values
	return string(b)
}

// canonical re-encodes a stored attrs document so formatting differences do
// not count as changes.
func canonical(s string) string {
	var m map[string]json.RawMessage
	if json.Unmarshal([]byte(s), &m) != nil {
		return s
	}
	if m == nil {
		m = map[string]json.RawMessage{}
	}
	b, _ := json.Marshal(m)
	return string(b)
}

// recomputeItem rewrites item.attrs from the facts. It reports whether the
// stored value changed; it does not touch updated_at or the FTS row.
func recomputeItem(ctx context.Context, q db.Querier, itemID string) (string, bool, error) {
	var productID sql.NullString
	var old string
	err := q.QueryRowContext(ctx, `SELECT product_id, attrs FROM item WHERE id = ?`, itemID).Scan(&productID, &old)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, httpapi.NotFound("no item %s", itemID)
	}
	if err != nil {
		return "", false, err
	}
	w, err := itemWinners(ctx, q, itemID, nullStr(productID))
	if err != nil {
		return "", false, err
	}
	next := attrsJSON(w)
	if next == canonical(old) {
		return next, false, nil
	}
	_, err = q.ExecContext(ctx, `UPDATE item SET attrs = ? WHERE id = ?`, next, itemID)
	return next, true, err
}

// recomputeProduct rewrites product.attrs and the attrs of every item that
// points at the product (touching and reindexing items whose attrs changed).
func recomputeProduct(ctx context.Context, q db.Querier, productID, now string) (string, error) {
	var old string
	err := q.QueryRowContext(ctx, `SELECT attrs FROM product WHERE id = ?`, productID).Scan(&old)
	if errors.Is(err, sql.ErrNoRows) {
		return "", httpapi.NotFound("no product %s", productID)
	}
	if err != nil {
		return "", err
	}
	w, err := winners(ctx, q, model.SubjectProduct, productID)
	if err != nil {
		return "", err
	}
	next := attrsJSON(w)
	if next != canonical(old) {
		if _, err := q.ExecContext(ctx, `UPDATE product SET attrs = ?, updated_at = ? WHERE id = ?`, next, now, productID); err != nil {
			return "", err
		}
	}
	ids, err := idList(ctx, q, `SELECT id FROM item WHERE product_id = ?`, productID)
	if err != nil {
		return "", err
	}
	for _, id := range ids {
		_, changed, err := recomputeItem(ctx, q, id)
		if err != nil {
			return "", err
		}
		if changed {
			if err := touchItem(ctx, q, id, now); err != nil {
				return "", err
			}
			if err := Reindex(ctx, q, id); err != nil {
				return "", err
			}
		}
	}
	return next, nil
}

// Recompute re-derives the attrs projection for a subject ("item" or
// "product"; a product also recomputes its items) and refreshes the search
// index. Call it inside the transaction that changed the facts. Exported for
// internal/ingest.
func Recompute(ctx context.Context, q db.Querier, subjectType, id string) error {
	now := db.Now()
	if subjectType == model.SubjectProduct {
		_, err := recomputeProduct(ctx, q, id, now)
		return err
	}
	return finishItem(ctx, q, id, now)
}

// finishItem is the tail of every item write: recompute attrs, bump
// updated_at, rebuild the FTS row.
func finishItem(ctx context.Context, q db.Querier, id, now string) error {
	if _, _, err := recomputeItem(ctx, q, id); err != nil {
		return err
	}
	if err := touchItem(ctx, q, id, now); err != nil {
		return err
	}
	return Reindex(ctx, q, id)
}

func touchItem(ctx context.Context, q db.Querier, id, now string) error {
	_, err := q.ExecContext(ctx, `UPDATE item SET updated_at = ? WHERE id = ?`, now, id)
	return err
}

func idList(ctx context.Context, q db.Querier, query string, args ...any) ([]string, error) {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
