package media

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/radruler/personal-inventory/internal/db"
	"github.com/radruler/personal-inventory/internal/httpapi"
)

// Upload contract (POST /api/media, multipart/form-data):
//
//   - one part named "file" (its filename becomes original_name) plus the
//     optional text fields context, link_subject_type, link_subject_id and
//     link_role, in any order (before or after the file).
//   - Accepted types, detected from the bytes (the part's Content-Type and
//     the filename are ignored): JPEG, PNG, WebP, HEIC/HEIF (HEVC brands:
//     heic, heix, mif1, msf1, … as iPhones write them), PDF and HTML.
//     Anything else is 415; AVIF is not accepted.
//   - Size limit INV_MAX_UPLOAD_MB (413).
//
// iOS note for the SPA (thread F, PLAN.md §7): iOS Safari converts HEIC to
// JPEG when the file input asks only for JPEG (accept="image/jpeg") and,
// depending on the iOS version, for accept="image/*". To receive the
// original HEIC (with its EXIF taken_at), leave `accept` off or list the
// types explicitly: accept="image/heic,image/heif,image/jpeg,image/png,image/webp"
// (add application/pdf,text/html where archives are wanted), and use
// `multiple` for camera-roll batches. Whether a photo arrived as HEIC is
// visible in the response (`mime: image/heic`); check this on a real device.
//
// Write behaviour: the file part streams (never buffered in memory or
// /tmp) into a temp file inside /media/orig while it is hashed. If the
// sha256 is already stored the temp file is deleted and nothing else is
// written (unless the request also asks for a link, or sets context on a
// row that has none). Otherwise the temp file is renamed to its
// content-addressed path and the row inserted in one transaction.

// fieldSlack is the room allowed for the text fields and multipart framing
// on top of INV_MAX_UPLOAD_MB.
const fieldSlack = 1 << 20

const maxFieldBytes = 64 << 10

type received struct {
	tmp  string // temp file path; "" once renamed or removed
	sha  string
	size int64
	typ  fileType
	name *string
}

func (u *received) cleanup() {
	if u.tmp != "" {
		os.Remove(u.tmp)
		u.tmp = ""
	}
}

func (h *handlers) tooLarge() error {
	return httpapi.Errf(http.StatusRequestEntityTooLarge, httpapi.CodeTooLarge,
		"upload exceeds the %d MB limit (INV_MAX_UPLOAD_MB)", h.d.Cfg.MaxUploadBytes>>20)
}

// bodyErr maps a request-body read error.
func (h *handlers) bodyErr(err error) error {
	var mbe *http.MaxBytesError
	if errors.As(err, &mbe) {
		return h.tooLarge()
	}
	return httpapi.BadRequest("reading upload: %v", err)
}

// receive streams the file part into a temp file in the orig directory,
// hashing as it goes.
func (h *handlers) receive(part io.Reader, filename string, up *received) error {
	head := make([]byte, sniffLen)
	n, err := io.ReadFull(part, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return h.bodyErr(err)
	}
	head = head[:n]
	if n == 0 {
		return httpapi.BadRequest("the file part is empty")
	}
	typ, ok := sniff(head)
	if !ok {
		return httpapi.Errf(http.StatusUnsupportedMediaType, httpapi.CodeUnsupported,
			"unsupported file type: JPEG, PNG, WebP, HEIC/HEIF, PDF or HTML expected")
	}
	root := filepath.Join(h.d.Cfg.MediaDir, "orig")
	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(root, ".upload-*")
	if err != nil {
		return err
	}
	up.tmp = f.Name()
	hash := sha256.New()
	hash.Write(head)
	if _, err := f.Write(head); err != nil {
		f.Close()
		return err
	}
	limit := h.d.Cfg.MaxUploadBytes
	copied, err := io.Copy(io.MultiWriter(f, hash), io.LimitReader(part, limit-int64(n)+1))
	if err != nil {
		f.Close()
		return h.bodyErr(err)
	}
	size := int64(n) + copied
	if size > limit {
		f.Close()
		return h.tooLarge()
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Chmod(up.tmp, 0o644); err != nil {
		return err
	}
	up.sha = hex.EncodeToString(hash.Sum(nil))
	up.size = size
	up.typ = typ
	if name := strings.TrimSpace(filename); name != "" {
		if len(name) > 255 {
			name = name[:255]
		}
		up.name = &name
	}
	return nil
}

// POST /api/media
func (h *handlers) upload(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	limit := h.d.Cfg.MaxUploadBytes + fieldSlack
	if r.ContentLength > limit {
		return h.tooLarge()
	}
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	mr, err := r.MultipartReader()
	if err != nil {
		return httpapi.BadRequest("expected a multipart/form-data body: %v", err)
	}
	var up received
	defer up.cleanup()
	fields := map[string]string{}
	seenFile := false
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return h.bodyErr(err)
		}
		name := part.FormName()
		switch name {
		case "file":
			if seenFile {
				return httpapi.BadRequest("send one file per request")
			}
			seenFile = true
			if err := h.receive(part, part.FileName(), &up); err != nil {
				return err
			}
		case "context", "link_subject_type", "link_subject_id", "link_role":
			if _, dup := fields[name]; dup {
				return httpapi.BadRequest("field %s given twice", name)
			}
			b, err := io.ReadAll(io.LimitReader(part, maxFieldBytes+1))
			if err != nil {
				return h.bodyErr(err)
			}
			if len(b) > maxFieldBytes {
				return httpapi.BadRequest("field %s is too long", name)
			}
			fields[name] = string(b)
		default:
			return httpapi.BadRequest("unknown form field %q", name)
		}
		part.Close()
	}
	if !seenFile {
		return httpapi.BadRequest("the multipart body needs a part named \"file\"")
	}

	var noteCtx *string
	if c := strings.TrimSpace(fields["context"]); c != "" {
		noteCtx = &c
	}
	var link *linkInput
	lt, lid := strings.TrimSpace(fields["link_subject_type"]), strings.TrimSpace(fields["link_subject_id"])
	if lt != "" || lid != "" {
		if lt == "" || lid == "" {
			return httpapi.Validation("link_subject_type and link_subject_id go together")
		}
		link = &linkInput{SubjectType: lt, SubjectID: lid}
		if role, ok := fields["link_role"]; ok {
			link.Role = &role
		}
	} else if _, ok := fields["link_role"]; ok {
		return httpapi.Validation("link_role needs link_subject_type and link_subject_id")
	}

	// Already stored? Then the temp file goes and nothing else is written.
	var existing string
	err = h.d.DB.QueryRowContext(ctx, `SELECT id FROM media WHERE sha256 = ?`, up.sha).Scan(&existing)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if existing != "" {
		up.cleanup()
		return h.deduplicated(w, r, existing, noteCtx, link)
	}

	m := imageMeta{}
	status := "none"
	if up.typ.isImage() {
		status = "pending"
		if m, err = readImageMeta(ctx, h.d.Cfg, up.tmp); err != nil {
			// Still store the original; the worker records the failure.
			h.d.Log.Warn("media metadata", "sha256", up.sha, "err", err)
		}
	}
	id := db.NewID()
	dst := OrigPath(h.d.Cfg, up.sha, up.typ.ext)
	renamed := false
	err = h.d.DB.Tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `INSERT INTO media (id, sha256, mime, ext, bytes, width, height,
			taken_at, original_name, exif, context, variant_status, created_at)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			id, up.sha, up.typ.mime, up.typ.ext, up.size, m.width, m.height,
			m.takenAt, up.name, exifJSON(m.exif), noteCtx, status, db.Now()); err != nil {
			return err
		}
		if link != nil {
			if _, err := upsertLink(ctx, tx, id, *link); err != nil {
				return err
			}
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		if err := os.Rename(up.tmp, dst); err != nil {
			return err
		}
		up.tmp, renamed = "", true
		return nil
	})
	if err != nil {
		if renamed {
			os.Remove(dst)
		}
		if db.IsUnique(err) { // a concurrent upload of the same bytes won
			up.cleanup()
			if err := h.d.DB.QueryRowContext(ctx, `SELECT id FROM media WHERE sha256 = ?`, up.sha).Scan(&existing); err == nil {
				return h.deduplicated(w, r, existing, noteCtx, link)
			}
		}
		return err
	}
	if status == "pending" {
		wake(h.d)
	}
	out, err := loadMedia(ctx, h.d.DB, id, false)
	if err != nil {
		return err
	}
	httpapi.WriteJSON(w, http.StatusCreated, UploadResult{Media: *out})
	return nil
}

// deduplicated answers an upload whose bytes are already stored. It writes
// only what the request explicitly asks for: a link, or a context note on
// a row that has none.
func (h *handlers) deduplicated(w http.ResponseWriter, r *http.Request, id string, noteCtx *string, link *linkInput) error {
	ctx := r.Context()
	if link != nil || noteCtx != nil {
		err := h.d.DB.Tx(ctx, func(tx *sql.Tx) error {
			if noteCtx != nil {
				if _, err := tx.ExecContext(ctx, `UPDATE media SET context = ? WHERE id = ? AND context IS NULL`, *noteCtx, id); err != nil {
					return err
				}
			}
			if link != nil {
				_, err := upsertLink(ctx, tx, id, *link)
				return err
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	out, err := loadMedia(ctx, h.d.DB, id, false)
	if err != nil {
		return err
	}
	httpapi.WriteJSON(w, http.StatusOK, UploadResult{Media: *out, Deduplicated: true})
	return nil
}
