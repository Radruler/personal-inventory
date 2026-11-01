package items

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/radruler/personal-inventory/internal/db"
	"github.com/radruler/personal-inventory/internal/httpapi"
	"github.com/radruler/personal-inventory/internal/model"
)

// ValueTypes are the accepted fact_key.value_type values.
var ValueTypes = []string{"string", "number", "boolean", "date", "money", "composition", "json"}

type factKeyInput struct {
	Label       string      `json:"label"`
	ValueType   *string     `json:"value_type"`
	Kinds       *[]string   `json:"kinds"`
	Filterable  *bool       `json:"filterable"`
	Sort        *int        `json:"sort"`
	Description Opt[string] `json:"description"`
}

type identifierSchemeInput struct {
	Label       string      `json:"label"`
	Brand       Opt[string] `json:"brand"`
	Description Opt[string] `json:"description"`
	Pattern     Opt[string] `json:"pattern"`
	Example     Opt[string] `json:"example"`
	Notes       Opt[string] `json:"notes"`
}

func (a *api) getSchema(w http.ResponseWriter, r *http.Request) error {
	ctx, q := r.Context(), a.d.DB
	out := SchemaInfo{
		LocationKinds: append([]string{}, model.LocationKinds...),
		FactBases:     append([]string{}, FactBases...),
		GoneReasons:   append([]string{}, GoneReasons...),
	}
	var err error
	if out.Kinds, err = registry(ctx, q, `SELECT kind, label, sort, description FROM item_kind ORDER BY sort, kind`); err != nil {
		return err
	}
	if out.ObservationSources, err = registry(ctx, q, `SELECT source, label, sort, description FROM observation_source ORDER BY sort, source`); err != nil {
		return err
	}
	rows, err := q.QueryContext(ctx, `SELECT role, label, sort, description, cover_rank FROM media_role ORDER BY sort, role`)
	if err != nil {
		return err
	}
	out.MediaRoles = []MediaRoleEntry{}
	for rows.Next() {
		var e MediaRoleEntry
		var desc sql.NullString
		var rank sql.NullInt64
		if err := rows.Scan(&e.Value, &e.Label, &e.Sort, &desc, &rank); err != nil {
			rows.Close()
			return err
		}
		e.Description = nullStr(desc)
		if rank.Valid {
			n := int(rank.Int64)
			e.CoverRank = &n
		}
		out.MediaRoles = append(out.MediaRoles, e)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	if out.FactKeys, err = factKeys(ctx, q, ``); err != nil {
		return err
	}
	if out.IdentifierSchemes, err = identifierSchemes(ctx, q, ``); err != nil {
		return err
	}
	httpapi.WriteJSON(w, http.StatusOK, out)
	return nil
}

func registry(ctx context.Context, q db.Querier, query string) ([]RegistryEntry, error) {
	rows, err := q.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []RegistryEntry{}
	for rows.Next() {
		var e RegistryEntry
		var desc sql.NullString
		if err := rows.Scan(&e.Value, &e.Label, &e.Sort, &desc); err != nil {
			return nil, err
		}
		e.Description = nullStr(desc)
		out = append(out, e)
	}
	return out, rows.Err()
}

// factKeys lists fact_key rows (optionally filtered by a WHERE tail).
func factKeys(ctx context.Context, q db.Querier, where string, args ...any) ([]FactKey, error) {
	rows, err := q.QueryContext(ctx, `SELECT key, label, value_type, kinds, filterable, sort, description FROM fact_key `+
		where+` ORDER BY sort, key`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []FactKey{}
	for rows.Next() {
		var k FactKey
		var kinds string
		var filterable int
		var desc sql.NullString
		if err := rows.Scan(&k.Key, &k.Label, &k.ValueType, &kinds, &filterable, &k.Sort, &desc); err != nil {
			return nil, err
		}
		k.Kinds = []string{}
		_ = json.Unmarshal([]byte(kinds), &k.Kinds)
		if k.Kinds == nil {
			k.Kinds = []string{}
		}
		k.Filterable, k.Description = filterable == 1, nullStr(desc)
		out = append(out, k)
	}
	return out, rows.Err()
}

func identifierSchemes(ctx context.Context, q db.Querier, where string, args ...any) ([]IdentifierScheme, error) {
	rows, err := q.QueryContext(ctx, `SELECT scheme, label, brand, description, pattern, example, notes FROM identifier_scheme `+
		where+` ORDER BY scheme`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []IdentifierScheme{}
	for rows.Next() {
		var s IdentifierScheme
		var brand, desc, pattern, example, notes sql.NullString
		if err := rows.Scan(&s.Scheme, &s.Label, &brand, &desc, &pattern, &example, &notes); err != nil {
			return nil, err
		}
		s.Brand, s.Description, s.Pattern, s.Example, s.Notes = nullStr(brand), nullStr(desc), nullStr(pattern), nullStr(example), nullStr(notes)
		out = append(out, s)
	}
	return out, rows.Err()
}

// putFactKey creates or updates a fact_key. On update, absent fields keep
// their stored values; on create they take the contract defaults.
func (a *api) putFactKey(w http.ResponseWriter, r *http.Request) error {
	var in factKeyInput
	if err := httpapi.DecodeJSON(r, &in); err != nil {
		return err
	}
	key := strings.TrimSpace(r.PathValue("key"))
	if !validKey(key) {
		return httpapi.Validation("key: %q is not a valid fact key (letters, digits, _ . -)", key)
	}
	label := strings.TrimSpace(in.Label)
	if label == "" {
		return httpapi.Validation("label: required")
	}
	if in.ValueType != nil && !contains(ValueTypes, *in.ValueType) {
		return httpapi.Validation("value_type: must be one of %s", strings.Join(ValueTypes, ", "))
	}
	if in.Kinds != nil {
		for _, k := range *in.Kinds {
			if !contains(model.Kinds, k) {
				return httpapi.Validation("kinds: %q must be one of %s", k, strings.Join(model.Kinds, ", "))
			}
		}
	}
	var out FactKey
	err := a.d.DB.Tx(r.Context(), func(tx *sql.Tx) error {
		ctx := r.Context()
		cur, err := factKeys(ctx, tx, `WHERE key = ?`, key)
		if err != nil {
			return err
		}
		k := FactKey{Key: key, ValueType: "string", Kinds: []string{}, Sort: 100}
		if len(cur) == 1 {
			k = cur[0]
		}
		k.Label = label
		if in.ValueType != nil {
			k.ValueType = *in.ValueType
		}
		if in.Kinds != nil {
			k.Kinds = append([]string{}, *in.Kinds...)
		}
		if in.Filterable != nil {
			k.Filterable = *in.Filterable
		}
		if in.Sort != nil {
			k.Sort = *in.Sort
		}
		if in.Description.Set {
			k.Description = in.Description.ptr()
		}
		kinds, _ := json.Marshal(k.Kinds)
		filterable := 0
		if k.Filterable {
			filterable = 1
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO fact_key (key, label, value_type, kinds, filterable, sort, description)
			VALUES (?,?,?,?,?,?,?)
			ON CONFLICT (key) DO UPDATE SET label = excluded.label, value_type = excluded.value_type, kinds = excluded.kinds,
			  filterable = excluded.filterable, sort = excluded.sort, description = excluded.description`,
			key, k.Label, k.ValueType, string(kinds), filterable, k.Sort, nullable(k.Description))
		out = k
		return err
	})
	if err != nil {
		return err
	}
	httpapi.WriteJSON(w, http.StatusOK, out)
	return nil
}

// putIdentifierScheme creates or updates an identifier_scheme (same merge
// semantics as putFactKey).
func (a *api) putIdentifierScheme(w http.ResponseWriter, r *http.Request) error {
	var in identifierSchemeInput
	if err := httpapi.DecodeJSON(r, &in); err != nil {
		return err
	}
	scheme := strings.TrimSpace(r.PathValue("scheme"))
	if scheme == "" || len(scheme) > 100 {
		return httpapi.Validation("scheme: required (at most 100 characters)")
	}
	label := strings.TrimSpace(in.Label)
	if label == "" {
		return httpapi.Validation("label: required")
	}
	var out IdentifierScheme
	err := a.d.DB.Tx(r.Context(), func(tx *sql.Tx) error {
		ctx := r.Context()
		cur, err := identifierSchemes(ctx, tx, `WHERE scheme = ?`, scheme)
		if err != nil {
			return err
		}
		s := IdentifierScheme{Scheme: scheme}
		if len(cur) == 1 {
			s = cur[0]
		}
		s.Label = label
		for _, f := range []struct {
			in  Opt[string]
			dst **string
		}{{in.Brand, &s.Brand}, {in.Description, &s.Description}, {in.Pattern, &s.Pattern}, {in.Example, &s.Example}, {in.Notes, &s.Notes}} {
			if f.in.Set {
				*f.dst = f.in.ptr()
			}
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO identifier_scheme (scheme, label, brand, description, pattern, example, notes)
			VALUES (?,?,?,?,?,?,?)
			ON CONFLICT (scheme) DO UPDATE SET label = excluded.label, brand = excluded.brand, description = excluded.description,
			  pattern = excluded.pattern, example = excluded.example, notes = excluded.notes`,
			scheme, s.Label, nullable(s.Brand), nullable(s.Description), nullable(s.Pattern), nullable(s.Example), nullable(s.Notes))
		out = s
		return err
	})
	if err != nil {
		return err
	}
	httpapi.WriteJSON(w, http.StatusOK, out)
	return nil
}
