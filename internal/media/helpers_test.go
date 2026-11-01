package media

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/radruler/personal-inventory/internal/config"
	"github.com/radruler/personal-inventory/internal/httpapi"
	"github.com/radruler/personal-inventory/internal/testutil"
)

func newEnv(t testing.TB) (*httpapi.Deps, http.Handler) {
	t.Helper()
	d := testutil.NewDeps(t)
	mux := http.NewServeMux()
	Register(mux, d)
	return d, mux
}

type part struct {
	name, filename string
	data           []byte
}

func fileField(filename string, data []byte) part { return part{"file", filename, data} }
func textField(name, value string) part           { return part{name, "", []byte(value)} }

func postMultipart(t testing.TB, h http.Handler, parts ...part) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for _, p := range parts {
		var w io.Writer
		var err error
		if p.filename != "" {
			w, err = mw.CreateFormFile(p.name, p.filename)
		} else {
			w, err = mw.CreateFormField(p.name)
		}
		if err != nil {
			t.Fatal(err)
		}
		w.Write(p.data)
	}
	mw.Close()
	req := httptest.NewRequest("POST", "/api/media", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// upload posts parts and decodes a 200/201 result.
func upload(t testing.TB, h http.Handler, want int, parts ...part) UploadResult {
	t.Helper()
	rec := postMultipart(t, h, parts...)
	if rec.Code != want {
		t.Fatalf("upload: status %d, want %d: %s", rec.Code, want, rec.Body.String())
	}
	var out UploadResult
	if want < 300 {
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode: %v: %s", err, rec.Body.String())
		}
	}
	return out
}

func get(t testing.TB, h http.Handler, method, path string, hdr map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func readFixture(t testing.TB, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func shaHex(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

func gradient(w, h int, seed uint8) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{uint8(x*255/w) ^ seed, uint8(y * 255 / h), seed, 255})
		}
	}
	return img
}

func pngBytes(t testing.TB, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func jpegBytes(t testing.TB, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 85}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// decodeVia converts any image vips can read to PNG and decodes it.
func decodeVia(t testing.TB, cfg config.Config, path string) image.Image {
	t.Helper()
	out := filepath.Join(t.TempDir(), "decoded.png")
	if b, err := exec.Command(cfg.Bin("vips"), "copy", path, out).CombinedOutput(); err != nil {
		t.Fatalf("vips copy %s: %v: %s", path, err, b)
	}
	f, err := os.Open(out)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	return img
}

func rgbAt(img image.Image, x, y int) (int, int, int) {
	r, g, b, _ := img.At(x, y).RGBA()
	return int(r >> 8), int(g >> 8), int(b >> 8)
}

// meanAbsDiff compares two same-sized images channel by channel.
func meanAbsDiff(t testing.TB, a, b image.Image) float64 {
	t.Helper()
	if a.Bounds().Size() != b.Bounds().Size() {
		t.Fatalf("sizes differ: %v vs %v", a.Bounds().Size(), b.Bounds().Size())
	}
	var sum, n float64
	ab, bb := a.Bounds(), b.Bounds()
	for y := 0; y < ab.Dy(); y++ {
		for x := 0; x < ab.Dx(); x++ {
			r1, g1, b1 := rgbAt(a, ab.Min.X+x, ab.Min.Y+y)
			r2, g2, b2 := rgbAt(b, bb.Min.X+x, bb.Min.Y+y)
			sum += abs(r1-r2) + abs(g1-g2) + abs(b1-b2)
			n += 3
		}
	}
	return sum / n
}

func abs(v int) float64 {
	if v < 0 {
		return float64(-v)
	}
	return float64(v)
}

// encodeTile encodes img as a single-image HEIC with vips and returns the
// HEVC tile (its properties and bitstream) for writeGridHEIF.
func encodeTile(t testing.TB, cfg config.Config, img image.Image) gridTile {
	t.Helper()
	dir := t.TempDir()
	in, out := filepath.Join(dir, "tile.png"), filepath.Join(dir, "tile.heic")
	if err := os.WriteFile(in, pngBytes(t, img), 0o644); err != nil {
		t.Fatal(err)
	}
	if b, err := exec.Command(cfg.Bin("vips"), "copy", in, out+"[Q=50]").CombinedOutput(); err != nil {
		t.Fatalf("vips heifsave: %v: %s", err, b)
	}
	f, err := os.Open(out)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	st, _ := f.Stat()
	hf, err := parseHEIF(f, st.Size())
	if err != nil {
		t.Fatal(err)
	}
	p := hf.items[hf.primary]
	if p.typ != "hvc1" {
		t.Fatalf("vips wrote a %q primary item", p.typ)
	}
	data, err := hf.itemData(p)
	if err != nil {
		t.Fatal(err)
	}
	return gridTile{props: hf.tileProps(p), data: data}
}

// writeGridFixture writes a rows x cols grid HEIC built from tiles (reused
// cyclically) with optional extra grid properties such as irot.
func writeGridFixture(t testing.TB, path string, rows, cols, width, height int, tiles []gridTile, props ...[]byte) {
	t.Helper()
	g := gridSpec{rows: rows, cols: cols, width: width, height: height, props: props}
	for i := 0; i < rows*cols; i++ {
		g.tiles = append(g.tiles, tiles[i%len(tiles)])
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeGridHEIF(f, g); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func irotBox(angle byte) []byte { return mkbox("irot", []byte{angle & 3}) }

func process(t testing.TB, d *httpapi.Deps) {
	t.Helper()
	if err := processPending(context.Background(), d); err != nil {
		t.Fatal(err)
	}
}

func exec1(t testing.TB, d *httpapi.Deps, q string, args ...any) {
	t.Helper()
	if _, err := d.DB.Exec(q, args...); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
}

func equalSnap(a, b map[string]string) (string, bool) {
	for k, v := range a {
		if b[k] != v {
			return k, false
		}
	}
	for k := range b {
		if _, ok := a[k]; !ok {
			return k, false
		}
	}
	return "", true
}
