package media

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/radruler/personal-inventory/internal/db"
	"github.com/radruler/personal-inventory/internal/httpapi"
)

// optString is a JSON field that distinguishes absent, null and a value.
type optString struct {
	Set   bool
	Value *string
}

func (o *optString) UnmarshalJSON(b []byte) error {
	o.Set = true
	if string(b) == "null" {
		o.Value = nil
		return nil
	}
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	o.Value = &s
	return nil
}

// linkInput is api/openapi.yaml MediaLinkInput.
type linkInput struct {
	SubjectType string    `json:"subject_type"`
	SubjectID   string    `json:"subject_id"`
	Role        *string   `json:"role"`
	Sort        *int      `json:"sort"`
	Caption     optString `json:"caption"`
}

// resolveSubject checks that the subject exists and returns its id. For
// locations, subject_id may be `code:<CODE>` (resolved here with SQL because
// media must not import internal/locations).
func resolveSubject(ctx context.Context, q db.Querier, typ, id string) (string, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return "", httpapi.Validation("subject_id is required")
	}
	var query string
	arg := id
	switch typ {
	case "item":
		query = `SELECT id FROM item WHERE id = ?`
	case "product":
		query = `SELECT id FROM product WHERE id = ?`
	case "location":
		query = `SELECT id FROM location WHERE id = ?`
		if code, ok := strings.CutPrefix(id, "code:"); ok {
			query, arg = `SELECT id FROM location WHERE code = ?`, strings.TrimSpace(code)
		}
	default:
		return "", httpapi.Validation("subject_type must be item, product or location")
	}
	var got string
	err := q.QueryRowContext(ctx, query, arg).Scan(&got)
	if errors.Is(err, sql.ErrNoRows) {
		return "", httpapi.Validation("%s %s not found", typ, id).WithDetails(map[string]string{"subject_id": id})
	}
	return got, err
}

// upsertLink links media to a subject. Omitted fields keep their current
// value on an existing link; a new link defaults to role "other" and the next
// sort position for the subject.
func upsertLink(ctx context.Context, tx *sql.Tx, mediaID string, in linkInput) (*Link, error) {
	sid, err := resolveSubject(ctx, tx, in.SubjectType, in.SubjectID)
	if err != nil {
		return nil, err
	}
	if in.Role != nil {
		r := strings.TrimSpace(*in.Role)
		if r == "" {
			r = "other"
		}
		in.Role = &r
	}
	cur, err := scanLink(tx.QueryRowContext(ctx, `SELECT media_id, subject_type, subject_id, role, sort, caption, created_at
		FROM media_link WHERE media_id = ? AND subject_type = ? AND subject_id = ?`, mediaID, in.SubjectType, sid))
	switch {
	case errors.Is(err, sql.ErrNoRows):
		l := Link{MediaID: mediaID, SubjectType: in.SubjectType, SubjectID: sid, Role: "other", CreatedAt: db.Now()}
		if in.Role != nil {
			l.Role = *in.Role
		}
		if in.Sort != nil {
			l.Sort = *in.Sort
		} else if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(sort) + 1, 0) FROM media_link
			WHERE subject_type = ? AND subject_id = ?`, in.SubjectType, sid).Scan(&l.Sort); err != nil {
			return nil, err
		}
		l.Caption = in.Caption.Value
		_, err := tx.ExecContext(ctx, `INSERT INTO media_link (media_id, subject_type, subject_id, role, sort, caption, created_at)
			VALUES (?,?,?,?,?,?,?)`, l.MediaID, l.SubjectType, l.SubjectID, l.Role, l.Sort, l.Caption, l.CreatedAt)
		return &l, err
	case err != nil:
		return nil, err
	}
	changed := false
	if in.Role != nil && *in.Role != cur.Role {
		cur.Role, changed = *in.Role, true
	}
	if in.Sort != nil && *in.Sort != cur.Sort {
		cur.Sort, changed = *in.Sort, true
	}
	if in.Caption.Set && !equalPtr(in.Caption.Value, cur.Caption) {
		cur.Caption, changed = in.Caption.Value, true
	}
	if changed {
		if _, err := tx.ExecContext(ctx, `UPDATE media_link SET role = ?, sort = ?, caption = ?
			WHERE media_id = ? AND subject_type = ? AND subject_id = ?`,
			cur.Role, cur.Sort, cur.Caption, mediaID, in.SubjectType, sid); err != nil {
			return nil, err
		}
	}
	return cur, nil
}

func equalPtr(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func mediaExists(ctx context.Context, q db.Querier, id string) error {
	var x int
	err := q.QueryRowContext(ctx, `SELECT 1 FROM media WHERE id = ?`, id).Scan(&x)
	if errors.Is(err, sql.ErrNoRows) {
		return httpapi.NotFound("media %s not found", id)
	}
	return err
}

// POST /api/media/{id}/links
func (h *handlers) link(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")
	var in linkInput
	if err := httpapi.DecodeJSON(r, &in); err != nil {
		return err
	}
	var out *Link
	err := h.d.DB.Tx(r.Context(), func(tx *sql.Tx) error {
		if err := mediaExists(r.Context(), tx, id); err != nil {
			return err
		}
		var err error
		out, err = upsertLink(r.Context(), tx, id, in)
		return err
	})
	if err != nil {
		return err
	}
	httpapi.WriteJSON(w, http.StatusOK, out)
	return nil
}

// DELETE /api/media/{id}/links?subject_type=&subject_id=
func (h *handlers) unlink(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")
	q := r.URL.Query()
	typ, sid := q.Get("subject_type"), strings.TrimSpace(q.Get("subject_id"))
	switch typ {
	case "item", "product", "location":
	default:
		return httpapi.BadRequest("subject_type must be item, product or location")
	}
	if sid == "" {
		return httpapi.BadRequest("subject_id is required")
	}
	err := h.d.DB.Tx(r.Context(), func(tx *sql.Tx) error {
		if err := mediaExists(r.Context(), tx, id); err != nil {
			return err
		}
		if code, ok := strings.CutPrefix(sid, "code:"); ok && typ == "location" {
			if err := tx.QueryRowContext(r.Context(), `SELECT id FROM location WHERE code = ?`, strings.TrimSpace(code)).Scan(&sid); errors.Is(err, sql.ErrNoRows) {
				return httpapi.NotFound("location %s not found", q.Get("subject_id"))
			} else if err != nil {
				return err
			}
		}
		res, err := tx.ExecContext(r.Context(), `DELETE FROM media_link WHERE media_id = ? AND subject_type = ? AND subject_id = ?`, id, typ, sid)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return httpapi.NotFound("media %s is not linked to %s %s", id, typ, sid)
		}
		return nil
	})
	if err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}
