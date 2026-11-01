package media

import (
	"bytes"
	"context"
	"encoding/json"
	"image/color"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/radruler/personal-inventory/internal/testutil"
)

func TestVariantsAndServing(t *testing.T) {
	d, h := newEnv(t)
	heic := readFixture(t, "IMG_0001.HEIC")
	up := upload(t, h, 201, fileField("IMG_0001.HEIC", heic))
	process(t, d)
	m := getMedia(t, h, up.ID)
	if m.VariantStatus != "ready" || m.ThumbURL == nil || *m.ThumbURL != "/media/"+up.SHA256+"/thumb" ||
		m.DisplayURL == nil || m.VariantError != nil {
		t.Fatalf("after processing: %+v", m)
	}

	// Variants are auto-rotated (coded 320x240, irot 90° cw → 240x320; the
	// red marker in the coded top-left ends up top-right), never upscaled,
	// and carry no metadata (so no GPS).
	for _, p := range []string{DisplayPath(d.Cfg, up.SHA256), ThumbPath(d.Cfg, up.SHA256)} {
		img := decodeVia(t, d.Cfg, p)
		if sz := img.Bounds().Size(); sz.X != 240 || sz.Y != 320 {
			t.Errorf("%s is %v, want 240x320", p, sz)
		}
		if r, g, b := rgbAt(img, 215, 25); r < 180 || g > 80 || b > 80 {
			t.Errorf("%s: top-right is %d,%d,%d, want the red marker", p, r, g, b)
		}
		if r, _, _ := rgbAt(img, 25, 25); r > 150 {
			t.Errorf("%s: top-left is red: rotation wrong", p)
		}
		hdr, err := exec.Command(d.Cfg.Bin("vipsheader"), "-a", p).CombinedOutput()
		if err != nil {
			t.Fatal(err)
		}
		if s := string(hdr); strings.Contains(s, "exif") || strings.Contains(s, "GPS") || strings.Contains(s, "icc-profile") {
			t.Errorf("%s keeps metadata:\n%s", p, s)
		}
	}
	if !strings.HasPrefix(ThumbPath(d.Cfg, up.SHA256), d.Cfg.DataDir) || !strings.HasPrefix(DisplayPath(d.Cfg, up.SHA256), d.Cfg.MediaDir) {
		t.Error("thumb must live on /data, display on /media")
	}

	// Serving, and GETs write nothing.
	before := testutil.Mtimes(t, d.Cfg.DataDir, d.Cfg.MediaDir)
	for _, v := range []string{"thumb", "display", "orig"} {
		path := "/media/" + up.SHA256 + "/" + v
		rec := get(t, h, "GET", path, nil)
		if rec.Code != 200 {
			t.Fatalf("%s: %d %s", path, rec.Code, rec.Body.String())
		}
		hd := rec.Header()
		wantType := "image/webp"
		if v == "orig" {
			wantType = "image/heic"
		}
		if hd.Get("Content-Type") != wantType || hd.Get("Cache-Control") != "public, max-age=31536000, immutable" {
			t.Errorf("%s: type %q cache %q", v, hd.Get("Content-Type"), hd.Get("Cache-Control"))
		}
		etag := hd.Get("ETag")
		if etag != `"`+up.SHA256+"-"+v+`"` {
			t.Errorf("%s: etag %q", v, etag)
		}
		if v == "orig" {
			if !bytes.Equal(rec.Body.Bytes(), heic) {
				t.Error("orig body differs from the upload")
			}
			if cd := hd.Get("Content-Disposition"); cd != `inline; filename=IMG_0001.HEIC` && cd != `inline; filename="IMG_0001.HEIC"` {
				t.Errorf("content-disposition %q", cd)
			}
		}
		if nm := get(t, h, "GET", path, map[string]string{"If-None-Match": etag}); nm.Code != 304 || nm.Body.Len() != 0 {
			t.Errorf("%s If-None-Match: %d", v, nm.Code)
		}
		if hr := get(t, h, "HEAD", path, nil); hr.Code != 200 {
			t.Errorf("%s HEAD: %d", v, hr.Code)
		}
		if rr := get(t, h, "GET", path, map[string]string{"Range": "bytes=0-9"}); rr.Code != 206 || rr.Body.Len() != 10 {
			t.Errorf("%s Range: %d %d bytes", v, rr.Code, rr.Body.Len())
		}
	}
	for _, p := range []string{
		"/media/" + up.SHA256 + "/huge", "/media/" + strings.Repeat("0", 64) + "/thumb",
		"/media/" + strings.Repeat("0", 64) + "/orig", "/media/xyz/thumb",
	} {
		if rec := get(t, h, "GET", p, nil); rec.Code != 404 {
			t.Errorf("%s: %d, want 404", p, rec.Code)
		}
	}
	for _, p := range []string{"/api/media/" + up.ID, "/api/media/sha256/" + up.SHA256, "/api/inbox", "/api/inbox?limit=1"} {
		if rec := get(t, h, "GET", p, nil); rec.Code != 200 {
			t.Errorf("%s: %d", p, rec.Code)
		}
	}
	get(t, h, "HEAD", "/api/media/sha256/"+up.SHA256, nil)
	after := testutil.Mtimes(t, d.Cfg.DataDir, d.Cfg.MediaDir)
	if k, ok := equalSnap(before, after); !ok {
		t.Errorf("GET changed %s", k)
	}
}

func TestServeDocuments(t *testing.T) {
	_, h := newEnv(t)
	page := []byte("<!DOCTYPE html><html><body><script>alert(1)</script>Archived</body></html>")
	up := upload(t, h, 201, fileField("Product page.html", page))
	if up.Mime != "text/html" || up.VariantStatus != "none" || up.Width != nil || up.ThumbURL != nil {
		t.Errorf("html upload: %+v", up)
	}
	rec := get(t, h, "GET", up.OrigURL, nil)
	if rec.Code != 200 || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("html orig: %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	if csp := rec.Header().Get("Content-Security-Policy"); !strings.HasPrefix(csp, "sandbox") || !strings.Contains(csp, "default-src 'none'") {
		t.Errorf("csp %q", csp)
	}
	if cd := rec.Header().Get("Content-Disposition"); !strings.Contains(cd, "Product page.html") {
		t.Errorf("content-disposition %q", cd)
	}
	if rec := get(t, h, "GET", "/media/"+up.SHA256+"/thumb", nil); rec.Code != 404 {
		t.Errorf("document thumb: %d", rec.Code)
	}
	pdf := upload(t, h, 201, fileField("receipt.pdf", []byte("%PDF-1.4\n1 0 obj<<>>endobj\n%%EOF")))
	if pdf.Mime != "application/pdf" || pdf.Ext != "pdf" || pdf.VariantStatus != "none" {
		t.Errorf("pdf: %+v", pdf)
	}
	if rec := get(t, h, "GET", pdf.OrigURL, nil); rec.Header().Get("Content-Type") != "application/pdf" || rec.Header().Get("Content-Security-Policy") != "" {
		t.Errorf("pdf headers %v", rec.Header())
	}
}

func TestWorkerProcessesPendingOnStart(t *testing.T) {
	d, h := newEnv(t)
	good := upload(t, h, 201, fileField("a.jpg", jpegBytes(t, gradient(800, 600, 5))))
	bad := upload(t, h, 201, fileField("broken.jpg", append([]byte{0xff, 0xd8, 0xff, 0xe0}, bytes.Repeat([]byte("junk"), 100)...)))
	if good.VariantStatus != "pending" || bad.VariantStatus != "pending" {
		t.Fatal("uploads should start pending")
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- StartWorker(ctx, d) }()

	waitStatus := func(id string) Media {
		deadline := time.Now().Add(30 * time.Second)
		for time.Now().Before(deadline) {
			m := getMedia(t, h, id)
			if m.VariantStatus != "pending" {
				return m
			}
			time.Sleep(50 * time.Millisecond)
		}
		t.Fatalf("media %s still pending", id)
		return Media{}
	}
	if m := waitStatus(good.ID); m.VariantStatus != "ready" {
		t.Errorf("good: %s %v", m.VariantStatus, m.VariantError)
	}
	if m := waitStatus(bad.ID); m.VariantStatus != "failed" || m.VariantError == nil || *m.VariantError == "" {
		t.Errorf("bad: %s %v", m.VariantStatus, m.VariantError)
	}
	// While running, a new upload wakes the worker.
	later := upload(t, h, 201, fileField("b.png", pngBytes(t, gradient(500, 300, 8))))
	if m := waitStatus(later.ID); m.VariantStatus != "ready" {
		t.Errorf("later: %s", m.VariantStatus)
	}
	// Idle: the worker writes nothing.
	before := testutil.Mtimes(t, d.Cfg.DataDir, d.Cfg.MediaDir)
	time.Sleep(200 * time.Millisecond)
	if k, ok := equalSnap(before, testutil.Mtimes(t, d.Cfg.DataDir, d.Cfg.MediaDir)); !ok {
		t.Errorf("idle worker changed %s", k)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("StartWorker returned %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not stop")
	}
	// Display (800x600 fits) and thumb (400x300).
	img := decodeVia(t, d.Cfg, ThumbPath(d.Cfg, good.SHA256))
	if sz := img.Bounds().Size(); sz.X != 400 || sz.Y != 300 {
		t.Errorf("thumb %v", sz)
	}
}

func TestRebuild(t *testing.T) {
	d, h := newEnv(t)
	up := upload(t, h, 201, fileField("a.png", pngBytes(t, gradient(64, 48, 2))))
	upload(t, h, 201, fileField("doc.pdf", []byte("%PDF-1.4\n%%EOF")))
	process(t, d)
	os.Remove(ThumbPath(d.Cfg, up.SHA256))
	os.Remove(DisplayPath(d.Cfg, up.SHA256))
	var log bytes.Buffer
	if err := Rebuild(context.Background(), d, &log); err != nil {
		t.Fatalf("rebuild: %v\n%s", err, log.String())
	}
	if !strings.Contains(log.String(), "rebuilt 1 image(s), 0 failed") {
		t.Errorf("log: %s", log.String())
	}
	for _, p := range []string{ThumbPath(d.Cfg, up.SHA256), DisplayPath(d.Cfg, up.SHA256)} {
		if _, err := os.Stat(p); err != nil {
			t.Error(err)
		}
	}
	// No temp files or work dirs left behind.
	for _, dir := range []string{filepath.Dir(ThumbPath(d.Cfg, up.SHA256)), filepath.Dir(DisplayPath(d.Cfg, up.SHA256))} {
		ents, _ := os.ReadDir(dir)
		for _, e := range ents {
			if strings.Contains(e.Name(), ".tmp") || strings.HasPrefix(e.Name(), ".work") {
				t.Errorf("left over: %s", e.Name())
			}
		}
	}
	// A missing original fails that row and the rebuild.
	os.Remove(OrigPath(d.Cfg, up.SHA256, "png"))
	log.Reset()
	if err := Rebuild(context.Background(), d, &log); err == nil {
		t.Error("rebuild with a missing original should fail")
	}
	if m := getMedia(t, h, up.ID); m.VariantStatus != "failed" || m.VariantError == nil || !strings.Contains(*m.VariantError, "original missing") {
		t.Errorf("after failed rebuild: %s %v", m.VariantStatus, m.VariantError)
	}
}

func TestHEIFParseThirdPartyGrid(t *testing.T) {
	// grid.heic was written by libheif's heif-enc, not by our writer.
	b := readFixture(t, "grid.heic")
	f, err := parseHEIF(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatal(err)
	}
	g, ok, err := f.grid()
	if err != nil || !ok {
		t.Fatalf("grid: %v %v", ok, err)
	}
	if g.rows != 2 || g.cols != 3 || g.width != 700 || g.height != 480 || g.tileW != 256 || g.tileH != 256 {
		t.Errorf("grid %+v", g)
	}
	if len(g.icc) == 0 || len(g.transforms) != 0 {
		t.Errorf("icc %d bytes, transforms %v", len(g.icc), g.transforms)
	}
	// Single images are not grids.
	b = readFixture(t, "IMG_0001.HEIC")
	f, err = parseHEIF(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := f.grid(); ok {
		t.Error("IMG_0001.HEIC is not a grid")
	}
	if _, err := parseHEIF(bytes.NewReader([]byte("not a heif file at all")), 22); err == nil {
		t.Error("garbage parsed")
	}
}

// The banded decoder must give the same picture as decoding the grid whole.
func TestBandedDisplayMatchesWhole(t *testing.T) {
	d, _ := newEnv(t)
	dir := t.TempDir()

	// A grid with irot 90° cw (as on an iPhone portrait photo), built with
	// our writer from vips-encoded tiles; 7x2 tiles of 512 so the long edge
	// (3500) allows a 2x box shrink.
	var tiles []gridTile
	for i := 0; i < 5; i++ {
		tiles = append(tiles, encodeTile(t, d.Cfg, gradient(512, 512, uint8(40*i+10))))
	}
	rotated := filepath.Join(dir, "rotated.heic")
	writeGridFixture(t, rotated, 2, 7, 3500, 1000, tiles, irotBox(3))

	for _, c := range []struct {
		name, src     string
		wantW, wantH  int
		wantFactor    int
		bandsAtBudget int
	}{
		{"heif-enc grid with P3 ICC", filepath.Join("testdata", "grid.heic"), 700, 480, 1, 3 * 256 * 256},
		{"our grid, irot, 2x shrink", rotated, 457, 1600, 2, 7 * 512 * 512},
	} {
		t.Run(c.name, func(t *testing.T) {
			f, g, closeFn := openGrid(c.src)
			defer closeFn()
			if g == nil {
				t.Fatal("not a splittable grid")
			}
			if sf := shrinkFactor(g); sf != c.wantFactor {
				t.Errorf("shrink factor %d, want %d", sf, c.wantFactor)
			}
			whole := filepath.Join(dir, "whole.webp")
			if err := runVips(context.Background(), d.Cfg, "vipsthumbnail", c.src, "--size", displayBox(),
				"--export-profile", "srgb", "-o", displaySave(whole)); err != nil {
				t.Fatal(err)
			}
			old := bandPixelBudget
			bandPixelBudget = c.bandsAtBudget // one tile row per band
			defer func() { bandPixelBudget = old }()
			banded := filepath.Join(dir, "banded.webp")
			if err := displayBanded(context.Background(), d.Cfg, f, g, banded); err != nil {
				t.Fatal(err)
			}
			a, b := decodeVia(t, d.Cfg, whole), decodeVia(t, d.Cfg, banded)
			if sz := b.Bounds().Size(); sz.X != c.wantW || sz.Y != c.wantH {
				t.Errorf("banded size %v, want %dx%d", sz, c.wantW, c.wantH)
			}
			if diff := meanAbsDiff(t, a, b); diff > 3 {
				t.Errorf("banded differs from whole decode: mean abs diff %.2f", diff)
			} else {
				t.Logf("mean abs diff %.2f", diff)
			}
		})
	}
}

func getMedia(t testing.TB, h http.Handler, id string) Media {
	t.Helper()
	var m Media
	rec := testutil.Do(t, h, "GET", "/api/media/"+id, nil, &m)
	if rec.Code != 200 {
		t.Fatalf("GET media %s: %d %s", id, rec.Code, rec.Body.String())
	}
	return m
}

// withOrientation inserts a minimal EXIF APP1 segment (IFD0 with only
// Orientation) after the JPEG SOI marker.
func withOrientation(jpg []byte, o byte) []byte {
	tiff := []byte("MM\x00\x2a\x00\x00\x00\x08" + // big-endian TIFF header, IFD0 at 8
		"\x00\x01" + // one entry
		"\x01\x12\x00\x03\x00\x00\x00\x01\x00" + string([]byte{o}) + "\x00\x00" + // Orientation SHORT
		"\x00\x00\x00\x00") // no next IFD
	payload := append([]byte("Exif\x00\x00"), tiff...)
	seg := append([]byte{0xff, 0xe1, byte((len(payload) + 2) >> 8), byte(len(payload) + 2)}, payload...)
	return append(append([]byte{0xff, 0xd8}, seg...), jpg[2:]...)
}

func TestJPEGOrientation(t *testing.T) {
	d, h := newEnv(t)
	img := gradient(64, 32, 0)
	for y := 0; y < 32; y++ {
		for x := 0; x < 16; x++ {
			img.Set(x, y, color.RGBA{230, 20, 20, 255}) // red left edge
		}
	}
	up := upload(t, h, 201, fileField("rot.jpg", withOrientation(jpegBytes(t, img), 6)))
	if deref(up.Width) != 32 || deref(up.Height) != 64 {
		t.Errorf("dims %v x %v, want 32 x 64 after orientation 6", deref(up.Width), deref(up.Height))
	}
	var exif map[string]any
	json.Unmarshal(up.Exif, &exif)
	if exif["Orientation"] != 6.0 {
		t.Errorf("exif %s", up.Exif)
	}
	process(t, d)
	disp := decodeVia(t, d.Cfg, DisplayPath(d.Cfg, up.SHA256))
	if sz := disp.Bounds().Size(); sz.X != 32 || sz.Y != 64 {
		t.Fatalf("display %v", sz)
	}
	// Rotating 90° clockwise puts the left edge on top.
	if r, g, _ := rgbAt(disp, 16, 4); r < 180 || g > 80 {
		t.Errorf("top is %d,%d: not rotated", r, g)
	}
}
