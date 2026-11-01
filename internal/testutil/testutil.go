// Package testutil provides helpers for httptest-based tests against a real
// temp SQLite database (and real vips, which the dev container provides).
package testutil

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/radruler/personal-inventory/internal/config"
	"github.com/radruler/personal-inventory/internal/db"
	"github.com/radruler/personal-inventory/internal/httpapi"
)

// NewDeps opens a fresh migrated database in t.TempDir() and returns Deps
// rooted there. ItemSummaries is left nil; tests that need it set it (for
// example to items.Summaries) or use the full server in internal/server.
func NewDeps(t testing.TB) *httpapi.Deps {
	t.Helper()
	dir := t.TempDir()
	cfg := config.ForTest(dir)
	for _, d := range []string{cfg.DataDir, cfg.MediaDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	d, err := db.Open(context.Background(), cfg.DBPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return &httpapi.Deps{
		DB:      d,
		Cfg:     cfg,
		Log:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		Version: "test",
	}
}

// Mtimes snapshots modification times of every file under dirs, for
// zero-write assertions.
func Mtimes(t testing.TB, dirs ...string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, root := range dirs {
		_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			if fi, err := d.Info(); err == nil {
				out[p] = fmt.Sprintf("%d/%d", fi.ModTime().UnixNano(), fi.Size())
			}
			return nil
		})
	}
	return out
}

// Do is a test helper: it sends a JSON request to h and decodes the JSON
// response into out (if non-nil). It returns the recorder for status and
// header checks. body may be nil, a []byte, an io.Reader or any value to be
// marshaled as JSON.
func Do(t testing.TB, h http.Handler, method, path string, body any, out any) *httptest.ResponseRecorder {
	t.Helper()
	var rd io.Reader
	switch b := body.(type) {
	case nil:
	case []byte:
		rd = bytes.NewReader(b)
	case io.Reader:
		rd = b
	default:
		buf, err := json.Marshal(b)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		rd = bytes.NewReader(buf)
	}
	req := httptest.NewRequest(method, path, rd)
	if rd != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if out != nil && rec.Body.Len() > 0 {
		if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
			t.Fatalf("%s %s: decode response (%d): %v\n%s", method, path, rec.Code, err, rec.Body.String())
		}
	}
	return rec
}
