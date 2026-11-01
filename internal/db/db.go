// Package db opens the SQLite database with the write-minimizing pragmas from
// PLAN.md §2, runs migrations, and provides small helpers (IDs, timestamps,
// transactions, error classification) shared by every domain package.
package db

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"
	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"

	"github.com/radruler/personal-inventory/migrations"
)

// DB wraps *sql.DB. All methods of *sql.DB are available.
type DB struct {
	*sql.DB
	Path string
}

// Querier is satisfied by *sql.DB, *sql.Tx and *DB, so read helpers can run
// inside or outside a transaction.
type Querier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// Open opens (creating if needed) the database at path and applies pending
// migrations. Opening an up-to-date database performs no writes.
func Open(ctx context.Context, path string) (*DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	q := url.Values{}
	for _, p := range []string{
		"busy_timeout(5000)",
		"journal_mode(WAL)",
		"synchronous(NORMAL)",
		"foreign_keys(1)",
		"temp_store(MEMORY)",
		"mmap_size(67108864)",
		"cache_size(-2000)",
	} {
		q.Add("_pragma", p)
	}
	// BEGIN IMMEDIATE for every transaction: writers queue on busy_timeout
	// instead of failing with SQLITE_BUSY on lock upgrade.
	q.Set("_txlock", "immediate")
	sqldb, err := sql.Open("sqlite", "file:"+path+"?"+q.Encode())
	if err != nil {
		return nil, err
	}
	// A small pool: one writer at a time (immediate tx), a few readers.
	// Connections are kept open; closing the last one checkpoints the WAL.
	sqldb.SetMaxOpenConns(4)
	sqldb.SetMaxIdleConns(4)
	d := &DB{DB: sqldb, Path: path}
	if err := d.Migrate(ctx); err != nil {
		sqldb.Close()
		return nil, err
	}
	return d, nil
}

// Migrate applies migrations/NNN_*.sql files newer than PRAGMA user_version.
func (d *DB) Migrate(ctx context.Context) error {
	var current int
	if err := d.QueryRowContext(ctx, "PRAGMA user_version").Scan(&current); err != nil {
		return fmt.Errorf("read user_version: %w", err)
	}
	files, err := fs.Glob(migrations.FS, "*.sql")
	if err != nil {
		return err
	}
	sort.Strings(files)
	for _, f := range files {
		n, err := strconv.Atoi(strings.SplitN(f, "_", 2)[0])
		if err != nil {
			return fmt.Errorf("migration %s: name must start with a number", f)
		}
		if n <= current {
			continue
		}
		body, err := migrations.FS.ReadFile(f)
		if err != nil {
			return err
		}
		err = d.Tx(ctx, func(tx *sql.Tx) error {
			if _, err := tx.ExecContext(ctx, string(body)); err != nil {
				return err
			}
			_, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", n))
			return err
		})
		if err != nil {
			return fmt.Errorf("migration %s: %w", f, err)
		}
		current = n
	}
	return nil
}

// Tx runs fn in a transaction, committing if it returns nil.
func (d *DB) Tx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

// SchemaVersion returns PRAGMA user_version.
func (d *DB) SchemaVersion(ctx context.Context) (int, error) {
	var v int
	err := d.QueryRowContext(ctx, "PRAGMA user_version").Scan(&v)
	return v, err
}

// TimeFormat is the canonical timestamp format stored in every *_at column.
const TimeFormat = "2006-01-02T15:04:05.000Z"

// Now returns the current UTC time formatted with TimeFormat.
func Now() string { return FormatTime(time.Now()) }

// FormatTime formats t (converted to UTC) with TimeFormat.
func FormatTime(t time.Time) string { return t.UTC().Format(TimeFormat) }

var (
	idMu      sync.Mutex
	idEntropy = ulid.Monotonic(rand.Reader, 0)
)

// NewID returns a new ULID string (26 chars, sortable by creation time).
func NewID() string {
	idMu.Lock()
	defer idMu.Unlock()
	return ulid.MustNew(ulid.Timestamp(time.Now()), idEntropy).String()
}

// IsUnique reports whether err is a UNIQUE or PRIMARY KEY constraint violation.
func IsUnique(err error) bool {
	var se *sqlite.Error
	if errors.As(err, &se) {
		c := se.Code()
		return c == sqlite3.SQLITE_CONSTRAINT_UNIQUE || c == sqlite3.SQLITE_CONSTRAINT_PRIMARYKEY
	}
	return false
}

// IsForeignKey reports whether err is a FOREIGN KEY constraint violation.
func IsForeignKey(err error) bool {
	var se *sqlite.Error
	if errors.As(err, &se) {
		return se.Code() == sqlite3.SQLITE_CONSTRAINT_FOREIGNKEY
	}
	return false
}

// IsCheck reports whether err is a CHECK constraint violation.
func IsCheck(err error) bool {
	var se *sqlite.Error
	if errors.As(err, &se) {
		return se.Code() == sqlite3.SQLITE_CONSTRAINT_CHECK
	}
	return false
}

// Placeholders returns "?,?,?" with n placeholders (n >= 1).
func Placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}

// Args converts a []string to []any for use with Placeholders.
func Args(ss []string) []any {
	out := make([]any, len(ss))
	for i, s := range ss {
		out[i] = s
	}
	return out
}
