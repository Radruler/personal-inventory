package server_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/radruler/personal-inventory/internal/server"
	"github.com/radruler/personal-inventory/internal/testutil"
)

func TestHealthAndSPA(t *testing.T) {
	d := testutil.NewDeps(t)
	h := server.New(d)

	var health server.Health
	rec := testutil.Do(t, h, "GET", "/api/health", nil, &health)
	if rec.Code != 200 || health.Status != "ok" || health.SchemaVersion < 1 {
		t.Fatalf("health: %d %+v", rec.Code, health)
	}
	if health.Vips == "" || health.Vips == "unavailable" {
		t.Errorf("vips not found: %q (run tests in the dev container)", health.Vips)
	}

	rec = testutil.Do(t, h, "GET", "/items/01ABC", nil, nil)
	if rec.Code != 200 || !strings.Contains(rec.Header().Get("Content-Type"), "text/html") {
		t.Errorf("SPA fallback: %d %s", rec.Code, rec.Header().Get("Content-Type"))
	}

	rec = testutil.Do(t, h, "GET", "/api/nope", nil, nil)
	if rec.Code != http.StatusNotFound || !strings.Contains(rec.Body.String(), `"not_found"`) {
		t.Errorf("unknown api route: %d %s", rec.Code, rec.Body.String())
	}
}

func TestAuthOnWrites(t *testing.T) {
	d := testutil.NewDeps(t)
	d.Cfg.Token = "s3cret"
	h := server.New(d)
	if rec := testutil.Do(t, h, "GET", "/api/health", nil, nil); rec.Code != 200 {
		t.Errorf("reads stay open: %d", rec.Code)
	}
	if rec := testutil.Do(t, h, "POST", "/api/items", map[string]any{}, nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("write without token: %d", rec.Code)
	}
}
