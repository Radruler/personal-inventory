package items

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"strings"

	"github.com/radruler/personal-inventory/internal/db"
	"github.com/radruler/personal-inventory/internal/httpapi"
	"github.com/radruler/personal-inventory/internal/locations"
	"github.com/radruler/personal-inventory/internal/model"
)

type mediaLinkCreate struct {
	MediaID string  `json:"media_id"`
	Role    *string `json:"role"`
	Sort    *int    `json:"sort"`
	Caption *string `json:"caption"`
}

type itemCreate struct {
	Kind           string             `json:"kind"`
	Title          string             `json:"title"`
	Category       *string            `json:"category"`
	Quantity       *int               `json:"quantity"`
	Notes          *string            `json:"notes"`
	Tags           []string           `json:"tags"`
	Location       *string            `json:"location"`
	TargetLocation *string            `json:"target_location"`
	ReviewState    *string            `json:"review_state"`
	ProductID      *string            `json:"product_id"`
	ParentItemID   *string            `json:"parent_item_id"`
	CoverMediaID   *string            `json:"cover_media_id"`
	ExtKey         *string            `json:"ext_key"`
	Media          []mediaLinkCreate  `json:"media"`
	Observations   []observationInput `json:"observations"`
	Identifiers    []nestedIdentifier `json:"identifiers"`
	Facts          []factInput        `json:"facts"`
	References     []referenceInput   `json:"references"`
	RecordedBy     *string            `json:"recorded_by"`
}

type itemPatch struct {
	Kind         Opt[string]   `json:"kind"`
	Title        Opt[string]   `json:"title"`
	Category     Opt[string]   `json:"category"`
	Quantity     Opt[int]      `json:"quantity"`
	Notes        Opt[string]   `json:"notes"`
	Tags         Opt[[]string] `json:"tags"`
	ReviewState  Opt[string]   `json:"review_state"`
	Lifecycle    Opt[string]   `json:"lifecycle"`
	GoneReason   Opt[string]   `json:"gone_reason"`
	GoneAt       Opt[string]   `json:"gone_at"`
	ProductID    Opt[string]   `json:"product_id"`
	ParentItemID Opt[string]   `json:"parent_item_id"`
	CoverMediaID Opt[string]   `json:"cover_media_id"`
	ExtKey       Opt[string]   `json:"ext_key"`
}

func (p itemPatch) empty() bool {
	return !(p.Kind.Set || p.Title.Set || p.Category.Set || p.Quantity.Set || p.Notes.Set || p.Tags.Set ||
		p.ReviewState.Set || p.Lifecycle.Set || p.GoneReason.Set || p.GoneAt.Set || p.ProductID.Set ||
		p.ParentItemID.Set || p.CoverMediaID.Set || p.ExtKey.Set)
}

func validKind(k string) error {
	if !contains(model.Kinds, k) {
		return httpapi.Validation("kind: must be one of %s", strings.Join(model.Kinds, ", "))
	}
	return nil
}

func validReview(s string) error {
	if s != model.ReviewOK && s != model.ReviewNeedsReview {
		return httpapi.Validation("review_state: must be ok or needs_review")
	}
	return nil
}

func (a *api) getItem(w http.ResponseWriter, r *http.Request) error {
	return a.writeItem(w, r, r.PathValue("id"), http.StatusOK)
}

func (a *api) writeItem(w http.ResponseWriter, r *http.Request, id string, status int) error {
	d, err := loadItemDetail(r.Context(), a.d.DB, id)
	if err != nil {
		return err
	}
	httpapi.WriteJSON(w, status, d)
	return nil
}

func (a *api) createItem(w http.ResponseWriter, r *http.Request) error {
	var in itemCreate
	if err := httpapi.DecodeJSON(r, &in); err != nil {
		return err
	}
	id, now := db.NewID(), db.Now()
	actor := actorOr(in.RecordedBy, httpapi.Actor(r))
	err := a.d.DB.Tx(r.Context(), func(tx *sql.Tx) error {
		return createItemTx(r.Context(), tx, id, in, now, actor)
	})
	if err != nil {
		return err
	}
	return a.writeItem(w, r, id, http.StatusCreated)
}

// createItemTx writes an item and everything nested in it.
func createItemTx(ctx context.Context, tx *sql.Tx, id string, in itemCreate, now, actor string) error {
	if err := validKind(in.Kind); err != nil {
		return err
	}
	title := strings.TrimSpace(in.Title)
	if title == "" {
		return httpapi.Validation("title: required")
	}
	qty := 1
	if in.Quantity != nil {
		qty = *in.Quantity
	}
	if qty < 1 {
		return httpapi.Validation("quantity: must be at least 1")
	}
	review := model.ReviewOK
	if in.ReviewState != nil {
		review = *in.ReviewState
	}
	if err := validReview(review); err != nil {
		return err
	}
	ext := cleanOpt(in.ExtKey)
	if ext != nil {
		if err := extKeyFree(ctx, tx, "item", *ext, ""); err != nil {
			return err
		}
	}
	var loc, target string
	var err error
	if in.Location != nil {
		if loc, err = locations.ResolveRef(ctx, tx, *in.Location); err != nil {
			return fieldErr("location", err)
		}
	}
	if in.TargetLocation != nil {
		if target, err = locations.ResolveRef(ctx, tx, *in.TargetLocation); err != nil {
			return fieldErr("target_location", err)
		}
	}
	if loc != "" && target != "" {
		// Already at (or inside) the target: nothing to stage, like packing.
		inside, err := isWithin(ctx, tx, loc, target)
		if err != nil {
			return err
		}
		if inside {
			target = ""
		}
	}
	productID, parentID, coverID := cleanOpt(in.ProductID), cleanOpt(in.ParentItemID), cleanOpt(in.CoverMediaID)
	for _, ref := range []struct {
		table, field string
		id           *string
	}{{"product", "product_id", productID}, {"item", "parent_item_id", parentID}, {"media", "cover_media_id", coverID}} {
		if ref.id != nil {
			if err := mustExist(ctx, tx, ref.table, ref.field, *ref.id); err != nil {
				return err
			}
		}
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO item (id, kind, title, category, product_id, parent_item_id, quantity,
		location_id, target_location_id, review_state, cover_media_id, notes, attrs, ext_key, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,'{}',?,?,?)`,
		id, in.Kind, title, nullable(cleanOpt(in.Category)), nullable(productID), nullable(parentID), qty,
		nullIfEmpty(loc), nullIfEmpty(target), review, nullable(coverID), nullable(in.Notes), nullable(ext), now, now)
	if err != nil {
		if db.IsUnique(err) && ext != nil {
			return extKeyFree(ctx, tx, "item", *ext, "")
		}
		return err
	}
	for _, t := range cleanTags(in.Tags) {
		if _, err := tx.ExecContext(ctx, `INSERT INTO tag (item_id, tag) VALUES (?, ?) ON CONFLICT DO NOTHING`, id, t); err != nil {
			return err
		}
	}
	for i, m := range in.Media {
		if err := linkMedia(ctx, tx, fmt.Sprintf("media[%d].", i), id, i, m, now); err != nil {
			return err
		}
	}
	obsIDs := make([]string, 0, len(in.Observations))
	for i, o := range in.Observations {
		ob, err := insertObservation(ctx, tx, fmt.Sprintf("observations[%d].", i), id, o, now, actor)
		if err != nil {
			return err
		}
		obsIDs = append(obsIDs, ob.ID)
	}
	refIDs := make([]string, 0, len(in.References))
	for i, rf := range in.References {
		ref, err := insertReference(ctx, tx, fmt.Sprintf("references[%d].", i), model.SubjectItem, id, rf, now, actor)
		if err != nil {
			return err
		}
		refIDs = append(refIDs, ref.ID)
	}
	for i, ni := range in.Identifiers {
		field := fmt.Sprintf("identifiers[%d].", i)
		idf := ni.identifierInput
		if ni.Observation != nil {
			if *ni.Observation < 0 || *ni.Observation >= len(obsIDs) {
				return httpapi.Validation("%sobservation: index %d out of range (%d observations)", field, *ni.Observation, len(obsIDs))
			}
			o := obsIDs[*ni.Observation]
			idf.ObservationID = &o
		}
		if _, err := insertIdentifier(ctx, tx, field, model.SubjectItem, id, idf, now, actor); err != nil {
			return err
		}
	}
	for i, f := range in.Facts {
		field := fmt.Sprintf("facts[%d].", i)
		src, err := buildSources(field, f.Sources, obsIDs, refIDs)
		if err != nil {
			return err
		}
		if _, err := insertFact(ctx, tx, field, model.SubjectItem, id, f, src, nil, now, actor); err != nil {
			return err
		}
	}
	if loc != "" {
		if err := addMovement(ctx, tx, id, "move", "", loc, now, actor); err != nil {
			return err
		}
	}
	if target != "" {
		if err := addMovement(ctx, tx, id, "stage", loc, target, now, actor); err != nil {
			return err
		}
	}
	return finishItem(ctx, tx, id, now)
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// fieldErr prefixes a validation message with the request field.
func fieldErr(field string, err error) error {
	if e, ok := err.(*httpapi.Error); ok && e.Code == httpapi.CodeValidation {
		return httpapi.Validation("%s: %s", field, e.Message)
	}
	return err
}

func linkMedia(ctx context.Context, tx *sql.Tx, field, itemID string, pos int, m mediaLinkCreate, now string) error {
	mid := strings.TrimSpace(m.MediaID)
	if mid == "" {
		return httpapi.Validation("%smedia_id: required", field)
	}
	if err := mustExist(ctx, tx, "media", field+"media_id", mid); err != nil {
		return err
	}
	role := "other"
	if s := cleanOpt(m.Role); s != nil {
		role = *s
	}
	sort := pos
	if m.Sort != nil {
		sort = *m.Sort
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO media_link (media_id, subject_type, subject_id, role, sort, caption, created_at)
		VALUES (?, 'item', ?, ?, ?, ?, ?)
		ON CONFLICT (media_id, subject_type, subject_id) DO UPDATE SET role = excluded.role, sort = excluded.sort, caption = excluded.caption`,
		mid, itemID, role, sort, nullable(m.Caption), now)
	return err
}

func addMovement(ctx context.Context, tx *sql.Tx, itemID, kind, from, to, now, actor string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO movement (id, item_id, kind, from_location_id, to_location_id, at, actor)
		VALUES (?,?,?,?,?,?,?)`, db.NewID(), itemID, kind, nullIfEmpty(from), nullIfEmpty(to), now, actor)
	return err
}

func (a *api) patchItem(w http.ResponseWriter, r *http.Request) error {
	var in itemPatch
	if err := httpapi.DecodeJSON(r, &in); err != nil {
		return err
	}
	if in.empty() {
		return httpapi.BadRequest("nothing to update")
	}
	id, now := r.PathValue("id"), db.Now()
	err := a.d.DB.Tx(r.Context(), func(tx *sql.Tx) error {
		return patchItemTx(r.Context(), tx, id, in, now)
	})
	if err != nil {
		return err
	}
	return a.writeItem(w, r, id, http.StatusOK)
}

func patchItemTx(ctx context.Context, tx *sql.Tx, id string, in itemPatch, now string) error {
	var lifecycle string
	var goneReason, goneAt sql.NullString
	err := tx.QueryRowContext(ctx, `SELECT lifecycle, gone_reason, gone_at FROM item WHERE id = ?`, id).Scan(&lifecycle, &goneReason, &goneAt)
	if err == sql.ErrNoRows {
		return httpapi.NotFound("no item %s", id)
	}
	if err != nil {
		return err
	}
	set := []string{"updated_at = ?"}
	args := []any{now}
	add := func(col string, v any) { set, args = append(set, col+" = ?"), append(args, v) }
	notNull := func(field string, o interface{ isNull() bool }) error {
		if o.isNull() {
			return httpapi.Validation("%s: cannot be null", field)
		}
		return nil
	}
	if in.Kind.Set {
		if err := notNull("kind", in.Kind); err != nil {
			return err
		}
		if err := validKind(in.Kind.V); err != nil {
			return err
		}
		add("kind", in.Kind.V)
	}
	if in.Title.Set {
		t := strings.TrimSpace(in.Title.V)
		if in.Title.Null || t == "" {
			return httpapi.Validation("title: required")
		}
		add("title", t)
	}
	if in.Category.Set {
		add("category", nullable(cleanOpt(in.Category.ptr())))
	}
	if in.Quantity.Set {
		if in.Quantity.Null || in.Quantity.V < 1 {
			return httpapi.Validation("quantity: must be at least 1")
		}
		add("quantity", in.Quantity.V)
	}
	if in.Notes.Set {
		add("notes", nullable(in.Notes.ptr()))
	}
	if in.ReviewState.Set {
		if err := notNull("review_state", in.ReviewState); err != nil {
			return err
		}
		if err := validReview(in.ReviewState.V); err != nil {
			return err
		}
		add("review_state", in.ReviewState.V)
	}

	// Lifecycle: gone sets gone_at (now unless given); owned clears both.
	next := lifecycle
	if in.Lifecycle.Set {
		if err := notNull("lifecycle", in.Lifecycle); err != nil {
			return err
		}
		if in.Lifecycle.V != model.LifecycleOwned && in.Lifecycle.V != model.LifecycleGone {
			return httpapi.Validation("lifecycle: must be owned or gone")
		}
		next = in.Lifecycle.V
	}
	reason, at := nullStr(goneReason), nullStr(goneAt)
	if in.GoneReason.Set {
		reason = cleanOpt(in.GoneReason.ptr())
	}
	if in.GoneAt.Set {
		at = nil
		if !in.GoneAt.Null {
			t, err := parseTime("gone_at", in.GoneAt.V)
			if err != nil {
				return err
			}
			at = &t
		}
	}
	switch next {
	case model.LifecycleGone:
		if at == nil {
			n := now
			at = &n
		}
	case model.LifecycleOwned:
		// An explicit lifecycle: owned wins over gone fields in the same body.
		givesGone := (in.GoneReason.Set && reason != nil) || (in.GoneAt.Set && at != nil)
		if givesGone && !in.Lifecycle.Set {
			return httpapi.Validation("gone_reason/gone_at: the item is owned; set lifecycle: gone")
		}
		reason, at = nil, nil
	}
	if in.Lifecycle.Set || in.GoneReason.Set || in.GoneAt.Set {
		add("lifecycle", next)
		add("gone_reason", nullable(reason))
		add("gone_at", nullable(at))
	}

	if in.Tags.Set && in.Tags.Null {
		return httpapi.Validation("tags: cannot be null (send [] to clear)")
	}
	if in.ProductID.Set {
		p := cleanOpt(in.ProductID.ptr())
		if p != nil {
			if err := mustExist(ctx, tx, "product", "product_id", *p); err != nil {
				return err
			}
		}
		add("product_id", nullable(p))
	}
	if in.ParentItemID.Set {
		p := cleanOpt(in.ParentItemID.ptr())
		if p != nil {
			if err := checkParent(ctx, tx, id, *p); err != nil {
				return err
			}
		}
		add("parent_item_id", nullable(p))
	}
	if in.CoverMediaID.Set {
		c := cleanOpt(in.CoverMediaID.ptr())
		if c != nil {
			if err := mustExist(ctx, tx, "media", "cover_media_id", *c); err != nil {
				return err
			}
		}
		add("cover_media_id", nullable(c))
	}
	if in.ExtKey.Set {
		e := cleanOpt(in.ExtKey.ptr())
		if e != nil {
			if err := extKeyFree(ctx, tx, "item", *e, id); err != nil {
				return err
			}
		}
		add("ext_key", nullable(e))
	}
	if _, err := tx.ExecContext(ctx, `UPDATE item SET `+strings.Join(set, ", ")+` WHERE id = ?`, append(args, id)...); err != nil {
		return err
	}
	if in.Tags.Set {
		if _, err := tx.ExecContext(ctx, `DELETE FROM tag WHERE item_id = ?`, id); err != nil {
			return err
		}
		for _, t := range cleanTags(in.Tags.V) {
			if _, err := tx.ExecContext(ctx, `INSERT INTO tag (item_id, tag) VALUES (?, ?) ON CONFLICT DO NOTHING`, id, t); err != nil {
				return err
			}
		}
	}
	return finishItem(ctx, tx, id, now)
}

func (o Opt[T]) isNull() bool { return o.Null }

// checkParent rejects unknown parents, self-parenting and cycles.
func checkParent(ctx context.Context, q db.Querier, id, parent string) error {
	if parent == id {
		return httpapi.Validation("parent_item_id: an item cannot be its own parent")
	}
	if err := mustExist(ctx, q, "item", "parent_item_id", parent); err != nil {
		return err
	}
	var n int
	err := q.QueryRowContext(ctx, `
		WITH RECURSIVE up(id, depth) AS (
		  SELECT parent_item_id, 0 FROM item WHERE id = ?
		  UNION SELECT i.parent_item_id, up.depth + 1 FROM item i JOIN up ON i.id = up.id
		   WHERE up.depth < 64
		) SELECT count(*) FROM up WHERE id = ?`, parent, id).Scan(&n)
	if err != nil {
		return err
	}
	if n > 0 {
		return httpapi.Validation("parent_item_id: %s is inside %s (cycle)", parent, id)
	}
	return nil
}

// retireItem is the soft delete: lifecycle gone, gone_at now (kept if the
// item was already gone), gone_reason from ?reason=.
func (a *api) retireItem(w http.ResponseWriter, r *http.Request) error {
	id, now := r.PathValue("id"), db.Now()
	reason := strings.TrimSpace(r.URL.Query().Get("reason"))
	err := a.d.DB.Tx(r.Context(), func(tx *sql.Tx) error {
		ctx := r.Context()
		if err := subjectExists(ctx, tx, model.SubjectItem, id); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `UPDATE item SET
			gone_at = CASE WHEN lifecycle = 'gone' AND gone_at IS NOT NULL THEN gone_at ELSE ? END,
			gone_reason = COALESCE(?, CASE WHEN lifecycle = 'gone' THEN gone_reason END),
			lifecycle = 'gone', updated_at = ? WHERE id = ?`, now, nullIfEmpty(reason), now, id)
		if err != nil {
			return err
		}
		return Reindex(ctx, tx, id)
	})
	if err != nil {
		return err
	}
	return a.writeItem(w, r, id, http.StatusOK)
}

// hardDeleteItem permanently removes an item and its knowledge. Media rows
// and files stay (unlinked media returns to the Inbox); children lose their
// parent.
func (a *api) hardDeleteItem(w http.ResponseWriter, r *http.Request) error {
	id, now := r.PathValue("id"), db.Now()
	err := a.d.DB.Tx(r.Context(), func(tx *sql.Tx) error {
		ctx := r.Context()
		if err := subjectExists(ctx, tx, model.SubjectItem, id); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE item SET parent_item_id = NULL, updated_at = ? WHERE parent_item_id = ?`, now, id); err != nil {
			return err
		}
		for _, stmt := range []string{
			`DELETE FROM identifier WHERE subject_type = 'item' AND subject_id = ?`,
			`DELETE FROM fact WHERE subject_type = 'item' AND subject_id = ?`,
			`DELETE FROM reference WHERE subject_type = 'item' AND subject_id = ?`,
			`DELETE FROM media_link WHERE subject_type = 'item' AND subject_id = ?`,
			`DELETE FROM item_fts WHERE item_id = ?`,
			`DELETE FROM item WHERE id = ?`, // cascades: observation, tag, movement
		} {
			if _, err := tx.ExecContext(ctx, stmt, id); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// isWithin reports whether location loc is anc itself or inside it.
func isWithin(ctx context.Context, q db.Querier, loc, anc string) (bool, error) {
	var n int
	err := q.QueryRowContext(ctx, `
		WITH RECURSIVE up(id, depth) AS (
		  SELECT ?, 0
		  UNION SELECT l.parent_id, up.depth + 1 FROM location l JOIN up ON l.id = up.id
		   WHERE l.parent_id IS NOT NULL AND up.depth < 32
		) SELECT count(*) FROM up WHERE id = ?`, loc, anc).Scan(&n)
	return n > 0, err
}
