package media

import (
	"context"
	"database/sql"

	"github.com/radruler/personal-inventory/internal/db"
	"github.com/radruler/personal-inventory/internal/model"
)

// Ref builds a MediaRef from row values.
func Ref(id, sha, mime string, width, height sql.NullInt64, status string) model.MediaRef {
	r := model.MediaRef{ID: id, SHA256: sha, Mime: mime, VariantStatus: status, OrigURL: URL(sha, "orig")}
	if width.Valid {
		w := int(width.Int64)
		r.Width = &w
	}
	if height.Valid {
		h := int(height.Int64)
		r.Height = &h
	}
	if status == "ready" {
		t, d := URL(sha, "thumb"), URL(sha, "display")
		r.ThumbURL, r.DisplayURL = &t, &d
	}
	return r
}

// Refs loads MediaRefs for ids. Missing ids are absent from the map.
// Frozen signature (used by items and locations); internals belong to thread B.
func Refs(ctx context.Context, q db.Querier, ids []string) (map[string]model.MediaRef, error) {
	out := make(map[string]model.MediaRef, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := q.QueryContext(ctx,
		`SELECT id, sha256, mime, width, height, variant_status FROM media WHERE id IN (`+db.Placeholders(len(ids))+`)`,
		db.Args(ids)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, sha, mime, status string
		var w, h sql.NullInt64
		if err := rows.Scan(&id, &sha, &mime, &w, &h, &status); err != nil {
			return nil, err
		}
		out[id] = Ref(id, sha, mime, w, h, status)
	}
	return out, rows.Err()
}

// CoverFor picks a cover media id per subject from media_link using the
// media_role.cover_rank order (flat_lay > stock > on_body > …), then sort.
// Subjects with no linkable photo are absent. Frozen signature.
func CoverFor(ctx context.Context, q db.Querier, subjectType string, ids []string) (map[string]string, error) {
	out := make(map[string]string, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	args := append([]any{subjectType}, db.Args(ids)...)
	rows, err := q.QueryContext(ctx, `
		SELECT subject_id, media_id FROM (
		  SELECT l.subject_id, l.media_id,
		         row_number() OVER (PARTITION BY l.subject_id
		           ORDER BY COALESCE(r.cover_rank, 1000), l.sort, l.created_at) AS rn
		  FROM media_link l
		  JOIN media m ON m.id = l.media_id
		  LEFT JOIN media_role r ON r.role = l.role
		  WHERE l.subject_type = ? AND l.subject_id IN (`+db.Placeholders(len(ids))+`)
		    AND m.mime LIKE 'image/%'
		    AND (r.role IS NULL OR r.cover_rank IS NOT NULL)
		) WHERE rn = 1`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var sid, mid string
		if err := rows.Scan(&sid, &mid); err != nil {
			return nil, err
		}
		out[sid] = mid
	}
	return out, rows.Err()
}
