package ingest_test

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"io/fs"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/radruler/personal-inventory/internal/ingest"
	"github.com/radruler/personal-inventory/internal/seed"
	"github.com/radruler/personal-inventory/internal/server"
	"github.com/radruler/personal-inventory/internal/testutil"
)

func readTree(t *testing.T, root string) map[string][]byte {
	t.Helper()
	out := map[string][]byte{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		out[filepath.ToSlash(rel)] = b
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// stripTime removes manifest.exported_at.
func stripTime(t *testing.T, b []byte) string {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("manifest: %v", err)
	}
	delete(m, "exported_at")
	out, _ := json.Marshal(m)
	return string(out)
}

func TestExportImportRoundTrip(t *testing.T) {
	ctx := context.Background()
	src := testutil.NewDeps(t)
	h1 := server.New(src)
	if _, err := seed.Demo(ctx, src); err != nil {
		t.Fatal(err)
	}
	tmp := t.TempDir()
	exp1 := filepath.Join(tmp, "exp1")
	var out bytes.Buffer
	if err := ingest.ExportCLI(ctx, src, []string{exp1}, &out); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(exp1 + ".tmp"); !os.IsNotExist(err) {
		t.Error("export left DIR.tmp behind")
	}
	if err := ingest.ExportCLI(ctx, src, []string{exp1}, &out); err == nil {
		t.Error("export over an existing directory should fail")
	}
	files1 := readTree(t, exp1)
	for _, name := range []string{"manifest.json", "items.jsonl", "products.jsonl", "locations.jsonl", "observations.jsonl",
		"identifiers.jsonl", "facts.jsonl", "references.jsonl", "media.jsonl", "media_links.jsonl", "movements.jsonl",
		"tags.jsonl", "registries.jsonl"} {
		if _, ok := files1[name]; !ok {
			t.Errorf("export lacks %s", name)
		}
	}
	var man ingest.Manifest
	json.Unmarshal(files1["manifest.json"], &man)
	if man.Counts["items"] != 61 || man.MediaFiles == 0 || man.MediaFiles != man.Counts["media"] || man.SchemaVersion != 1 {
		t.Errorf("manifest: %+v", man)
	}
	// Plain JSON: JSON columns are nested, not strings.
	line, _, _ := strings.Cut(string(files1["facts.jsonl"]), "\n")
	var fact map[string]any
	if err := json.Unmarshal([]byte(line), &fact); err != nil {
		t.Fatal(err)
	}
	if _, ok := fact["sources"].([]any); !ok {
		t.Errorf("facts.sources should be a nested array: %s", line)
	}
	line, _, _ = strings.Cut(string(files1["items.jsonl"]), "\n")
	var item map[string]any
	json.Unmarshal([]byte(line), &item)
	if _, ok := item["attrs"].(map[string]any); !ok || item["id"] == nil || item["ext_key"] == nil {
		t.Errorf("items.jsonl row: %s", line)
	}

	// Import into a fresh database, build variants, export again.
	dst := testutil.NewDeps(t)
	h2 := server.New(dst)
	out.Reset()
	if err := ingest.ImportCLI(ctx, dst, []string{exp1, "--variants"}, &out); err != nil {
		t.Fatalf("import: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "sha256 verified") || strings.Contains(out.String(), "failed: ") {
		t.Errorf("import output:\n%s", out.String())
	}
	exp2 := filepath.Join(tmp, "exp2")
	if _, err := ingest.ExportDir(ctx, dst, exp2, true); err != nil {
		t.Fatal(err)
	}
	files2 := readTree(t, exp2)
	if len(files1) != len(files2) {
		t.Errorf("file count %d vs %d", len(files1), len(files2))
	}
	for name, b1 := range files1 {
		b2, ok := files2[name]
		switch {
		case !ok:
			t.Errorf("%s missing after round trip", name)
		case name == "manifest.json":
			if stripTime(t, b1) != stripTime(t, b2) {
				t.Errorf("manifest differs:\n%s\n%s", b1, b2)
			}
		case !bytes.Equal(b1, b2):
			t.Errorf("%s differs after round trip", name)
		}
	}

	// The API sees the same items (detail, search via the rebuilt FTS).
	var ids []string
	rows, err := src.DB.Query(`SELECT id FROM item ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var id string
		rows.Scan(&id)
		ids = append(ids, id)
	}
	rows.Close()
	for i := 0; i < len(ids); i += 6 {
		a := testutil.Do(t, h1, "GET", "/api/items/"+ids[i], nil, nil)
		b := testutil.Do(t, h2, "GET", "/api/items/"+ids[i], nil, nil)
		if a.Code != 200 || a.Body.String() != b.Body.String() {
			t.Errorf("GET /api/items/%s differs after import:\n%s\n%s", ids[i], a.Body.String(), b.Body.String())
		}
	}
	for _, q := range []string{"/api/items?q=uniqlo&limit=200", "/api/items?q=341456789", "/api/facets"} {
		a := testutil.Do(t, h1, "GET", q, nil, nil)
		b := testutil.Do(t, h2, "GET", q, nil, nil)
		if a.Body.String() != b.Body.String() {
			t.Errorf("GET %s differs after import", q)
		}
	}

	// Import refuses a non-empty database.
	if err := ingest.ImportCLI(ctx, dst, []string{exp1}, io.Discard); err == nil || !strings.Contains(err.Error(), "empty") {
		t.Errorf("import into a non-empty db: %v", err)
	}

	// A corrupted original is caught by the sha256 check.
	exp3 := filepath.Join(tmp, "exp3")
	os.MkdirAll(filepath.Join(exp3, "media"), 0o755)
	var victim string
	for name, b := range files1 {
		if strings.HasPrefix(name, "media/") && victim == "" {
			victim = name
			b = append([]byte{}, b...)
			b[len(b)-1] ^= 0xff
		}
		os.WriteFile(filepath.Join(exp3, filepath.FromSlash(name)), b, 0o644)
	}
	if err := ingest.ImportCLI(ctx, testutil.NewDeps(t), []string{exp3}, io.Discard); err == nil || !strings.Contains(err.Error(), "sha256") {
		t.Errorf("corrupt media: %v", err)
	}

	// GET /api/export streams the same layout as a tar.
	srv := httptest.NewServer(h1)
	defer srv.Close()
	for _, withMedia := range []bool{true, false} {
		url := srv.URL + "/api/export"
		if !withMedia {
			url += "?media=false"
		}
		resp, err := srv.Client().Get(url)
		if err != nil {
			t.Fatal(err)
		}
		if resp.Header.Get("Content-Type") != "application/x-tar" || !strings.Contains(resp.Header.Get("Content-Disposition"), ".tar") {
			t.Errorf("export headers: %v", resp.Header)
		}
		tr := tar.NewReader(resp.Body)
		seen := 0
		for {
			hdr, err := tr.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatalf("tar: %v", err)
			}
			b, _ := io.ReadAll(tr)
			seen++
			want, ok := files1[hdr.Name]
			switch {
			case !ok:
				t.Errorf("tar has unexpected %s", hdr.Name)
			case hdr.Name == "manifest.json":
				var m ingest.Manifest
				json.Unmarshal(b, &m)
				if m.MediaIncluded != withMedia {
					t.Errorf("manifest media_included = %v", m.MediaIncluded)
				}
			case !bytes.Equal(b, want):
				t.Errorf("tar %s differs from the directory export", hdr.Name)
			}
			if !withMedia && strings.HasPrefix(hdr.Name, "media/") {
				t.Errorf("media=false still has %s", hdr.Name)
			}
		}
		resp.Body.Close()
		wantN := len(files1)
		if !withMedia {
			wantN = 13
		}
		if seen != wantN {
			t.Errorf("tar (media=%v) has %d entries, want %d", withMedia, seen, wantN)
		}
	}
}

// Export reads only: no file under /data or /media changes.
func TestExportWritesNothing(t *testing.T) {
	ctx := context.Background()
	d := testutil.NewDeps(t)
	if _, err := seed.Demo(ctx, d); err != nil {
		t.Fatal(err)
	}
	h := server.New(d)
	before := testutil.Mtimes(t, d.Cfg.DataDir, d.Cfg.MediaDir)
	rec := testutil.Do(t, h, "GET", "/api/export", nil, nil)
	if rec.Code != 200 || rec.Body.Len() == 0 {
		t.Fatalf("export: %d", rec.Code)
	}
	if after := testutil.Mtimes(t, d.Cfg.DataDir, d.Cfg.MediaDir); !equalStrMaps(before, after) {
		t.Error("GET /api/export wrote files")
	}
}
