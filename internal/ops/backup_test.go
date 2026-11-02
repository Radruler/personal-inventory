package ops_test

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/radruler/personal-inventory/internal/db"
	"github.com/radruler/personal-inventory/internal/ops"
	"github.com/radruler/personal-inventory/internal/testutil"
)

func insertItem(ctx context.Context, q db.Querier, title string) error {
	id := db.NewID()
	if _, err := q.ExecContext(ctx, `INSERT INTO item (id, kind, title) VALUES (?, 'object', ?)`, id, title); err != nil {
		return err
	}
	_, err := q.ExecContext(ctx, `INSERT INTO tag (item_id, tag) VALUES (?, 'backup-test')`, id)
	return err
}

func counts(t *testing.T, c *sql.DB) (items, tags int) {
	t.Helper()
	if err := c.QueryRow(`SELECT (SELECT count(*) FROM item), (SELECT count(*) FROM tag)`).Scan(&items, &tags); err != nil {
		t.Fatal(err)
	}
	return
}

// TestBackupWhileWriting takes a backup while another connection (its own
// pool, like `inv serve` next to `docker exec … inv backup`) keeps
// committing item+tag pairs. The copy must be a consistent snapshot: it
// opens, passes integrity_check, is not in WAL mode, and has exactly as many
// tags as items (every transaction is all or nothing).
func TestBackupWhileWriting(t *testing.T) {
	ctx := context.Background()
	d := testutil.NewDeps(t)
	const initial = 500
	err := d.DB.Tx(ctx, func(tx *sql.Tx) error {
		for i := 0; i < initial; i++ {
			if err := insertItem(ctx, tx, fmt.Sprintf("item %d", i)); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	writer, err := db.Open(ctx, d.Cfg.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	var (
		stop    atomic.Bool
		written atomic.Int64
		wg      sync.WaitGroup
		werr    error
	)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; !stop.Load(); i++ {
			err := writer.Tx(ctx, func(tx *sql.Tx) error { return insertItem(ctx, tx, fmt.Sprintf("concurrent %d", i)) })
			if err != nil {
				werr = err
				return
			}
			written.Add(1)
		}
	}()
	// Let the writer get going so the WAL holds uncheckpointed frames.
	for written.Load() < 20 {
		time.Sleep(time.Millisecond)
	}

	dest := filepath.Join(t.TempDir(), "copy.db")
	var out bytes.Buffer
	if err := ops.BackupCLI(ctx, d, []string{dest}, &out); err != nil {
		t.Fatal(err)
	}
	during := written.Load()
	stop.Store(true)
	wg.Wait()
	if werr != nil {
		t.Fatalf("concurrent writer: %v", werr)
	}
	if during < 20 {
		t.Fatalf("writer committed only %d transactions", during)
	}
	if !strings.HasPrefix(out.String(), dest+"\t") || !strings.Contains(out.String(), " bytes") {
		t.Errorf("output %q: want path and size", out.String())
	}

	c, err := sql.Open("sqlite", "file:"+dest+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	var ic, mode string
	if err := c.QueryRow(`PRAGMA integrity_check`).Scan(&ic); err != nil || ic != "ok" {
		t.Fatalf("integrity_check = %q, %v", ic, err)
	}
	if err := c.QueryRow(`PRAGMA journal_mode`).Scan(&mode); err != nil || mode != "delete" {
		t.Errorf("journal_mode = %q, %v; want delete (a self-contained file)", mode, err)
	}
	var v int
	if err := c.QueryRow(`PRAGMA user_version`).Scan(&v); err != nil || v < 1 {
		t.Errorf("user_version = %d, %v", v, err)
	}
	items, tags := counts(t, c)
	t.Logf("copy has %d items (initial %d, %d committed concurrently before the backup returned)", items, initial, during)
	if items != tags {
		t.Errorf("inconsistent snapshot: %d items, %d tags", items, tags)
	}
	if items < initial || items > initial+int(during) {
		t.Errorf("copy has %d items, want %d..%d", items, initial, initial+int(during))
	}
	// The live database is unaffected.
	liveItems, liveTags := counts(t, d.DB.DB)
	if liveItems != initial+int(written.Load()) || liveTags != liveItems {
		t.Errorf("live db: %d items, %d tags; want %d", liveItems, liveTags, initial+written.Load())
	}
	for _, sfx := range []string{"-wal", "-shm", "-journal"} {
		if _, err := os.Stat(dest + sfx); err == nil {
			t.Errorf("%s left behind", dest+sfx)
		}
	}
	if left, _ := filepath.Glob(filepath.Join(filepath.Dir(dest), ".*.tmp*")); len(left) > 0 {
		t.Errorf("temp files left: %v", left)
	}

	// Never overwrite.
	before, _ := os.Stat(dest)
	if before.Mode().Perm() != 0o644 {
		t.Errorf("backup mode %v, want 0644 like the live database", before.Mode().Perm())
	}
	if err := ops.BackupCLI(ctx, d, []string{dest}, &out); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Errorf("second backup to the same path: %v, want already exists", err)
	}
	if after, _ := os.Stat(dest); !after.ModTime().Equal(before.ModTime()) || after.Size() != before.Size() {
		t.Errorf("existing backup was modified")
	}
}

func TestBackupDefaultPathAndPrune(t *testing.T) {
	ctx := context.Background()
	d := testutil.NewDeps(t)
	if err := insertItem(ctx, d.DB, "one"); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(d.Cfg.DataDir, "backups")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Older backups plus files that must never be pruned.
	old := []string{"inv-20250101T000000Z.db", "inv-20250102T000000Z.db", "inv-20250103T000000Z.db"}
	keepAlways := []string{"inv-manual.db", "notes.txt", "inv-20250101T000000Z.db.bak"}
	for _, n := range append(append([]string{}, old...), keepAlways...) {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	var out bytes.Buffer
	if err := ops.BackupCLI(ctx, d, []string{"--keep", "2"}, &out); err != nil {
		t.Fatal(err)
	}
	ents, _ := os.ReadDir(dir)
	var got []string
	for _, e := range ents {
		got = append(got, e.Name())
	}
	var fresh string
	for _, n := range got {
		if ok, _ := filepath.Match("inv-????????T??????Z.db", n); ok && n > old[2] {
			fresh = n
		}
	}
	if fresh == "" {
		t.Fatalf("no new timestamped backup in %v\n%s", got, out.String())
	}
	want := map[string]bool{fresh: true, old[2]: true}
	for _, n := range keepAlways {
		want[n] = true
	}
	if len(got) != len(want) {
		t.Errorf("after --keep 2: %v, want %v", got, want)
	}
	for _, n := range got {
		if !want[n] {
			t.Errorf("unexpected file %s", n)
		}
	}
	if !strings.Contains(out.String(), "pruned "+filepath.Join(dir, old[0])) || !strings.Contains(out.String(), "pruned "+filepath.Join(dir, old[1])) {
		t.Errorf("output does not list pruned files:\n%s", out.String())
	}

	// DEST may be a directory, and flags may follow it.
	other := t.TempDir()
	out.Reset()
	if err := ops.BackupCLI(ctx, d, []string{other, "--keep", "0"}, &out); err != nil {
		t.Fatal(err)
	}
	m, _ := filepath.Glob(filepath.Join(other, "inv-*Z.db"))
	if len(m) != 1 {
		t.Errorf("backup into a directory: %v\n%s", m, out.String())
	}
	c, err := sql.Open("sqlite", "file:"+m[0]+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if n, _ := counts(t, c); n != 1 {
		t.Errorf("copy has %d items, want 1", n)
	}

	if err := ops.BackupCLI(ctx, d, []string{"a.db", "b.db"}, &out); err == nil {
		t.Errorf("two destinations should fail")
	}
	if err := ops.BackupCLI(ctx, d, []string{"--keep", "-1"}, &out); err == nil {
		t.Errorf("negative --keep should fail")
	}
}
