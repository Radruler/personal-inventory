package media

import (
	"context"

	"github.com/radruler/personal-inventory/internal/httpapi"
)

// StartWorker starts the variant worker (concurrency 1) that turns pending
// media into thumb/display variants. It returns when ctx is done or the
// worker fails to start. Thread B implements it; the stub does nothing.
func StartWorker(ctx context.Context, d *httpapi.Deps) error { return nil }
