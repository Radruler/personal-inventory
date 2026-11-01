package items

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"unicode"

	"github.com/radruler/personal-inventory/internal/db"
	"github.com/radruler/personal-inventory/internal/httpapi"
	"github.com/radruler/personal-inventory/internal/model"
)

// NormIdentifier normalizes an identifier value for matching: uppercase,
// with spaces (any whitespace), dashes, dots, slashes and underscores removed.
func NormIdentifier(v string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case '-', '.', '/', '_':
			return -1
		}
		if unicode.IsSpace(r) {
			return -1
		}
		return unicode.ToUpper(r)
	}, v)
}

// ---------------------------------------------------------------- inputs

type observationInput struct {
	Source     *string         `json:"source"`
	MediaID    *string         `json:"media_id"`
	Region     json.RawMessage `json:"region"`
	Text       *string         `json:"text"`
	Data       json.RawMessage `json:"data"`
	RecordedBy *string         `json:"recorded_by"`
}

type identifierInput struct {
	Scheme        string  `json:"scheme"`
	Value         string  `json:"value"`
	ObservationID *string `json:"observation_id"`
	Note          *string `json:"note"`
	RecordedBy    *string `json:"recorded_by"`
}

type nestedIdentifier struct {
	identifierInput
	Observation *int `json:"observation"`
}

type factSource struct {
	ObservationID *string `json:"observation_id,omitempty"`
	ReferenceID   *string `json:"reference_id,omitempty"`
	MediaID       *string `json:"media_id,omitempty"`
	URL           *string `json:"url,omitempty"`
	Note          *string `json:"note,omitempty"`
	Observation   *int    `json:"observation,omitempty"`
	Reference     *int    `json:"reference,omitempty"`
}

type factInput struct {
	Key        string          `json:"key"`
	Value      json.RawMessage `json:"value"`
	Basis      string          `json:"basis"`
	Confidence *float64        `json:"confidence"`
	Sources    []factSource    `json:"sources"`
	Note       *string         `json:"note"`
	RecordedBy *string         `json:"recorded_by"`
}

type factSupersede struct {
	Key        *string         `json:"key"`
	Value      json.RawMessage `json:"value"`
	Basis      string          `json:"basis"`
	Confidence *float64        `json:"confidence"`
	Sources    []factSource    `json:"sources"`
	Note       *string         `json:"note"`
	RecordedBy *string         `json:"recorded_by"`
}

type factReject struct {
	Note       *string `json:"note"`
	RecordedBy *string `json:"recorded_by"`
}

type referenceInput struct {
	URL            *string `json:"url"`
	Title          *string `json:"title"`
	Kind           *string `json:"kind"`
	ArchiveMediaID *string `json:"archive_media_id"`
	FetchedAt      *string `json:"fetched_at"`
	Notes          *string `json:"notes"`
	RecordedBy     *string `json:"recorded_by"`
}

type tagEdit struct {
	Add    []string `json:"add"`
	Remove []string `json:"remove"`
}

// ---------------------------------------------------------------- inserts

// insertObservation validates and stores one observation.
func insertObservation(ctx context.Context, q db.Querier, field, itemID string, in observationInput, now, actor string) (Observation, error) {
	o := Observation{ID: db.NewID(), ItemID: itemID, Source: "other", RecordedAt: now,
		RecordedBy: actorOr(in.RecordedBy, actor), Text: in.Text, MediaID: cleanOpt(in.MediaID)}
	if s := cleanOpt(in.Source); s != nil {
		o.Source = *s
	}
	if o.MediaID != nil {
		if err := mustExist(ctx, q, "media", field+"media_id", *o.MediaID); err != nil {
			return o, err
		}
	}
	region, err := cleanRegion(field, in.Region)
	if err != nil {
		return o, err
	}
	o.Region = region
	o.Data = json.RawMessage(`{}`)
	if len(in.Data) > 0 && string(bytes.TrimSpace(in.Data)) != "null" {
		if !isJSONObject(in.Data) {
			return o, httpapi.Validation("%sdata: must be a JSON object", field)
		}
		o.Data = json.RawMessage(compactJSON(in.Data))
	}
	var regionArg any
	if o.Region != nil {
		regionArg = string(o.Region)
	}
	_, err = q.ExecContext(ctx, `INSERT INTO observation (id, item_id, source, media_id, region, text, data, recorded_by, recorded_at)
		VALUES (?,?,?,?,?,?,?,?,?)`, o.ID, itemID, o.Source, nullable(o.MediaID), regionArg, nullable(o.Text), string(o.Data), o.RecordedBy, now)
	return o, err
}

func cleanRegion(field string, raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 || string(bytes.TrimSpace(raw)) == "null" {
		return nil, nil
	}
	var r struct {
		X, Y, W, H *float64
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&r); err != nil || r.X == nil || r.Y == nil || r.W == nil || r.H == nil {
		return nil, httpapi.Validation("%sregion: want {x, y, w, h} as 0..1 fractions", field)
	}
	for _, v := range []float64{*r.X, *r.Y, *r.W, *r.H} {
		if v < 0 || v > 1 {
			return nil, httpapi.Validation("%sregion: values must be between 0 and 1", field)
		}
	}
	b, _ := json.Marshal(map[string]float64{"x": *r.X, "y": *r.Y, "w": *r.W, "h": *r.H})
	return b, nil
}

// insertIdentifier validates and stores one identifier. obsID, when not nil,
// overrides in.ObservationID (nested creates resolve indexes first).
func insertIdentifier(ctx context.Context, q db.Querier, field, subjectType, subjectID string, in identifierInput, now, actor string) (Identifier, error) {
	idf := Identifier{ID: db.NewID(), SubjectType: subjectType, SubjectID: subjectID,
		Scheme: strings.TrimSpace(in.Scheme), Value: strings.TrimSpace(in.Value),
		ObservationID: cleanOpt(in.ObservationID), Note: in.Note, RecordedBy: actorOr(in.RecordedBy, actor), RecordedAt: now}
	if idf.Scheme == "" {
		return idf, httpapi.Validation("%sscheme: required", field)
	}
	if idf.Value == "" {
		return idf, httpapi.Validation("%svalue: required", field)
	}
	idf.ValueNorm = NormIdentifier(idf.Value)
	if idf.ValueNorm == "" {
		return idf, httpapi.Validation("%svalue: %q has no letters or digits", field, idf.Value)
	}
	if idf.ObservationID != nil {
		var owner string
		err := q.QueryRowContext(ctx, `SELECT item_id FROM observation WHERE id = ?`, *idf.ObservationID).Scan(&owner)
		if errors.Is(err, sql.ErrNoRows) {
			return idf, httpapi.Validation("%sobservation_id: unknown observation %q", field, *idf.ObservationID)
		}
		if err != nil {
			return idf, err
		}
		if subjectType == model.SubjectItem && owner != subjectID {
			return idf, httpapi.Validation("%sobservation_id: observation %s belongs to another item", field, *idf.ObservationID)
		}
	}
	_, err := q.ExecContext(ctx, `INSERT INTO identifier (id, subject_type, subject_id, scheme, value, value_norm, observation_id, note, recorded_by, recorded_at)
		VALUES (?,?,?,?,?,?,?,?,?,?)`, idf.ID, subjectType, subjectID, idf.Scheme, idf.Value, idf.ValueNorm,
		nullable(idf.ObservationID), nullable(idf.Note), idf.RecordedBy, now)
	return idf, err
}

// buildSources validates fact sources. obs/refs map request-local indexes to
// ids; they are nil outside nested creates, where indexes are rejected.
func buildSources(field string, in []factSource, obs, refs []string) (json.RawMessage, error) {
	out := make([]factSource, 0, len(in))
	for i, s := range in {
		f := fmt.Sprintf("%ssources[%d].", field, i)
		if s.Observation != nil {
			if obs == nil {
				return nil, httpapi.Validation("%sobservation: indexes are only accepted in nested creates; use observation_id", f)
			}
			if *s.Observation < 0 || *s.Observation >= len(obs) {
				return nil, httpapi.Validation("%sobservation: index %d out of range (%d observations)", f, *s.Observation, len(obs))
			}
			id := obs[*s.Observation]
			s.ObservationID, s.Observation = &id, nil
		}
		if s.Reference != nil {
			if refs == nil {
				return nil, httpapi.Validation("%sreference: indexes are only accepted in nested creates; use reference_id", f)
			}
			if *s.Reference < 0 || *s.Reference >= len(refs) {
				return nil, httpapi.Validation("%sreference: index %d out of range (%d references)", f, *s.Reference, len(refs))
			}
			id := refs[*s.Reference]
			s.ReferenceID, s.Reference = &id, nil
		}
		if s == (factSource{}) {
			continue
		}
		out = append(out, s)
	}
	b, _ := json.Marshal(out)
	return b, nil
}

// insertFact validates and stores one active fact. sources must already be
// resolved (buildSources).
func insertFact(ctx context.Context, q db.Querier, field, subjectType, subjectID string, in factInput, sources json.RawMessage, supersedes *string, now, actor string) (Fact, error) {
	f := Fact{ID: db.NewID(), SubjectType: subjectType, SubjectID: subjectID, Key: strings.TrimSpace(in.Key),
		Basis: in.Basis, Confidence: 1, Sources: sources, Status: "active", SupersedesID: supersedes,
		Note: in.Note, RecordedBy: actorOr(in.RecordedBy, actor), RecordedAt: now}
	if !validKey(f.Key) {
		return f, httpapi.Validation("%skey: %q is not a valid fact key (letters, digits, _ . -)", field, in.Key)
	}
	if len(bytes.TrimSpace(in.Value)) == 0 || string(bytes.TrimSpace(in.Value)) == "null" {
		return f, httpapi.Validation("%svalue: required (any JSON value except null)", field)
	}
	f.Value = json.RawMessage(compactJSON(in.Value))
	if !contains(FactBases, f.Basis) {
		return f, httpapi.Validation("%sbasis: must be one of %s", field, strings.Join(FactBases, ", "))
	}
	if in.Confidence != nil {
		f.Confidence = *in.Confidence
	}
	if f.Confidence < 0 || f.Confidence > 1 {
		return f, httpapi.Validation("%sconfidence: must be between 0 and 1", field)
	}
	if f.Sources == nil {
		f.Sources = json.RawMessage(`[]`)
	}
	_, err := q.ExecContext(ctx, `INSERT INTO fact (id, subject_type, subject_id, key, value, basis, confidence, sources, status, supersedes_id, note, recorded_by, recorded_at)
		VALUES (?,?,?,?,?,?,?,?,'active',?,?,?,?)`, f.ID, subjectType, subjectID, f.Key, string(f.Value), f.Basis, f.Confidence,
		string(f.Sources), nullable(supersedes), nullable(f.Note), f.RecordedBy, now)
	return f, err
}

func insertReference(ctx context.Context, q db.Querier, field, subjectType, subjectID string, in referenceInput, now, actor string) (Reference, error) {
	ref := Reference{ID: db.NewID(), SubjectType: subjectType, SubjectID: subjectID, URL: cleanOpt(in.URL), Title: in.Title,
		Kind: "other", ArchiveMediaID: cleanOpt(in.ArchiveMediaID), Notes: in.Notes, RecordedBy: actorOr(in.RecordedBy, actor), RecordedAt: now}
	if k := cleanOpt(in.Kind); k != nil {
		ref.Kind = *k
	}
	if ref.ArchiveMediaID != nil {
		if err := mustExist(ctx, q, "media", field+"archive_media_id", *ref.ArchiveMediaID); err != nil {
			return ref, err
		}
	}
	if fa := cleanOpt(in.FetchedAt); fa != nil {
		t, err := parseTime(field+"fetched_at", *fa)
		if err != nil {
			return ref, err
		}
		ref.FetchedAt = &t
	}
	_, err := q.ExecContext(ctx, `INSERT INTO reference (id, subject_type, subject_id, url, title, kind, archive_media_id, fetched_at, notes, recorded_by, recorded_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?)`, ref.ID, subjectType, subjectID, nullable(ref.URL), nullable(ref.Title), ref.Kind,
		nullable(ref.ArchiveMediaID), nullable(ref.FetchedAt), nullable(ref.Notes), ref.RecordedBy, now)
	return ref, err
}

// ---------------------------------------------------------------- handlers

// subjectExists returns 404 when the item/product is missing.
func subjectExists(ctx context.Context, q db.Querier, subjectType, id string) error {
	ok, err := exists(ctx, q, subjectType, id)
	if err != nil {
		return err
	}
	if !ok {
		return httpapi.NotFound("no %s %s", subjectType, id)
	}
	return nil
}

// afterSubjectWrite recomputes attrs / search for the subject and returns
// the subject's attrs.
func afterSubjectWrite(ctx context.Context, q db.Querier, subjectType, id, now string) (map[string]any, error) {
	if subjectType == model.SubjectProduct {
		attrs, err := recomputeProduct(ctx, q, id, now)
		if err != nil {
			return nil, err
		}
		if _, err := q.ExecContext(ctx, `UPDATE product SET updated_at = ? WHERE id = ?`, now, id); err != nil {
			return nil, err
		}
		return decodeAttrs(attrs), nil
	}
	if err := finishItem(ctx, q, id, now); err != nil {
		return nil, err
	}
	var attrs string
	if err := q.QueryRowContext(ctx, `SELECT attrs FROM item WHERE id = ?`, id).Scan(&attrs); err != nil {
		return nil, err
	}
	return decodeAttrs(attrs), nil
}

func (a *api) addObservation(w http.ResponseWriter, r *http.Request) error {
	var in observationInput
	if err := httpapi.DecodeJSON(r, &in); err != nil {
		return err
	}
	id, now, actor := r.PathValue("id"), db.Now(), httpapi.Actor(r)
	var out Observation
	err := a.d.DB.Tx(r.Context(), func(tx *sql.Tx) error {
		ctx := r.Context()
		if err := subjectExists(ctx, tx, model.SubjectItem, id); err != nil {
			return err
		}
		var err error
		if out, err = insertObservation(ctx, tx, "", id, in, now, actor); err != nil {
			return err
		}
		_, err = afterSubjectWrite(ctx, tx, model.SubjectItem, id, now)
		return err
	})
	if err != nil {
		return err
	}
	httpapi.WriteJSON(w, http.StatusCreated, out)
	return nil
}

func (a *api) addIdentifier(subjectType string) func(http.ResponseWriter, *http.Request) error {
	return func(w http.ResponseWriter, r *http.Request) error {
		var in identifierInput
		if err := httpapi.DecodeJSON(r, &in); err != nil {
			return err
		}
		id, now, actor := r.PathValue("id"), db.Now(), httpapi.Actor(r)
		var out Identifier
		err := a.d.DB.Tx(r.Context(), func(tx *sql.Tx) error {
			ctx := r.Context()
			if err := subjectExists(ctx, tx, subjectType, id); err != nil {
				return err
			}
			var err error
			if out, err = insertIdentifier(ctx, tx, "", subjectType, id, in, now, actor); err != nil {
				return err
			}
			_, err = afterSubjectWrite(ctx, tx, subjectType, id, now)
			return err
		})
		if err != nil {
			return err
		}
		httpapi.WriteJSON(w, http.StatusCreated, out)
		return nil
	}
}

func (a *api) addFact(subjectType string) func(http.ResponseWriter, *http.Request) error {
	return func(w http.ResponseWriter, r *http.Request) error {
		var in factInput
		if err := httpapi.DecodeJSON(r, &in); err != nil {
			return err
		}
		id, now, actor := r.PathValue("id"), db.Now(), httpapi.Actor(r)
		var out FactWrite
		err := a.d.DB.Tx(r.Context(), func(tx *sql.Tx) error {
			ctx := r.Context()
			if err := subjectExists(ctx, tx, subjectType, id); err != nil {
				return err
			}
			src, err := buildSources("", in.Sources, nil, nil)
			if err != nil {
				return err
			}
			if out.Fact, err = insertFact(ctx, tx, "", subjectType, id, in, src, nil, now, actor); err != nil {
				return err
			}
			out.Attrs, err = afterSubjectWrite(ctx, tx, subjectType, id, now)
			return err
		})
		if err != nil {
			return err
		}
		httpapi.WriteJSON(w, http.StatusCreated, out)
		return nil
	}
}

func (a *api) addReference(subjectType string) func(http.ResponseWriter, *http.Request) error {
	return func(w http.ResponseWriter, r *http.Request) error {
		var in referenceInput
		if err := httpapi.DecodeJSON(r, &in); err != nil {
			return err
		}
		id, now, actor := r.PathValue("id"), db.Now(), httpapi.Actor(r)
		var out Reference
		err := a.d.DB.Tx(r.Context(), func(tx *sql.Tx) error {
			ctx := r.Context()
			if err := subjectExists(ctx, tx, subjectType, id); err != nil {
				return err
			}
			var err error
			if out, err = insertReference(ctx, tx, "", subjectType, id, in, now, actor); err != nil {
				return err
			}
			_, err = afterSubjectWrite(ctx, tx, subjectType, id, now)
			return err
		})
		if err != nil {
			return err
		}
		httpapi.WriteJSON(w, http.StatusCreated, out)
		return nil
	}
}

func (a *api) editTags(w http.ResponseWriter, r *http.Request) error {
	var in tagEdit
	if err := httpapi.DecodeJSON(r, &in); err != nil {
		return err
	}
	id, now := r.PathValue("id"), db.Now()
	var out TagSet
	err := a.d.DB.Tx(r.Context(), func(tx *sql.Tx) error {
		ctx := r.Context()
		if err := subjectExists(ctx, tx, model.SubjectItem, id); err != nil {
			return err
		}
		for _, t := range cleanTags(in.Remove) {
			if _, err := tx.ExecContext(ctx, `DELETE FROM tag WHERE item_id = ? AND tag = ?`, id, t); err != nil {
				return err
			}
		}
		for _, t := range cleanTags(in.Add) {
			if _, err := tx.ExecContext(ctx, `INSERT INTO tag (item_id, tag) VALUES (?, ?) ON CONFLICT DO NOTHING`, id, t); err != nil {
				return err
			}
		}
		if err := finishItem(ctx, tx, id, now); err != nil {
			return err
		}
		var err error
		out.Tags, err = itemTags(ctx, tx, id)
		return err
	})
	if err != nil {
		return err
	}
	httpapi.WriteJSON(w, http.StatusOK, out)
	return nil
}

func itemTags(ctx context.Context, q db.Querier, id string) ([]string, error) {
	return idList(ctx, q, `SELECT tag FROM tag WHERE item_id = ? ORDER BY tag`, id)
}

// loadFact reads one fact row.
func loadFact(ctx context.Context, q db.Querier, id string) (Fact, error) {
	fs, err := queryFacts(ctx, q, `WHERE id = ?`, id)
	if err != nil {
		return Fact{}, err
	}
	if len(fs) == 0 {
		return Fact{}, httpapi.NotFound("no fact %s", id)
	}
	return fs[0], nil
}

func (a *api) supersedeFact(w http.ResponseWriter, r *http.Request) error {
	var in factSupersede
	if err := httpapi.DecodeJSON(r, &in); err != nil {
		return err
	}
	id, now, actor := r.PathValue("id"), db.Now(), httpapi.Actor(r)
	var out FactWrite
	err := a.d.DB.Tx(r.Context(), func(tx *sql.Tx) error {
		ctx := r.Context()
		old, err := loadFact(ctx, tx, id)
		if err != nil {
			return err
		}
		if old.Status != "active" {
			return httpapi.Conflict("fact %s is %s, not active", id, old.Status)
		}
		fi := factInput{Key: old.Key, Value: in.Value, Basis: in.Basis, Confidence: in.Confidence,
			Note: in.Note, RecordedBy: in.RecordedBy}
		if in.Key != nil {
			fi.Key = *in.Key
		}
		src, err := buildSources("", in.Sources, nil, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE fact SET status = 'superseded' WHERE id = ?`, id); err != nil {
			return err
		}
		if out.Fact, err = insertFact(ctx, tx, "", old.SubjectType, old.SubjectID, fi, src, &old.ID, now, actor); err != nil {
			return err
		}
		out.Attrs, err = afterSubjectWrite(ctx, tx, old.SubjectType, old.SubjectID, now)
		return err
	})
	if err != nil {
		return err
	}
	httpapi.WriteJSON(w, http.StatusCreated, out)
	return nil
}

func (a *api) rejectFact(w http.ResponseWriter, r *http.Request) error {
	// The body is optional.
	var in factReject
	body, err := io.ReadAll(io.LimitReader(r.Body, httpapi.MaxJSONBody))
	if err != nil {
		return httpapi.BadRequest("read body: %v", err)
	}
	if len(bytes.TrimSpace(body)) > 0 {
		r.Body = io.NopCloser(bytes.NewReader(body))
		if err := httpapi.DecodeJSON(r, &in); err != nil {
			return err
		}
	}
	id, now := r.PathValue("id"), db.Now()
	var out FactWrite
	err = a.d.DB.Tx(r.Context(), func(tx *sql.Tx) error {
		ctx := r.Context()
		old, err := loadFact(ctx, tx, id)
		if err != nil {
			return err
		}
		if old.Status != "active" {
			return httpapi.Conflict("fact %s is %s, not active", id, old.Status)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE fact SET status = 'rejected', note = COALESCE(?, note) WHERE id = ?`, nullable(in.Note), id); err != nil {
			return err
		}
		if out.Fact, err = loadFact(ctx, tx, id); err != nil {
			return err
		}
		out.Attrs, err = afterSubjectWrite(ctx, tx, old.SubjectType, old.SubjectID, now)
		return err
	})
	if err != nil {
		return err
	}
	httpapi.WriteJSON(w, http.StatusOK, out)
	return nil
}

func (a *api) lookupIdentifier(w http.ResponseWriter, r *http.Request) error {
	ctx, qv := r.Context(), r.URL.Query()
	value := strings.TrimSpace(qv.Get("value"))
	if value == "" {
		return httpapi.BadRequest("value is required")
	}
	out := IdentifierLookup{ValueNorm: NormIdentifier(value), Matches: []IdentifierMatch{}}
	if out.ValueNorm != "" {
		where, args := `WHERE value_norm = ?`, []any{out.ValueNorm}
		if s := strings.TrimSpace(qv.Get("scheme")); s != "" {
			where += ` AND scheme = ?`
			args = append(args, s)
		}
		ids, err := queryIdentifiers(ctx, a.d.DB, where+` ORDER BY recorded_at, id`, args...)
		if err != nil {
			return err
		}
		var itemIDs, productIDs []string
		for _, idf := range ids {
			if idf.SubjectType == model.SubjectItem {
				itemIDs = append(itemIDs, idf.SubjectID)
			} else {
				productIDs = append(productIDs, idf.SubjectID)
			}
		}
		sums, err := Summaries(ctx, a.d.DB, itemIDs)
		if err != nil {
			return err
		}
		byItem := map[string]*model.ItemSummary{}
		for i := range sums {
			byItem[sums[i].ID] = &sums[i]
		}
		prods, err := loadProducts(ctx, a.d.DB, productIDs)
		if err != nil {
			return err
		}
		for _, idf := range ids {
			m := IdentifierMatch{Identifier: idf}
			if idf.SubjectType == model.SubjectItem {
				m.Item = byItem[idf.SubjectID]
			} else if p, ok := prods[idf.SubjectID]; ok {
				m.Product = &p
			}
			out.Matches = append(out.Matches, m)
		}
	}
	httpapi.WriteJSON(w, http.StatusOK, out)
	return nil
}

// ---------------------------------------------------------------- readers

func queryIdentifiers(ctx context.Context, q db.Querier, tail string, args ...any) ([]Identifier, error) {
	rows, err := q.QueryContext(ctx, `SELECT id, subject_type, subject_id, scheme, value, value_norm, observation_id, note,
		recorded_by, recorded_at, ext_key FROM identifier `+tail, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Identifier{}
	for rows.Next() {
		var i Identifier
		var obs, note, ext sql.NullString
		if err := rows.Scan(&i.ID, &i.SubjectType, &i.SubjectID, &i.Scheme, &i.Value, &i.ValueNorm, &obs, &note,
			&i.RecordedBy, &i.RecordedAt, &ext); err != nil {
			return nil, err
		}
		i.ObservationID, i.Note, i.ExtKey = nullStr(obs), nullStr(note), nullStr(ext)
		out = append(out, i)
	}
	return out, rows.Err()
}

func queryFacts(ctx context.Context, q db.Querier, tail string, args ...any) ([]Fact, error) {
	rows, err := q.QueryContext(ctx, `SELECT id, subject_type, subject_id, key, value, basis, confidence, sources, status,
		supersedes_id, note, recorded_by, recorded_at, ext_key FROM fact `+tail, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Fact{}
	for rows.Next() {
		var f Fact
		var value, sources string
		var sup, note, ext sql.NullString
		if err := rows.Scan(&f.ID, &f.SubjectType, &f.SubjectID, &f.Key, &value, &f.Basis, &f.Confidence, &sources,
			&f.Status, &sup, &note, &f.RecordedBy, &f.RecordedAt, &ext); err != nil {
			return nil, err
		}
		f.Value, f.Sources = json.RawMessage(value), json.RawMessage(sources)
		f.SupersedesID, f.Note, f.ExtKey = nullStr(sup), nullStr(note), nullStr(ext)
		out = append(out, f)
	}
	return out, rows.Err()
}

func queryReferences(ctx context.Context, q db.Querier, tail string, args ...any) ([]ReferenceView, error) {
	rows, err := q.QueryContext(ctx, `SELECT id, subject_type, subject_id, url, title, kind, archive_media_id, fetched_at,
		notes, recorded_by, recorded_at, ext_key FROM reference `+tail, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ReferenceView{}
	for rows.Next() {
		var ref Reference
		var url, title, arch, fetched, notes, ext sql.NullString
		if err := rows.Scan(&ref.ID, &ref.SubjectType, &ref.SubjectID, &url, &title, &ref.Kind, &arch, &fetched,
			&notes, &ref.RecordedBy, &ref.RecordedAt, &ext); err != nil {
			return nil, err
		}
		ref.URL, ref.Title, ref.ArchiveMediaID = nullStr(url), nullStr(title), nullStr(arch)
		ref.FetchedAt, ref.Notes, ref.ExtKey = nullStr(fetched), nullStr(notes), nullStr(ext)
		out = append(out, ReferenceView{Reference: ref})
	}
	return out, rows.Err()
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
