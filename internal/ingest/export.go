package ingest

import (
	"archive/tar"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/radruler/personal-inventory/internal/db"
	"github.com/radruler/personal-inventory/internal/httpapi"
	"github.com/radruler/personal-inventory/internal/media"
)

// Export layout (api/openapi.yaml GET /api/export), readable without the app:
//
//	manifest.json        format, schema/app version, counts, time
//	<table>.jsonl        one JSON object per row: column names as keys, JSON
//	                     columns (attrs, data, value, sources, …) nested
//	registries.jsonl     registry rows with a "registry" key naming the table
//	media/<sha256>.<ext> originals, byte for byte (variants are regenerated)
//
// Rows are sorted by primary key, so two exports of the same data are
// byte-identical except manifest.exported_at.

// ExportFormat names the layout in manifest.json.
const ExportFormat = "personal-inventory-export"

type tableSpec struct {
	file, table, order string
	json               []string
}

var exportTables = []tableSpec{
	{"locations.jsonl", "location", "id", []string{"layout"}},
	{"media.jsonl", "media", "id", []string{"exif"}},
	{"products.jsonl", "product", "id", []string{"attrs"}},
	{"items.jsonl", "item", "id", []string{"attrs"}},
	{"tags.jsonl", "tag", "item_id, tag", nil},
	{"observations.jsonl", "observation", "id", []string{"region", "data"}},
	{"identifiers.jsonl", "identifier", "id", nil},
	{"references.jsonl", "reference", "id", nil},
	{"facts.jsonl", "fact", "id", []string{"value", "sources"}},
	{"media_links.jsonl", "media_link", "media_id, subject_type, subject_id", nil},
	{"movements.jsonl", "movement", "id", nil},
}

// registryTables are exported to registries.jsonl, in this order.
var registryTables = []tableSpec{
	{"", "item_kind", "kind", nil},
	{"", "media_role", "role", nil},
	{"", "observation_source", "source", nil},
	{"", "fact_key", "key", []string{"kinds"}},
	{"", "identifier_scheme", "scheme", nil},
}

const registriesFile = "registries.jsonl"

// Manifest is manifest.json.
type Manifest struct {
	Format        string         `json:"format"`
	FormatVersion int            `json:"format_version"`
	SchemaVersion int            `json:"schema_version"`
	AppVersion    string         `json:"app_version"`
	ExportedAt    string         `json:"exported_at"`
	MediaIncluded bool           `json:"media_included"`
	Counts        map[string]int `json:"counts"`
	MediaFiles    int            `json:"media_files"`
	MissingMedia  []string       `json:"missing_media"`
}

// sink receives export files: a tar stream or a directory.
type sink interface {
	file(name string, size int64, write func(io.Writer) error) error
}

type tarSink struct {
	tw *tar.Writer
	at time.Time
}

func (s *tarSink) file(name string, size int64, write func(io.Writer) error) error {
	if err := s.tw.WriteHeader(&tar.Header{Name: name, Size: size, Mode: 0o644, ModTime: s.at, Typeflag: tar.TypeReg}); err != nil {
		return err
	}
	return write(s.tw)
}

type dirSink struct{ dir string }

func (s *dirSink) file(name string, _ int64, write func(io.Writer) error) error {
	p := filepath.Join(s.dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	f, err := os.Create(p)
	if err != nil {
		return err
	}
	if err := write(f); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

type countWriter struct{ n int64 }

func (c *countWriter) Write(p []byte) (int, error) { c.n += int64(len(p)); return len(p), nil }

// exporter writes one consistent snapshot (a read transaction).
type exporter struct {
	ctx       context.Context
	q         *sql.Tx
	cfg       httpapi.Deps
	withMedia bool
}

type exportPlan struct {
	sizes   map[string]int64
	man     Manifest
	files   []mediaFile
	present []mediaFile
}

type mediaFile struct {
	sha, ext string
	size     int64
}

func (mf mediaFile) name() string { return "media/" + mf.sha + "." + mf.ext }

// writeExport writes the whole export to s.
func writeExport(ctx context.Context, d *httpapi.Deps, s sink, withMedia bool) (*Manifest, error) {
	tx, err := d.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true}) // deferred: a read snapshot, no write lock
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	e := &exporter{ctx: ctx, q: tx, cfg: *d, withMedia: withMedia}
	plan, err := e.plan()
	if err != nil {
		return nil, err
	}
	mb, _ := json.MarshalIndent(plan.man, "", "  ")
	mb = append(mb, '\n')
	if err := s.file("manifest.json", int64(len(mb)), func(w io.Writer) error { _, err := w.Write(mb); return err }); err != nil {
		return nil, err
	}
	for _, t := range exportTables {
		if err := s.file(t.file, plan.sizes[t.file], func(w io.Writer) error { _, err := e.table(w, t, ""); return err }); err != nil {
			return nil, err
		}
	}
	if err := s.file(registriesFile, plan.sizes[registriesFile], func(w io.Writer) error { _, err := e.registries(w); return err }); err != nil {
		return nil, err
	}
	for _, mf := range plan.present {
		src := media.OrigPath(d.Cfg, mf.sha, mf.ext)
		err := s.file(mf.name(), mf.size, func(w io.Writer) error {
			f, err := os.Open(src)
			if err != nil {
				return err
			}
			defer f.Close()
			n, err := io.Copy(w, f)
			if err == nil && n != mf.size {
				err = fmt.Errorf("%s changed size during export", src)
			}
			return err
		})
		if err != nil {
			return nil, err
		}
	}
	return &plan.man, nil
}

// plan runs the counting pass: sizes and row counts for the manifest.
func (e *exporter) plan() (*exportPlan, error) {
	p := &exportPlan{sizes: map[string]int64{}}
	v, err := schemaVersion(e.ctx, e.q)
	if err != nil {
		return nil, err
	}
	p.man = Manifest{Format: ExportFormat, FormatVersion: 1, SchemaVersion: v, AppVersion: e.cfg.Version,
		ExportedAt: db.Now(), MediaIncluded: e.withMedia, Counts: map[string]int{}, MissingMedia: []string{}}
	for _, t := range exportTables {
		var c countWriter
		n, err := e.table(&c, t, "")
		if err != nil {
			return nil, err
		}
		p.sizes[t.file] = c.n
		p.man.Counts[strings.TrimSuffix(t.file, ".jsonl")] = n
	}
	var c countWriter
	n, err := e.registries(&c)
	if err != nil {
		return nil, err
	}
	p.sizes[registriesFile] = c.n
	p.man.Counts["registries"] = n
	if e.withMedia {
		rows, err := e.q.QueryContext(e.ctx, `SELECT sha256, ext FROM media ORDER BY sha256`)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var mf mediaFile
			if err := rows.Scan(&mf.sha, &mf.ext); err != nil {
				rows.Close()
				return nil, err
			}
			p.files = append(p.files, mf)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
		for _, mf := range p.files {
			fi, err := os.Stat(media.OrigPath(e.cfg.Cfg, mf.sha, mf.ext))
			if err != nil {
				p.man.MissingMedia = append(p.man.MissingMedia, mf.sha)
				continue
			}
			mf.size = fi.Size()
			p.present = append(p.present, mf)
		}
		p.man.MediaFiles = len(p.present)
	}
	return p, nil
}

func schemaVersion(ctx context.Context, q db.Querier) (int, error) {
	var v int
	err := q.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&v)
	return v, err
}

// table writes one table as JSONL; extra (a registry name) is emitted as a
// leading "registry" key.
func (e *exporter) table(w io.Writer, t tableSpec, extra string) (int, error) {
	rows, err := e.q.QueryContext(e.ctx, `SELECT * FROM `+t.table+` ORDER BY `+t.order)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return 0, err
	}
	isJSON := make([]bool, len(cols))
	for i, c := range cols {
		isJSON[i] = contains(t.json, c)
	}
	vals := make([]any, len(cols))
	ptrs := make([]any, len(cols))
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	n := 0
	var buf bytes.Buffer
	for rows.Next() {
		if err := rows.Scan(ptrs...); err != nil {
			return n, err
		}
		buf.Reset()
		buf.WriteByte('{')
		if extra != "" {
			buf.WriteString(`"registry":`)
			writeJSONString(&buf, extra)
		}
		for i, c := range cols {
			if i > 0 || extra != "" {
				buf.WriteByte(',')
			}
			writeJSONString(&buf, c)
			buf.WriteByte(':')
			if err := writeValue(&buf, vals[i], isJSON[i]); err != nil {
				return n, fmt.Errorf("%s.%s: %w", t.table, c, err)
			}
		}
		buf.WriteString("}\n")
		if _, err := w.Write(buf.Bytes()); err != nil {
			return n, err
		}
		n++
	}
	return n, rows.Err()
}

func (e *exporter) registries(w io.Writer) (int, error) {
	total := 0
	for _, t := range registryTables {
		n, err := e.table(w, t, t.table)
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

func writeJSONString(buf *bytes.Buffer, s string) {
	enc := json.NewEncoder(buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s)
	buf.Truncate(buf.Len() - 1) // Encode adds a newline
}

func writeValue(buf *bytes.Buffer, v any, isJSON bool) error {
	switch t := v.(type) {
	case nil:
		buf.WriteString("null")
	case int64:
		buf.WriteString(strconv.FormatInt(t, 10))
	case float64:
		b, err := json.Marshal(t)
		if err != nil {
			return err
		}
		buf.Write(b)
	case []byte:
		return writeValue(buf, string(t), isJSON)
	case string:
		if isJSON {
			if !json.Valid([]byte(t)) {
				return fmt.Errorf("stored JSON is invalid")
			}
			buf.WriteString(compactJSON([]byte(t)))
			return nil
		}
		writeJSONString(buf, t)
	case bool:
		buf.WriteString(strconv.FormatBool(t))
	default:
		return fmt.Errorf("unexpected column type %T", v)
	}
	return nil
}

// GET /api/export[?media=false]: a tar stream. Nothing is written on the
// server (a read transaction is the snapshot; the tar goes to the client).
func (h *handlers) export(w http.ResponseWriter, r *http.Request) error {
	withMedia := true
	if v := r.URL.Query().Get("media"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return httpapi.BadRequest("media must be true or false")
		}
		withMedia = b
	}
	now := time.Now().UTC()
	w.Header().Set("Content-Type", "application/x-tar")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="inventory-%s.tar"`, now.Format("20060102T150405Z")))
	cw := &trackWriter{w: w}
	tw := tar.NewWriter(cw)
	if _, err := writeExport(r.Context(), h.d, &tarSink{tw: tw, at: now}, withMedia); err != nil {
		if !cw.wrote {
			w.Header().Del("Content-Disposition")
			return err
		}
		// Headers are gone; the truncated tar tells the client it failed.
		h.d.Log.Error("export failed", "err", err)
		return nil
	}
	if err := tw.Close(); err != nil {
		h.d.Log.Error("export failed", "err", err)
	}
	return nil
}

type trackWriter struct {
	w     io.Writer
	wrote bool
}

func (t *trackWriter) Write(p []byte) (int, error) { t.wrote = true; return t.w.Write(p) }
