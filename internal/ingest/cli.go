package ingest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/radruler/personal-inventory/internal/httpapi"
)

const ingestUsage = "usage: inv ingest BUNDLE.json [--server URL] [--token T] [--dry-run]"

// RunCLI implements `inv ingest BUNDLE [--server URL] [--dry-run] [--token T]`:
// upload local photos the server doesn't have (by sha256), rewrite `path`
// references, then POST the bundle. It talks to a server over HTTP and
// needs no database.
//
// Paths (bundle media, observation `media`, reference `archive`) are
// resolved relative to the bundle file. A media entry without `context`
// picks up a sidecar `<photo>.txt` (IMG_1.HEIC.txt or IMG_1.txt), else the
// folder's `context.txt`. The bundle is validated with a server dry run
// before anything is uploaded; --dry-run stops there. A rejected bundle
// (422) prints every problem and returns an error (non-zero exit).
func RunCLI(ctx context.Context, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("ingest", flag.ContinueOnError)
	fs.SetOutput(out)
	server := fs.String("server", envOr("INV_SERVER", "http://localhost:8480"), "server base URL (env INV_SERVER)")
	token := fs.String("token", os.Getenv("INV_TOKEN"), "bearer token when the server has INV_TOKEN set (env INV_TOKEN)")
	dry := fs.Bool("dry-run", false, "upload nothing; print what the bundle would change")
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return errors.New(ingestUsage)
	}
	c := &client{base: strings.TrimRight(*server, "/"), token: *token, http: http.DefaultClient}
	return ingestFile(ctx, c, pos[0], *dry, out)
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// parseInterspersed parses flags that may appear before or after positional
// arguments.
func parseInterspersed(fs *flag.FlagSet, args []string) ([]string, error) {
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		if fs.NArg() == 0 {
			return pos, nil
		}
		pos = append(pos, fs.Arg(0))
		args = fs.Args()[1:]
	}
}

// ---------------------------------------------------------------- HTTP client

type client struct {
	base, token string
	http        *http.Client
}

func (c *client) do(ctx context.Context, method, path string, body io.Reader, ctype string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, body)
	if err != nil {
		return nil, err
	}
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	return c.http.Do(req)
}

type apiError struct {
	Error struct {
		Code    string          `json:"code"`
		Message string          `json:"message"`
		Details json.RawMessage `json:"details"`
	} `json:"error"`
}

func readAPIError(resp *http.Response) error {
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var e apiError
	if json.Unmarshal(b, &e) == nil && e.Error.Message != "" {
		return fmt.Errorf("%s: %s (HTTP %d)", e.Error.Code, e.Error.Message, resp.StatusCode)
	}
	return fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
}

// has reports whether the server already stores sha.
func (c *client) has(ctx context.Context, sha string) (bool, error) {
	resp, err := c.do(ctx, http.MethodHead, "/api/media/sha256/"+sha, nil, "")
	if err != nil {
		return false, err
	}
	resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
		return true, nil
	case http.StatusNotFound:
		return false, nil
	}
	return false, fmt.Errorf("HEAD /api/media/sha256/%s: HTTP %d", sha, resp.StatusCode)
}

// upload streams one file to POST /api/media and checks the stored sha256.
func (c *client) upload(ctx context.Context, path, sha string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)
	go func() {
		part, err := mw.CreateFormFile("file", filepath.Base(path))
		if err == nil {
			_, err = io.Copy(part, f)
		}
		if err == nil {
			err = mw.Close()
		}
		pw.CloseWithError(err)
	}()
	resp, err := c.do(ctx, http.MethodPost, "/api/media", pr, mw.FormDataContentType())
	if err != nil {
		pr.CloseWithError(err)
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("upload %s: %w", path, readAPIError(resp))
	}
	var m struct {
		SHA256 string `json:"sha256"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		return fmt.Errorf("upload %s: %w", path, err)
	}
	if m.SHA256 != sha {
		return fmt.Errorf("upload %s: server stored sha256 %s, want %s", path, m.SHA256, sha)
	}
	return nil
}

// ---------------------------------------------------------------- bundle walk

// fileRef is one local path in the bundle and how to rewrite it.
type fileRef struct {
	where   string // JSON path, for messages
	rel     string // as written
	abs     string
	obj     map[string]any // BundleMediaRef object (nil for observation media strings)
	setStr  func(string)   // observation media: replaces the string
	isMedia bool           // a media link (gets sidecar context)
}

func ingestFile(ctx context.Context, c *client, bundlePath string, dry bool, out io.Writer) error {
	raw, err := os.ReadFile(bundlePath)
	if err != nil {
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber() // numbers go back to the server exactly as written
	var doc map[string]any
	if err := dec.Decode(&doc); err != nil {
		return fmt.Errorf("%s: invalid JSON: %w", bundlePath, err)
	}
	baseDir, err := filepath.Abs(filepath.Dir(bundlePath))
	if err != nil {
		return err
	}
	refs := collectRefs(doc, baseDir)

	// Every file must exist before anything is uploaded.
	var missing []string
	for _, r := range refs {
		if fi, err := os.Stat(r.abs); err != nil || fi.IsDir() {
			missing = append(missing, fmt.Sprintf("  %s: %s not found", r.where, r.rel))
		}
	}
	if len(missing) > 0 {
		fmt.Fprintf(out, "%d %s not found (paths are relative to %s):\n%s\n", len(missing),
			plural(len(missing), "file", "files"), baseDir, strings.Join(missing, "\n"))
		return fmt.Errorf("ingest: %d missing %s", len(missing), plural(len(missing), "file", "files"))
	}

	if err := c.health(ctx); err != nil {
		return err
	}

	// Hash and ask the server which files it already has.
	shaOf := map[string]string{}
	var missingFiles []string // abs paths to upload, one per sha
	seenSha := map[string]bool{}
	present := 0
	for _, r := range refs {
		if _, ok := shaOf[r.abs]; ok {
			continue
		}
		sha, err := fileSHA(r.abs)
		if err != nil {
			return err
		}
		shaOf[r.abs] = sha
		if seenSha[sha] {
			continue
		}
		seenSha[sha] = true
		ok, err := c.has(ctx, sha)
		if err != nil {
			return err
		}
		if ok {
			present++
		} else {
			missingFiles = append(missingFiles, r.abs)
		}
	}

	// Rewrite paths to sha256 refs; sidecar context for media links.
	for _, r := range refs {
		sha := shaOf[r.abs]
		if r.obj != nil {
			delete(r.obj, "path")
			r.obj["sha256"] = sha
			if r.isMedia && emptyContext(r.obj["context"]) {
				if note := sidecar(r.abs); note != "" {
					r.obj["context"] = note
				}
			}
		} else {
			r.setStr("sha256:" + sha)
		}
	}
	body, err := json.Marshal(doc)
	if err != nil {
		return err
	}

	// Validate before uploading anything: a dry run needs no uploads.
	nFiles := len(seenSha)
	res, probs, msg, err := c.postBundle(ctx, body, true)
	if err != nil {
		return err
	}
	if probs != nil {
		fmt.Fprintf(out, "bundle rejected: %s; nothing was uploaded or written\n", msg)
		for _, p := range probs {
			fmt.Fprintf(out, "  %s: %s\n", p.Path, p.Message)
		}
		return fmt.Errorf("ingest: bundle rejected (%d %s)", len(probs), plural(len(probs), "problem", "problems"))
	}
	if dry {
		fmt.Fprintf(out, "media: %d %s, %d already on the server, %d would be uploaded\n", nFiles, plural(nFiles, "file", "files"), present, len(missingFiles))
		printResult(out, res)
		return nil
	}
	for _, abs := range missingFiles {
		if err := c.upload(ctx, abs, shaOf[abs]); err != nil {
			return err
		}
	}
	fmt.Fprintf(out, "media: %d %s, %d uploaded, %d already on the server\n", nFiles, plural(nFiles, "file", "files"), len(missingFiles), present)
	res, probs, msg, err = c.postBundle(ctx, body, false)
	if err != nil {
		return err
	}
	if probs != nil {
		fmt.Fprintf(out, "bundle rejected: %s; nothing was written\n", msg)
		for _, p := range probs {
			fmt.Fprintf(out, "  %s: %s\n", p.Path, p.Message)
		}
		return fmt.Errorf("ingest: bundle rejected (%d %s)", len(probs), plural(len(probs), "problem", "problems"))
	}
	printResult(out, res)
	return nil
}

// postBundle POSTs the bundle. A 422 returns its problems (non-nil) and message.
func (c *client) postBundle(ctx context.Context, body []byte, dry bool) (*Result, []Problem, string, error) {
	q := ""
	if dry {
		q = "?dry_run=true"
	}
	resp, err := c.do(ctx, http.MethodPost, "/api/ingest/bundle"+q, bytes.NewReader(body), "application/json")
	if err != nil {
		return nil, nil, "", err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusUnprocessableEntity:
		b, _ := io.ReadAll(resp.Body)
		var e apiError
		_ = json.Unmarshal(b, &e)
		probs := []Problem{}
		_ = json.Unmarshal(e.Error.Details, &probs)
		return nil, probs, e.Error.Message, nil
	default:
		return nil, nil, "", fmt.Errorf("POST /api/ingest/bundle: %w", readAPIError(resp))
	}
	var res Result
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, nil, "", err
	}
	return &res, nil, "", nil
}

func (c *client) health(ctx context.Context) error {
	resp, err := c.do(ctx, http.MethodGet, "/api/health", nil, "")
	if err != nil {
		return fmt.Errorf("cannot reach the server at %s (set --server or INV_SERVER): %w", c.base, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s/api/health: %w", c.base, readAPIError(resp))
	}
	return nil
}

func emptyContext(v any) bool {
	s, ok := v.(string)
	return v == nil || (ok && strings.TrimSpace(s) == "")
}

// sidecar returns the owner's note for a photo: <photo>.txt, <stem>.txt,
// else the folder's context.txt.
func sidecar(photo string) string {
	stem := strings.TrimSuffix(photo, filepath.Ext(photo))
	for _, p := range []string{photo + ".txt", stem + ".txt", filepath.Join(filepath.Dir(photo), "context.txt")} {
		if p == photo {
			continue
		}
		if b, err := os.ReadFile(p); err == nil {
			if s := strings.TrimSpace(string(b)); s != "" {
				return s
			}
		}
	}
	return ""
}

func collectRefs(doc map[string]any, baseDir string) []*fileRef {
	var refs []*fileRef
	abs := func(p string) string {
		if filepath.IsAbs(p) {
			return filepath.Clean(p)
		}
		return filepath.Join(baseDir, filepath.FromSlash(p))
	}
	mediaRef := func(where string, v any, isMedia bool) {
		m, ok := v.(map[string]any)
		if !ok {
			return
		}
		p, ok := m["path"].(string)
		if !ok || strings.TrimSpace(p) == "" {
			return
		}
		refs = append(refs, &fileRef{where: where, rel: p, abs: abs(p), obj: m, isMedia: isMedia})
	}
	mediaList := func(where string, v any) {
		list, _ := v.([]any)
		for i, e := range list {
			mediaRef(fmt.Sprintf("%s[%d]", where, i), e, true)
		}
	}
	referenceList := func(where string, v any) {
		list, _ := v.([]any)
		for i, e := range list {
			if m, ok := e.(map[string]any); ok {
				mediaRef(fmt.Sprintf("%s[%d].archive", where, i), m["archive"], false)
			}
		}
	}
	product := func(where string, v any) {
		m, ok := v.(map[string]any)
		if !ok {
			return
		}
		mediaList(where+".media", m["media"])
		referenceList(where+".references", m["references"])
	}
	prods, _ := doc["products"].([]any)
	for i, p := range prods {
		product(fmt.Sprintf("products[%d]", i), p)
	}
	itemList, _ := doc["items"].([]any)
	for i, e := range itemList {
		it, ok := e.(map[string]any)
		if !ok {
			continue
		}
		where := fmt.Sprintf("items[%d]", i)
		mediaList(where+".media", it["media"])
		referenceList(where+".references", it["references"])
		product(where+".product", it["product"])
		obs, _ := it["observations"].([]any)
		for j, o := range obs {
			om, ok := o.(map[string]any)
			if !ok {
				continue
			}
			s, ok := om["media"].(string)
			if !ok || !looksLikePath(s, abs(s)) {
				continue
			}
			om2 := om
			refs = append(refs, &fileRef{where: fmt.Sprintf("%s.observations[%d].media", where, j), rel: s, abs: abs(s),
				setStr: func(v string) { om2["media"] = v }})
		}
	}
	return refs
}

// looksLikePath tells a local path from a media id or sha256:<hex> in an
// observation's `media`.
func looksLikePath(s, abs string) bool {
	s = strings.TrimSpace(s)
	if s == "" || strings.HasPrefix(strings.ToLower(s), "sha256:") {
		return false
	}
	if _, err := os.Stat(abs); err == nil {
		return true
	}
	return strings.ContainsAny(s, "/\\.")
}

func printResult(out io.Writer, res *Result) {
	section := func(name string, list []Outcome) {
		if len(list) == 0 {
			return
		}
		counts := map[string]int{}
		for _, o := range list {
			counts[o.Action]++
		}
		fmt.Fprintf(out, "%s: %d created, %d updated, %d unchanged\n", name, counts["created"], counts["updated"], counts["unchanged"])
		for _, o := range list {
			id := "(new)"
			if o.ID != nil {
				id = *o.ID
			}
			line := fmt.Sprintf("  %-9s %s  %s", o.Action, o.ExtKey, id)
			if o.Action != "unchanged" && len(o.Changes) > 0 {
				line += "  " + strings.Join(o.Changes, ", ")
			}
			fmt.Fprintln(out, line)
		}
	}
	section("products", res.Products)
	section("items", res.Items)
	var warns []string
	for _, l := range [][]Outcome{res.Products, res.Items} {
		for _, o := range l {
			for _, w := range o.Warnings {
				warns = append(warns, fmt.Sprintf("  %s: %s", o.ExtKey, w))
			}
		}
	}
	if len(warns) > 0 {
		sort.Strings(warns)
		fmt.Fprintf(out, "warnings (%d):\n%s\n", len(warns), strings.Join(warns, "\n"))
	}
	if res.DryRun {
		fmt.Fprintln(out, "dry run: nothing was written")
	}
}

// ---------------------------------------------------------------- export / import

// ExportCLI implements `inv export DIR [--no-media]` (PLAN.md §7, openapi
// /api/export layout). It writes DIR.tmp and renames it to DIR when done.
func ExportCLI(ctx context.Context, d *httpapi.Deps, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("export", flag.ContinueOnError)
	fs.SetOutput(out)
	noMedia := fs.Bool("no-media", false, "leave out the original media files")
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return errors.New("usage: inv export DIR [--no-media]")
	}
	man, err := ExportDir(ctx, d, pos[0], !*noMedia)
	if err != nil {
		return err
	}
	keys := make([]string, 0, len(man.Counts))
	for k := range man.Counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var parts []string
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s %d", k, man.Counts[k]))
	}
	fmt.Fprintf(out, "exported to %s: %s; %d media files\n", pos[0], strings.Join(parts, ", "), man.MediaFiles)
	if len(man.MissingMedia) > 0 {
		fmt.Fprintf(out, "warning: %d originals were missing on disk (listed in manifest.json missing_media)\n", len(man.MissingMedia))
	}
	return nil
}

// ExportDir writes the export layout to dir atomically (dir.tmp, then
// rename). dir must not exist.
func ExportDir(ctx context.Context, d *httpapi.Deps, dir string, withMedia bool) (*Manifest, error) {
	dir = filepath.Clean(dir)
	if _, err := os.Stat(dir); err == nil {
		return nil, fmt.Errorf("%s already exists; export to a new directory", dir)
	}
	tmp := dir + ".tmp"
	if err := os.RemoveAll(tmp); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(tmp, 0o755); err != nil {
		return nil, err
	}
	man, err := writeExport(ctx, d, &dirSink{dir: tmp}, withMedia)
	if err != nil {
		os.RemoveAll(tmp)
		return nil, err
	}
	if err := os.Rename(tmp, dir); err != nil {
		return nil, err
	}
	return man, nil
}

// ImportCLI implements `inv import DIR [--variants]` into an empty database.
func ImportCLI(ctx context.Context, d *httpapi.Deps, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("import", flag.ContinueOnError)
	fs.SetOutput(out)
	variants := fs.Bool("variants", false, "generate image variants now instead of leaving them to the server's worker")
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return errors.New("usage: inv import DIR [--variants]")
	}
	st, err := ImportDir(ctx, d, pos[0], *variants, out)
	if err != nil {
		return err
	}
	keys := make([]string, 0, len(st.Rows))
	for k := range st.Rows {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var parts []string
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s %d", k, st.Rows[k]))
	}
	fmt.Fprintf(out, "imported %s: %s\n", pos[0], strings.Join(parts, ", "))
	fmt.Fprintf(out, "media: %d originals copied (sha256 verified), %d not in the export\n", st.MediaCopied, st.MediaMissing)
	if *variants {
		fmt.Fprintf(out, "variants: %d built, %d failed\n", st.VariantsReady, st.VariantsFail)
	} else {
		fmt.Fprintln(out, "variants: pending; `inv serve` builds them in the background")
	}
	return nil
}

// ImportDir is ImportCLI without the flag parsing and summary.
func ImportDir(ctx context.Context, d *httpapi.Deps, dir string, variants bool, log io.Writer) (*ImportStats, error) {
	return importDir(ctx, d, dir, variants, log)
}
