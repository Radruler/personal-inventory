package media

import (
	"database/sql"
	"errors"
	"net/http"
	"os"
	"regexp"
	"strings"

	"github.com/radruler/personal-inventory/internal/httpapi"
)

type handlers struct{ d *httpapi.Deps }

var shaRe = regexp.MustCompile(`^[0-9a-f]{64}$`)

// GET /api/media/{id}
func (h *handlers) get(w http.ResponseWriter, r *http.Request) error {
	m, err := loadMedia(r.Context(), h.d.DB, r.PathValue("id"), false)
	if err != nil {
		return err
	}
	httpapi.WriteJSON(w, http.StatusOK, m)
	return nil
}

// GET and HEAD /api/media/sha256/{sha}
func (h *handlers) getBySha(w http.ResponseWriter, r *http.Request) error {
	sha := strings.ToLower(r.PathValue("sha"))
	head := r.Method == http.MethodHead
	if !shaRe.MatchString(sha) {
		if head {
			w.WriteHeader(http.StatusBadRequest)
			return nil
		}
		return httpapi.BadRequest("sha must be 64 lowercase hex characters")
	}
	if head {
		var id string
		err := h.d.DB.QueryRowContext(r.Context(), `SELECT id FROM media WHERE sha256 = ?`, sha).Scan(&id)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			w.WriteHeader(http.StatusNotFound)
			return nil
		case err != nil:
			return err
		}
		w.Header().Set("X-Media-Id", id)
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusOK)
		return nil
	}
	m, err := loadMedia(r.Context(), h.d.DB, sha, true)
	if err != nil {
		return err
	}
	w.Header().Set("X-Media-Id", m.ID)
	httpapi.WriteJSON(w, http.StatusOK, m)
	return nil
}

// PATCH /api/media/{id}  {"context": "…" | null}
func (h *handlers) patch(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")
	var in struct {
		Context optString `json:"context"`
	}
	if err := httpapi.DecodeJSON(r, &in); err != nil {
		return err
	}
	err := h.d.DB.Tx(r.Context(), func(tx *sql.Tx) error {
		if err := mediaExists(r.Context(), tx, id); err != nil {
			return err
		}
		if !in.Context.Set {
			return nil
		}
		var v *string
		if in.Context.Value != nil {
			if s := strings.TrimSpace(*in.Context.Value); s != "" {
				v = &s
			}
		}
		_, err := tx.ExecContext(r.Context(), `UPDATE media SET context = ? WHERE id = ? AND context IS NOT ?`, v, id, v)
		return err
	})
	if err != nil {
		return err
	}
	return h.get(w, r)
}

// DELETE /api/media/{id}: only media nothing uses (the Inbox). Its files go
// too.
func (h *handlers) delete(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	id := r.PathValue("id")
	var sha, ext string
	err := h.d.DB.Tx(ctx, func(tx *sql.Tx) error {
		err := tx.QueryRowContext(ctx, `SELECT sha256, ext FROM media WHERE id = ?`, id).Scan(&sha, &ext)
		if errors.Is(err, sql.ErrNoRows) {
			return httpapi.NotFound("media %s not found", id)
		}
		if err != nil {
			return err
		}
		var links, obs, refs, covers int
		if err := tx.QueryRowContext(ctx, `SELECT
			(SELECT COUNT(*) FROM media_link WHERE media_id = ?1),
			(SELECT COUNT(*) FROM observation WHERE media_id = ?1),
			(SELECT COUNT(*) FROM reference WHERE archive_media_id = ?1),
			(SELECT COUNT(*) FROM item WHERE cover_media_id = ?1) +
			(SELECT COUNT(*) FROM location WHERE cover_media_id = ?1)`, id).Scan(&links, &obs, &refs, &covers); err != nil {
			return err
		}
		if links+obs+refs+covers > 0 {
			return httpapi.Conflict("media %s is in use; unlink it first", id).WithDetails(map[string]int{
				"links": links, "observations": obs, "references": refs, "covers": covers,
			})
		}
		_, err = tx.ExecContext(ctx, `DELETE FROM media WHERE id = ?`, id)
		return err
	})
	if err != nil {
		return err
	}
	for _, p := range []string{OrigPath(h.d.Cfg, sha, ext), DisplayPath(h.d.Cfg, sha), ThumbPath(h.d.Cfg, sha)} {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			h.d.Log.Warn("media delete: remove file", "path", p, "err", err)
		}
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}
