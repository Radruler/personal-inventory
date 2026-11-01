package items

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"github.com/radruler/personal-inventory/internal/db"
	"github.com/radruler/personal-inventory/internal/httpapi"
	"github.com/radruler/personal-inventory/internal/media"
	"github.com/radruler/personal-inventory/internal/model"
)

type productCreate struct {
	Brand  *string `json:"brand"`
	Name   string  `json:"name"`
	Notes  *string `json:"notes"`
	ExtKey *string `json:"ext_key"`
}

type productPatch struct {
	Brand  Opt[string] `json:"brand"`
	Name   Opt[string] `json:"name"`
	Notes  Opt[string] `json:"notes"`
	ExtKey Opt[string] `json:"ext_key"`
}

const productCols = `p.id, p.brand, p.name, p.attrs, p.notes, p.ext_key, p.created_at, p.updated_at,
	(SELECT count(*) FROM item WHERE item.product_id = p.id)`

func scanProducts(ctx context.Context, q db.Querier, rows *sql.Rows) ([]Product, error) {
	defer rows.Close()
	var out []Product
	for rows.Next() {
		var p Product
		var brand, notes, ext sql.NullString
		var attrs string
		if err := rows.Scan(&p.ID, &brand, &p.Name, &attrs, &notes, &ext, &p.CreatedAt, &p.UpdatedAt, &p.ItemCount); err != nil {
			return nil, err
		}
		p.Brand, p.Notes, p.ExtKey = nullStr(brand), nullStr(notes), nullStr(ext)
		p.Attrs = decodeAttrs(attrs)
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	ids := make([]string, len(out))
	for i, p := range out {
		ids[i] = p.ID
	}
	covers, err := media.CoverFor(ctx, q, model.SubjectProduct, ids)
	if err != nil {
		return nil, err
	}
	refs, err := media.Refs(ctx, q, mapValues(covers))
	if err != nil {
		return nil, err
	}
	for i := range out {
		if mid, ok := covers[out[i].ID]; ok {
			if m, ok := refs[mid]; ok {
				out[i].Cover = &m
			}
		}
	}
	return out, nil
}

func mapValues(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for _, v := range m {
		out = append(out, v)
	}
	return out
}

// loadProducts loads Product cards by id.
func loadProducts(ctx context.Context, q db.Querier, ids []string) (map[string]Product, error) {
	out := map[string]Product{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := q.QueryContext(ctx, `SELECT `+productCols+` FROM product p WHERE p.id IN (`+db.Placeholders(len(ids))+`)`, db.Args(ids)...)
	if err != nil {
		return nil, err
	}
	list, err := scanProducts(ctx, q, rows)
	if err != nil {
		return nil, err
	}
	for _, p := range list {
		out[p.ID] = p
	}
	return out, nil
}

// loadProductDetail assembles ProductDetail; items is [] unless withItems.
func loadProductDetail(ctx context.Context, q db.Querier, id string, withItems bool) (*ProductDetail, error) {
	ps, err := loadProducts(ctx, q, []string{id})
	if err != nil {
		return nil, err
	}
	p, ok := ps[id]
	if !ok {
		return nil, httpapi.NotFound("no product %s", id)
	}
	d := &ProductDetail{Product: p, Items: []model.ItemSummary{}}
	if d.Media, err = linkedMedia(ctx, q, model.SubjectProduct, id); err != nil {
		return nil, err
	}
	if d.Identifiers, err = queryIdentifiers(ctx, q, `WHERE subject_type = 'product' AND subject_id = ? ORDER BY recorded_at, id`, id); err != nil {
		return nil, err
	}
	if d.Facts, err = queryFacts(ctx, q, `WHERE subject_type = 'product' AND subject_id = ? ORDER BY recorded_at DESC, id DESC`, id); err != nil {
		return nil, err
	}
	if d.References, err = referencesFor(ctx, q, model.SubjectProduct, id); err != nil {
		return nil, err
	}
	if withItems {
		ids, err := idList(ctx, q, `SELECT id FROM item WHERE product_id = ? ORDER BY updated_at DESC, id DESC`, id)
		if err != nil {
			return nil, err
		}
		if d.Items, err = Summaries(ctx, q, ids); err != nil {
			return nil, err
		}
	}
	return d, nil
}

func (a *api) listProducts(w http.ResponseWriter, r *http.Request) error {
	ctx, qv := r.Context(), r.URL.Query()
	limit, err := httpapi.Limit(r, 60, 200)
	if err != nil {
		return err
	}
	cur, err := httpapi.DecodeCursor(qv.Get("cursor"))
	if err != nil {
		return err
	}
	const kb, kn = `lower(COALESCE(p.brand, ''))`, `lower(p.name)`
	var where []string
	var args []any
	if s := strings.TrimSpace(qv.Get("q")); s != "" {
		like := "%" + likeEscape(s) + "%"
		where = append(where, `(p.brand LIKE ? ESCAPE '\' OR p.name LIKE ? ESCAPE '\')`)
		args = append(args, like, like)
	}
	if b := strings.TrimSpace(qv.Get("brand")); b != "" {
		where = append(where, `p.brand = ? COLLATE NOCASE`)
		args = append(args, b)
	}
	if cur != nil {
		cb, ok1 := cur["b"].(string)
		cn, ok2 := cur["n"].(string)
		cid, ok3 := cur["id"].(string)
		if !ok1 || !ok2 || !ok3 {
			return httpapi.BadRequest("invalid cursor")
		}
		where = append(where, `(`+kb+`, `+kn+`, p.id) > (?, ?, ?)`)
		args = append(args, cb, cn, cid)
	}
	// Collect ids and sort keys first, then load cards.
	query := `SELECT p.id, ` + kb + `, ` + kn + ` FROM product p`
	if len(where) > 0 {
		query += ` WHERE ` + strings.Join(where, ` AND `)
	}
	query += ` ORDER BY ` + kb + `, ` + kn + `, p.id LIMIT ?`
	args = append(args, limit+1)
	rows, err := a.d.DB.QueryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	type key struct{ id, b, n string }
	var keys []key
	for rows.Next() {
		var k key
		if err := rows.Scan(&k.id, &k.b, &k.n); err != nil {
			rows.Close()
			return err
		}
		keys = append(keys, k)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	out := ProductList{Products: []Product{}}
	if len(keys) > limit {
		last := keys[limit-1]
		c := httpapi.EncodeCursor(httpapi.Cursor{"b": last.b, "n": last.n, "id": last.id})
		out.NextCursor = &c
		keys = keys[:limit]
	}
	ids := make([]string, len(keys))
	for i, k := range keys {
		ids[i] = k.id
	}
	byID, err := loadProducts(ctx, a.d.DB, ids)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if p, ok := byID[id]; ok {
			out.Products = append(out.Products, p)
		}
	}
	httpapi.WriteJSON(w, http.StatusOK, out)
	return nil
}

func (a *api) createProduct(w http.ResponseWriter, r *http.Request) error {
	var in productCreate
	if err := httpapi.DecodeJSON(r, &in); err != nil {
		return err
	}
	name := strings.TrimSpace(in.Name)
	if name == "" {
		return httpapi.Validation("name: required")
	}
	id, now := db.NewID(), db.Now()
	ext := cleanOpt(in.ExtKey)
	err := a.d.DB.Tx(r.Context(), func(tx *sql.Tx) error {
		ctx := r.Context()
		if ext != nil {
			if err := extKeyFree(ctx, tx, "product", *ext, ""); err != nil {
				return err
			}
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO product (id, brand, name, attrs, notes, ext_key, created_at, updated_at)
			VALUES (?, ?, ?, '{}', ?, ?, ?, ?)`, id, nullable(cleanOpt(in.Brand)), name, nullable(in.Notes), nullable(ext), now, now)
		return err
	})
	if err != nil {
		return err
	}
	return a.writeProduct(w, r, id, http.StatusCreated)
}

func (a *api) writeProduct(w http.ResponseWriter, r *http.Request, id string, status int) error {
	d, err := loadProductDetail(r.Context(), a.d.DB, id, true)
	if err != nil {
		return err
	}
	httpapi.WriteJSON(w, status, d)
	return nil
}

func (a *api) getProduct(w http.ResponseWriter, r *http.Request) error {
	return a.writeProduct(w, r, r.PathValue("id"), http.StatusOK)
}

func (a *api) patchProduct(w http.ResponseWriter, r *http.Request) error {
	var in productPatch
	if err := httpapi.DecodeJSON(r, &in); err != nil {
		return err
	}
	if !in.Brand.Set && !in.Name.Set && !in.Notes.Set && !in.ExtKey.Set {
		return httpapi.BadRequest("nothing to update")
	}
	id, now := r.PathValue("id"), db.Now()
	err := a.d.DB.Tx(r.Context(), func(tx *sql.Tx) error {
		ctx := r.Context()
		if err := subjectExists(ctx, tx, model.SubjectProduct, id); err != nil {
			return err
		}
		set := []string{"updated_at = ?"}
		args := []any{now}
		if in.Brand.Set {
			set, args = append(set, "brand = ?"), append(args, nullable(cleanOpt(in.Brand.ptr())))
		}
		if in.Name.Set {
			name := strings.TrimSpace(in.Name.V)
			if in.Name.Null || name == "" {
				return httpapi.Validation("name: required")
			}
			set, args = append(set, "name = ?"), append(args, name)
		}
		if in.Notes.Set {
			set, args = append(set, "notes = ?"), append(args, nullable(in.Notes.ptr()))
		}
		if in.ExtKey.Set {
			ext := cleanOpt(in.ExtKey.ptr())
			if ext != nil {
				if err := extKeyFree(ctx, tx, "product", *ext, id); err != nil {
					return err
				}
			}
			set, args = append(set, "ext_key = ?"), append(args, nullable(ext))
		}
		_, err := tx.ExecContext(ctx, `UPDATE product SET `+strings.Join(set, ", ")+` WHERE id = ?`, append(args, id)...)
		return err
	})
	if err != nil {
		return err
	}
	return a.writeProduct(w, r, id, http.StatusOK)
}

func (a *api) deleteProduct(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")
	err := a.d.DB.Tx(r.Context(), func(tx *sql.Tx) error {
		ctx := r.Context()
		if err := subjectExists(ctx, tx, model.SubjectProduct, id); err != nil {
			return err
		}
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT count(*) FROM item WHERE product_id = ?`, id).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			return httpapi.Conflict("product %s is used by %d item(s)", id, n).WithDetails(map[string]any{"item_count": n})
		}
		for _, stmt := range []string{
			`DELETE FROM identifier WHERE subject_type = 'product' AND subject_id = ?`,
			`DELETE FROM fact WHERE subject_type = 'product' AND subject_id = ?`,
			`DELETE FROM reference WHERE subject_type = 'product' AND subject_id = ?`,
			`DELETE FROM media_link WHERE subject_type = 'product' AND subject_id = ?`,
			`DELETE FROM product WHERE id = ?`,
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

// extKeyFree returns 409 (details.id = existing) when ext_key is taken by
// another row of table.
func extKeyFree(ctx context.Context, q db.Querier, table, ext, self string) error {
	var id string
	err := q.QueryRowContext(ctx, `SELECT id FROM `+table+` WHERE ext_key = ?`, ext).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && id == self) {
		return nil
	}
	if err != nil {
		return err
	}
	return httpapi.Conflict("%s ext_key %q already exists", table, ext).WithDetails(map[string]any{"id": id})
}

func likeEscape(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}
