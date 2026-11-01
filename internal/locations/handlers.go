package locations

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/radruler/personal-inventory/internal/db"
	"github.com/radruler/personal-inventory/internal/httpapi"
	"github.com/radruler/personal-inventory/internal/media"
	"github.com/radruler/personal-inventory/internal/model"
)

// detailMovements is how many movements a location page shows.
const detailMovements = 50

// Field limits (sanity, not contract).
const (
	maxCode = 64
	maxName = 200
)

func boolParam(r *http.Request, name string) (bool, error) {
	s := r.URL.Query().Get(name)
	if s == "" {
		return false, nil
	}
	b, err := strconv.ParseBool(s)
	if err != nil {
		return false, httpapi.BadRequest("%s must be true or false", name)
	}
	return b, nil
}

// ---------------------------------------------------------------- GET /api/locations

func (h *handlers) list(w http.ResponseWriter, r *http.Request) error {
	inclArchived, err := boolParam(r, "include_archived")
	if err != nil {
		return err
	}
	t, err := loadTree(r.Context(), h.d.DB)
	if err != nil {
		return err
	}
	out := LocationTree{Roots: []*LocationNode{}, UnknownCount: t.unknown}
	for _, n := range t.roots {
		if inclArchived || n.ArchivedAt == nil {
			out.Roots = append(out.Roots, n.pruned(inclArchived))
		}
	}
	httpapi.WriteJSON(w, http.StatusOK, out)
	return nil
}

// ---------------------------------------------------------------- GET /api/locations/{id}

func (h *handlers) get(w http.ResponseWriter, r *http.Request) error {
	deep, err := boolParam(r, "deep")
	if err != nil {
		return err
	}
	id, err := h.pathID(r.Context(), h.d.DB, r)
	if err != nil {
		return err
	}
	det, err := h.detail(r.Context(), h.d.DB, id, deep)
	if err != nil {
		return err
	}
	httpapi.WriteJSON(w, http.StatusOK, det)
	return nil
}

// pathID resolves the {id} path value (id or code:<CODE>); unknown → 404.
func (h *handlers) pathID(ctx context.Context, q db.Querier, r *http.Request) (string, error) {
	ref := r.PathValue("id")
	id, ok, err := lookup(ctx, q, ref)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", httpapi.NotFound("location %q not found", ref)
	}
	return id, nil
}

// detail builds a LocationDetail. Children omit archived locations.
func (h *handlers) detail(ctx context.Context, q db.Querier, id string, deep bool) (*LocationDetail, error) {
	t, err := loadTree(ctx, q)
	if err != nil {
		return nil, err
	}
	n, ok := t.byID[id]
	if !ok {
		return nil, httpapi.NotFound("location %q not found", id)
	}
	where := []string{id}
	if deep {
		where = n.subtreeIDs()
	}
	itemIDs, err := queryIDs(ctx, q, `SELECT id FROM item
		WHERE lifecycle = 'owned' AND location_id IN (`+db.Placeholders(len(where))+`)
		ORDER BY created_at DESC, id DESC`, db.Args(where)...)
	if err != nil {
		return nil, err
	}
	stagedIDs, err := queryIDs(ctx, q, `SELECT id FROM item
		WHERE lifecycle = 'owned' AND target_location_id = ?
		ORDER BY created_at DESC, id DESC`, id)
	if err != nil {
		return nil, err
	}
	det := &LocationDetail{LocationNode: *n.pruned(false), Path: n.path}
	if det.Items, err = h.summaries(ctx, q, itemIDs); err != nil {
		return nil, err
	}
	if det.StagedItems, err = h.summaries(ctx, q, stagedIDs); err != nil {
		return nil, err
	}
	if det.Movements, err = MovementsForLocation(ctx, q, id, detailMovements); err != nil {
		return nil, err
	}
	if det.Media, err = locationMedia(ctx, q, id); err != nil {
		return nil, err
	}
	return det, nil
}

func (h *handlers) summaries(ctx context.Context, q db.Querier, ids []string) ([]model.ItemSummary, error) {
	if len(ids) == 0 {
		return []model.ItemSummary{}, nil
	}
	if h.d.ItemSummaries == nil {
		return nil, errors.New("locations: Deps.ItemSummaries is not set")
	}
	s, err := h.d.ItemSummaries(ctx, q, ids)
	if s == nil && err == nil {
		s = []model.ItemSummary{}
	}
	return s, err
}

func queryIDs(ctx context.Context, q db.Querier, query string, args ...any) ([]string, error) {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// locationMedia lists media linked to a location as MediaLinkViews, ordered by
// role sort then link sort.
func locationMedia(ctx context.Context, q db.Querier, id string) ([]MediaLinkView, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT l.media_id, l.role, l.sort, l.caption
		  FROM media_link l LEFT JOIN media_role r ON r.role = l.role
		 WHERE l.subject_type = 'location' AND l.subject_id = ?
		 ORDER BY COALESCE(r.sort, 1000), l.sort, l.created_at, l.media_id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []MediaLinkView{}
	var ids []string
	for rows.Next() {
		var v MediaLinkView
		var caption sql.NullString
		if err := rows.Scan(&v.Media.ID, &v.Role, &v.Sort, &caption); err != nil {
			return nil, err
		}
		v.Caption = nullStr(caption)
		ids = append(ids, v.Media.ID)
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	if len(ids) == 0 {
		return out, nil
	}
	ms, err := loadMedia(ctx, q, ids)
	if err != nil {
		return nil, err
	}
	for i := range out {
		out[i].Media = ms[out[i].Media.ID]
	}
	return out, nil
}

// loadMedia renders full Media objects (with all their links) for ids.
func loadMedia(ctx context.Context, q db.Querier, ids []string) (map[string]mediaView, error) {
	ph, args := db.Placeholders(len(ids)), db.Args(ids)
	rows, err := q.QueryContext(ctx, `
		SELECT id, sha256, mime, ext, bytes, width, height, taken_at, original_name, exif,
		       context, variant_status, variant_error, created_at
		  FROM media WHERE id IN (`+ph+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]mediaView{}
	for rows.Next() {
		var m mediaView
		var id, sha, mime, status string
		var w, hgt sql.NullInt64
		var taken, orig, exif, mctx, verr sql.NullString
		if err := rows.Scan(&id, &sha, &mime, &m.Ext, &m.Bytes, &w, &hgt, &taken, &orig, &exif,
			&mctx, &status, &verr, &m.CreatedAt); err != nil {
			return nil, err
		}
		m.MediaRef = media.Ref(id, sha, mime, w, hgt, status)
		m.TakenAt, m.OriginalName, m.Context, m.VariantError = nullStr(taken), nullStr(orig), nullStr(mctx), nullStr(verr)
		if exif.Valid {
			m.Exif = []byte(exif.String)
		}
		m.Links = []mediaLink{}
		out[id] = m
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	lrows, err := q.QueryContext(ctx, `
		SELECT media_id, subject_type, subject_id, role, sort, caption, created_at
		  FROM media_link WHERE media_id IN (`+ph+`)
		 ORDER BY media_id, sort, created_at`, args...)
	if err != nil {
		return nil, err
	}
	defer lrows.Close()
	for lrows.Next() {
		var l mediaLink
		var caption sql.NullString
		if err := lrows.Scan(&l.MediaID, &l.SubjectType, &l.SubjectID, &l.Role, &l.Sort, &caption, &l.CreatedAt); err != nil {
			return nil, err
		}
		l.Caption = nullStr(caption)
		m := out[l.MediaID]
		m.Links = append(m.Links, l)
		out[l.MediaID] = m
	}
	return out, lrows.Err()
}

// ---------------------------------------------------------------- POST /api/locations

type createReq struct {
	Code         *string         `json:"code"`
	Name         string          `json:"name"`
	Kind         string          `json:"kind"`
	Parent       *string         `json:"parent"`
	Notes        *string         `json:"notes"`
	CoverMediaID *string         `json:"cover_media_id"`
	Layout       json.RawMessage `json:"layout"`
}

func (h *handlers) create(w http.ResponseWriter, r *http.Request) error {
	var req createReq
	if err := httpapi.DecodeJSON(r, &req); err != nil {
		return err
	}
	ctx := r.Context()
	code, err := cleanCode(req.Code)
	if err != nil {
		return err
	}
	name, err := cleanName(req.Name)
	if err != nil {
		return err
	}
	if err := checkKind(req.Kind); err != nil {
		return err
	}
	layout, err := cleanLayout(req.Layout)
	if err != nil {
		return err
	}
	id := db.NewID()
	err = h.d.DB.Tx(ctx, func(tx *sql.Tx) error {
		var parentID *string
		if req.Parent != nil && strings.TrimSpace(*req.Parent) != "" {
			pid, err := ResolveRef(ctx, tx, *req.Parent)
			if err != nil {
				return err
			}
			p, err := locKindArchived(ctx, tx, pid)
			if err != nil {
				return err
			}
			if p.archived {
				return httpapi.Validation("parent location %q is archived", *req.Parent)
			}
			if err := checkNesting(req.Kind, p.kind); err != nil {
				return err
			}
			parentID = &pid
		}
		if err := checkMedia(ctx, tx, req.CoverMediaID); err != nil {
			return err
		}
		now := db.Now()
		_, err := tx.ExecContext(ctx, `INSERT INTO location
			(id, code, name, kind, parent_id, cover_media_id, notes, layout, created_at, updated_at)
			VALUES (?,?,?,?,?,?,?,?,?,?)`,
			id, code, name, req.Kind, parentID, emptyNil(req.CoverMediaID), req.Notes, layout, now, now)
		return codeConflict(ctx, tx, err, code)
	})
	if err != nil {
		return err
	}
	det, err := h.detail(ctx, h.d.DB, id, false)
	if err != nil {
		return err
	}
	httpapi.WriteJSON(w, http.StatusCreated, det)
	return nil
}

// ---------------------------------------------------------------- PATCH /api/locations/{id}

type patchReq struct {
	Code         optString `json:"code"`
	Name         optString `json:"name"`
	Kind         optString `json:"kind"`
	Notes        optString `json:"notes"`
	CoverMediaID optString `json:"cover_media_id"`
	Layout       optRaw    `json:"layout"`
	Archived     *bool     `json:"archived"`
}

func (h *handlers) patch(w http.ResponseWriter, r *http.Request) error {
	var req patchReq
	if err := httpapi.DecodeJSON(r, &req); err != nil {
		return err
	}
	if !req.Code.Set && !req.Name.Set && !req.Kind.Set && !req.Notes.Set &&
		!req.CoverMediaID.Set && !req.Layout.Set && req.Archived == nil {
		return httpapi.Validation("nothing to update")
	}
	ctx := r.Context()
	var id string
	err := h.d.DB.Tx(ctx, func(tx *sql.Tx) error {
		var err error
		if id, err = h.pathID(ctx, tx, r); err != nil {
			return err
		}
		cur, err := locKindArchived(ctx, tx, id)
		if err != nil {
			return err
		}
		sets, args := []string{}, []any{}
		set := func(col string, v any) { sets = append(sets, col+" = ?"); args = append(args, v) }
		var code *string
		if req.Code.Set {
			if code, err = cleanCode(req.Code.Val); err != nil {
				return err
			}
			set("code", code)
		}
		if req.Name.Set {
			if req.Name.Val == nil {
				return httpapi.Validation("name cannot be null")
			}
			name, err := cleanName(*req.Name.Val)
			if err != nil {
				return err
			}
			set("name", name)
		}
		if req.Kind.Set {
			if req.Kind.Val == nil {
				return httpapi.Validation("kind cannot be null")
			}
			kind := *req.Kind.Val
			if err := checkKind(kind); err != nil {
				return err
			}
			if kind != cur.kind {
				if err := checkKindChange(ctx, tx, id, cur.parentKind, kind); err != nil {
					return err
				}
			}
			set("kind", kind)
		}
		if req.Notes.Set {
			set("notes", req.Notes.Val)
		}
		if req.CoverMediaID.Set {
			if err := checkMedia(ctx, tx, req.CoverMediaID.Val); err != nil {
				return err
			}
			set("cover_media_id", emptyNil(req.CoverMediaID.Val))
		}
		if req.Layout.Set {
			layout, err := cleanLayout(req.Layout.Val)
			if err != nil {
				return err
			}
			set("layout", layout)
		}
		if req.Archived != nil && *req.Archived != cur.archived {
			if *req.Archived {
				if err := checkArchivable(ctx, tx, id); err != nil {
					return err
				}
				set("archived_at", db.Now())
			} else {
				if cur.parentArchived {
					return httpapi.Conflict("parent location is archived; un-archive it first")
				}
				set("archived_at", nil)
			}
		}
		if len(sets) == 0 {
			return nil
		}
		set("updated_at", db.Now())
		_, err = tx.ExecContext(ctx, `UPDATE location SET `+strings.Join(sets, ", ")+` WHERE id = ?`,
			append(args, id)...)
		return codeConflict(ctx, tx, err, code)
	})
	if err != nil {
		return err
	}
	det, err := h.detail(ctx, h.d.DB, id, false)
	if err != nil {
		return err
	}
	httpapi.WriteJSON(w, http.StatusOK, det)
	return nil
}

// ---------------------------------------------------------------- DELETE /api/locations/{id}

func (h *handlers) archive(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	err := h.d.DB.Tx(ctx, func(tx *sql.Tx) error {
		id, err := h.pathID(ctx, tx, r)
		if err != nil {
			return err
		}
		cur, err := locKindArchived(ctx, tx, id)
		if err != nil {
			return err
		}
		if cur.archived {
			return nil // already archived: nothing to write
		}
		if err := checkArchivable(ctx, tx, id); err != nil {
			return err
		}
		now := db.Now()
		_, err = tx.ExecContext(ctx, `UPDATE location SET archived_at = ?, updated_at = ? WHERE id = ?`, now, now, id)
		return err
	})
	if err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// checkArchivable returns 409 when owned items are directly inside, a
// non-archived child exists, or owned items are staged to go here. Gone items
// are history and do not block (they cannot be unstaged).
func checkArchivable(ctx context.Context, q db.Querier, id string) error {
	var items, children, staged int
	err := q.QueryRowContext(ctx, `SELECT
		(SELECT count(*) FROM item WHERE lifecycle = 'owned' AND location_id = ?1),
		(SELECT count(*) FROM location WHERE parent_id = ?1 AND archived_at IS NULL),
		(SELECT count(*) FROM item WHERE lifecycle = 'owned' AND target_location_id = ?1)`, id).
		Scan(&items, &children, &staged)
	if err != nil {
		return err
	}
	if items+children+staged > 0 {
		return httpapi.Conflict("location is not empty: %d items, %d child locations, %d items staged to go here",
			items, children, staged).
			WithDetails(map[string]int{"items": items, "children": children, "staged": staged})
	}
	return nil
}

// ---------------------------------------------------------------- validation helpers

type locState struct {
	kind, parentKind         string
	archived, parentArchived bool
}

func locKindArchived(ctx context.Context, q db.Querier, id string) (locState, error) {
	var s locState
	var pkind sql.NullString
	var arch, parch sql.NullString
	err := q.QueryRowContext(ctx, `SELECT l.kind, l.archived_at, p.kind, p.archived_at
		FROM location l LEFT JOIN location p ON p.id = l.parent_id WHERE l.id = ?`, id).
		Scan(&s.kind, &arch, &pkind, &parch)
	if errors.Is(err, sql.ErrNoRows) {
		return s, httpapi.NotFound("location %q not found", id)
	}
	s.parentKind, s.archived, s.parentArchived = pkind.String, arch.Valid, parch.Valid
	return s, err
}

func checkKind(k string) error {
	if !slices.Contains(model.LocationKinds, k) {
		return httpapi.Validation("kind must be one of %s", strings.Join(model.LocationKinds, ", "))
	}
	return nil
}

// isHolder reports kinds that hold things (shelves and containers) rather
// than being places (sites, rooms, units).
func isHolder(kind string) bool { return kind == model.LocShelf || kind == model.LocContainer }

// checkNesting enforces the one nesting rule: a place (site, room or unit)
// cannot sit inside a holder (shelf or container). Everything else is allowed
// (box in box, shelf in a unit, unit in a site or room, room in a site, …).
func checkNesting(childKind, parentKind string) error {
	if parentKind != "" && isHolder(parentKind) && !isHolder(childKind) {
		return httpapi.Validation("a %s cannot be inside a %s", childKind, parentKind)
	}
	return nil
}

// checkKindChange validates re-kinding a location against its parent and
// its direct children.
func checkKindChange(ctx context.Context, q db.Querier, id, parentKind, kind string) error {
	if err := checkNesting(kind, parentKind); err != nil {
		return err
	}
	if !isHolder(kind) {
		return nil
	}
	var n int
	if err := q.QueryRowContext(ctx, `SELECT count(*) FROM location
		WHERE parent_id = ? AND kind IN ('site', 'room', 'unit')`, id).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return httpapi.Validation("cannot make this a %s: it contains %d site/room/unit locations", kind, n)
	}
	return nil
}

func cleanCode(c *string) (*string, error) {
	if c == nil {
		return nil, nil
	}
	s := strings.TrimSpace(*c)
	if s == "" {
		return nil, nil
	}
	if utf8.RuneCountInString(s) > maxCode {
		return nil, httpapi.Validation("code is longer than %d characters", maxCode)
	}
	if _, ok := cutCodePrefix(s); ok {
		return nil, httpapi.Validation(`code must not start with "code:"`)
	}
	return &s, nil
}

func cleanName(n string) (string, error) {
	s := strings.TrimSpace(n)
	if s == "" {
		return "", httpapi.Validation("name is required")
	}
	if utf8.RuneCountInString(s) > maxName {
		return "", httpapi.Validation("name is longer than %d characters", maxName)
	}
	return s, nil
}

// cleanLayout validates layout (an object or null) and returns the text to
// store (nil = NULL).
func cleanLayout(raw json.RawMessage) (any, error) {
	if len(bytes.TrimSpace(raw)) == 0 || string(bytes.TrimSpace(raw)) == "null" {
		return nil, nil
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil || m == nil {
		return nil, httpapi.Validation("layout must be an object or null")
	}
	b, _ := json.Marshal(m)
	return string(b), nil
}

func checkMedia(ctx context.Context, q db.Querier, id *string) error {
	if id == nil || *id == "" {
		return nil
	}
	var n int
	if err := q.QueryRowContext(ctx, `SELECT count(*) FROM media WHERE id = ?`, *id).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		return httpapi.Validation("unknown media %q", *id)
	}
	return nil
}

// codeConflict maps a UNIQUE violation on location.code to 409 with the
// existing location's id in details.
func codeConflict(ctx context.Context, q db.Querier, err error, code *string) error {
	if err == nil || !db.IsUnique(err) || code == nil {
		return err
	}
	var existing string
	_ = q.QueryRowContext(ctx, `SELECT id FROM location WHERE code = ?`, *code).Scan(&existing)
	return httpapi.Conflict("location code %q is already used", *code).
		WithDetails(map[string]string{"id": existing})
}

func emptyNil(s *string) any {
	if s == nil || *s == "" {
		return nil
	}
	return *s
}
