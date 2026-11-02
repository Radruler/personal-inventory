package ingest

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/radruler/personal-inventory/internal/httpapi"
	"github.com/radruler/personal-inventory/internal/items"
	"github.com/radruler/personal-inventory/internal/media"
)

// ImportStats summarizes an import.
type ImportStats struct {
	Rows          map[string]int
	MediaCopied   int
	MediaMissing  int
	VariantsReady int
	VariantsFail  int
}

// importDir loads an export directory into an empty database: ids,
// ext_keys, timestamps and statuses are kept; originals are copied (sha256
// verified); image variants are marked pending (or built when variants is
// true); item_fts is rebuilt.
func importDir(ctx context.Context, d *httpapi.Deps, dir string, variants bool, log io.Writer) (*ImportStats, error) {
	mb, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}
	var man Manifest
	if err := json.Unmarshal(mb, &man); err != nil {
		return nil, fmt.Errorf("manifest.json: %w", err)
	}
	if man.Format != ExportFormat {
		return nil, fmt.Errorf("manifest.json: format %q, want %q", man.Format, ExportFormat)
	}
	cur, err := schemaVersion(ctx, d.DB)
	if err != nil {
		return nil, err
	}
	if man.SchemaVersion != cur {
		return nil, fmt.Errorf("export has schema_version %d but this database is at %d", man.SchemaVersion, cur)
	}
	if err := mustBeEmpty(ctx, d); err != nil {
		return nil, err
	}

	st := &ImportStats{Rows: map[string]int{}}
	// Originals first (content-addressed, so a failed import leaves nothing
	// that a later one would not reuse).
	mediaRows, err := readJSONL(filepath.Join(dir, "media.jsonl"))
	if err != nil {
		return nil, err
	}
	for i, row := range mediaRows {
		var sha, ext string
		if json.Unmarshal(row["sha256"], &sha) != nil || json.Unmarshal(row["ext"], &ext) != nil || !shaRE.MatchString(sha) {
			return nil, fmt.Errorf("media.jsonl line %d: bad sha256/ext", i+1)
		}
		src := filepath.Join(dir, "media", sha+"."+ext)
		if _, err := os.Stat(src); errors.Is(err, os.ErrNotExist) {
			st.MediaMissing++
			continue
		}
		if err := copyVerified(src, media.OrigPath(d.Cfg, sha, ext), sha); err != nil {
			return nil, err
		}
		st.MediaCopied++
	}

	var itemIDs []string
	err = d.DB.Tx(ctx, func(tx *sql.Tx) error {
		cache := map[string][]column{}
		if _, err := tx.ExecContext(ctx, `PRAGMA defer_foreign_keys = ON`); err != nil {
			return err
		}
		regs, err := readJSONL(filepath.Join(dir, registriesFile))
		if err != nil {
			return err
		}
		for i, row := range regs {
			var name string
			_ = json.Unmarshal(row["registry"], &name)
			spec, ok := registrySpec(name)
			if !ok {
				return fmt.Errorf("%s line %d: unknown registry %q", registriesFile, i+1, name)
			}
			delete(row, "registry")
			if err := insertRow(ctx, tx, spec, row, "INSERT OR REPLACE", cache); err != nil {
				return fmt.Errorf("%s line %d: %w", registriesFile, i+1, err)
			}
		}
		st.Rows["registries"] = len(regs)
		for _, t := range exportTables {
			rows := mediaRows
			if t.table != "media" {
				if rows, err = readJSONL(filepath.Join(dir, t.file)); err != nil {
					return err
				}
			}
			for i, row := range rows {
				if err := insertRow(ctx, tx, t, row, "INSERT", cache); err != nil {
					return fmt.Errorf("%s line %d: %w", t.file, i+1, err)
				}
				if t.table == "item" {
					var id string
					_ = json.Unmarshal(row["id"], &id)
					itemIDs = append(itemIDs, id)
				}
			}
			st.Rows[strings.TrimSuffix(t.file, ".jsonl")] = len(rows)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE media SET variant_error = NULL,
			variant_status = CASE WHEN mime LIKE 'image/%' THEN 'pending' ELSE 'none' END`); err != nil {
			return err
		}
		for _, id := range itemIDs {
			if err := items.Reindex(ctx, tx, id); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if variants {
		if err := buildPending(ctx, d, st, log); err != nil {
			return st, err
		}
	}
	return st, nil
}

func mustBeEmpty(ctx context.Context, d *httpapi.Deps) error {
	var n int
	err := d.DB.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM item) + (SELECT count(*) FROM product) +
		(SELECT count(*) FROM location) + (SELECT count(*) FROM media) + (SELECT count(*) FROM observation) +
		(SELECT count(*) FROM identifier) + (SELECT count(*) FROM fact) + (SELECT count(*) FROM reference) +
		(SELECT count(*) FROM movement)`).Scan(&n)
	if err != nil {
		return err
	}
	if n > 0 {
		return errors.New("import needs an empty database (this one has data); point INV_DATA_DIR/INV_DB_PATH at a fresh location")
	}
	return nil
}

func registrySpec(name string) (tableSpec, bool) {
	for _, t := range registryTables {
		if t.table == name {
			return t, true
		}
	}
	return tableSpec{}, false
}

// readJSONL reads one JSON object per line (blank lines skipped).
func readJSONL(path string) ([]map[string]json.RawMessage, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	br := bufio.NewReaderSize(f, 1<<16)
	var out []map[string]json.RawMessage
	for line := 1; ; line++ {
		b, err := br.ReadBytes('\n')
		if len(bytes.TrimSpace(b)) > 0 {
			var row map[string]json.RawMessage
			if jerr := json.Unmarshal(b, &row); jerr != nil {
				return nil, fmt.Errorf("%s line %d: %w", filepath.Base(path), line, jerr)
			}
			out = append(out, row)
		}
		if errors.Is(err, io.EOF) {
			return out, nil
		}
		if err != nil {
			return nil, err
		}
	}
}

type column struct{ name, typ string }

func tableColumns(ctx context.Context, tx *sql.Tx, table string, colCache map[string][]column) ([]column, error) {
	if c, ok := colCache[table]; ok {
		return c, nil
	}
	rows, err := tx.QueryContext(ctx, `SELECT name, type FROM pragma_table_info(?)`, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var cols []column
	for rows.Next() {
		var c column
		if err := rows.Scan(&c.name, &c.typ); err != nil {
			return nil, err
		}
		c.typ = strings.ToUpper(c.typ)
		cols = append(cols, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	colCache[table] = cols
	return cols, nil
}

// insertRow inserts one exported row. Keys must be columns of the table.
func insertRow(ctx context.Context, tx *sql.Tx, t tableSpec, row map[string]json.RawMessage, verb string, cache map[string][]column) error {
	cols, err := tableColumns(ctx, tx, t.table, cache)
	if err != nil {
		return err
	}
	byName := map[string]column{}
	for _, c := range cols {
		byName[c.name] = c
	}
	keys := make([]string, 0, len(row))
	for k := range row {
		if _, ok := byName[k]; !ok {
			return fmt.Errorf("unknown column %q for %s", k, t.table)
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	args := make([]any, len(keys))
	for i, k := range keys {
		v, err := sqlValue(row[k], byName[k].typ, contains(t.json, k))
		if err != nil {
			return fmt.Errorf("%s: %w", k, err)
		}
		args[i] = v
	}
	q := verb + ` INTO ` + t.table + ` (` + strings.Join(keys, ", ") + `) VALUES (` + strings.TrimSuffix(strings.Repeat("?,", len(keys)), ",") + `)`
	_, err = tx.ExecContext(ctx, q, args...)
	return err
}

func sqlValue(raw json.RawMessage, typ string, isJSON bool) (any, error) {
	if isNull(raw) {
		return nil, nil
	}
	if isJSON {
		if !json.Valid(raw) {
			return nil, errors.New("invalid JSON")
		}
		return compactJSON(raw), nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	switch t := v.(type) {
	case string:
		return t, nil
	case bool:
		if t {
			return int64(1), nil
		}
		return int64(0), nil
	case json.Number:
		switch typ {
		case "INTEGER":
			return strconv.ParseInt(t.String(), 10, 64)
		case "REAL":
			return strconv.ParseFloat(t.String(), 64)
		}
		return t.String(), nil
	}
	return nil, fmt.Errorf("unexpected JSON %s", string(raw))
}

// copyVerified copies src to dst through a temp file in dst's directory,
// checking the sha256 before the rename. An existing dst with the right
// hash is kept as is.
func copyVerified(src, dst, sha string) error {
	if got, err := fileSHA(dst); err == nil && got == sha {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".import-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(tmp, h), in); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != sha {
		return fmt.Errorf("%s: sha256 is %s, want %s (corrupt export?)", src, got, sha)
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), dst)
}

func fileSHA(p string) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// buildPending generates variants for pending images synchronously.
func buildPending(ctx context.Context, d *httpapi.Deps, st *ImportStats, log io.Writer) error {
	rows, err := d.DB.QueryContext(ctx, `SELECT id, sha256, ext FROM media WHERE variant_status = 'pending' ORDER BY created_at, id`)
	if err != nil {
		return err
	}
	type row struct{ id, sha, ext string }
	var all []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.sha, &r.ext); err != nil {
			rows.Close()
			return err
		}
		all = append(all, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for i, r := range all {
		src := media.OrigPath(d.Cfg, r.sha, r.ext)
		var gerr error
		if _, err := os.Stat(src); err != nil {
			gerr = fmt.Errorf("original missing: %w", err)
		} else {
			vctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
			gerr = media.GenerateVariants(vctx, d.Cfg, src, r.sha)
			cancel()
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if gerr != nil {
			st.VariantsFail++
			msg := gerr.Error()
			if len(msg) > 500 {
				msg = msg[:500]
			}
			fmt.Fprintf(log, "[%d/%d] %s variants failed: %s\n", i+1, len(all), r.sha, msg)
			if _, err := d.DB.ExecContext(ctx, `UPDATE media SET variant_status = 'failed', variant_error = ? WHERE id = ?`, msg, r.id); err != nil {
				return err
			}
			continue
		}
		st.VariantsReady++
		if _, err := d.DB.ExecContext(ctx, `UPDATE media SET variant_status = 'ready', variant_error = NULL WHERE id = ?`, r.id); err != nil {
			return err
		}
	}
	return nil
}
