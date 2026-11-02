package ingest

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/radruler/personal-inventory/internal/db"
	"github.com/radruler/personal-inventory/internal/httpapi"
	"github.com/radruler/personal-inventory/internal/items"
	"github.com/radruler/personal-inventory/internal/locations"
	"github.com/radruler/personal-inventory/internal/model"
)

// POST /api/ingest/bundle (PLAN.md §7).
//
// The bundle is validated as a whole first (every problem is reported in one
// 422), then applied in one transaction. Re-applying an unchanged bundle
// executes no write statement at all. A dry run applies the bundle inside
// the transaction and rolls it back, so its result is exactly what a real
// apply would report; ids of rows it would create are null.
func (h *handlers) applyBundle(w http.ResponseWriter, r *http.Request) error {
	var b Bundle
	if err := httpapi.DecodeJSON(r, &b); err != nil {
		return err
	}
	dry := false
	if v := r.URL.Query().Get("dry_run"); v != "" {
		var err error
		if dry, err = strconv.ParseBool(v); err != nil {
			return httpapi.BadRequest("dry_run must be true or false")
		}
	}
	res, err := Apply(r.Context(), h.d.DB, &b, httpapi.Actor(r), dry)
	if err != nil {
		return err
	}
	httpapi.WriteJSON(w, http.StatusOK, res)
	return nil
}

type handlers struct{ d *httpapi.Deps }

// Apply validates and applies b. actor is the request actor (X-Actor or
// "user"); it is used when neither the item nor defaults set recorded_by.
// Validation failures are a 422 *httpapi.Error with []Problem details.
func Apply(ctx context.Context, d *db.DB, b *Bundle, actor string, dryRun bool) (*Result, error) {
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	a := newApplier(ctx, tx, b, actor, dryRun)
	if err := a.validate(); err != nil {
		return nil, err
	}
	if len(a.problems) > 0 {
		return nil, a.problemErr()
	}
	if err := a.apply(); err != nil {
		return nil, err
	}
	if len(a.problems) > 0 { // found while applying (parent cycles)
		return nil, a.problemErr()
	}
	res := a.result()
	if dryRun {
		return res, tx.Rollback()
	}
	return res, tx.Commit()
}

// ---------------------------------------------------------------- state

type itemRow struct {
	kind, title, review                   string
	quantity                              int
	category, notes, product, parent, cov sql.NullString
}

type itemPlan struct {
	in     *bundleItem
	path   string
	id     string
	exists bool
	cur    itemRow
	actor  string
	out    *Outcome
	// resolved during validation
	kind              string
	loc, target       string
	productExt        string
	mediaIDs          []string
	obsMedia          []*string
	obsKeys, idKeys   []string
	factKeys, refKeys []string
	refArchive        []*string
	touched           bool
}

type productRow struct {
	brand, notes sql.NullString
	name         string
}

type productPlan struct {
	ext     string
	decl    *bundleProduct
	path    string
	id      string
	exists  bool
	cur     productRow
	out     *Outcome
	touched bool
	// resolved during validation
	mediaIDs         []string
	idKeys, factKeys []string
	refKeys          []string
	refArchive       []*string
	actor            string
}

type applier struct {
	ctx      context.Context
	tx       *sql.Tx
	b        *Bundle
	def      bundleDefaults
	actor    string
	dry      bool
	now      string
	problems []Problem

	media        map[string]string // "id:<id>" / "sha:<sha>" -> media id ("" = unknown)
	placeholders []string          // dry run: shas not uploaded yet

	items    []*itemPlan
	itemByEx map[string]*itemPlan
	products []*productPlan // declared or referenced, in first-seen order
	prodByEx map[string]*productPlan
	childKey map[string]map[string]string // table -> ext_key -> path
}

func newApplier(ctx context.Context, tx *sql.Tx, b *Bundle, actor string, dry bool) *applier {
	a := &applier{ctx: ctx, tx: tx, b: b, actor: actor, dry: dry, now: db.Now(),
		media: map[string]string{}, itemByEx: map[string]*itemPlan{}, prodByEx: map[string]*productPlan{},
		childKey: map[string]map[string]string{}}
	if b.Defaults != nil {
		a.def = *b.Defaults
	}
	return a
}

func (a *applier) problem(path, format string, args ...any) {
	a.problems = append(a.problems, Problem{Path: path, Message: fmt.Sprintf(format, args...)})
}

func (a *applier) problemErr() error {
	n := len(a.problems)
	return httpapi.Validation("bundle has %d %s", n, plural(n, "problem", "problems")).WithDetails(a.problems)
}

func (a *applier) result() *Result {
	res := &Result{DryRun: a.dry, Items: []Outcome{}, Products: []Outcome{}}
	for _, p := range a.products {
		if p.out != nil {
			res.Products = append(res.Products, a.finalOutcome(p.out))
		}
	}
	for _, it := range a.items {
		res.Items = append(res.Items, a.finalOutcome(it.out))
	}
	return res
}

func (a *applier) finalOutcome(o *Outcome) Outcome {
	out := *o
	if a.dry && out.Action == "created" {
		out.ID = nil
	}
	if out.Action != "created" {
		if len(out.Changes) > 0 {
			out.Action = "updated"
		} else {
			out.Action = "unchanged"
		}
	}
	if out.Changes == nil {
		out.Changes = []string{}
	}
	if out.Warnings == nil {
		out.Warnings = []string{}
	}
	return out
}

func (o *Outcome) change(format string, args ...any) {
	o.Changes = append(o.Changes, fmt.Sprintf(format, args...))
}

func (o *Outcome) warn(format string, args ...any) {
	o.Warnings = append(o.Warnings, fmt.Sprintf(format, args...))
}

// ---------------------------------------------------------------- validation

func (a *applier) validate() error {
	ctx, tx := a.ctx, a.tx
	if a.b.Items == nil {
		a.problem("items", "required (may be empty)")
	}
	if k := a.def.Kind; k != nil && !contains(model.Kinds, *k) {
		a.problem("defaults.kind", "must be %s", strings.Join(model.Kinds, ", "))
	}
	if rs := a.def.ReviewState; rs != nil && *rs != model.ReviewOK && *rs != model.ReviewNeedsReview {
		a.problem("defaults.review_state", "must be ok or needs_review")
	}
	defLoc, err := a.location("defaults.location", a.def.Location)
	if err != nil {
		return err
	}
	defTarget, err := a.location("defaults.target_location", a.def.TargetLocation)
	if err != nil {
		return err
	}

	// Products: top-level declarations first, then nested ones.
	for i := range a.b.Products {
		if err := a.collectProduct(fmt.Sprintf("products[%d]", i), &a.b.Products[i]); err != nil {
			return err
		}
	}
	for i := range a.b.Items {
		if p := a.b.Items[i].Product; p != nil {
			if err := a.collectProduct(fmt.Sprintf("items[%d].product", i), p); err != nil {
				return err
			}
		}
	}
	for _, p := range a.products {
		if err := a.validateProduct(p); err != nil {
			return err
		}
	}

	// Items.
	for i := range a.b.Items {
		it := &a.b.Items[i]
		path := fmt.Sprintf("items[%d]", i)
		ext := strings.TrimSpace(it.ExtKey)
		pl := &itemPlan{in: it, path: path, actor: a.recordedBy(it.RecordedBy), out: &Outcome{ExtKey: ext}}
		a.items = append(a.items, pl)
		if ext == "" {
			a.problem(path+".ext_key", "required")
		} else if prev, dup := a.itemByEx[ext]; dup {
			a.problem(path+".ext_key", "duplicate ext_key %q (also %s)", ext, prev.path)
		} else {
			a.itemByEx[ext] = pl
		}
		it.ExtKey = ext
		if ext != "" {
			var cur itemRow
			err := tx.QueryRowContext(ctx, `SELECT id, kind, title, review_state, quantity, category, notes, product_id,
				parent_item_id, cover_media_id FROM item WHERE ext_key = ?`, ext).Scan(&pl.id, &cur.kind, &cur.title,
				&cur.review, &cur.quantity, &cur.category, &cur.notes, &cur.product, &cur.parent, &cur.cov)
			switch {
			case errors.Is(err, sql.ErrNoRows):
			case err != nil:
				return err
			default:
				pl.exists, pl.cur = true, cur
				id := pl.id
				pl.out.ID = &id
			}
		}
		if err := a.validateItem(pl, defLoc, defTarget); err != nil {
			return err
		}
	}
	return nil
}

func (a *applier) recordedBy(own *string) string {
	if s := clean(own); s != nil {
		return *s
	}
	if s := clean(a.def.RecordedBy); s != nil {
		return *s
	}
	return a.actor
}

// location resolves an optional location reference, recording a problem.
func (a *applier) location(path string, ref *string) (string, error) {
	if ref == nil || strings.TrimSpace(*ref) == "" {
		return "", nil
	}
	id, err := locations.ResolveRef(a.ctx, a.tx, *ref)
	var he *httpapi.Error
	if errors.As(err, &he) && he.Code == httpapi.CodeValidation {
		a.problem(path, "%s", he.Message)
		return "", nil
	}
	return id, err
}

func (a *applier) collectProduct(path string, p *bundleProduct) error {
	ext := strings.TrimSpace(p.ExtKey)
	p.ExtKey = ext
	if ext == "" {
		a.problem(path+".ext_key", "required")
		return nil
	}
	prev, ok := a.prodByEx[ext]
	if !ok {
		pl := &productPlan{ext: ext, path: path}
		if p.declares() {
			pl.decl = p
		}
		a.prodByEx[ext] = pl
		a.products = append(a.products, pl)
		return nil
	}
	if !p.declares() {
		return nil
	}
	if prev.decl == nil {
		prev.decl, prev.path = p, path
		return nil
	}
	x, _ := json.Marshal(prev.decl)
	y, _ := json.Marshal(p)
	if canonicalJSON(x) != canonicalJSON(y) {
		a.problem(path+".ext_key", "product %q is declared again with different content (first at %s); declare it once in products[] and link it with {\"ext_key\": %q}", ext, prev.path, ext)
	}
	return nil
}

func (a *applier) validateProduct(p *productPlan) error {
	ctx := a.ctx
	err := a.tx.QueryRowContext(ctx, `SELECT id, brand, name, notes FROM product WHERE ext_key = ?`, p.ext).
		Scan(&p.id, &p.cur.brand, &p.cur.name, &p.cur.notes)
	switch {
	case errors.Is(err, sql.ErrNoRows):
	case err != nil:
		return err
	default:
		p.exists = true
	}
	if p.decl == nil {
		if !p.exists {
			a.problem(p.path+".ext_key", "unknown product %q: declare it (with a name) in products[] or inline", p.ext)
		}
		return nil
	}
	p.out = &Outcome{ExtKey: p.ext}
	if p.exists {
		id := p.id
		p.out.ID = &id
	}
	p.actor = a.recordedBy(nil)
	d := p.decl
	if d.Name != nil && strings.TrimSpace(*d.Name) == "" {
		a.problem(p.path+".name", "cannot be empty")
	}
	if !p.exists && (d.Name == nil || strings.TrimSpace(*d.Name) == "") {
		a.problem(p.path+".name", "required to create product %q", p.ext)
	}
	for j := range d.Media {
		id, err := a.mediaRef(fmt.Sprintf("%s.media[%d]", p.path, j), &d.Media[j], p.out)
		if err != nil {
			return err
		}
		p.mediaIDs = append(p.mediaIDs, id)
	}
	subj := subject{typ: model.SubjectProduct, id: p.id, exists: p.exists, ext: p.ext}
	for j := range d.References {
		key, arch, err := a.validateReference(fmt.Sprintf("%s.references[%d]", p.path, j), subj, j, &d.References[j], p.out)
		if err != nil {
			return err
		}
		p.refKeys, p.refArchive = append(p.refKeys, key), append(p.refArchive, arch)
	}
	for j := range d.Identifiers {
		key, err := a.validateIdentifier(fmt.Sprintf("%s.identifiers[%d]", p.path, j), subj, j, &d.Identifiers[j], -1)
		if err != nil {
			return err
		}
		p.idKeys = append(p.idKeys, key)
	}
	for j := range d.Facts {
		key, err := a.validateFact(fmt.Sprintf("%s.facts[%d]", p.path, j), subj, j, &d.Facts[j], -1, len(d.References))
		if err != nil {
			return err
		}
		p.factKeys = append(p.factKeys, key)
	}
	return nil
}

type subject struct {
	typ, id string
	exists  bool
	ext     string
}

func (a *applier) validateItem(pl *itemPlan, defLoc, defTarget string) error {
	it, path := pl.in, pl.path
	switch {
	case it.Kind != nil:
		pl.kind = strings.TrimSpace(*it.Kind)
	case a.def.Kind != nil:
		pl.kind = *a.def.Kind
	}
	if it.Kind != nil && !contains(model.Kinds, pl.kind) {
		a.problem(path+".kind", "must be %s", strings.Join(model.Kinds, ", "))
	} else if pl.kind == "" {
		a.problem(path+".kind", "required (or set defaults.kind)")
	}
	if it.Title == nil || strings.TrimSpace(*it.Title) == "" {
		a.problem(path+".title", "required")
	}
	if it.Quantity != nil && *it.Quantity < 1 {
		a.problem(path+".quantity", "must be at least 1")
	}
	if rs := it.ReviewState; rs != nil && *rs != model.ReviewOK && *rs != model.ReviewNeedsReview {
		a.problem(path+".review_state", "must be ok or needs_review")
	}
	var err error
	pl.loc, pl.target = defLoc, defTarget
	if it.Location != nil {
		if pl.loc, err = a.location(path+".location", it.Location); err != nil {
			return err
		}
	}
	if it.TargetLocation != nil {
		if pl.target, err = a.location(path+".target_location", it.TargetLocation); err != nil {
			return err
		}
	}
	if it.Product != nil {
		pl.productExt = strings.TrimSpace(it.Product.ExtKey)
	}
	if it.Parent != nil {
		pe := strings.TrimSpace(*it.Parent)
		switch {
		case pe == "":
			a.problem(path+".parent", "cannot be empty")
		case pe == it.ExtKey:
			a.problem(path+".parent", "an item cannot be its own parent")
		default:
			if _, ok := a.itemByExtAny(pe); !ok {
				var id string
				err := a.tx.QueryRowContext(a.ctx, `SELECT id FROM item WHERE ext_key = ?`, pe).Scan(&id)
				if errors.Is(err, sql.ErrNoRows) {
					a.problem(path+".parent", "unknown item ext_key %q (not in this bundle or the inventory)", pe)
				} else if err != nil {
					return err
				}
			}
		}
	}
	for _, t := range it.Tags {
		if strings.TrimSpace(t) == "" {
			a.problem(path+".tags", "tags cannot be empty strings")
			break
		}
	}
	for j := range it.Media {
		id, err := a.mediaRef(fmt.Sprintf("%s.media[%d]", path, j), &it.Media[j], pl.out)
		if err != nil {
			return err
		}
		pl.mediaIDs = append(pl.mediaIDs, id)
	}
	if it.Cover != nil && (*it.Cover < 0 || *it.Cover >= len(it.Media)) {
		a.problem(path+".cover", "index %d out of range (%d media)", *it.Cover, len(it.Media))
	}
	subj := subject{typ: model.SubjectItem, id: pl.id, exists: pl.exists, ext: it.ExtKey}
	for j := range it.Observations {
		key, mid, err := a.validateObservation(fmt.Sprintf("%s.observations[%d]", path, j), subj, j, &it.Observations[j], pl)
		if err != nil {
			return err
		}
		pl.obsKeys, pl.obsMedia = append(pl.obsKeys, key), append(pl.obsMedia, mid)
	}
	for j := range it.References {
		key, arch, err := a.validateReference(fmt.Sprintf("%s.references[%d]", path, j), subj, j, &it.References[j], pl.out)
		if err != nil {
			return err
		}
		pl.refKeys, pl.refArchive = append(pl.refKeys, key), append(pl.refArchive, arch)
	}
	for j := range it.Identifiers {
		key, err := a.validateIdentifier(fmt.Sprintf("%s.identifiers[%d]", path, j), subj, j, &it.Identifiers[j], len(it.Observations))
		if err != nil {
			return err
		}
		pl.idKeys = append(pl.idKeys, key)
	}
	for j := range it.Facts {
		key, err := a.validateFact(fmt.Sprintf("%s.facts[%d]", path, j), subj, j, &it.Facts[j], len(it.Observations), len(it.References))
		if err != nil {
			return err
		}
		pl.factKeys = append(pl.factKeys, key)
	}
	return nil
}

// itemByExtAny finds an item ext_key anywhere in the bundle (also later items).
func (a *applier) itemByExtAny(ext string) (*bundleItem, bool) {
	for i := range a.b.Items {
		if strings.TrimSpace(a.b.Items[i].ExtKey) == ext {
			return &a.b.Items[i], true
		}
	}
	return nil, false
}

// childExtKey derives or validates the ext_key of a nested row and checks
// that no other subject (or other bundle row) owns it.
func (a *applier) childExtKey(path, table, short string, subj subject, idx int, explicit *string) (string, error) {
	key := fmt.Sprintf("%s#%s/%d", subj.ext, short, idx)
	if explicit != nil {
		key = strings.TrimSpace(*explicit)
		if key == "" {
			a.problem(path+".ext_key", "cannot be empty")
			return "", nil
		}
	}
	if subj.ext == "" {
		return "", nil
	}
	seen := a.childKey[table]
	if seen == nil {
		seen = map[string]string{}
		a.childKey[table] = seen
	}
	if prev, dup := seen[key]; dup {
		a.problem(path, "duplicate %s ext_key %q (also %s)", table, key, prev)
		return key, nil
	}
	seen[key] = path
	_, st, sid, found, err := rowOwner(a.ctx, a.tx, table, key)
	if err != nil {
		return "", err
	}
	if found && (!subj.exists || st != subj.typ || sid != subj.id) {
		a.problem(path, "%s ext_key %q already belongs to %s %s", table, key, st, sid)
	}
	return key, nil
}

// mediaRef resolves a BundleMediaRef to a media id.
func (a *applier) mediaRef(path string, m *bundleMediaRef, out *Outcome) (string, error) {
	n := 0
	for _, s := range []*string{m.MediaID, m.SHA256, m.Path} {
		if s != nil {
			n++
		}
	}
	if n != 1 {
		a.problem(path, "exactly one of media_id, sha256 or path is required")
		return "", nil
	}
	switch {
	case m.Path != nil:
		a.problem(path+".path", "local path %q is only understood by `inv ingest`; upload the file and reference it by sha256 or media_id", *m.Path)
		return "", nil
	case m.SHA256 != nil:
		return a.bySha(path+".sha256", strings.ToLower(strings.TrimSpace(*m.SHA256)), out)
	default:
		return a.byID(path+".media_id", strings.TrimSpace(*m.MediaID))
	}
}

// mediaString resolves an observation `media` value: a media id,
// sha256:<hex>, or (rejected here) a local path.
func (a *applier) mediaString(path, s string, out *Outcome) (string, error) {
	s = strings.TrimSpace(s)
	if len(s) > 7 && strings.EqualFold(s[:7], "sha256:") {
		return a.bySha(path, strings.ToLower(s[7:]), out)
	}
	if strings.ContainsAny(s, "/\\.") {
		a.problem(path, "local path %q is only understood by `inv ingest`; use sha256:<hex> or a media id", s)
		return "", nil
	}
	return a.byID(path, s)
}

func (a *applier) bySha(path, sha string, out *Outcome) (string, error) {
	if !shaRE.MatchString(sha) {
		a.problem(path, "%q is not a sha256 (64 lowercase hex characters)", sha)
		return "", nil
	}
	if id, ok := a.media["sha:"+sha]; ok {
		if id == "" && !a.dry {
			a.problem(path, "unknown sha256 %s; upload it first", short(sha))
		}
		return id, nil
	}
	var id string
	err := a.tx.QueryRowContext(a.ctx, `SELECT id FROM media WHERE sha256 = ?`, sha).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		if a.dry {
			// Dry run: not uploaded yet is expected (`inv ingest --dry-run`
			// uploads nothing); a placeholder row stands in until rollback.
			id = db.NewID()
			a.media["sha:"+sha] = id
			a.placeholders = append(a.placeholders, id+" "+sha)
			out.warn("media %s is not on the server yet (upload it before applying)", short(sha))
			return id, nil
		}
		a.media["sha:"+sha] = ""
		a.problem(path, "unknown sha256 %s; upload it first", short(sha))
		return "", nil
	}
	if err != nil {
		return "", err
	}
	a.media["sha:"+sha] = id
	return id, nil
}

func (a *applier) byID(path, id string) (string, error) {
	if id == "" {
		a.problem(path, "cannot be empty")
		return "", nil
	}
	if v, ok := a.media["id:"+id]; ok {
		if v == "" {
			a.problem(path, "unknown media_id %q", id)
		}
		return v, nil
	}
	var got string
	err := a.tx.QueryRowContext(a.ctx, `SELECT id FROM media WHERE id = ?`, id).Scan(&got)
	if errors.Is(err, sql.ErrNoRows) {
		a.media["id:"+id] = ""
		a.problem(path, "unknown media_id %q", id)
		return "", nil
	}
	if err != nil {
		return "", err
	}
	a.media["id:"+id] = got
	return got, nil
}

func short(sha string) string {
	if len(sha) > 12 {
		return sha[:12] + "…"
	}
	return sha
}

func (a *applier) validateObservation(path string, subj subject, j int, o *bundleObservation, pl *itemPlan) (string, *string, error) {
	key, err := a.childExtKey(path, "observation", "obs", subj, j, o.ExtKey)
	if err != nil {
		return "", nil, err
	}
	var mid *string
	if o.Media != nil {
		id, err := a.mediaString(path+".media", *o.Media, pl.out)
		if err != nil {
			return "", nil, err
		}
		mid = &id
	}
	if s := clean(o.MediaID); s != nil {
		id, err := a.byID(path+".media_id", *s)
		if err != nil {
			return "", nil, err
		}
		if mid != nil && *mid != id && id != "" && *mid != "" {
			a.problem(path, "media and media_id name different photos")
		}
		mid = &id
	}
	if mid != nil && *mid != "" && !contains(pl.mediaIDs, *mid) {
		pl.out.warn("observations[%d]: its photo is not in this item's media, so the gallery won't show it; add it to media (e.g. role care_label)", j)
	}
	if _, msg := cleanRegion(o.Region); msg != "" {
		a.problem(path+".region", "%s", msg)
	}
	if !isNull(o.Data) && !isJSONObject(o.Data) {
		a.problem(path+".data", "must be a JSON object")
	}
	return key, mid, nil
}

func (a *applier) validateIdentifier(path string, subj subject, j int, in *bundleIdentifier, nObs int) (string, error) {
	key, err := a.childExtKey(path, "identifier", "id", subj, j, in.ExtKey)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(in.Scheme) == "" {
		a.problem(path+".scheme", "required")
	}
	if v := strings.TrimSpace(in.Value); v == "" {
		a.problem(path+".value", "required")
	} else if items.NormIdentifier(v) == "" {
		a.problem(path+".value", "%q has no letters or digits", v)
	}
	if in.Observation != nil {
		switch {
		case nObs < 0:
			a.problem(path+".observation", "products have no observations; use observation_id")
		case *in.Observation < 0 || *in.Observation >= nObs:
			a.problem(path+".observation", "index %d out of range (%d observations)", *in.Observation, nObs)
		}
		if in.ObservationID != nil {
			a.problem(path, "give observation or observation_id, not both")
		}
	}
	if oid := clean(in.ObservationID); oid != nil {
		var owner string
		err := a.tx.QueryRowContext(a.ctx, `SELECT item_id FROM observation WHERE id = ?`, *oid).Scan(&owner)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			a.problem(path+".observation_id", "unknown observation %q", *oid)
		case err != nil:
			return "", err
		case subj.typ == model.SubjectItem && (!subj.exists || owner != subj.id):
			a.problem(path+".observation_id", "observation %s belongs to another item", *oid)
		}
	}
	return key, nil
}

func (a *applier) validateFact(path string, subj subject, j int, in *bundleFact, nObs, nRefs int) (string, error) {
	key, err := a.childExtKey(path, "fact", "fact", subj, j, in.ExtKey)
	if err != nil {
		return "", err
	}
	if !keyRE.MatchString(strings.TrimSpace(in.Key)) {
		a.problem(path+".key", "%q is not a valid fact key (letters, digits, _ . -)", in.Key)
	} else if key != "" {
		// Same ext_key, different fact key: almost always two bundles using
		// the same derived (positional) key, not a correction.
		var cur string
		err := a.tx.QueryRowContext(a.ctx, `SELECT key FROM fact WHERE ext_key = ?`, key).Scan(&cur)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return "", err
		}
		if err == nil && cur != strings.TrimSpace(in.Key) {
			a.problem(path+".ext_key", "ext_key %q belongs to a %q fact, not %q; give this fact its own ext_key", key, cur, strings.TrimSpace(in.Key))
		}
	}
	if isNull(in.Value) {
		a.problem(path+".value", "required (any JSON value except null)")
	}
	if !contains(factBases, in.Basis) {
		a.problem(path+".basis", "must be one of %s", strings.Join(factBases, ", "))
	}
	if c := in.Confidence; c != nil && (*c < 0 || *c > 1) {
		a.problem(path+".confidence", "must be between 0 and 1")
	}
	for k, s := range in.Sources {
		sp := fmt.Sprintf("%s.sources[%d]", path, k)
		if s.Observation != nil {
			switch {
			case nObs < 0:
				a.problem(sp+".observation", "products have no observations; use observation_id")
			case *s.Observation < 0 || *s.Observation >= nObs:
				a.problem(sp+".observation", "index %d out of range (%d observations)", *s.Observation, nObs)
			}
		}
		if s.Reference != nil && (*s.Reference < 0 || *s.Reference >= nRefs) {
			a.problem(sp+".reference", "index %d out of range (%d references)", *s.Reference, nRefs)
		}
	}
	return key, nil
}

func (a *applier) validateReference(path string, subj subject, j int, in *bundleReference, out *Outcome) (string, *string, error) {
	key, err := a.childExtKey(path, "reference", "ref", subj, j, in.ExtKey)
	if err != nil {
		return "", nil, err
	}
	var arch *string
	if in.Archive != nil {
		if in.ArchiveMediaID != nil {
			a.problem(path, "give archive or archive_media_id, not both")
		}
		id, err := a.mediaRef(path+".archive", in.Archive, out)
		if err != nil {
			return "", nil, err
		}
		arch = &id
	} else if s := clean(in.ArchiveMediaID); s != nil {
		id, err := a.byID(path+".archive_media_id", *s)
		if err != nil {
			return "", nil, err
		}
		arch = &id
	}
	if fa := clean(in.FetchedAt); fa != nil {
		if _, ok := parseTime(*fa); !ok {
			a.problem(path+".fetched_at", "want an ISO-8601 timestamp like 2026-11-01T10:00:00.000Z")
		}
	}
	return key, arch, nil
}
