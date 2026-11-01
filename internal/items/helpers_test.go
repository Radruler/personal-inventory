package items

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/radruler/personal-inventory/internal/db"
	"github.com/radruler/personal-inventory/internal/httpapi"
	"github.com/radruler/personal-inventory/internal/testutil"
)

type env struct {
	t *testing.T
	d *httpapi.Deps
	h http.Handler
	n int
}

func newEnv(t *testing.T) *env {
	t.Helper()
	d := testutil.NewDeps(t)
	d.ItemSummaries = Summaries
	mux := http.NewServeMux()
	Register(mux, d)
	return &env{t: t, d: d, h: mux}
}

// do sends a request and fails the test unless the status matches want.
func (e *env) do(method, path string, body any, want int, out any) *httptest.ResponseRecorder {
	e.t.Helper()
	rec := testutil.Do(e.t, e.h, method, path, body, out)
	if rec.Code != want {
		e.t.Fatalf("%s %s: status %d, want %d\n%s", method, path, rec.Code, want, rec.Body.String())
	}
	return rec
}

// raw sends a request and returns the decoded JSON as a generic map.
func (e *env) raw(method, path string, body any, want int) map[string]any {
	e.t.Helper()
	var m map[string]any
	e.do(method, path, body, want, &m)
	return m
}

func (e *env) exec(q string, args ...any) {
	e.t.Helper()
	if _, err := e.d.DB.Exec(q, args...); err != nil {
		e.t.Fatalf("exec %q: %v", q, err)
	}
}

func (e *env) count(q string, args ...any) int {
	e.t.Helper()
	var n int
	if err := e.d.DB.QueryRow(q, args...).Scan(&n); err != nil {
		e.t.Fatalf("count %q: %v", q, err)
	}
	return n
}

// loc inserts a location directly (thread C owns the API).
func (e *env) loc(code, name, kind, parent string) string {
	e.t.Helper()
	id := db.NewID()
	var p any
	if parent != "" {
		p = parent
	}
	now := db.Now()
	e.exec(`INSERT INTO location (id, code, name, kind, parent_id, created_at, updated_at) VALUES (?,?,?,?,?,?,?)`,
		id, code, name, kind, p, now, now)
	return id
}

// media inserts a media row directly (thread B owns the API).
func (e *env) media(mime string) string {
	e.t.Helper()
	e.n++
	id := db.NewID()
	sha := fmt.Sprintf("%064x", e.n)
	e.exec(`INSERT INTO media (id, sha256, mime, ext, bytes, width, height, original_name, variant_status, created_at)
		VALUES (?,?,?,?,?,?,?,?,?,?)`, id, sha, mime, strings.TrimPrefix(mime, "image/"), 1234, 800, 600,
		fmt.Sprintf("IMG_%04d.JPG", e.n), "ready", db.Now())
	return id
}

func (e *env) link(mediaID, subjectType, subjectID, role string, sort int) {
	e.t.Helper()
	e.exec(`INSERT INTO media_link (media_id, subject_type, subject_id, role, sort, created_at) VALUES (?,?,?,?,?,?)`,
		mediaID, subjectType, subjectID, role, sort, db.Now())
}

// item creates an item through the API and returns its detail.
func (e *env) item(body map[string]any) ItemDetail {
	e.t.Helper()
	var d ItemDetail
	e.do("POST", "/api/items", body, http.StatusCreated, &d)
	return d
}

func (e *env) fact(subject, id string, body map[string]any) FactWrite {
	e.t.Helper()
	var fw FactWrite
	e.do("POST", "/api/"+subject+"s/"+id+"/facts", body, http.StatusCreated, &fw)
	return fw
}

func (e *env) list(query string) ItemList {
	e.t.Helper()
	var l ItemList
	e.do("GET", "/api/items?"+query, nil, http.StatusOK, &l)
	return l
}

func titles(l ItemList) []string {
	out := []string{}
	for _, it := range l.Items {
		out = append(out, it.Title)
	}
	return out
}

func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	m := map[string]int{}
	for _, s := range a {
		m[s]++
	}
	for _, s := range b {
		m[s]--
	}
	for _, v := range m {
		if v != 0 {
			return false
		}
	}
	return true
}

func jsonEq(t *testing.T, got json.RawMessage, want string) {
	t.Helper()
	var a, b any
	_ = json.Unmarshal(got, &a)
	_ = json.Unmarshal([]byte(want), &b)
	ga, _ := json.Marshal(a)
	wb, _ := json.Marshal(b)
	if string(ga) != string(wb) {
		t.Fatalf("json = %s, want %s", got, want)
	}
}

// keysOf returns the JSON object keys of m.
func keysOf(m map[string]any) []string {
	out := []string{}
	for k := range m {
		out = append(out, k)
	}
	return out
}

// requireKeys fails if any of keys is missing from m (null counts as present).
func requireKeys(t *testing.T, what string, m map[string]any, keys ...string) {
	t.Helper()
	for _, k := range keys {
		if _, ok := m[k]; !ok {
			t.Errorf("%s: missing key %q (have %v)", what, k, keysOf(m))
		}
	}
}

// httptestReq sends a JSON request with an X-Actor header.
func httptestReq(e *env, method, path string, body any, actor string) *httptest.ResponseRecorder {
	e.t.Helper()
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(method, path, strings.NewReader(string(b)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Actor", actor)
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	return rec
}

func decode(t *testing.T, b []byte, out any) {
	t.Helper()
	if err := json.Unmarshal(b, out); err != nil {
		t.Fatalf("decode: %v\n%s", err, b)
	}
}
