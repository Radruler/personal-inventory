package ops_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/radruler/personal-inventory/internal/httpapi"
	"github.com/radruler/personal-inventory/internal/media"
	"github.com/radruler/personal-inventory/internal/seed"
	"github.com/radruler/personal-inventory/internal/server"
	"github.com/radruler/personal-inventory/internal/testutil"
)

// TestZeroWriteSoak is PLAN.md §2's verification: zero disk writes when idle
// and on read-only browsing. It runs the real handler (server.New) and the
// media worker on a temp data/media pair with the demo seed plus one real
// HEIC upload, checkpoints the WAL, then
//
//  1. idles (INV_SOAK_IDLE, default 4s) with the worker running, and
//  2. browses read-only (INV_SOAK_ROUNDS rounds, default 2) through every
//     GET route the SPA uses: health, schema, item lists with search,
//     filters and cursor paging, facets, every item, products, the
//     location tree and every location (by id and by code:), staging,
//     inbox, media metadata, thumbs for every cover, display/orig variants
//     with conditional and range requests, the SPA index, a deep link and
//     an asset.
//
// After each phase it asserts that no file under the data or media dirs was
// created, removed, resized or had its mtime changed (this includes inv.db,
// inv.db-wal and inv.db-shm; see below), and that /proc/self/io write_bytes
// did not grow.
//
// write_bytes counts bytes this process (and children it has reaped, such
// as vips) caused to be dirtied in the page cache of a block-backed
// filesystem, at the time of dirtying. The test binary itself writes no
// files during the measured windows (its own output goes to a pipe), and
// the counters are read immediately before and after each window, so the
// deltas belong to the server. A canary write first proves the counter
// works on the temp filesystem (it does not on tmpfs); if it doesn't, only
// the mtime/size assertions run.
//
// The -shm file: SQLite maps it into memory as the WAL index; a reader may
// store a "read mark" into it when it starts a read transaction against a
// WAL that holds frames not yet copied back into the database. After a
// full checkpoint every frame is backfilled, readers take read-mark 0, which
// needs no store, so the -shm file must stay unchanged like everything else.
// We therefore assert on it rather than exclude it. (In production, between
// a write and the next checkpoint, the first reader may store one read mark:
// a single 4 KiB page, written back at most once per dirty-expire interval.)
func TestZeroWriteSoak(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("needs Linux /proc/self/io")
	}
	if testing.Short() {
		t.Skip("seeds ~100 images through vips")
	}
	idle := envDuration(t, "INV_SOAK_IDLE", 4*time.Second)
	rounds := envInt(t, "INV_SOAK_ROUNDS", 2)

	d := testutil.NewDeps(t)
	ctx, cancel := context.WithCancel(context.Background())
	n, err := seed.Demo(ctx, d)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(server.New(d))
	workerDone := make(chan error, 1)
	go func() { workerDone <- media.StartWorker(ctx, d) }()
	t.Cleanup(func() {
		srv.Close()
		cancel()
		<-workerDone
	})

	// One real upload so the worker has actually run and the inbox is not empty.
	heic, err := os.ReadFile(filepath.Join("..", "media", "testdata", "IMG_0001.HEIC"))
	if err != nil {
		t.Fatal(err)
	}
	uploadFile(t, srv.URL, "IMG_0001.HEIC", heic)
	settle(t, d)

	if _, err := d.DB.ExecContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		t.Fatal(err)
	}
	plan := browsePlan(t, d)
	t.Logf("seeded %d items; browse plan: %d requests per round", n, len(plan))

	ioOK := canary(t)
	dirs := []string{d.Cfg.DataDir, d.Cfg.MediaDir}

	before := testutil.Mtimes(t, dirs...)
	logDBFiles(t, "after checkpoint", d.Cfg.DBPath)
	w0 := writeBytes(t)
	time.Sleep(idle)
	w1 := writeBytes(t)
	afterIdle := testutil.Mtimes(t, dirs...)
	t.Logf("idle %s: write_bytes delta %d", idle, w1-w0)
	assertSame(t, "idle", before, afterIdle)
	if ioOK && w1 != w0 {
		t.Errorf("idle: write_bytes grew by %d", w1-w0)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	var reqs int
	var served int64
	w2 := writeBytes(t)
	for r := 0; r < rounds; r++ {
		for _, step := range plan {
			served += step.run(t, client, srv.URL)
			reqs++
		}
	}
	w3 := writeBytes(t)
	afterBrowse := testutil.Mtimes(t, dirs...)
	logDBFiles(t, "after browse", d.Cfg.DBPath)
	t.Logf("browse: %d requests, %d bytes served, write_bytes delta %d", reqs, served, w3-w2)
	assertSame(t, "browse", before, afterBrowse)
	if ioOK && w3 != w2 {
		t.Errorf("browse: write_bytes grew by %d", w3-w2)
	}

	// Sensitivity: one real write must show up in both detectors.
	var id string
	if err := d.DB.QueryRow(`SELECT id FROM item LIMIT 1`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest("PATCH", srv.URL+"/api/items/"+id, strings.NewReader(`{"notes":"soak sensitivity check"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("PATCH item: %d", resp.StatusCode)
	}
	w4 := writeBytes(t)
	afterWrite := testutil.Mtimes(t, dirs...)
	if afterWrite[d.Cfg.DBPath+"-wal"] == afterBrowse[d.Cfg.DBPath+"-wal"] {
		t.Errorf("sensitivity: a PATCH did not change the WAL's mtime/size")
	}
	if ioOK && w4 == w3 {
		t.Errorf("sensitivity: a PATCH did not grow write_bytes")
	}
	t.Logf("sensitivity: one PATCH wrote %d bytes (write_bytes)", w4-w3)
}

// settle waits until the worker has no pending media.
func settle(t *testing.T, d *httpapi.Deps) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Minute)
	for {
		var pending int
		if err := d.DB.QueryRow(`SELECT count(*) FROM media WHERE variant_status = 'pending'`).Scan(&pending); err != nil {
			t.Fatal(err)
		}
		if pending == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d media still pending", pending)
		}
		time.Sleep(50 * time.Millisecond)
	}
	var failed int
	_ = d.DB.QueryRow(`SELECT count(*) FROM media WHERE variant_status = 'failed'`).Scan(&failed)
	if failed > 0 {
		t.Fatalf("%d media failed variant generation", failed)
	}
}

func uploadFile(t *testing.T, base, name string, data []byte) {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, err := mw.CreateFormFile("file", name)
	if err != nil {
		t.Fatal(err)
	}
	fw.Write(data)
	mw.WriteField("context", "soak test upload")
	mw.Close()
	resp, err := http.Post(base+"/api/media", mw.FormDataContentType(), &body)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 201 && resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("upload: %d %s", resp.StatusCode, b)
	}
}

// step is one browse request and the status it must return.
type step struct {
	method, path string
	hdr          map[string]string
	want         int
}

func (s step) run(t *testing.T, c *http.Client, base string) int64 {
	t.Helper()
	req, err := http.NewRequest(s.method, base+s.path, nil)
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range s.hdr {
		req.Header.Set(k, v)
	}
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", s.method, s.path, err)
	}
	defer resp.Body.Close()
	n, _ := io.Copy(io.Discard, resp.Body)
	if resp.StatusCode != s.want {
		t.Errorf("%s %s: %d, want %d", s.method, s.path, resp.StatusCode, s.want)
	}
	return n
}

func get(path string) step { return step{method: "GET", path: path, want: 200} }

// browsePlan lists the read-only requests. Ids come from the database and
// from responses fetched now, before any measurement starts.
func browsePlan(t *testing.T, d *httpapi.Deps) []step {
	t.Helper()
	srv := httptest.NewServer(server.New(d))
	defer srv.Close()
	getJSON := func(path string, out any) {
		t.Helper()
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("GET %s: %d", path, resp.StatusCode)
		}
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			t.Fatalf("GET %s: %v", path, err)
		}
	}
	strs := func(q string) []string {
		t.Helper()
		rows, err := d.DB.Query(q)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var out []string
		for rows.Next() {
			var s string
			if err := rows.Scan(&s); err != nil {
				t.Fatal(err)
			}
			out = append(out, s)
		}
		return out
	}

	plan := []step{
		get("/api/health"),
		get("/api/schema"),
		get("/api/items"),
		get("/api/items?q=uniqlo"),
		get("/api/items?q=jack&sort=relevance"),
		get("/api/items?kind=clothing&sort=title"),
		get("/api/items?status=staged"),
		get("/api/items?status=unknown&status=approximate"),
		get("/api/items?review=needs_review"),
		get("/api/items?lifecycle=all&sort=created"),
		get("/api/items?attr.color=navy"),
		get("/api/facets"),
		get("/api/facets?kind=clothing&q=shirt"),
		get("/api/identifiers/lookup?value=12345"),
		get("/api/products"),
		get("/api/locations"),
		get("/api/staging"),
		get("/api/inbox"),
		get("/"),
		get("/items/01ABCDEFGHJKMNPQRSTVWXYZ00"),
		get("/locations"),
		{method: "GET", path: "/api/items/01ABCDEFGHJKMNPQRSTVWXYZ00", want: 404},
	}

	// Cursor pages through the whole owned list, 12 at a time.
	type page struct {
		Items []struct {
			Cover *struct {
				SHA256 string `json:"sha256"`
			} `json:"cover"`
		} `json:"items"`
		NextCursor *string `json:"next_cursor"`
	}
	covers := map[string]bool{}
	cursor := ""
	for i := 0; ; i++ {
		path := "/api/items?limit=12&lifecycle=all"
		if cursor != "" {
			path += "&cursor=" + url.QueryEscape(cursor)
		}
		var p page
		getJSON(path, &p)
		plan = append(plan, get(path))
		for _, it := range p.Items {
			if it.Cover != nil {
				covers[it.Cover.SHA256] = true
			}
		}
		if p.NextCursor == nil || i > 50 {
			break
		}
		cursor = *p.NextCursor
	}
	if len(covers) == 0 {
		t.Fatal("no covers in the item list")
	}

	for _, id := range strs(`SELECT id FROM item ORDER BY id`) {
		plan = append(plan, get("/api/items/"+id))
	}
	for _, id := range strs(`SELECT id FROM product ORDER BY id`) {
		plan = append(plan, get("/api/products/"+id))
	}
	for _, id := range strs(`SELECT id FROM location ORDER BY id`) {
		plan = append(plan, get("/api/locations/"+id))
	}
	for _, code := range strs(`SELECT code FROM location WHERE code IS NOT NULL ORDER BY code`) {
		plan = append(plan, get("/api/locations/"+url.PathEscape("code:"+code)))
	}
	shas := strs(`SELECT sha256 FROM media ORDER BY sha256`)
	for _, id := range strs(`SELECT id FROM media ORDER BY id LIMIT 10`) {
		plan = append(plan, get("/api/media/"+id))
	}
	coverList := make([]string, 0, len(covers))
	for sha := range covers {
		coverList = append(coverList, sha)
	}
	sort.Strings(coverList)
	for _, sha := range coverList {
		plan = append(plan, get(media.URL(sha, "thumb")))
	}
	for i, sha := range shas {
		if i >= 8 {
			break
		}
		plan = append(plan,
			step{method: "HEAD", path: "/api/media/sha256/" + sha, want: 200},
			get(media.URL(sha, "display")),
			get(media.URL(sha, "orig")),
			step{method: "GET", path: media.URL(sha, "orig"), hdr: map[string]string{"Range": "bytes=0-99"}, want: 206},
		)
	}
	// Conditional request with the ETag a browser would have cached.
	resp, err := http.Get(srv.URL + media.URL(coverList[0], "thumb"))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if etag := resp.Header.Get("ETag"); etag != "" {
		plan = append(plan, step{method: "GET", path: media.URL(coverList[0], "thumb"), hdr: map[string]string{"If-None-Match": etag}, want: 304})
	}
	// One SPA asset referenced by index.html (absent with the placeholder dist).
	resp, err = http.Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	idx, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if m := regexp.MustCompile(`/_app/immutable/[^"'\s]+\.(?:js|css)`).Find(idx); m != nil {
		plan = append(plan, step{method: "GET", path: string(m), hdr: map[string]string{"Accept-Encoding": "br, gzip"}, want: 200})
	} else {
		t.Log("index.html references no /_app asset (placeholder SPA); skipping the asset request")
	}
	return plan
}

// canary checks that write_bytes accounts writes on the temp filesystem.
func canary(t *testing.T) bool {
	t.Helper()
	const size = 1 << 20
	w0 := writeBytes(t)
	f, err := os.Create(filepath.Join(t.TempDir(), "canary"))
	if err != nil {
		t.Fatal(err)
	}
	f.Write(make([]byte, size))
	f.Sync()
	f.Close()
	if dw := writeBytes(t) - w0; dw < size {
		t.Logf("write_bytes grew by %d for a %d-byte canary write (tmpfs?): skipping the write_bytes assertions", dw, size)
		return false
	}
	return true
}

func writeBytes(t *testing.T) int64 {
	t.Helper()
	b, err := os.ReadFile("/proc/self/io")
	if err != nil {
		t.Fatalf("read /proc/self/io: %v", err)
	}
	for _, line := range strings.Split(string(b), "\n") {
		if v, ok := strings.CutPrefix(line, "write_bytes:"); ok {
			n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
			if err != nil {
				t.Fatal(err)
			}
			return n
		}
	}
	t.Fatal("no write_bytes in /proc/self/io")
	return 0
}

func logDBFiles(t *testing.T, when, dbPath string) {
	t.Helper()
	var parts []string
	for _, p := range []string{dbPath, dbPath + "-wal", dbPath + "-shm"} {
		if st, err := os.Stat(p); err == nil {
			parts = append(parts, fmt.Sprintf("%s %d B mtime %s", filepath.Base(p), st.Size(), st.ModTime().Format(time.RFC3339Nano)))
		} else {
			parts = append(parts, filepath.Base(p)+" absent")
		}
	}
	t.Logf("%s: %s", when, strings.Join(parts, "; "))
}

func assertSame(t *testing.T, phase string, a, b map[string]string) {
	t.Helper()
	for k, v := range a {
		if w, ok := b[k]; !ok {
			t.Errorf("%s: %s was removed", phase, k)
		} else if w != v {
			t.Errorf("%s: %s changed (mtime/size %s -> %s)", phase, k, v, w)
		}
	}
	for k := range b {
		if _, ok := a[k]; !ok {
			t.Errorf("%s: %s was created", phase, k)
		}
	}
}

func envDuration(t *testing.T, k string, def time.Duration) time.Duration {
	if v := os.Getenv(k); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			t.Fatalf("%s: %v", k, err)
		}
		return d
	}
	return def
}

func envInt(t *testing.T, k string, def int) int {
	if v := os.Getenv(k); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			t.Fatalf("%s: %v", k, err)
		}
		return n
	}
	return def
}
