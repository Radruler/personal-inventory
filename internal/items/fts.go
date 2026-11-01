package items

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/radruler/personal-inventory/internal/db"
)

// ftsAttrKeys are the projected attrs whose values are searchable.
var ftsAttrKeys = []string{"brand", "color", "size", "material", "season", "style_name", "model", "pattern", "connector"}

// Reindex rebuilds the item_fts row for one item. Call it inside the same
// transaction as any write that changes searchable content (title, notes,
// category, tags, observations, identifiers, attrs). Frozen signature.
func Reindex(ctx context.Context, q db.Querier, itemID string) error {
	var title string
	var category, notes sql.NullString
	var attrs string
	err := q.QueryRowContext(ctx, `SELECT title, category, notes, attrs FROM item WHERE id = ?`, itemID).
		Scan(&title, &category, &notes, &attrs)
	if err == sql.ErrNoRows {
		_, err = q.ExecContext(ctx, `DELETE FROM item_fts WHERE item_id = ?`, itemID)
		return err
	}
	if err != nil {
		return err
	}
	tags, err := joinCol(ctx, q, " ", `SELECT tag FROM tag WHERE item_id = ? ORDER BY tag`, itemID)
	if err != nil {
		return err
	}
	obs, err := joinCol(ctx, q, "\n", `SELECT COALESCE(text, '') FROM observation WHERE item_id = ? ORDER BY recorded_at`, itemID)
	if err != nil {
		return err
	}
	ids, err := joinCol(ctx, q, " ", `SELECT value || ' ' || value_norm FROM identifier WHERE subject_type = 'item' AND subject_id = ?`, itemID)
	if err != nil {
		return err
	}
	var m map[string]any
	_ = json.Unmarshal([]byte(attrs), &m)
	var av []string
	for _, k := range ftsAttrKeys {
		if v, ok := m[k]; ok {
			av = append(av, flatten(v)...)
		}
	}
	sort.Strings(av)
	if _, err := q.ExecContext(ctx, `DELETE FROM item_fts WHERE item_id = ?`, itemID); err != nil {
		return err
	}
	_, err = q.ExecContext(ctx,
		`INSERT INTO item_fts (item_id, title, category, notes, tags, observations, identifiers, attrs)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		itemID, title, strings.ReplaceAll(category.String, "/", " "), notes.String, tags, obs, ids, strings.Join(av, " "))
	return err
}

func joinCol(ctx context.Context, q db.Querier, sep, query string, args ...any) (string, error) {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	var parts []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return "", err
		}
		parts = append(parts, s)
	}
	return strings.Join(parts, sep), rows.Err()
}

func flatten(v any) []string {
	switch t := v.(type) {
	case string:
		return []string{t}
	case nil:
		return nil
	case []any:
		var out []string
		for _, e := range t {
			out = append(out, flatten(e)...)
		}
		return out
	case map[string]any:
		var out []string
		for _, e := range t {
			out = append(out, flatten(e)...)
		}
		return out
	default:
		return []string{fmt.Sprint(t)}
	}
}
