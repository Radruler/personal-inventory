package ingest_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/radruler/personal-inventory/internal/httpapi"
	"github.com/radruler/personal-inventory/internal/server"
	"github.com/radruler/personal-inventory/internal/testutil"
)

type env struct {
	t   *testing.T
	d   *httpapi.Deps
	h   http.Handler
	srv *httptest.Server
	loc map[string]string // code -> id
}

// newEnv starts a real server (real temp SQLite, real vips) with the
// locations the fixture bundle uses: BEDROOM (room) and U2 (unit).
func newEnv(t *testing.T) *env {
	t.Helper()
	d := testutil.NewDeps(t)
	h := server.New(d)
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	e := &env{t: t, d: d, h: h, srv: srv, loc: map[string]string{}}
	e.location("BEDROOM", "Bedroom", "room", "")
	e.location("U2", "Unit 2", "unit", "")
	return e
}

func (e *env) location(code, name, kind, parent string) string {
	e.t.Helper()
	body := map[string]any{"code": code, "name": name, "kind": kind}
	if parent != "" {
		body["parent"] = "code:" + parent
	}
	var out struct {
		ID string `json:"id"`
	}
	rec := testutil.Do(e.t, e.h, "POST", "/api/locations", body, &out)
	if rec.Code != http.StatusCreated {
		e.t.Fatalf("create location %s: %d %s", code, rec.Code, rec.Body.String())
	}
	e.loc[code] = out.ID
	return out.ID
}

// req sends a JSON request with optional headers and decodes the response.
func (e *env) req(method, path string, body any, out any, hdr ...string) int {
	e.t.Helper()
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			e.t.Fatal(err)
		}
		rd = bytes.NewReader(b)
	}
	r := httptest.NewRequest(method, path, rd)
	r.Header.Set("Content-Type", "application/json")
	for i := 0; i+1 < len(hdr); i += 2 {
		r.Header.Set(hdr[i], hdr[i+1])
	}
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, r)
	if out != nil && rec.Body.Len() > 0 {
		if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
			e.t.Fatalf("%s %s: %d %v: %s", method, path, rec.Code, err, rec.Body.String())
		}
	}
	return rec.Code
}

// apply POSTs a bundle (a JSON string or value) and expects 200.
func (e *env) apply(bundle any, dry bool, hdr ...string) result {
	e.t.Helper()
	path := "/api/ingest/bundle"
	if dry {
		path += "?dry_run=true"
	}
	if s, ok := bundle.(string); ok {
		bundle = json.RawMessage(s)
	}
	var res result
	if code := e.req("POST", path, bundle, &res, hdr...); code != http.StatusOK {
		e.t.Fatalf("apply bundle: %d %+v", code, res)
	}
	return res
}

type outcome struct {
	ExtKey   string   `json:"ext_key"`
	ID       *string  `json:"id"`
	Action   string   `json:"action"`
	Changes  []string `json:"changes"`
	Warnings []string `json:"warnings"`
}

type result struct {
	DryRun   bool      `json:"dry_run"`
	Items    []outcome `json:"items"`
	Products []outcome `json:"products"`
}

var countTables = []string{"item", "product", "location", "media", "media_link", "observation", "identifier",
	"fact", "reference", "movement", "tag", "item_fts"}

func (e *env) counts() map[string]int {
	e.t.Helper()
	out := map[string]int{}
	for _, t := range countTables {
		var n int
		if err := e.d.DB.QueryRow(`SELECT count(*) FROM ` + t).Scan(&n); err != nil {
			e.t.Fatal(err)
		}
		out[t] = n
	}
	return out
}

func (e *env) scalar(q string, args ...any) string {
	e.t.Helper()
	var s *string
	if err := e.d.DB.QueryRow(q, args...).Scan(&s); err != nil {
		e.t.Fatalf("%s: %v", q, err)
	}
	if s == nil {
		return "<null>"
	}
	return *s
}

// copyFixture copies testdata/folder to a temp dir and returns the bundle path.
func copyFixture(t *testing.T) string {
	t.Helper()
	dst := t.TempDir()
	src := filepath.Join("testdata", "folder")
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dst, d.Name()), b, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(dst, "bundle.json")
}

func (e *env) ingestCLI(args ...string) (string, error) {
	e.t.Helper()
	var out bytes.Buffer
	err := runCLI(context.Background(), append(args, "--server", e.srv.URL), &out)
	return out.String(), err
}

func equalMaps(a, b map[string]int) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

func equalStrMaps(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}
