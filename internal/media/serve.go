package media

import (
	"database/sql"
	"errors"
	"mime"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/radruler/personal-inventory/internal/httpapi"
)

// htmlArchiveCSP sandboxes archived pages: no scripts, forms, plugins or
// same-origin access, and nothing fetched from other hosts.
const htmlArchiveCSP = "sandbox; default-src 'none'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; font-src 'self' data:; media-src 'self' data:"

// GET /media/{sha}/{variant}: thumb | display | orig. Read-only: it opens a
// file and (for orig only) reads one DB row. Thumbs are served from /data
// without touching the DB or /media, so grid browsing never wakes the HDDs.
func (h *handlers) serve(w http.ResponseWriter, r *http.Request) error {
	sha, variant := r.PathValue("sha"), r.PathValue("variant")
	if !shaRe.MatchString(sha) {
		return httpapi.NotFound("no such media")
	}
	var path, ctype, disposition, csp string
	switch variant {
	case "thumb":
		path, ctype = ThumbPath(h.d.Cfg, sha), "image/webp"
	case "display":
		path, ctype = DisplayPath(h.d.Cfg, sha), "image/webp"
	case "orig":
		var ext string
		var name sql.NullString
		err := h.d.DB.QueryRowContext(r.Context(), `SELECT mime, ext, original_name FROM media WHERE sha256 = ?`, sha).Scan(&ctype, &ext, &name)
		if errors.Is(err, sql.ErrNoRows) {
			return httpapi.NotFound("no such media")
		}
		if err != nil {
			return err
		}
		path = OrigPath(h.d.Cfg, sha, ext)
		filename := sha + "." + ext
		if name.Valid && strings.TrimSpace(name.String) != "" {
			filename = name.String
		}
		disposition = mime.FormatMediaType("inline", map[string]string{"filename": filename})
		if disposition == "" {
			disposition = mime.FormatMediaType("inline", map[string]string{"filename": sha + "." + ext})
		}
		if ctype == "text/html" {
			ctype = "text/html; charset=utf-8"
			csp = htmlArchiveCSP
		}
	default:
		return httpapi.NotFound("variant must be thumb, display or orig")
	}
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return httpapi.NotFound("no %s for %s", variant, sha)
	}
	if err != nil {
		return err
	}
	defer f.Close()
	hd := w.Header()
	hd.Set("Content-Type", ctype)
	hd.Set("Cache-Control", "public, max-age=31536000, immutable")
	hd.Set("ETag", `"`+sha+"-"+variant+`"`)
	hd.Set("X-Content-Type-Options", "nosniff")
	if disposition != "" {
		hd.Set("Content-Disposition", disposition)
	}
	if csp != "" {
		hd.Set("Content-Security-Policy", csp)
	}
	// ServeContent answers If-None-Match with 304 and handles Range. The
	// zero modtime keeps Last-Modified out: the ETag is the validator.
	http.ServeContent(w, r, "", time.Time{}, f)
	return nil
}
