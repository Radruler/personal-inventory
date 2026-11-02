package ingest

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/radruler/personal-inventory/internal/db"
	"github.com/radruler/personal-inventory/internal/items"
	"github.com/radruler/personal-inventory/internal/locations"
	"github.com/radruler/personal-inventory/internal/model"
)

// apply writes the validated bundle. Every write is preceded by a
// comparison with the stored row, so an unchanged bundle writes nothing.
func (a *applier) apply() error {
	for _, ph := range a.placeholders {
		id, sha, _ := strings.Cut(ph, " ")
		if _, err := a.tx.ExecContext(a.ctx, `INSERT INTO media (id, sha256, mime, ext, bytes, variant_status, created_at)
			VALUES (?, ?, 'application/octet-stream', 'bin', 0, 'none', ?)`, id, sha, a.now); err != nil {
			return err
		}
	}
	for _, p := range a.products {
		if p.decl != nil {
			if err := a.applyProduct(p); err != nil {
				return err
			}
		}
	}
	for _, it := range a.items {
		if err := a.applyItemCore(it); err != nil {
			return err
		}
	}
	for _, it := range a.items {
		if err := a.applyItemRest(it); err != nil {
			return err
		}
	}
	if err := a.checkCycles(); err != nil {
		return err
	}
	// Projections and search: products first (they also refresh their
	// items' attrs), then items.
	for _, p := range a.products {
		if p.touched {
			if err := items.Recompute(a.ctx, a.tx, model.SubjectProduct, p.id); err != nil {
				return err
			}
		}
	}
	for _, it := range a.items {
		if it.touched {
			if err := items.Recompute(a.ctx, a.tx, model.SubjectItem, it.id); err != nil {
				return err
			}
		}
	}
	return a.duplicateWarnings()
}

func (a *applier) exec(query string, args ...any) (int64, error) {
	res, err := a.tx.ExecContext(a.ctx, query, args...)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// ---------------------------------------------------------------- products

func (a *applier) applyProduct(p *productPlan) error {
	d := p.decl
	if !p.exists {
		p.id = db.NewID()
		id := p.id
		p.out.ID, p.out.Action = &id, "created"
		name := strings.TrimSpace(*d.Name)
		if _, err := a.exec(`INSERT INTO product (id, brand, name, attrs, notes, ext_key, created_at, updated_at)
			VALUES (?,?,?,'{}',?,?,?,?)`, p.id, nullable(clean(d.Brand.ptr())), name, nullable(d.Notes.ptr()), p.ext, a.now, a.now); err != nil {
			return err
		}
		p.touched = true
	} else {
		var set []string
		var args []any
		if d.Brand.Set {
			if b := clean(d.Brand.ptr()); !eqStr(b, nullStr(p.cur.brand)) {
				set, args = append(set, "brand = ?"), append(args, nullable(b))
				p.out.change("brand")
			}
		}
		if d.Name != nil {
			if n := strings.TrimSpace(*d.Name); n != p.cur.name {
				set, args = append(set, "name = ?"), append(args, n)
				p.out.change("name")
			}
		}
		if d.Notes.Set && !eqStr(d.Notes.ptr(), nullStr(p.cur.notes)) {
			set, args = append(set, "notes = ?"), append(args, nullable(d.Notes.ptr()))
			p.out.change("notes")
		}
		if len(set) > 0 {
			if _, err := a.exec(`UPDATE product SET `+strings.Join(set, ", ")+`, updated_at = ? WHERE id = ?`, append(args, a.now, p.id)...); err != nil {
				return err
			}
			p.touched = true
		}
	}
	subj := subject{typ: model.SubjectProduct, id: p.id, exists: true, ext: p.ext}
	n := len(p.out.Changes)
	if err := a.applyMedia(subj, d.Media, p.mediaIDs, p.out); err != nil {
		return err
	}
	refIDs, err := a.applyReferences(subj, d.References, p.refKeys, p.refArchive, p.actor, p.out)
	if err != nil {
		return err
	}
	if err := a.applyIdentifiers(subj, d.Identifiers, p.idKeys, nil, p.actor, p.out); err != nil {
		return err
	}
	if err := a.applyFacts(subj, d.Facts, p.factKeys, nil, refIDs, p.actor, p.out); err != nil {
		return err
	}
	if len(p.out.Changes) > n {
		p.touched = true
		if p.exists {
			if _, err := a.exec(`UPDATE product SET updated_at = ? WHERE id = ?`, a.now, p.id); err != nil {
				return err
			}
		}
	}
	if p.out.Action == "created" {
		p.out.Changes = append([]string{"created"}, p.out.Changes...)
	}
	return nil
}

// ---------------------------------------------------------------- items

func (a *applier) productID(ext string) string {
	if p := a.prodByEx[ext]; p != nil {
		return p.id
	}
	return ""
}

func (a *applier) applyItemCore(pl *itemPlan) error {
	it := pl.in
	title := strings.TrimSpace(*it.Title)
	var productID *string
	if pl.productExt != "" {
		id := a.productID(pl.productExt)
		productID = &id
	}
	var cover *string
	if it.Cover != nil {
		c := pl.mediaIDs[*it.Cover]
		cover = &c
	}
	if !pl.exists {
		pl.id = db.NewID()
		id := pl.id
		pl.out.ID, pl.out.Action = &id, "created"
		qty := 1
		if it.Quantity != nil {
			qty = *it.Quantity
		}
		review := model.ReviewNeedsReview
		if it.ReviewState != nil {
			review = *it.ReviewState
		} else if a.def.ReviewState != nil {
			review = *a.def.ReviewState
		}
		if pl.loc != "" && pl.target != "" {
			// Already at (or inside) the target: nothing to stage (like POST /api/items).
			refs, err := locations.Refs(a.ctx, a.tx, []string{pl.loc})
			if err != nil {
				return err
			}
			if r, ok := refs[pl.loc]; ok && locations.Within(&r, pl.target) {
				pl.target = ""
			}
		}
		_, err := a.exec(`INSERT INTO item (id, kind, title, category, product_id, quantity, location_id, target_location_id,
			review_state, cover_media_id, notes, attrs, ext_key, created_at, updated_at)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,'{}',?,?,?)`,
			pl.id, pl.kind, title, nullable(clean(it.Category.ptr())), nullable(productID), qty,
			nullIfEmpty(pl.loc), nullIfEmpty(pl.target), review, nullable(cover), nullable(it.Notes.ptr()),
			it.ExtKey, a.now, a.now)
		if err != nil {
			return err
		}
		pl.touched = true
		pl.out.Changes = []string{"created"}
		return nil
	}
	cur := pl.cur
	var set []string
	var args []any
	upd := func(field, col string, v any) {
		set, args = append(set, col+" = ?"), append(args, v)
		pl.out.change("%s", field)
	}
	if pl.kind != cur.kind {
		upd("kind", "kind", pl.kind)
	}
	if title != cur.title {
		upd("title", "title", title)
	}
	if it.Category.Set {
		if c := clean(it.Category.ptr()); !eqStr(c, nullStr(cur.category)) {
			upd("category", "category", nullable(c))
		}
	}
	if it.Quantity != nil && *it.Quantity != cur.quantity {
		upd("quantity", "quantity", *it.Quantity)
	}
	if it.Notes.Set && !eqStr(it.Notes.ptr(), nullStr(cur.notes)) {
		upd("notes", "notes", nullable(it.Notes.ptr()))
	}
	// review_state changes only when the item itself says so: a default
	// never flips an item the owner has already reviewed.
	if it.ReviewState != nil && *it.ReviewState != cur.review {
		upd("review_state", "review_state", *it.ReviewState)
	}
	if productID != nil && !eqStr(productID, nullStr(cur.product)) {
		upd("product", "product_id", *productID)
	}
	if cover != nil && !eqStr(cover, nullStr(cur.cov)) {
		upd("cover", "cover_media_id", *cover)
	}
	if len(set) > 0 {
		if _, err := a.exec(`UPDATE item SET `+strings.Join(set, ", ")+`, updated_at = ? WHERE id = ?`, append(args, a.now, pl.id)...); err != nil {
			return err
		}
		pl.touched = true
	}
	return nil
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func (a *applier) itemID(ext string) (string, error) {
	if pl := a.itemByEx[ext]; pl != nil {
		return pl.id, nil
	}
	var id string
	err := a.tx.QueryRowContext(a.ctx, `SELECT id FROM item WHERE ext_key = ?`, ext).Scan(&id)
	return id, err
}

func (a *applier) applyItemRest(pl *itemPlan) error {
	it, out := pl.in, pl.out
	n := len(out.Changes)
	if it.Parent != nil {
		pid, err := a.itemID(strings.TrimSpace(*it.Parent))
		if err != nil {
			return err
		}
		if !pl.exists || !eqStr(&pid, nullStr(pl.cur.parent)) {
			if _, err := a.exec(`UPDATE item SET parent_item_id = ? WHERE id = ?`, pid, pl.id); err != nil {
				return err
			}
			if pl.exists {
				out.change("parent")
			}
		}
	}
	var added []string
	for _, t := range cleanTags(it.Tags) {
		k, err := a.exec(`INSERT INTO tag (item_id, tag) VALUES (?, ?) ON CONFLICT DO NOTHING`, pl.id, t)
		if err != nil {
			return err
		}
		if k > 0 {
			added = append(added, t)
		}
	}
	if len(added) > 0 {
		out.change("+%s %s", plural(len(added), "tag", "tags"), strings.Join(added, ", "))
	}
	subj := subject{typ: model.SubjectItem, id: pl.id, exists: true, ext: it.ExtKey}
	if err := a.applyMedia(subj, it.Media, pl.mediaIDs, out); err != nil {
		return err
	}
	obsIDs, err := a.applyObservations(pl)
	if err != nil {
		return err
	}
	refIDs, err := a.applyReferences(subj, it.References, pl.refKeys, pl.refArchive, pl.actor, out)
	if err != nil {
		return err
	}
	if err := a.applyIdentifiers(subj, it.Identifiers, pl.idKeys, obsIDs, pl.actor, out); err != nil {
		return err
	}
	if err := a.applyFacts(subj, it.Facts, pl.factKeys, obsIDs, refIDs, pl.actor, out); err != nil {
		return err
	}
	if !pl.exists {
		if pl.loc != "" {
			if _, err := a.exec(`INSERT INTO movement (id, item_id, kind, from_location_id, to_location_id, at, actor)
				VALUES (?,?,'move',NULL,?,?,?)`, db.NewID(), pl.id, pl.loc, a.now, pl.actor); err != nil {
				return err
			}
		}
		if pl.target != "" {
			if _, err := a.exec(`INSERT INTO movement (id, item_id, kind, from_location_id, to_location_id, at, actor)
				VALUES (?,?,'stage',?,?,?,?)`, db.NewID(), pl.id, nullIfEmpty(pl.loc), pl.target, a.now, pl.actor); err != nil {
				return err
			}
		}
	}
	if len(out.Changes) > n {
		pl.touched = true
	}
	return nil
}

func (a *applier) checkCycles() error {
	for _, pl := range a.items {
		if pl.in.Parent == nil {
			continue
		}
		var n int
		err := a.tx.QueryRowContext(a.ctx, `
			WITH RECURSIVE up(id, depth) AS (
			  SELECT parent_item_id, 0 FROM item WHERE id = ?
			  UNION SELECT i.parent_item_id, up.depth + 1 FROM item i JOIN up ON i.id = up.id
			   WHERE up.depth < 64
			) SELECT count(*) FROM up WHERE id = ?`, pl.id, pl.id).Scan(&n)
		if err != nil {
			return err
		}
		if n > 0 {
			a.problem(pl.path+".parent", "parent chain loops back to this item")
		}
	}
	return nil
}

// ---------------------------------------------------------------- media links

func (a *applier) applyMedia(subj subject, refs []bundleMediaRef, ids []string, out *Outcome) error {
	created := 0
	for j, m := range refs {
		mid := ids[j]
		var role string
		var sort int
		var caption sql.NullString
		err := a.tx.QueryRowContext(a.ctx, `SELECT role, sort, caption FROM media_link
			WHERE media_id = ? AND subject_type = ? AND subject_id = ?`, mid, subj.typ, subj.id).Scan(&role, &sort, &caption)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			role, sort := "other", j
			if r := clean(m.Role); r != nil {
				role = *r
			}
			if m.Sort != nil {
				sort = *m.Sort
			}
			if _, err := a.exec(`INSERT INTO media_link (media_id, subject_type, subject_id, role, sort, caption, created_at)
				VALUES (?,?,?,?,?,?,?)`, mid, subj.typ, subj.id, role, sort, nullable(m.Caption.ptr()), a.now); err != nil {
				return err
			}
			created++
		case err != nil:
			return err
		default:
			var set []string
			var args []any
			if r := clean(m.Role); r != nil && *r != role {
				set, args = append(set, "role = ?"), append(args, *r)
			}
			if m.Sort != nil && *m.Sort != sort {
				set, args = append(set, "sort = ?"), append(args, *m.Sort)
			}
			if m.Caption.Set && !eqStr(m.Caption.ptr(), nullStr(caption)) {
				set, args = append(set, "caption = ?"), append(args, nullable(m.Caption.ptr()))
			}
			if len(set) > 0 {
				if _, err := a.exec(`UPDATE media_link SET `+strings.Join(set, ", ")+
					` WHERE media_id = ? AND subject_type = ? AND subject_id = ?`, append(args, mid, subj.typ, subj.id)...); err != nil {
					return err
				}
				out.change("media[%d] link", j)
			}
		}
		if c := clean(m.Context); c != nil {
			k, err := a.exec(`UPDATE media SET context = ? WHERE id = ? AND (context IS NULL OR context = '')`, *c, mid)
			if err != nil {
				return err
			}
			if k > 0 {
				out.change("media[%d] context", j)
			}
		}
	}
	if created > 0 {
		out.change("+%d media", created)
	}
	return nil
}

// ---------------------------------------------------------------- observations

func (a *applier) applyObservations(pl *itemPlan) ([]string, error) {
	ids := make([]string, len(pl.in.Observations))
	created, replaced := 0, 0
	for j, o := range pl.in.Observations {
		key := pl.obsKeys[j]
		source := "other"
		if s := clean(o.Source); s != nil {
			source = *s
		}
		region, _ := cleanRegion(o.Region)
		data := "{}"
		if !isNull(o.Data) {
			data = compactJSON(o.Data)
		}
		mid := pl.obsMedia[j]
		var (
			oid                    string
			cSource, cData         string
			cMedia, cRegion, cText sql.NullString
		)
		err := a.tx.QueryRowContext(a.ctx, `SELECT id, source, media_id, region, text, data FROM observation WHERE ext_key = ?`, key).
			Scan(&oid, &cSource, &cMedia, &cRegion, &cText, &cData)
		switch {
		case errors.Is(err, sql.ErrNoRows):
		case err != nil:
			return nil, err
		default:
			same := cSource == source && eqStr(nullStr(cMedia), mid) && eqStr(nullStr(cText), o.Text) &&
				canonicalJSON([]byte(cData)) == canonicalJSON([]byte(data)) &&
				eqCanon(nullStr(cRegion), region)
			if same {
				ids[j] = oid
				continue
			}
			// Observations are append-only: the old row stays as history and
			// gives up its ext_key to the corrected one.
			if _, err := a.exec(`UPDATE observation SET ext_key = NULL WHERE id = ?`, oid); err != nil {
				return nil, err
			}
			replaced++
		}
		id := db.NewID()
		if _, err := a.exec(`INSERT INTO observation (id, item_id, source, media_id, region, text, data, recorded_by, recorded_at, ext_key)
			VALUES (?,?,?,?,?,?,?,?,?,?)`, id, pl.id, source, nullable(mid), nullable(region), nullable(o.Text), data,
			a.rowActor(o.RecordedBy, pl.actor), a.now, key); err != nil {
			return nil, err
		}
		ids[j] = id
		if oid == "" {
			created++
		}
	}
	if created > 0 {
		pl.out.change("+%d %s", created, plural(created, "observation", "observations"))
	}
	if replaced > 0 {
		pl.out.change("%d %s replaced (old kept as history)", replaced, plural(replaced, "observation", "observations"))
	}
	return ids, nil
}

func eqCanon(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return canonicalJSON([]byte(*a)) == canonicalJSON([]byte(*b))
}

func (a *applier) rowActor(own *string, fallback string) string {
	if s := clean(own); s != nil {
		return *s
	}
	return fallback
}

// ---------------------------------------------------------------- references

func (a *applier) applyReferences(subj subject, in []bundleReference, keys []string, archive []*string, actor string, out *Outcome) ([]string, error) {
	ids := make([]string, len(in))
	created := 0
	for j, r := range in {
		kind := "other"
		if k := clean(r.Kind); k != nil {
			kind = *k
		}
		var fetched *string
		if fa := clean(r.FetchedAt); fa != nil {
			t, _ := parseTime(*fa)
			fetched = &t
		}
		url := clean(r.URL)
		var (
			rid, cKind                         string
			cURL, cTitle, cArch, cFetch, cNote sql.NullString
		)
		err := a.tx.QueryRowContext(a.ctx, `SELECT id, url, title, kind, archive_media_id, fetched_at, notes FROM reference WHERE ext_key = ?`, keys[j]).
			Scan(&rid, &cURL, &cTitle, &cKind, &cArch, &cFetch, &cNote)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			rid = db.NewID()
			if _, err := a.exec(`INSERT INTO reference (id, subject_type, subject_id, url, title, kind, archive_media_id, fetched_at, notes, recorded_by, recorded_at, ext_key)
				VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`, rid, subj.typ, subj.id, nullable(url), nullable(r.Title), kind, nullable(archive[j]),
				nullable(fetched), nullable(r.Notes), a.rowActor(r.RecordedBy, actor), a.now, keys[j]); err != nil {
				return nil, err
			}
			created++
		case err != nil:
			return nil, err
		default:
			same := eqStr(nullStr(cURL), url) && eqStr(nullStr(cTitle), r.Title) && cKind == kind &&
				eqStr(nullStr(cArch), archive[j]) && eqStr(nullStr(cFetch), fetched) && eqStr(nullStr(cNote), r.Notes)
			if !same {
				if _, err := a.exec(`UPDATE reference SET url = ?, title = ?, kind = ?, archive_media_id = ?, fetched_at = ?, notes = ? WHERE id = ?`,
					nullable(url), nullable(r.Title), kind, nullable(archive[j]), nullable(fetched), nullable(r.Notes), rid); err != nil {
					return nil, err
				}
				out.change("reference %d updated", j)
			}
		}
		ids[j] = rid
	}
	if created > 0 {
		out.change("+%d %s", created, plural(created, "reference", "references"))
	}
	return ids, nil
}

// ---------------------------------------------------------------- identifiers

func (a *applier) applyIdentifiers(subj subject, in []bundleIdentifier, keys, obsIDs []string, actor string, out *Outcome) error {
	created := 0
	for j, idf := range in {
		scheme, value := strings.TrimSpace(idf.Scheme), strings.TrimSpace(idf.Value)
		norm := items.NormIdentifier(value)
		obs := clean(idf.ObservationID)
		if idf.Observation != nil {
			o := obsIDs[*idf.Observation]
			obs = &o
		}
		var (
			iid, cScheme, cValue string
			cObs, cNote          sql.NullString
		)
		err := a.tx.QueryRowContext(a.ctx, `SELECT id, scheme, value, observation_id, note FROM identifier WHERE ext_key = ?`, keys[j]).
			Scan(&iid, &cScheme, &cValue, &cObs, &cNote)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			if _, err := a.exec(`INSERT INTO identifier (id, subject_type, subject_id, scheme, value, value_norm, observation_id, note, recorded_by, recorded_at, ext_key)
				VALUES (?,?,?,?,?,?,?,?,?,?,?)`, db.NewID(), subj.typ, subj.id, scheme, value, norm, nullable(obs), nullable(idf.Note),
				a.rowActor(idf.RecordedBy, actor), a.now, keys[j]); err != nil {
				return err
			}
			created++
		case err != nil:
			return err
		default:
			if cScheme != scheme || cValue != value || !eqStr(nullStr(cObs), obs) || !eqStr(nullStr(cNote), idf.Note) {
				if _, err := a.exec(`UPDATE identifier SET scheme = ?, value = ?, value_norm = ?, observation_id = ?, note = ? WHERE id = ?`,
					scheme, value, norm, nullable(obs), nullable(idf.Note), iid); err != nil {
					return err
				}
				out.change("identifier %s %s updated", scheme, value)
			}
		}
	}
	if created > 0 {
		out.change("+%d %s", created, plural(created, "identifier", "identifiers"))
	}
	return nil
}

// ---------------------------------------------------------------- facts

func (a *applier) applyFacts(subj subject, in []bundleFact, keys, obsIDs, refIDs []string, actor string, out *Outcome) error {
	created := 0
	for j, f := range in {
		key := strings.TrimSpace(f.Key)
		value := compactJSON(f.Value)
		conf := 1.0
		if f.Confidence != nil {
			conf = *f.Confidence
		}
		srcs := make([]factSource, 0, len(f.Sources))
		for _, s := range f.Sources {
			if s.Observation != nil {
				id := obsIDs[*s.Observation]
				s.ObservationID, s.Observation = &id, nil
			}
			if s.Reference != nil {
				id := refIDs[*s.Reference]
				s.ReferenceID, s.Reference = &id, nil
			}
			if s == (factSource{}) {
				continue
			}
			srcs = append(srcs, s)
		}
		sb, _ := json.Marshal(srcs)
		sources := string(sb)
		var (
			fid, cKey, cValue, cBasis, cSources, cStatus string
			cConf                                        float64
			cNote                                        sql.NullString
		)
		err := a.tx.QueryRowContext(a.ctx, `SELECT id, key, value, basis, confidence, sources, status, note FROM fact WHERE ext_key = ?`, keys[j]).
			Scan(&fid, &cKey, &cValue, &cBasis, &cConf, &cSources, &cStatus, &cNote)
		var supersedes *string
		switch {
		case errors.Is(err, sql.ErrNoRows):
		case err != nil:
			return err
		default:
			same := cKey == key && canonicalJSON([]byte(cValue)) == canonicalJSON([]byte(value)) && cBasis == f.Basis &&
				cConf == conf && canonicalJSON([]byte(cSources)) == canonicalJSON(sb) && eqStr(nullStr(cNote), f.Note)
			if same {
				continue // includes facts the owner rejected: never resurrected
			}
			// Supersede: the old fact keeps its history; the ext_key moves on.
			if cStatus == "active" {
				if _, err := a.exec(`UPDATE fact SET status = 'superseded', ext_key = NULL WHERE id = ?`, fid); err != nil {
					return err
				}
			} else if _, err := a.exec(`UPDATE fact SET ext_key = NULL WHERE id = ?`, fid); err != nil {
				return err
			}
			old := fid
			supersedes = &old
			out.change("fact %s superseded", key)
		}
		if _, err := a.exec(`INSERT INTO fact (id, subject_type, subject_id, key, value, basis, confidence, sources, status, supersedes_id, note, recorded_by, recorded_at, ext_key)
			VALUES (?,?,?,?,?,?,?,?,'active',?,?,?,?,?)`, db.NewID(), subj.typ, subj.id, key, value, f.Basis, conf, sources,
			nullable(supersedes), nullable(f.Note), a.rowActor(f.RecordedBy, actor), a.now, keys[j]); err != nil {
			return err
		}
		if supersedes == nil {
			created++
		}
	}
	if created > 0 {
		out.change("+%d %s", created, plural(created, "fact", "facts"))
	}
	return nil
}

// ---------------------------------------------------------------- warnings

// duplicateWarnings flags identifiers whose normalized value is already on
// a different item or product (the item's own product and a product's own
// items excepted; company identifiers such as RN numbers are skipped).
func (a *applier) duplicateWarnings() error {
	check := func(subj subject, related string, in []bundleIdentifier, out *Outcome) error {
		for _, idf := range in {
			scheme := strings.TrimSpace(idf.Scheme)
			if companySchemes[scheme] {
				continue
			}
			norm := items.NormIdentifier(strings.TrimSpace(idf.Value))
			q := `SELECT i.subject_type, i.subject_id, COALESCE(it.ext_key, p.ext_key, '') FROM identifier i
				LEFT JOIN item it ON i.subject_type = 'item' AND it.id = i.subject_id
				LEFT JOIN product p ON i.subject_type = 'product' AND p.id = i.subject_id
				WHERE i.value_norm = ? AND NOT (i.subject_type = ? AND i.subject_id = ?)`
			args := []any{norm, subj.typ, subj.id}
			if subj.typ == model.SubjectItem && related != "" {
				q += ` AND NOT (i.subject_type = 'product' AND i.subject_id = ?)`
				args = append(args, related)
			}
			if subj.typ == model.SubjectProduct {
				q += ` AND NOT (i.subject_type = 'item' AND COALESCE(it.product_id, '') = ?)`
				args = append(args, subj.id)
			}
			rows, err := a.tx.QueryContext(a.ctx, q+` GROUP BY i.subject_type, i.subject_id ORDER BY i.subject_type, i.subject_id`, args...)
			if err != nil {
				return err
			}
			for rows.Next() {
				var st, sid, ext string
				if err := rows.Scan(&st, &sid, &ext); err != nil {
					rows.Close()
					return err
				}
				who := st + " " + sid
				if ext != "" {
					who = fmt.Sprintf("%s %s (%s)", st, ext, sid)
					if a.dry {
						who = st + " " + ext
					}
				}
				out.warn("identifier %s %s also on %s (possible duplicate)", scheme, strings.TrimSpace(idf.Value), who)
			}
			rows.Close()
			if err := rows.Err(); err != nil {
				return err
			}
		}
		return nil
	}
	for _, p := range a.products {
		if p.decl != nil {
			if err := check(subject{typ: model.SubjectProduct, id: p.id}, "", p.decl.Identifiers, p.out); err != nil {
				return err
			}
		}
	}
	for _, pl := range a.items {
		var related string
		if err := a.tx.QueryRowContext(a.ctx, `SELECT COALESCE(product_id, '') FROM item WHERE id = ?`, pl.id).Scan(&related); err != nil {
			return err
		}
		if err := check(subject{typ: model.SubjectItem, id: pl.id}, related, pl.in.Identifiers, pl.out); err != nil {
			return err
		}
	}
	return nil
}
