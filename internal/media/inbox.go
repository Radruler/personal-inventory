package media

import (
	"net/http"

	"github.com/radruler/personal-inventory/internal/httpapi"
	"github.com/radruler/personal-inventory/internal/model"
)

// unusedMedia is true for media nothing refers to: no media_link, not an
// observation's source photo, not a reference archive and not an explicit
// item/location cover. That is the Inbox, and what DELETE allows.
const unusedMedia = `NOT EXISTS (SELECT 1 FROM media_link l WHERE l.media_id = m.id)
	AND NOT EXISTS (SELECT 1 FROM observation o WHERE o.media_id = m.id)
	AND NOT EXISTS (SELECT 1 FROM reference r WHERE r.archive_media_id = m.id)
	AND NOT EXISTS (SELECT 1 FROM item i WHERE i.cover_media_id = m.id)
	AND NOT EXISTS (SELECT 1 FROM location c WHERE c.cover_media_id = m.id)`

// inboxOrder: oldest first by taken_at (media without one last), then
// created_at, then id. The cursor carries the same four values.
const inboxKey = `(m.taken_at IS NULL), COALESCE(m.taken_at, ''), m.created_at, m.id`

// maxReviewItems caps review_items (api/openapi.yaml Inbox).
const maxReviewItems = 200

// Inbox is api/openapi.yaml Inbox.
type Inbox struct {
	Media       []*Media            `json:"media"`
	NextCursor  *string             `json:"next_cursor"`
	MediaCount  int                 `json:"media_count"`
	ReviewItems []model.ItemSummary `json:"review_items"`
	ReviewCount int                 `json:"review_count"`
}

// GET /api/inbox
func (h *handlers) inbox(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	limit, err := httpapi.Limit(r, 200, 500)
	if err != nil {
		return err
	}
	cur, err := httpapi.DecodeCursor(r.URL.Query().Get("cursor"))
	if err != nil {
		return err
	}
	where := unusedMedia
	var args []any
	if cur != nil {
		noTaken, ok1 := cur["n"].(float64)
		taken, ok2 := cur["t"].(string)
		created, ok3 := cur["c"].(string)
		id, ok4 := cur["id"].(string)
		if !ok1 || !ok2 || !ok3 || !ok4 {
			return httpapi.BadRequest("invalid cursor")
		}
		where += ` AND (` + inboxKey + `) > (?, ?, ?, ?)`
		args = append(args, int(noTaken), taken, created, id)
	}
	rows, err := h.d.DB.QueryContext(ctx, `SELECT `+mediaCols+` FROM media m WHERE `+where+
		` ORDER BY `+inboxKey+` LIMIT ?`, append(args, limit+1)...)
	if err != nil {
		return err
	}
	out := Inbox{Media: []*Media{}, ReviewItems: []model.ItemSummary{}}
	for rows.Next() {
		m, err := scanMedia(rows)
		if err != nil {
			rows.Close()
			return err
		}
		out.Media = append(out.Media, m)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	if len(out.Media) > limit {
		out.Media = out.Media[:limit]
		last := out.Media[limit-1]
		c := httpapi.Cursor{"n": 0, "t": "", "c": last.CreatedAt, "id": last.ID}
		if last.TakenAt == nil {
			c["n"] = 1
		} else {
			c["t"] = *last.TakenAt
		}
		s := httpapi.EncodeCursor(c)
		out.NextCursor = &s
	}
	// Unused media has no links by definition, but covers can't be
	// expressed as links, so keep the shape honest.
	if err := attachLinks(ctx, h.d.DB, out.Media); err != nil {
		return err
	}
	if err := h.d.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM media m WHERE `+unusedMedia).Scan(&out.MediaCount); err != nil {
		return err
	}
	if err := h.d.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM item WHERE review_state = 'needs_review'`).Scan(&out.ReviewCount); err != nil {
		return err
	}
	if h.d.ItemSummaries != nil && out.ReviewCount > 0 {
		idRows, err := h.d.DB.QueryContext(ctx, `SELECT id FROM item WHERE review_state = 'needs_review'
			ORDER BY created_at DESC, id DESC LIMIT ?`, maxReviewItems)
		if err != nil {
			return err
		}
		var ids []string
		for idRows.Next() {
			var id string
			if err := idRows.Scan(&id); err != nil {
				idRows.Close()
				return err
			}
			ids = append(ids, id)
		}
		idRows.Close()
		if err := idRows.Err(); err != nil {
			return err
		}
		items, err := h.d.ItemSummaries(ctx, h.d.DB, ids)
		if err != nil {
			return err
		}
		if items != nil {
			out.ReviewItems = items
		}
	}
	httpapi.WriteJSON(w, http.StatusOK, out)
	return nil
}
