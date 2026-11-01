package locations

import (
	"context"
	"database/sql"
	"net/http"
	"slices"
	"strings"

	"github.com/radruler/personal-inventory/internal/db"
	"github.com/radruler/personal-inventory/internal/httpapi"
	"github.com/radruler/personal-inventory/internal/model"
)

// maxMoveItems matches MoveRequest.item_ids maxItems.
const maxMoveItems = 1000

type moveReq struct {
	Action      string    `json:"action"`
	ItemIDs     []string  `json:"item_ids"`
	LocationID  *string   `json:"location_id"`
	To          optString `json:"to"`
	ClearTarget bool      `json:"clear_target"`
	Note        *string   `json:"note"`
	Actor       *string   `json:"actor"`
}

// move handles POST /api/moves (see the action table in api/openapi.yaml).
//
//   - Everything happens in one transaction; any error rolls back all of it.
//   - One movement row per thing that actually changed; no-ops are skipped.
//   - Item move: a location change records kind=move (from old to new
//     location). When only the target is cleared (the item is already at the
//     destination), kind=unstage (from old target to null) is recorded.
//   - Box move: one row with location_moved_id; item rows are not touched.
func (h *handlers) move(w http.ResponseWriter, r *http.Request) error {
	var req moveReq
	if err := httpapi.DecodeJSON(r, &req); err != nil {
		return err
	}
	if err := req.validate(); err != nil {
		return err
	}
	m := mover{
		ctx:   r.Context(),
		actor: httpapi.Actor(r),
		note:  req.Note,
		res:   MoveResult{Movements: []Movement{}, Items: []model.ItemSummary{}},
	}
	if req.Actor != nil && strings.TrimSpace(*req.Actor) != "" {
		m.actor = strings.TrimSpace(*req.Actor)
	}
	if m.note != nil && strings.TrimSpace(*m.note) == "" {
		m.note = nil
	}
	err := h.d.DB.Tx(m.ctx, func(tx *sql.Tx) error {
		m.tx = tx
		var err error
		if req.LocationID != nil {
			err = m.moveLocation(*req.LocationID, req.To.Val)
		} else {
			err = m.items(req)
		}
		if err != nil {
			return err
		}
		if err := m.resolveMovementRefs(); err != nil {
			return err
		}
		if req.LocationID == nil {
			m.res.Items, err = h.summaries(m.ctx, tx, m.itemIDs)
			return err
		}
		return nil
	})
	if err != nil {
		return err
	}
	httpapi.WriteJSON(w, http.StatusOK, m.res)
	return nil
}

func (req *moveReq) validate() error {
	switch req.Action {
	case MoveKindMove, MoveKindStage, MoveKindUnstage:
	default:
		return httpapi.Validation("action must be move, stage or unstage")
	}
	hasItems, hasLoc := req.ItemIDs != nil, req.LocationID != nil
	if hasItems == hasLoc {
		return httpapi.Validation("give exactly one of item_ids or location_id")
	}
	if hasItems && (len(req.ItemIDs) == 0 || len(req.ItemIDs) > maxMoveItems) {
		return httpapi.Validation("item_ids must have 1 to %d entries", maxMoveItems)
	}
	if hasLoc && req.Action != MoveKindMove {
		return httpapi.Validation("location_id can only be used with action move")
	}
	if hasLoc && req.ClearTarget {
		return httpapi.Validation("clear_target applies to item moves only")
	}
	switch req.Action {
	case MoveKindMove:
		if !req.To.Set {
			return httpapi.Validation("to is required for move (null = unknown for items, top level for a location)")
		}
	case MoveKindStage:
		if req.To.Val == nil || strings.TrimSpace(*req.To.Val) == "" {
			return httpapi.Validation("to is required for stage")
		}
		if req.ClearTarget {
			return httpapi.Validation("clear_target applies to move only")
		}
	case MoveKindUnstage:
		if req.To.Val != nil {
			return httpapi.Validation("unstage takes no destination")
		}
		if req.ClearTarget {
			return httpapi.Validation("clear_target applies to move only")
		}
	}
	return nil
}

type mover struct {
	ctx   context.Context
	tx    *sql.Tx
	actor string
	note  *string
	now   string

	itemIDs []string // de-duplicated request order
	pending []pendingMove
	res     MoveResult
}

type pendingMove struct {
	m        Movement
	from, to *string
}

// destination resolves `to` (nil = unknown / top level) and returns its
// LocationRef. Archived destinations are rejected.
func (m *mover) destination(to *string) (*string, *model.LocationRef, error) {
	if to == nil {
		return nil, nil, nil
	}
	if strings.TrimSpace(*to) == "" {
		// Only an explicit null means unknown / top level.
		return nil, nil, httpapi.Validation("to must be a location reference or null")
	}
	id, err := ResolveRef(m.ctx, m.tx, *to)
	if err != nil {
		return nil, nil, err
	}
	refs, err := Refs(m.ctx, m.tx, []string{id})
	if err != nil {
		return nil, nil, err
	}
	ref := refs[id]
	if ref.Archived {
		return nil, nil, httpapi.Validation("location %q is archived", *to)
	}
	return &id, &ref, nil
}

func (m *mover) record(kind string, itemID, locID, from, to *string) {
	if m.now == "" {
		m.now = db.Now()
	}
	m.pending = append(m.pending, pendingMove{
		m: Movement{ID: db.NewID(), ItemID: itemID, LocationMovedID: locID, Kind: kind,
			At: m.now, Note: m.note, Actor: m.actor},
		from: from, to: to,
	})
	m.res.Moved++
}

func (m *mover) flush() error {
	for _, p := range m.pending {
		if _, err := m.tx.ExecContext(m.ctx, `INSERT INTO movement
			(id, item_id, location_moved_id, kind, from_location_id, to_location_id, at, note, actor)
			VALUES (?,?,?,?,?,?,?,?,?)`,
			p.m.ID, p.m.ItemID, p.m.LocationMovedID, p.m.Kind, p.from, p.to, p.m.At, p.m.Note, p.m.Actor); err != nil {
			return err
		}
	}
	return nil
}

// resolveMovementRefs fills From/To on the recorded movements (after the
// writes, so paths reflect the new state) and adds them to the result.
func (m *mover) resolveMovementRefs() error {
	var ids []string
	for _, p := range m.pending {
		ids = append(ids, deref(p.from), deref(p.to))
	}
	refs, err := Refs(m.ctx, m.tx, ids)
	if err != nil {
		return err
	}
	for _, p := range m.pending {
		mv := p.m
		mv.From = refPtr(refs, sql.NullString{String: deref(p.from), Valid: p.from != nil})
		mv.To = refPtr(refs, sql.NullString{String: deref(p.to), Valid: p.to != nil})
		m.res.Movements = append(m.res.Movements, mv)
	}
	return nil
}

// ---------------------------------------------------------------- items

type itemState struct {
	id, lifecycle string
	loc, target   *string
}

func (m *mover) items(req moveReq) error {
	m.itemIDs = uniqueNonEmpty(req.ItemIDs)
	if len(m.itemIDs) == 0 {
		return httpapi.Validation("item_ids must not be empty")
	}
	states, err := m.loadItems()
	if err != nil {
		return err
	}
	var dest *string
	var destRef *model.LocationRef
	if req.Action != MoveKindUnstage {
		if dest, destRef, err = m.destination(req.To.Val); err != nil {
			return err
		}
	}
	for _, id := range m.itemIDs {
		st := states[id]
		newLoc, newTarget := st.loc, st.target
		switch req.Action {
		case MoveKindMove:
			newLoc = dest
			if st.target != nil && (req.ClearTarget || within(destRef, *st.target)) {
				newTarget = nil
			}
		case MoveKindStage:
			newTarget = dest
		case MoveKindUnstage:
			newTarget = nil
		}
		locChanged, targetChanged := !sameID(st.loc, newLoc), !sameID(st.target, newTarget)
		if !locChanged && !targetChanged {
			continue
		}
		itemID := id
		switch {
		case locChanged:
			m.record(MoveKindMove, &itemID, nil, st.loc, newLoc)
		case newTarget == nil:
			m.record(MoveKindUnstage, &itemID, nil, st.target, nil)
		default:
			m.record(MoveKindStage, &itemID, nil, st.target, newTarget)
		}
		if _, err := m.tx.ExecContext(m.ctx, `UPDATE item
			SET location_id = ?, target_location_id = ?, updated_at = ? WHERE id = ?`,
			newLoc, newTarget, m.now, id); err != nil {
			return err
		}
	}
	return m.flush()
}

func (m *mover) loadItems() (map[string]itemState, error) {
	rows, err := m.tx.QueryContext(m.ctx, `SELECT id, lifecycle, location_id, target_location_id
		FROM item WHERE id IN (`+db.Placeholders(len(m.itemIDs))+`)`, db.Args(m.itemIDs)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]itemState{}
	for rows.Next() {
		var s itemState
		var loc, target sql.NullString
		if err := rows.Scan(&s.id, &s.lifecycle, &loc, &target); err != nil {
			return nil, err
		}
		s.loc, s.target = nullStr(loc), nullStr(target)
		out[s.id] = s
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	var missing, gone []string
	for _, id := range m.itemIDs {
		s, ok := out[id]
		switch {
		case !ok:
			missing = append(missing, id)
		case s.lifecycle != model.LifecycleOwned:
			gone = append(gone, id)
		}
	}
	if len(missing) > 0 {
		return nil, httpapi.NotFound("%d item(s) not found", len(missing)).
			WithDetails(map[string][]string{"missing": missing})
	}
	if len(gone) > 0 {
		return nil, httpapi.Conflict("%d item(s) are gone and cannot be moved or staged", len(gone)).
			WithDetails(map[string][]string{"gone": gone})
	}
	return out, nil
}

// within reports whether dest is the location id or inside it.
func within(dest *model.LocationRef, id string) bool {
	if dest == nil {
		return false
	}
	return slices.ContainsFunc(dest.Path, func(c model.LocationCrumb) bool { return c.ID == id })
}

func sameID(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// ---------------------------------------------------------------- whole location

// moveLocation re-parents a location. Items keep pointing at it, so they move
// with it without any item-row writes.
func (m *mover) moveLocation(ref string, to *string) error {
	id, ok, err := lookup(m.ctx, m.tx, ref)
	if err != nil {
		return err
	}
	if !ok {
		return httpapi.NotFound("location %q not found", ref)
	}
	cur, err := locKindArchived(m.ctx, m.tx, id)
	if err != nil {
		return err
	}
	if cur.archived {
		return httpapi.Validation("location %q is archived", ref)
	}
	var oldParent sql.NullString
	if err := m.tx.QueryRowContext(m.ctx, `SELECT parent_id FROM location WHERE id = ?`, id).Scan(&oldParent); err != nil {
		return err
	}
	dest, destRef, err := m.destination(to)
	if err != nil {
		return err
	}
	if dest != nil {
		if within(destRef, id) {
			return httpapi.Conflict("cannot move a location into itself or one of its descendants")
		}
		if err := checkNesting(cur.kind, destRef.Kind); err != nil {
			return err
		}
	}
	old := nullStr(oldParent)
	if !sameID(old, dest) {
		m.record(MoveKindMove, nil, &id, old, dest)
		if _, err := m.tx.ExecContext(m.ctx, `UPDATE location SET parent_id = ?, updated_at = ? WHERE id = ?`,
			dest, m.now, id); err != nil {
			return err
		}
		if err := m.flush(); err != nil {
			return err
		}
	}
	t, err := loadTree(m.ctx, m.tx)
	if err != nil {
		return err
	}
	if n, ok := t.byID[id]; ok {
		m.res.Location = n.pruned(false)
	}
	return nil
}
