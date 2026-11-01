package items

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/radruler/personal-inventory/internal/db"
	"github.com/radruler/personal-inventory/internal/httpapi"
	"github.com/radruler/personal-inventory/internal/locations"
	"github.com/radruler/personal-inventory/internal/media"
	"github.com/radruler/personal-inventory/internal/model"
)

// loadItemDetail assembles the full ItemDetail. It only reads.
func loadItemDetail(ctx context.Context, q db.Querier, id string) (*ItemDetail, error) {
	sums, err := Summaries(ctx, q, []string{id})
	if err != nil {
		return nil, err
	}
	if len(sums) == 0 {
		return nil, httpapi.NotFound("no item %s", id)
	}
	d := &ItemDetail{ItemSummary: sums[0]}
	var notes, parent, loc, target, cover, goneReason, goneAt, productID sql.NullString
	if err := q.QueryRowContext(ctx, `SELECT notes, parent_item_id, location_id, target_location_id, cover_media_id,
		gone_reason, gone_at, product_id FROM item WHERE id = ?`, id).
		Scan(&notes, &parent, &loc, &target, &cover, &goneReason, &goneAt, &productID); err != nil {
		return nil, err
	}
	d.Notes, d.ParentItemID, d.LocationID = nullStr(notes), nullStr(parent), nullStr(loc)
	d.TargetLocationID, d.CoverMediaID = nullStr(target), nullStr(cover)
	d.GoneReason, d.GoneAt = nullStr(goneReason), nullStr(goneAt)

	w, err := itemWinners(ctx, q, id, nullStr(productID))
	if err != nil {
		return nil, err
	}
	d.AttrSources = make(map[string]AttrSource, len(w))
	for k, v := range w {
		d.AttrSources[k] = AttrSource{FactID: v.FactID, SubjectType: v.SubjectType}
	}

	if d.Media, err = linkedMedia(ctx, q, model.SubjectItem, id); err != nil {
		return nil, err
	}
	if d.Observations, err = observationsFor(ctx, q, id); err != nil {
		return nil, err
	}
	if d.Identifiers, err = queryIdentifiers(ctx, q, `WHERE subject_type = 'item' AND subject_id = ? ORDER BY recorded_at, id`, id); err != nil {
		return nil, err
	}
	if d.Facts, err = queryFacts(ctx, q, `WHERE subject_type = 'item' AND subject_id = ? ORDER BY recorded_at DESC, id DESC`, id); err != nil {
		return nil, err
	}
	if d.References, err = referencesFor(ctx, q, model.SubjectItem, id); err != nil {
		return nil, err
	}
	if d.Movements, err = locations.MovementsForItem(ctx, q, id, 0); err != nil {
		return nil, err
	}
	if productID.Valid {
		if d.Product, err = loadProductDetail(ctx, q, productID.String, false); err != nil {
			var he *httpapi.Error
			if !errors.As(err, &he) {
				return nil, err
			}
			d.Product = nil
		}
	}
	if parent.Valid {
		ps, err := Summaries(ctx, q, []string{parent.String})
		if err != nil {
			return nil, err
		}
		if len(ps) == 1 {
			d.Parent = &ps[0]
		}
	}
	kids, err := idList(ctx, q, `SELECT id FROM item WHERE parent_item_id = ? ORDER BY created_at, id`, id)
	if err != nil {
		return nil, err
	}
	if d.Children, err = Summaries(ctx, q, kids); err != nil {
		return nil, err
	}
	return d, nil
}

// linkedMedia returns a subject's photos ordered by role sort then link sort.
func linkedMedia(ctx context.Context, q db.Querier, subjectType, id string) ([]MediaLinkView, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT l.media_id, l.role, l.sort, l.caption FROM media_link l
		  LEFT JOIN media_role r ON r.role = l.role
		 WHERE l.subject_type = ? AND l.subject_id = ?
		 ORDER BY COALESCE(r.sort, 1000), l.sort, l.created_at, l.media_id`, subjectType, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type link struct {
		mid, role string
		sort      int
		caption   sql.NullString
	}
	var links []link
	var ids []string
	for rows.Next() {
		var l link
		if err := rows.Scan(&l.mid, &l.role, &l.sort, &l.caption); err != nil {
			return nil, err
		}
		links = append(links, l)
		ids = append(ids, l.mid)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	full, err := loadMedia(ctx, q, ids)
	if err != nil {
		return nil, err
	}
	out := make([]MediaLinkView, 0, len(links))
	for _, l := range links {
		m, ok := full[l.mid]
		if !ok {
			continue
		}
		out = append(out, MediaLinkView{Media: m, Role: l.role, Sort: l.sort, Caption: nullStr(l.caption)})
	}
	return out, nil
}

// loadMedia loads full Media objects (with all of their links) for ids.
func loadMedia(ctx context.Context, q db.Querier, ids []string) (map[string]Media, error) {
	out := map[string]Media{}
	if len(ids) == 0 {
		return out, nil
	}
	refs, err := media.Refs(ctx, q, ids)
	if err != nil {
		return nil, err
	}
	rows, err := q.QueryContext(ctx, `SELECT id, ext, bytes, taken_at, original_name, exif, context, variant_error, created_at
		FROM media WHERE id IN (`+db.Placeholders(len(ids))+`)`, db.Args(ids)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var m Media
		var id string
		var taken, name, exif, mctx, verr sql.NullString
		if err := rows.Scan(&id, &m.Ext, &m.Bytes, &taken, &name, &exif, &mctx, &verr, &m.CreatedAt); err != nil {
			return nil, err
		}
		ref, ok := refs[id]
		if !ok {
			continue
		}
		m.MediaRef = ref
		m.TakenAt, m.OriginalName, m.Context, m.VariantError = nullStr(taken), nullStr(name), nullStr(mctx), nullStr(verr)
		if exif.Valid {
			m.Exif = json.RawMessage(exif.String)
		}
		m.Links = []MediaLink{}
		out[id] = m
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	lrows, err := q.QueryContext(ctx, `SELECT media_id, subject_type, subject_id, role, sort, caption, created_at
		FROM media_link WHERE media_id IN (`+db.Placeholders(len(ids))+`) ORDER BY created_at, subject_type, subject_id`, db.Args(ids)...)
	if err != nil {
		return nil, err
	}
	defer lrows.Close()
	for lrows.Next() {
		var l MediaLink
		var caption sql.NullString
		if err := lrows.Scan(&l.MediaID, &l.SubjectType, &l.SubjectID, &l.Role, &l.Sort, &caption, &l.CreatedAt); err != nil {
			return nil, err
		}
		l.Caption = nullStr(caption)
		if m, ok := out[l.MediaID]; ok {
			m.Links = append(m.Links, l)
			out[l.MediaID] = m
		}
	}
	return out, lrows.Err()
}

func observationsFor(ctx context.Context, q db.Querier, itemID string) ([]ObservationView, error) {
	rows, err := q.QueryContext(ctx, `SELECT id, item_id, source, media_id, region, text, data, recorded_by, recorded_at, ext_key
		FROM observation WHERE item_id = ? ORDER BY recorded_at, id`, itemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ObservationView{}
	var mids []string
	for rows.Next() {
		var o Observation
		var mid, region, text, ext sql.NullString
		var data string
		if err := rows.Scan(&o.ID, &o.ItemID, &o.Source, &mid, &region, &text, &data, &o.RecordedBy, &o.RecordedAt, &ext); err != nil {
			return nil, err
		}
		o.MediaID, o.Text, o.ExtKey = nullStr(mid), nullStr(text), nullStr(ext)
		if region.Valid {
			o.Region = json.RawMessage(region.String)
		}
		o.Data = json.RawMessage(data)
		if mid.Valid {
			mids = append(mids, mid.String)
		}
		out = append(out, ObservationView{Observation: o})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	refs, err := media.Refs(ctx, q, mids)
	if err != nil {
		return nil, err
	}
	for i := range out {
		if out[i].MediaID != nil {
			if m, ok := refs[*out[i].MediaID]; ok {
				out[i].Media = &m
			}
		}
	}
	return out, nil
}

func referencesFor(ctx context.Context, q db.Querier, subjectType, id string) ([]ReferenceView, error) {
	refs, err := queryReferences(ctx, q, `WHERE subject_type = ? AND subject_id = ? ORDER BY recorded_at, id`, subjectType, id)
	if err != nil {
		return nil, err
	}
	var mids []string
	for _, r := range refs {
		if r.ArchiveMediaID != nil {
			mids = append(mids, *r.ArchiveMediaID)
		}
	}
	mrefs, err := media.Refs(ctx, q, mids)
	if err != nil {
		return nil, err
	}
	for i := range refs {
		if refs[i].ArchiveMediaID != nil {
			if m, ok := mrefs[*refs[i].ArchiveMediaID]; ok {
				refs[i].Archive = &m
			}
		}
	}
	return refs, nil
}
