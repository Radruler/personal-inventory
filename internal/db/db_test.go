package db_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/radruler/personal-inventory/internal/db"
)

func TestMigrateAndReopenWritesNothing(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "inv.db")
	d, err := db.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	v, err := d.SchemaVersion(ctx)
	if err != nil || v < 1 {
		t.Fatalf("schema version = %d, %v", v, err)
	}
	var mode string
	if err := d.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&mode); err != nil || mode != "wal" {
		t.Fatalf("journal_mode = %q, %v", mode, err)
	}
	var n int
	if err := d.QueryRowContext(ctx, "SELECT count(*) FROM fact_key WHERE filterable = 1").Scan(&n); err != nil || n == 0 {
		t.Fatalf("seeded fact_key filterable = %d, %v", n, err)
	}
	if _, err := d.ExecContext(ctx, `INSERT INTO item_fts (item_id, title) VALUES ('x', 'navy shell jacket')`); err != nil {
		t.Fatalf("fts5 insert: %v", err)
	}
	if err := d.QueryRowContext(ctx, `SELECT count(*) FROM item_fts WHERE item_fts MATCH 'jack*'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("fts5 match = %d, %v", n, err)
	}
	d.Close() // checkpoints and removes the WAL

	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	d, err = db.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	// Reads on an up-to-date database must not touch the main file.
	if err := d.QueryRowContext(ctx, "SELECT count(*) FROM item").Scan(&n); err != nil {
		t.Fatal(err)
	}
	st2, _ := os.Stat(path)
	if !st2.ModTime().Equal(st.ModTime()) || st2.Size() != st.Size() {
		t.Fatalf("reopen modified the database file")
	}
	if wal, err := os.Stat(path + "-wal"); err == nil && wal.Size() > 0 {
		t.Fatalf("reopen wrote %d bytes to the WAL", wal.Size())
	}
	d.Close()
}

func TestIDsAreSortable(t *testing.T) {
	a, b := db.NewID(), db.NewID()
	if len(a) != 26 || a >= b {
		t.Fatalf("ids not monotonic: %s %s", a, b)
	}
}
