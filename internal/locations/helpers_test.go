package locations_test

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"testing"

	"github.com/radruler/personal-inventory/internal/db"
	"github.com/radruler/personal-inventory/internal/httpapi"
	"github.com/radruler/personal-inventory/internal/locations"
	"github.com/radruler/personal-inventory/internal/model"
	"github.com/radruler/personal-inventory/internal/testutil"
)

// oldStamp is the updated_at given to test items so tests can prove a box
// move never rewrites item rows.
const oldStamp = "2020-01-01T00:00:00.000Z"

type env struct {
	t   *testing.T
	d   *httpapi.Deps
	h   http.Handler
	seq int
}

func newEnv(t *testing.T) *env {
	t.Helper()
	d := testutil.NewDeps(t)
	d.ItemSummaries = stubSummaries
	mux := http.NewServeMux()
	locations.Register(mux, d)
	return &env{t: t, d: d, h: mux}
}

// stubSummaries stands in for items.Summaries (which this package must not
// import): id/kind/title/lifecycle plus placement from the location columns.
func stubSummaries(ctx context.Context, q db.Querier, ids []string) ([]model.ItemSummary, error) {
	out := []model.ItemSummary{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := q.QueryContext(ctx, `SELECT id, kind, title, quantity, lifecycle, gone_reason, gone_at,
		review_state, location_id, target_location_id, created_at, updated_at
		FROM item WHERE id IN (`+db.Placeholders(len(ids))+`)`, db.Args(ids)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type row struct {
		s           model.ItemSummary
		reason, at  sql.NullString
		loc, target sql.NullString
	}
	byID := map[string]*row{}
	var locIDs []string
	for rows.Next() {
		r := &row{}
		if err := rows.Scan(&r.s.ID, &r.s.Kind, &r.s.Title, &r.s.Quantity, &r.s.Lifecycle, &r.reason, &r.at,
			&r.s.ReviewState, &r.loc, &r.target, &r.s.CreatedAt, &r.s.UpdatedAt); err != nil {
			return nil, err
		}
		r.s.Attrs, r.s.Tags = map[string]any{}, []string{}
		locIDs = append(locIDs, r.loc.String, r.target.String)
		byID[r.s.ID] = r
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	refs, err := locations.Refs(ctx, q, locIDs)
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		r, ok := byID[id]
		if !ok {
			continue
		}
		var loc, target *model.LocationRef
		if l, ok := refs[r.loc.String]; ok {
			loc = &l
		}
		if l, ok := refs[r.target.String]; ok {
			target = &l
		}
		r.s.Placement = locations.DerivePlacement(r.s.Lifecycle, ns(r.reason), ns(r.at), loc, target)
		out = append(out, r.s)
	}
	return out, nil
}

func ns(n sql.NullString) *string {
	if !n.Valid {
		return nil
	}
	return &n.String
}

func (e *env) exec(q string, args ...any) {
	e.t.Helper()
	if _, err := e.d.DB.ExecContext(context.Background(), q, args...); err != nil {
		e.t.Fatalf("%s: %v", q, err)
	}
}

func (e *env) count(q string, args ...any) int {
	e.t.Helper()
	var n int
	if err := e.d.DB.QueryRowContext(context.Background(), q, args...).Scan(&n); err != nil {
		e.t.Fatalf("%s: %v", q, err)
	}
	return n
}

func (e *env) str(q string, args ...any) string {
	e.t.Helper()
	var s sql.NullString
	if err := e.d.DB.QueryRowContext(context.Background(), q, args...).Scan(&s); err != nil {
		e.t.Fatalf("%s: %v", q, err)
	}
	return s.String
}

// loc creates a location through the API and returns its id. parent is a
// LocationRefString or "".
func (e *env) loc(code, name, kind, parent string) string {
	e.t.Helper()
	body := map[string]any{"code": code, "name": name, "kind": kind}
	if parent != "" {
		body["parent"] = parent
	}
	var out locations.LocationDetail
	rec := e.do("POST", "/api/locations", body, &out)
	if rec.Code != http.StatusCreated {
		e.t.Fatalf("create %s: %d %s", code, rec.Code, rec.Body.String())
	}
	return out.ID
}

// item inserts an item with SQL. loc/target are location ids ("" = NULL).
func (e *env) item(title, loc, target, lifecycle string) string {
	e.t.Helper()
	e.seq++
	id := db.NewID()
	created := fmt.Sprintf("2026-01-01T00:00:%02d.000Z", e.seq%60)
	var goneReason, goneAt any
	if lifecycle == model.LifecycleGone {
		goneReason, goneAt = "sold", "2026-03-04T00:00:00.000Z"
	}
	e.exec(`INSERT INTO item (id, kind, title, location_id, target_location_id, lifecycle, gone_reason, gone_at,
		created_at, updated_at) VALUES (?, 'object', ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, title, nilIfEmpty(loc), nilIfEmpty(target), lifecycle, goneReason, goneAt, created, oldStamp)
	return id
}

func nilIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func (e *env) do(method, path string, body, out any) *httpResult {
	e.t.Helper()
	rec := testutil.Do(e.t, e.h, method, path, body, out)
	return &httpResult{Code: rec.Code, Body: rec.Body}
}

type httpResult struct {
	Code int
	Body interface{ String() string }
}

// want asserts a status code.
func (e *env) want(r *httpResult, code int) {
	e.t.Helper()
	if r.Code != code {
		e.t.Fatalf("status %d, want %d: %s", r.Code, code, r.Body.String())
	}
}

type errBody struct {
	Error struct {
		Code    string         `json:"code"`
		Message string         `json:"message"`
		Details map[string]any `json:"details"`
	} `json:"error"`
}

func findNode(ns []*locations.LocationNode, id string) *locations.LocationNode {
	for _, n := range ns {
		if n.ID == id {
			return n
		}
		if f := findNode(n.Children, id); f != nil {
			return f
		}
	}
	return nil
}

func ids(items []model.ItemSummary) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.ID
	}
	return out
}
