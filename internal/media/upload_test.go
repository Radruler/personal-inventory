package media

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/radruler/personal-inventory/internal/testutil"
)

func TestSniff(t *testing.T) {
	ftyp := func(major string, compat ...string) []byte {
		b := mkbox("ftyp", append([][]byte{[]byte(major), be32(0)}, func() [][]byte {
			var out [][]byte
			for _, c := range compat {
				out = append(out, []byte(c))
			}
			return out
		}()...)...)
		return append(b, make([]byte, 32)...)
	}
	cases := []struct {
		name string
		b    []byte
		mime string // "" = rejected
	}{
		{"jpeg", []byte{0xff, 0xd8, 0xff, 0xe1, 0, 0}, "image/jpeg"},
		{"png", []byte("\x89PNG\r\n\x1a\n\x00\x00"), "image/png"},
		{"webp", []byte("RIFF\x10\x00\x00\x00WEBPVP8 "), "image/webp"},
		{"iphone heic", ftyp("heic", "mif1", "MiHE", "MiPr", "miaf", "MiHB", "heic"), "image/heic"},
		{"heix", ftyp("heix", "mif1", "heix"), "image/heic"},
		{"mif1 major, heic compat", ftyp("mif1", "mif1", "heic"), "image/heic"},
		{"plain heif", ftyp("mif1", "mif1", "miaf"), "image/heif"},
		{"heif sequence", ftyp("msf1", "msf1"), "image/heif"},
		{"avif", ftyp("avif", "mif1", "miaf"), ""},
		{"mp4", ftyp("isom", "isom", "iso2", "mp41"), ""},
		{"quicktime", ftyp("qt  ", "qt  "), ""},
		{"pdf", []byte("%PDF-1.7\n%\xe2\xe3"), "application/pdf"},
		{"html doctype", []byte("\xef\xbb\xbf  <!DOCTYPE html><html><head>"), "text/html"},
		{"html tag", []byte("<html lang=en>"), "text/html"},
		{"gif", []byte("GIF89a\x01\x00"), ""},
		{"text", []byte("hello world"), ""},
		{"zip", []byte("PK\x03\x04"), ""},
	}
	for _, c := range cases {
		got, ok := sniff(c.b)
		if c.mime == "" {
			if ok {
				t.Errorf("%s: accepted as %s, want rejected", c.name, got.mime)
			}
			continue
		}
		if !ok || got.mime != c.mime {
			t.Errorf("%s: got %q (%v), want %s", c.name, got.mime, ok, c.mime)
		}
	}
	// The committed fixtures sniff as HEIC.
	for _, f := range []string{"IMG_0001.HEIC", "grid.heic"} {
		if got, ok := sniff(readFixture(t, f)); !ok || got.mime != "image/heic" || got.ext != "heic" {
			t.Errorf("%s: %+v %v", f, got, ok)
		}
	}
}

func TestUploadStoresOriginalByteForByte(t *testing.T) {
	d, h := newEnv(t)
	heic := readFixture(t, "IMG_0001.HEIC")
	out := upload(t, h, 201, fileField("IMG_0001.HEIC", heic), textField("context", "  the grey one, not the navy  "))
	if out.Deduplicated {
		t.Error("deduplicated on first upload")
	}
	if out.SHA256 != shaHex(heic) {
		t.Errorf("sha256 %s, want %s", out.SHA256, shaHex(heic))
	}
	if out.Mime != "image/heic" || out.Ext != "heic" || out.Bytes != int64(len(heic)) {
		t.Errorf("mime/ext/bytes = %s/%s/%d", out.Mime, out.Ext, out.Bytes)
	}
	if out.VariantStatus != "pending" || out.ThumbURL != nil || out.DisplayURL != nil {
		t.Errorf("variant_status %s thumb %v display %v", out.VariantStatus, out.ThumbURL, out.DisplayURL)
	}
	if out.OrigURL != "/media/"+out.SHA256+"/orig" {
		t.Errorf("orig_url %s", out.OrigURL)
	}
	if out.OriginalName == nil || *out.OriginalName != "IMG_0001.HEIC" {
		t.Errorf("original_name %v", out.OriginalName)
	}
	if out.Context == nil || *out.Context != "the grey one, not the navy" {
		t.Errorf("context %v", out.Context)
	}
	stored, err := os.ReadFile(OrigPath(d.Cfg, out.SHA256, "heic"))
	if err != nil {
		t.Fatal(err)
	}
	if shaHex(stored) != shaHex(heic) {
		t.Error("stored original differs from the upload")
	}
	// No temp files left behind.
	left, _ := filepath.Glob(filepath.Join(d.Cfg.MediaDir, "orig", ".upload-*"))
	if len(left) > 0 {
		t.Errorf("temp files left: %v", left)
	}

	// The JSON shape: required nullable fields are present, links is [].
	rec := postMultipart(t, h, fileField("x.png", pngBytes(t, gradient(20, 10, 1))))
	var raw map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"id", "sha256", "mime", "width", "height", "variant_status", "thumb_url", "display_url", "orig_url",
		"ext", "bytes", "taken_at", "original_name", "exif", "context", "variant_error", "created_at", "links", "deduplicated"} {
		if _, ok := raw[k]; !ok {
			t.Errorf("response lacks %q", k)
		}
	}
	if l, ok := raw["links"].([]any); !ok || len(l) != 0 {
		t.Errorf("links = %#v, want []", raw["links"])
	}
	if raw["exif"] != nil || raw["taken_at"] != nil || raw["context"] != nil {
		t.Errorf("png without EXIF: exif %v taken_at %v context %v", raw["exif"], raw["taken_at"], raw["context"])
	}
	if raw["width"] != 20.0 || raw["height"] != 10.0 {
		t.Errorf("png dims %v x %v", raw["width"], raw["height"])
	}
}

func TestUploadHEICMetadata(t *testing.T) {
	_, h := newEnv(t)
	out := upload(t, h, 201, fileField("IMG_0001.HEIC", readFixture(t, "IMG_0001.HEIC")))
	// Coded 320x240 with irot 90°: stored dimensions are after rotation.
	if out.Width == nil || out.Height == nil || *out.Width != 240 || *out.Height != 320 {
		t.Errorf("dims %v x %v, want 240 x 320", deref(out.Width), deref(out.Height))
	}
	// 2026:10:30 14:05:09.123 at -07:00 → UTC.
	if out.TakenAt == nil || *out.TakenAt != "2026-10-30T21:05:09.123Z" {
		t.Errorf("taken_at %v", out.TakenAt)
	}
	var exif map[string]any
	if err := json.Unmarshal(out.Exif, &exif); err != nil {
		t.Fatalf("exif %s: %v", out.Exif, err)
	}
	want := map[string]any{
		"Make": "Apple", "Model": "iPhone 15 Pro", "Software": "18.1",
		"OffsetTimeOriginal": "-07:00", "DateTimeOriginal": "2026:10:30 14:05:09",
		"LensModel": "iPhone 15 Pro back triple camera 6.765mm f/1.78", "FNumber": 1.78,
	}
	for k, v := range want {
		if exif[k] != v {
			t.Errorf("exif[%s] = %#v, want %#v", k, exif[k], v)
		}
	}
	// libheif applies irot itself, so vips drops EXIF Orientation for HEIF
	// (keeping it would invite a second rotation).
	if _, ok := exif["Orientation"]; ok {
		t.Errorf("HEIC exif keeps Orientation %v", exif["Orientation"])
	}
	if s := strings.ToLower(string(out.Exif)); strings.Contains(s, "gps") || strings.Contains(s, "37.33") || strings.Contains(s, "latitude") {
		t.Errorf("exif leaks GPS: %s", out.Exif)
	}
}

func deref(p *int) any {
	if p == nil {
		return nil
	}
	return *p
}

func TestTakenAt(t *testing.T) {
	cases := []struct{ dt, sub, off, want string }{
		{"2026:10:30 14:05:09", "", "-07:00", "2026-10-30T21:05:09.000Z"},
		{"2026:10:30 14:05:09", "5", "+09:00", "2026-10-30T05:05:09.500Z"},
		{"2026:10:30 14:05:09", "", "", "2026-10-30T14:05:09.000"},
		{"2026:10:30 14:05:09", "", "garbage", "2026-10-30T14:05:09.000"},
		{"0000:00:00 00:00:00", "", "", ""},
		{"", "", "", ""},
	}
	for _, c := range cases {
		got := takenAt(c.dt, c.sub, c.off)
		if (got == nil && c.want != "") || (got != nil && *got != c.want) {
			t.Errorf("takenAt(%q,%q,%q) = %v, want %q", c.dt, c.sub, c.off, got, c.want)
		}
	}
}

func TestParseVipsHeaderOrientation(t *testing.T) {
	m := parseVipsHeader("x.jpg: 4032x3024 uchar, 3 bands, srgb, jpegload\nwidth: 4032\nheight: 3024\norientation: 6\n" +
		"exif-ifd0-Model: A (B) (A (B), ASCII, 6 components, 6 bytes)\n" +
		"exif-ifd2-ExposureTime: 1/120 (1/120 sec., Rational, 1 components, 8 bytes)\n" +
		"exif-ifd2-ISOSpeedRatings: 80 (80, Short, 1 components, 2 bytes)\n" +
		"exif-ifd3-GPSLatitude: 37/1 19/1 48/1 (37, 19, 48, Rational, 3 components, 24 bytes)\n")
	if *m.width != 3024 || *m.height != 4032 {
		t.Errorf("orientation 6 should swap: %d x %d", *m.width, *m.height)
	}
	if m.exif["Model"] != "A (B)" || m.exif["ExposureTime"] != "1/120" || m.exif["ISOSpeedRatings"] != int64(80) {
		t.Errorf("exif %#v", m.exif)
	}
	if _, ok := m.exif["GPSLatitude"]; ok {
		t.Error("GPS kept")
	}
}

func TestUploadFieldOrder(t *testing.T) {
	d, h := newEnv(t)
	exec1(t, d, `INSERT INTO item (id, kind, title) VALUES ('it1', 'clothing', 'Jacket')`)
	img := pngBytes(t, gradient(30, 20, 7))
	// Fields after the file.
	a := upload(t, h, 201, fileField("a.png", img), textField("context", "after"),
		textField("link_subject_type", "item"), textField("link_subject_id", "it1"), textField("link_role", "flat_lay"))
	// Fields before the file.
	b := upload(t, h, 201, textField("context", "before"), textField("link_subject_type", "item"),
		textField("link_subject_id", "it1"), fileField("b.png", pngBytes(t, gradient(30, 20, 9))))
	if *a.Context != "after" || *b.Context != "before" {
		t.Errorf("context %v %v", *a.Context, *b.Context)
	}
	if len(a.Links) != 1 || a.Links[0].Role != "flat_lay" || a.Links[0].SubjectID != "it1" || a.Links[0].Sort != 0 {
		t.Errorf("a links %+v", a.Links)
	}
	if len(b.Links) != 1 || b.Links[0].Role != "other" || b.Links[0].Sort != 1 {
		t.Errorf("b links %+v (want role other, appended at sort 1)", b.Links)
	}
}

func TestUploadRejects(t *testing.T) {
	d, h := newEnv(t)
	d.Cfg.MaxUploadBytes = 4096 // handlers read d.Cfg per request
	big := append([]byte{0xff, 0xd8, 0xff}, make([]byte, 5000)...)

	cases := []struct {
		name  string
		parts []part
		code  int
		err   string
	}{
		{"gif", []part{fileField("a.gif", []byte("GIF89a\x01\x00\x01\x00"))}, 415, "unsupported_media_type"},
		{"text", []part{fileField("a.txt", []byte("just text"))}, 415, "unsupported_media_type"},
		{"too large", []part{fileField("big.jpg", big)}, 413, "payload_too_large"},
		{"no file", []part{textField("context", "x")}, 400, "bad_request"},
		{"empty file", []part{fileField("e.jpg", nil)}, 400, "bad_request"},
		{"unknown field", []part{textField("colour", "x"), fileField("a.png", pngBytes(t, gradient(4, 4, 1)))}, 400, "bad_request"},
		{"two files", []part{fileField("a.png", pngBytes(t, gradient(4, 4, 1))), fileField("b.png", pngBytes(t, gradient(4, 4, 2)))}, 400, "bad_request"},
		{"link to missing item", []part{fileField("a.png", pngBytes(t, gradient(4, 4, 3))),
			textField("link_subject_type", "item"), textField("link_subject_id", "nope")}, 422, "validation"},
		{"half a link", []part{fileField("a.png", pngBytes(t, gradient(4, 4, 4))), textField("link_subject_type", "item")}, 422, "validation"},
		{"bad subject type", []part{fileField("a.png", pngBytes(t, gradient(4, 4, 5))),
			textField("link_subject_type", "box"), textField("link_subject_id", "x")}, 422, "validation"},
	}
	for _, c := range cases {
		rec := postMultipart(t, h, c.parts...)
		if rec.Code != c.code || !strings.Contains(rec.Body.String(), `"code":"`+c.err+`"`) {
			t.Errorf("%s: %d %s, want %d %s", c.name, rec.Code, rec.Body.String(), c.code, c.err)
		}
	}
	// Not multipart at all.
	if rec := testutil.Do(t, h, "POST", "/api/media", map[string]string{"a": "b"}, nil); rec.Code != 400 {
		t.Errorf("json body: %d", rec.Code)
	}
	// Nothing was stored: no rows, no files (temp files included).
	var n int
	d.DB.QueryRow(`SELECT COUNT(*) FROM media`).Scan(&n)
	if n != 0 {
		t.Errorf("%d media rows after rejected uploads", n)
	}
	if files := testutil.Mtimes(t, filepath.Join(d.Cfg.MediaDir, "orig")); len(files) != 0 {
		t.Errorf("files left: %v", files)
	}
}

func TestUploadDedupeWritesNothing(t *testing.T) {
	d, h := newEnv(t)
	heic := readFixture(t, "IMG_0001.HEIC")
	first := upload(t, h, 201, fileField("IMG_0001.HEIC", heic), textField("context", "first"))
	process(t, d)

	before := testutil.Mtimes(t, d.Cfg.DataDir, d.Cfg.MediaDir)
	if _, ok := before[d.Cfg.DBPath]; !ok {
		t.Fatalf("snapshot lacks the DB file: %v", before)
	}
	again := upload(t, h, 200, fileField("renamed.heic", heic), textField("context", "second"))
	after := testutil.Mtimes(t, d.Cfg.DataDir, d.Cfg.MediaDir)
	if k, ok := equalSnap(before, after); !ok {
		t.Errorf("dedupe changed %s (before %q, after %q)", k, before[k], after[k])
	}
	if !again.Deduplicated || again.ID != first.ID || again.VariantStatus != "ready" {
		t.Errorf("dedupe result: %+v", again)
	}
	// The existing context and name are kept.
	if *again.Context != "first" || *again.OriginalName != "IMG_0001.HEIC" {
		t.Errorf("context %q name %q", *again.Context, *again.OriginalName)
	}
}

func TestUploadDedupeAppliesRequestedLink(t *testing.T) {
	d, h := newEnv(t)
	exec1(t, d, `INSERT INTO item (id, kind, title) VALUES ('it1', 'clothing', 'Jacket')`)
	img := pngBytes(t, gradient(16, 16, 3))
	first := upload(t, h, 201, fileField("a.png", img))
	again := upload(t, h, 200, fileField("a.png", img), textField("context", "late note"),
		textField("link_subject_type", "item"), textField("link_subject_id", "it1"))
	if !again.Deduplicated || again.ID != first.ID || len(again.Links) != 1 || *again.Context != "late note" {
		t.Errorf("dedupe with link: %+v", again)
	}
}
