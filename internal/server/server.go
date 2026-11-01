// Package server assembles the HTTP handler: middleware, /api/health, every
// domain package's routes and the embedded SPA. Route wiring lives only here.
package server

import (
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/radruler/personal-inventory/internal/httpapi"
	"github.com/radruler/personal-inventory/internal/ingest"
	"github.com/radruler/personal-inventory/internal/items"
	"github.com/radruler/personal-inventory/internal/locations"
	"github.com/radruler/personal-inventory/internal/media"
	"github.com/radruler/personal-inventory/internal/webui"
)

// New returns the root handler. It fills in d.ItemSummaries.
func New(d *httpapi.Deps) http.Handler {
	if d.ItemSummaries == nil {
		d.ItemSummaries = items.Summaries
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", d.Handler(health(d)))

	items.Register(mux, d)
	media.Register(mux, d)
	locations.Register(mux, d)
	ingest.Register(mux, d)

	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		httpapi.WriteError(w, r, nil, httpapi.NotFound("no route for %s %s", r.Method, r.URL.Path))
	})
	mux.Handle("/", webui.Handler())

	var h http.Handler = mux
	h = httpapi.Auth(d.Cfg.Token, h)
	if d.Cfg.AccessLog {
		h = httpapi.AccessLog(d.Log, h)
	}
	return httpapi.Recover(d.Log, h)
}

// Health is the /api/health response.
type Health struct {
	Status        string `json:"status"`
	Version       string `json:"version"`
	SchemaVersion int    `json:"schema_version"`
	Vips          string `json:"vips"`
	AuthRequired  bool   `json:"auth_required"`
	Time          string `json:"time"`
}

var (
	vipsOnce    sync.Once
	vipsVersion string
)

func health(d *httpapi.Deps) func(w http.ResponseWriter, r *http.Request) error {
	return func(w http.ResponseWriter, r *http.Request) error {
		v, err := d.DB.SchemaVersion(r.Context())
		if err != nil {
			return err
		}
		vipsOnce.Do(func() {
			out, err := exec.Command(d.Cfg.Bin("vips"), "--version").Output()
			if err != nil {
				vipsVersion = "unavailable"
				return
			}
			vipsVersion = strings.TrimSpace(strings.TrimPrefix(string(out), "vips-"))
		})
		httpapi.WriteJSON(w, http.StatusOK, Health{
			Status: "ok", Version: d.Version, SchemaVersion: v, Vips: vipsVersion,
			AuthRequired: d.Cfg.Token != "", Time: time.Now().UTC().Format(time.RFC3339),
		})
		return nil
	}
}
