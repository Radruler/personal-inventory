// Command inv is the Personal Inventory server and CLI.
//
//	inv serve                 run the HTTP server (default)
//	inv seed --demo           fill an empty database with demo content
//	inv ingest bundle.json    upload missing photos and apply an ingest bundle
//	inv export DIR | import DIR
//	inv media rebuild         regenerate image variants
//	inv backup [DEST]         VACUUM INTO a consistent copy of the database
//	inv version
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"
	"time"

	"github.com/radruler/personal-inventory/internal/config"
	"github.com/radruler/personal-inventory/internal/db"
	"github.com/radruler/personal-inventory/internal/httpapi"
	"github.com/radruler/personal-inventory/internal/media"
	"github.com/radruler/personal-inventory/internal/seed"
	"github.com/radruler/personal-inventory/internal/server"
)

// version is set at build time with -ldflags "-X main.version=…".
var version = "dev"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "inv:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	cmd := "serve"
	if len(args) > 0 {
		cmd, args = args[0], args[1:]
	}
	switch cmd {
	case "serve":
		return serve()
	case "seed":
		return withDeps(func(ctx context.Context, d *httpapi.Deps) error { return seed.Run(ctx, d, args) })
	case "version", "--version", "-v":
		fmt.Println(version)
		return nil
	case "ingest", "export", "import", "backup":
		return fmt.Errorf("%s: not implemented yet", cmd)
	case "media":
		return fmt.Errorf("media %v: not implemented yet", args)
	case "help", "-h", "--help":
		fmt.Println(usage)
		return nil
	}
	return fmt.Errorf("unknown command %q\n%s", cmd, usage)
}

const usage = `usage: inv [serve | seed --demo | ingest BUNDLE | export DIR | import DIR | media rebuild | backup [DEST] | version]

Configuration comes from INV_* environment variables (see CLAUDE.md).`

func newLogger(cfg config.Config) *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))
}

// withDeps opens the database for one-shot CLI commands.
func withDeps(fn func(context.Context, *httpapi.Deps) error) error {
	cfg, err := config.FromEnv()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	d, err := db.Open(ctx, cfg.DBPath)
	if err != nil {
		return err
	}
	defer d.Close()
	return fn(ctx, &httpapi.Deps{DB: d, Cfg: cfg, Log: newLogger(cfg), Version: version})
}

func serve() error {
	cfg, err := config.FromEnv()
	if err != nil {
		return err
	}
	log := newLogger(cfg)
	if os.Getenv("GOMEMLIMIT") == "" {
		debug.SetMemoryLimit(64 << 20)
	}
	for _, dir := range []string{cfg.DataDir, cfg.MediaDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	d, err := db.Open(ctx, cfg.DBPath)
	if err != nil {
		return err
	}
	defer d.Close()

	deps := &httpapi.Deps{DB: d, Cfg: cfg, Log: log, Version: version}
	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           server.New(deps),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	workerErr := make(chan error, 1)
	go func() { workerErr <- media.StartWorker(ctx, deps) }()

	srvErr := make(chan error, 1)
	go func() {
		log.Info("listening", "addr", cfg.Addr, "version", version, "data", cfg.DataDir, "media", cfg.MediaDir, "auth", cfg.Token != "")
		srvErr <- srv.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
	case err := <-srvErr:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	}
	log.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err = srv.Shutdown(shutdownCtx)
	select {
	case werr := <-workerErr:
		if werr != nil && !errors.Is(werr, context.Canceled) {
			log.Error("media worker", "err", werr)
		}
	case <-time.After(5 * time.Second):
	}
	return err
}
