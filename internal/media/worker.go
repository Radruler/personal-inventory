package media

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/radruler/personal-inventory/internal/httpapi"
)

// The variant worker turns pending media into thumb/display variants, one
// photo at a time (concurrency 1 caps memory). It scans for pending rows once
// at start and again whenever an upload wakes it; idle, it only waits on a
// channel, so it never reads or writes the disk.

var (
	wakeMu sync.Mutex
	wakers = map[*httpapi.Deps]chan struct{}{}
)

func waker(d *httpapi.Deps) chan struct{} {
	wakeMu.Lock()
	defer wakeMu.Unlock()
	ch := wakers[d]
	if ch == nil {
		ch = make(chan struct{}, 1)
		wakers[d] = ch
	}
	return ch
}

// wake tells d's worker there is pending work. It never blocks.
func wake(d *httpapi.Deps) {
	select {
	case waker(d) <- struct{}{}:
	default:
	}
}

// variantTimeout bounds one photo's processing.
const variantTimeout = 5 * time.Minute

// StartWorker runs the variant worker until ctx is done (it then returns
// nil). cmd/inv serve starts it in a goroutine.
func StartWorker(ctx context.Context, d *httpapi.Deps) error {
	ch := waker(d)
	defer func() {
		wakeMu.Lock()
		delete(wakers, d)
		wakeMu.Unlock()
	}()
	for {
		if err := processPending(ctx, d); err != nil && ctx.Err() == nil {
			d.Log.Error("media worker", "err", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ch:
		}
	}
}

// processPending handles pending rows oldest first until none are left.
func processPending(ctx context.Context, d *httpapi.Deps) error {
	for ctx.Err() == nil {
		var id, sha, ext string
		err := d.DB.QueryRowContext(ctx, `SELECT id, sha256, ext FROM media
			WHERE variant_status = 'pending' ORDER BY created_at, id LIMIT 1`).Scan(&id, &sha, &ext)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if err := buildVariants(ctx, d, id, sha, ext); err != nil {
			return err
		}
	}
	return nil
}

// buildVariants generates both variants for one media row and records the
// outcome. It returns an error only when the outcome could not be recorded
// (or ctx ended), so a bad photo never stalls the queue.
func buildVariants(ctx context.Context, d *httpapi.Deps, id, sha, ext string) error {
	src := OrigPath(d.Cfg, sha, ext)
	var genErr error
	if _, err := os.Stat(src); err != nil {
		genErr = fmt.Errorf("original missing: %w", err)
	} else {
		vctx, cancel := context.WithTimeout(ctx, variantTimeout)
		genErr = GenerateVariants(vctx, d.Cfg, src, sha)
		cancel()
	}
	if ctx.Err() != nil {
		return ctx.Err() // shutting down: leave it pending for next start
	}
	if genErr != nil {
		msg := genErr.Error()
		if len(msg) > 500 {
			msg = msg[:500]
		}
		d.Log.Warn("media variants failed", "id", id, "sha256", sha, "err", msg)
		_, err := d.DB.ExecContext(ctx, `UPDATE media SET variant_status = 'failed', variant_error = ? WHERE id = ?`, msg, id)
		return err
	}
	_, err := d.DB.ExecContext(ctx, `UPDATE media SET variant_status = 'ready', variant_error = NULL WHERE id = ?`, id)
	return err
}

// Rebuild regenerates the variants of every image (`inv media rebuild`),
// for example after the variant spec changes. Progress goes to logw. It
// returns an error if any image failed (each failure is also recorded on its
// row as variant_status failed).
func Rebuild(ctx context.Context, d *httpapi.Deps, logw io.Writer) error {
	rows, err := d.DB.QueryContext(ctx, `SELECT id, sha256, ext FROM media WHERE mime LIKE 'image/%' ORDER BY created_at, id`)
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
	failed := 0
	start := time.Now()
	for i, r := range all {
		if err := buildVariants(ctx, d, r.id, r.sha, r.ext); err != nil {
			return err
		}
		var status string
		var verr *string
		if err := d.DB.QueryRowContext(ctx, `SELECT variant_status, variant_error FROM media WHERE id = ?`, r.id).Scan(&status, &verr); err != nil {
			return err
		}
		if status != "ready" {
			failed++
			msg := ""
			if verr != nil {
				msg = *verr
			}
			fmt.Fprintf(logw, "[%d/%d] %s failed: %s\n", i+1, len(all), r.sha, msg)
		} else {
			fmt.Fprintf(logw, "[%d/%d] %s ok\n", i+1, len(all), r.sha)
		}
	}
	fmt.Fprintf(logw, "rebuilt %d image(s), %d failed, in %s\n", len(all)-failed, failed, time.Since(start).Round(time.Millisecond))
	if failed > 0 {
		return fmt.Errorf("%d of %d images failed", failed, len(all))
	}
	return nil
}
