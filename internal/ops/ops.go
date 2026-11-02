// Package ops owns operational commands and budget checks: `inv backup`,
// memory and zero-write soak tests (thread H).
package ops

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite" // database/sql driver "sqlite" (also registered via internal/db)

	"github.com/radruler/personal-inventory/internal/httpapi"
)

// DefaultKeep is how many timestamped backups `inv backup` keeps in the
// destination directory unless --keep says otherwise.
const DefaultKeep = 7

// backupName matches the file names BackupCLI generates; only these are
// ever pruned, so hand-named copies in the same directory are left alone.
var backupName = regexp.MustCompile(`^inv-\d{8}T\d{6}Z\.db$`)

// BackupName is the default file name for a backup taken at t.
func BackupName(t time.Time) string {
	return "inv-" + t.UTC().Format("20060102T150405Z") + ".db"
}

// BackupCLI implements `inv backup [--keep N] [DEST]`: a consistent copy of
// the live database via VACUUM INTO. It is safe while `inv serve` runs (WAL
// mode: the copy is one read transaction; it never blocks the server's
// writers and never writes to the source).
//
// DEST defaults to $INV_DATA_DIR/backups/inv-<UTC timestamp>.db. If DEST is
// an existing directory (or ends in "/"), the timestamped name is used
// inside it. An existing file is never overwritten. After the copy, older
// timestamped backups in the same directory beyond the newest N are deleted
// (--keep 0 disables pruning).
func BackupCLI(ctx context.Context, d *httpapi.Deps, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("backup", flag.ContinueOnError)
	fs.SetOutput(out)
	keep := fs.Int("keep", DefaultKeep, "keep the newest `N` inv-*.db backups in the destination directory (0 = keep all)")
	fs.Usage = func() {
		fmt.Fprintln(out, "usage: inv backup [--keep N] [DEST]")
		fs.PrintDefaults()
	}
	// Accept flags before or after DEST.
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return nil
			}
			return err
		}
		if fs.NArg() == 0 {
			break
		}
		pos = append(pos, fs.Arg(0))
		args = fs.Args()[1:]
	}
	if len(pos) > 1 || *keep < 0 {
		fs.Usage()
		return errors.New("backup: bad arguments")
	}

	now := time.Now()
	dest := filepath.Join(d.Cfg.DataDir, "backups", BackupName(now))
	if len(pos) == 1 {
		dest = pos[0]
		if st, err := os.Stat(dest); (err == nil && st.IsDir()) || strings.HasSuffix(dest, string(os.PathSeparator)) {
			dest = filepath.Join(dest, BackupName(now))
		}
	}
	res, err := Backup(ctx, d.DB.DB, dest)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "%s\t%d bytes\n", res.Path, res.Bytes)
	if *keep > 0 {
		removed, err := Prune(filepath.Dir(res.Path), *keep)
		for _, p := range removed {
			fmt.Fprintf(out, "pruned %s\n", p)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// BackupResult describes a finished backup.
type BackupResult struct {
	Path  string
	Bytes int64
}

// Backup writes a consistent, integrity-checked copy of the database behind
// src to dest, which must not exist. The copy is written under a hidden
// temporary name in dest's directory and linked into place only once it is
// complete, so dest never names a partial file. The copy uses the rollback
// journal (not WAL), so it is a single self-contained file.
func Backup(ctx context.Context, src *sql.DB, dest string) (BackupResult, error) {
	dest, err := filepath.Abs(dest)
	if err != nil {
		return BackupResult{}, err
	}
	if _, err := os.Lstat(dest); err == nil {
		return BackupResult{}, fmt.Errorf("backup: %s already exists", dest)
	} else if !errors.Is(err, os.ErrNotExist) {
		return BackupResult{}, err
	}
	dir := filepath.Dir(dest)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return BackupResult{}, err
	}

	// Reserve a unique, empty temp file (VACUUM INTO accepts an empty file).
	f, err := os.CreateTemp(dir, "."+filepath.Base(dest)+".*.tmp")
	if err != nil {
		return BackupResult{}, err
	}
	tmp := f.Name()
	f.Close()
	// Same mode SQLite gives the live database (CreateTemp uses 0600).
	if err := os.Chmod(tmp, 0o644); err != nil {
		return BackupResult{}, err
	}
	defer os.Remove(tmp)
	for _, sfx := range []string{"-journal", "-wal", "-shm"} {
		defer os.Remove(tmp + sfx)
	}

	if _, err := src.ExecContext(ctx, "VACUUM INTO ?", tmp); err != nil {
		return BackupResult{}, fmt.Errorf("backup: vacuum into: %w", err)
	}
	if err := finishCopy(ctx, tmp); err != nil {
		return BackupResult{}, err
	}

	// Link (not rename) so an existing dest is never replaced.
	if err := os.Link(tmp, dest); err != nil {
		if errors.Is(err, os.ErrExist) {
			return BackupResult{}, fmt.Errorf("backup: %s already exists", dest)
		}
		// Filesystems without hard links: rename after a fresh check.
		if _, serr := os.Lstat(dest); serr == nil {
			return BackupResult{}, fmt.Errorf("backup: %s already exists", dest)
		}
		if err := os.Rename(tmp, dest); err != nil {
			return BackupResult{}, err
		}
	}
	st, err := os.Stat(dest)
	if err != nil {
		return BackupResult{}, err
	}
	return BackupResult{Path: dest, Bytes: st.Size()}, nil
}

// finishCopy switches the copy to the rollback journal, runs
// integrity_check and fsyncs it.
func finishCopy(ctx context.Context, path string) error {
	c, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(5000)")
	if err != nil {
		return err
	}
	defer c.Close()
	c.SetMaxOpenConns(1)
	var mode string
	if err := c.QueryRowContext(ctx, "PRAGMA journal_mode=DELETE").Scan(&mode); err != nil {
		return fmt.Errorf("backup: journal_mode: %w", err)
	}
	rows, err := c.QueryContext(ctx, "PRAGMA integrity_check")
	if err != nil {
		return fmt.Errorf("backup: integrity_check: %w", err)
	}
	var msgs []string
	for rows.Next() {
		var m string
		if err := rows.Scan(&m); err != nil {
			rows.Close()
			return err
		}
		msgs = append(msgs, m)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	if len(msgs) != 1 || msgs[0] != "ok" {
		return fmt.Errorf("backup: integrity_check failed: %s", strings.Join(msgs, "; "))
	}
	if err := c.Close(); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}

// Prune deletes all but the newest keep timestamped backups (inv-*.db) in
// dir and returns the removed paths. Names sort chronologically.
func Prune(dir string, keep int) ([]string, error) {
	if keep <= 0 {
		return nil, nil
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range ents {
		if e.Type().IsRegular() && backupName.MatchString(e.Name()) {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	if len(names) <= keep {
		return nil, nil
	}
	var removed []string
	for _, n := range names[:len(names)-keep] {
		p := filepath.Join(dir, n)
		if err := os.Remove(p); err != nil {
			return removed, err
		}
		removed = append(removed, p)
	}
	return removed, nil
}
