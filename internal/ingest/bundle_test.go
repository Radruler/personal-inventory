package ingest_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/radruler/personal-inventory/internal/ingest"
	"github.com/radruler/personal-inventory/internal/testutil"
)

var runCLI = ingest.RunCLI

type itemDetail struct {
	ID          string         `json:"id"`
	Title       string         `json:"title"`
	Category    *string        `json:"category"`
	ReviewState string         `json:"review_state"`
	UpdatedAt   string         `json:"updated_at"`
	Attrs       map[string]any `json:"attrs"`
	Tags        []string       `json:"tags"`
	ProductID   *string        `json:"product_id"`
	ParentID    *string        `json:"parent_item_id"`
	CoverID     *string        `json:"cover_media_id"`
	LocationID  *string        `json:"location_id"`
	TargetID    *string        `json:"target_location_id"`
	Placement   struct {
		Status string `json:"status"`
	} `json:"placement"`
	Observations []struct {
		ID         string  `json:"id"`
		Text       *string `json:"text"`
		RecordedBy string  `json:"recorded_by"`
		ExtKey     *string `json:"ext_key"`
	} `json:"observations"`
	Facts []struct {
		ID           string          `json:"id"`
		Key          string          `json:"key"`
		Value        json.RawMessage `json:"value"`
		Status       string          `json:"status"`
		SupersedesID *string         `json:"supersedes_id"`
		ExtKey       *string         `json:"ext_key"`
		Sources      json.RawMessage `json:"sources"`
	} `json:"facts"`
	Identifiers []struct {
		Value         string  `json:"value"`
		ObservationID *string `json:"observation_id"`
	} `json:"identifiers"`
	Movements []struct {
		Kind  string `json:"kind"`
		Actor string `json:"actor"`
	} `json:"movements"`
	Media []struct {
		Role  string `json:"role"`
		Media struct {
			SHA256  string  `json:"sha256"`
			Context *string `json:"context"`
		} `json:"media"`
	} `json:"media"`
}

func (e *env) item(ext string) itemDetail {
	e.t.Helper()
	id := e.scalar(`SELECT id FROM item WHERE ext_key = ?`, ext)
	var d itemDetail
	if code := e.req("GET", "/api/items/"+id, nil, &d); code != 200 {
		e.t.Fatalf("get item %s: %d", ext, code)
	}
	return d
}

func TestFolderIngestTwice(t *testing.T) {
	e := newEnv(t)
	bundle := copyFixture(t)

	out, err := e.ingestCLI(bundle)
	if err != nil {
		t.Fatalf("ingest: %v\n%s", err, out)
	}
	for _, want := range []string{"media: 4 files, 4 uploaded, 0 already", "items: 3 created, 0 updated, 0 unchanged", "products: 1 created"} {
		if !strings.Contains(out, want) {
			t.Errorf("first run output lacks %q:\n%s", want, out)
		}
	}
	want := map[string]int{"item": 3, "product": 1, "location": 2, "media": 4, "media_link": 4, "observation": 2,
		"identifier": 5, "fact": 10, "reference": 0, "movement": 6, "tag": 1, "item_fts": 3}
	first := e.counts()
	if !equalMaps(first, want) {
		t.Fatalf("counts after first run:\n got %v\nwant %v", first, want)
	}

	jacket := e.item("batch-2026-11-01/navy-shell-jacket")
	if jacket.ReviewState != "needs_review" || jacket.Placement.Status != "staged" {
		t.Errorf("jacket review/placement: %s %s", jacket.ReviewState, jacket.Placement.Status)
	}
	if jacket.Attrs["brand"] != "Uniqlo" || jacket.Attrs["season"] != "2023 Spring/Summer" || jacket.Attrs["color"] != "navy" {
		t.Errorf("jacket attrs (item + product facts): %v", jacket.Attrs)
	}
	if len(jacket.Movements) != 2 || jacket.Movements[0].Actor != "agent:claude-opus-5-5" {
		t.Errorf("movements: %+v", jacket.Movements)
	}
	if len(jacket.Observations) != 1 || jacket.Observations[0].RecordedBy != "agent:claude-opus-5-5" ||
		!strings.Contains(*jacket.Observations[0].Text, "C[?]LD") {
		t.Errorf("observation: %+v", jacket.Observations)
	}
	for _, idf := range jacket.Identifiers {
		if idf.ObservationID == nil || *idf.ObservationID != jacket.Observations[0].ID {
			t.Errorf("identifier %s not tied to its observation", idf.Value)
		}
	}
	if jacket.CoverID == nil {
		t.Error("cover not set")
	}
	// Originals byte for byte; sidecar and folder context.
	ctxBySha := map[string]string{}
	for _, m := range jacket.Media {
		if m.Media.Context != nil {
			ctxBySha[m.Media.SHA256] = *m.Media.Context
		}
	}
	dir := filepath.Dir(bundle)
	for name, wantCtx := range map[string]string{"IMG_4101.HEIC": "Bought in Tokyo", "IMG_4102.JPG": "Bedroom closet clear-out"} {
		b, _ := os.ReadFile(filepath.Join(dir, name))
		sha := sha(b)
		if !strings.HasPrefix(ctxBySha[sha], wantCtx) {
			t.Errorf("%s context = %q, want prefix %q", name, ctxBySha[sha], wantCtx)
		}
		ext := e.scalar(`SELECT ext FROM media WHERE sha256 = ?`, sha)
		stored, err := os.ReadFile(filepath.Join(e.d.Cfg.MediaDir, "orig", sha[:2], sha[2:4], sha+"."+ext))
		if err != nil || string(stored) != string(b) {
			t.Errorf("%s not stored byte for byte: %v", name, err)
		}
	}
	cables := e.item("batch-2026-11-01/usb-c-cables")
	if cables.Attrs["length"] != "1 m" {
		t.Errorf("cable attrs: %v", cables.Attrs)
	}

	// Second run: nothing uploaded, nothing written.
	updated := e.scalar(`SELECT group_concat(updated_at, ',') FROM (SELECT updated_at FROM item ORDER BY id)`)
	prodUpdated := e.scalar(`SELECT updated_at FROM product`)
	before := testutil.Mtimes(t, e.d.Cfg.DataDir, e.d.Cfg.MediaDir)
	out, err = e.ingestCLI(bundle)
	if err != nil {
		t.Fatalf("second ingest: %v\n%s", err, out)
	}
	for _, want := range []string{"media: 4 files, 0 uploaded, 4 already", "items: 0 created, 0 updated, 3 unchanged", "products: 0 created, 0 updated, 1 unchanged"} {
		if !strings.Contains(out, want) {
			t.Errorf("second run output lacks %q:\n%s", want, out)
		}
	}
	if got := e.counts(); !equalMaps(got, first) {
		t.Errorf("second run changed counts: %v -> %v", first, got)
	}
	if got := e.scalar(`SELECT group_concat(updated_at, ',') FROM (SELECT updated_at FROM item ORDER BY id)`); got != updated {
		t.Errorf("item.updated_at changed: %s -> %s", updated, got)
	}
	if got := e.scalar(`SELECT updated_at FROM product`); got != prodUpdated {
		t.Errorf("product.updated_at changed")
	}
	if after := testutil.Mtimes(t, e.d.Cfg.DataDir, e.d.Cfg.MediaDir); !equalStrMaps(before, after) {
		t.Errorf("second run wrote files:\n before %v\n after  %v", before, after)
	}
}

func sha(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }

func TestDryRunWritesNothing(t *testing.T) {
	e := newEnv(t)
	bundle := copyFixture(t)
	before := testutil.Mtimes(t, e.d.Cfg.DataDir, e.d.Cfg.MediaDir)
	c0 := e.counts()
	out, err := e.ingestCLI(bundle, "--dry-run")
	if err != nil {
		t.Fatalf("dry run: %v\n%s", err, out)
	}
	for _, want := range []string{"0 already on the server, 4 would be uploaded", "items: 3 created", "dry run: nothing was written", "(new)"} {
		if !strings.Contains(out, want) {
			t.Errorf("dry-run output lacks %q:\n%s", want, out)
		}
	}
	if c := e.counts(); !equalMaps(c, c0) {
		t.Errorf("dry run changed counts: %v -> %v", c0, c)
	}
	if after := testutil.Mtimes(t, e.d.Cfg.DataDir, e.d.Cfg.MediaDir); !equalStrMaps(before, after) {
		t.Errorf("dry run wrote files:\n before %v\n after  %v", before, after)
	}

	// After a real run, a dry run of a changed bundle reports the change
	// and still writes nothing.
	if out, err := e.ingestCLI(bundle); err != nil {
		t.Fatalf("ingest: %v\n%s", err, out)
	}
	b, _ := os.ReadFile(bundle)
	b = []byte(strings.Replace(string(b), `"title": "Navy shell jacket"`, `"title": "Navy rain shell"`, 1))
	if err := os.WriteFile(bundle, b, 0o644); err != nil {
		t.Fatal(err)
	}
	c1 := e.counts()
	out, err = e.ingestCLI(bundle, "--dry-run")
	if err != nil || !strings.Contains(out, "items: 0 created, 1 updated, 2 unchanged") || !strings.Contains(out, "title") {
		t.Errorf("dry run of a change: %v\n%s", err, out)
	}
	if e.item("batch-2026-11-01/navy-shell-jacket").Title != "Navy shell jacket" || !equalMaps(e.counts(), c1) {
		t.Error("dry run wrote the change")
	}
}

func TestValidationProblems(t *testing.T) {
	e := newEnv(t)
	bundle := `{
	  "defaults": {"location": "code:NOPE"},
	  "items": [
	    {"ext_key": "b/a", "kind": "hat", "title": "A",
	     "media": [{"sha256": "` + strings.Repeat("ab", 32) + `"}, {"media_id": "01NOTREAL"}, {"path": "./x.jpg"}],
	     "cover": 5,
	     "observations": [{"text": "x", "media": "IMG_1.HEIC"}],
	     "identifiers": [{"scheme": "rn", "value": "1", "observation": 3}],
	     "facts": [{"key": "color", "value": "red", "basis": "guess", "sources": [{"reference": 0}]}]},
	    {"ext_key": "b/a", "kind": "object", "title": ""},
	    {"ext_key": "b/c", "kind": "object", "title": "C", "product": {"ext_key": "nope/1"}, "parent": "b/missing"}
	  ]
	}`
	var errResp struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
			Details []struct {
				Path    string `json:"path"`
				Message string `json:"message"`
			} `json:"details"`
		} `json:"error"`
	}
	c0 := e.counts()
	if code := e.req("POST", "/api/ingest/bundle", json.RawMessage(bundle), &errResp); code != http.StatusUnprocessableEntity {
		t.Fatalf("status %d", code)
	}
	got := map[string]string{}
	for _, d := range errResp.Error.Details {
		got[d.Path] += d.Message + "; "
	}
	for path, frag := range map[string]string{
		"defaults.location":                      "unknown location",
		"items[0].kind":                          "must be",
		"items[0].media[0].sha256":               "unknown sha256",
		"items[0].media[1].media_id":             "unknown media_id",
		"items[0].media[2].path":                 "inv ingest",
		"items[0].cover":                         "out of range",
		"items[0].observations[0].media":         "inv ingest",
		"items[0].identifiers[0].observation":    "out of range",
		"items[0].facts[0].basis":                "must be one of",
		"items[0].facts[0].sources[0].reference": "out of range",
		"items[1].ext_key":                       "duplicate",
		"items[1].title":                         "required",
		"items[2].product.ext_key":               "unknown product",
		"items[2].parent":                        "unknown item",
	} {
		if !strings.Contains(got[path], frag) {
			t.Errorf("%s: want %q, got %q", path, frag, got[path])
		}
	}
	if errResp.Error.Code != "validation" || !strings.Contains(errResp.Error.Message, "problems") {
		t.Errorf("error: %+v", errResp.Error)
	}
	if !equalMaps(e.counts(), c0) {
		t.Error("rejected bundle wrote rows")
	}

	// The CLI exits non-zero, prints every problem and uploads nothing.
	path := copyFixture(t)
	b, _ := os.ReadFile(path)
	b = []byte(strings.Replace(string(b), `"kind": "utility"`, `"kind": "gadget"`, 1))
	b = []byte(strings.Replace(string(b), `"target_location": "code:U2"`, `"target_location": "code:U9"`, 1))
	os.WriteFile(path, b, 0o644)
	out, err := e.ingestCLI(path)
	if err == nil || !strings.Contains(out, "items[2].kind") || !strings.Contains(out, "defaults.target_location") ||
		!strings.Contains(out, "nothing was uploaded") {
		t.Errorf("CLI on a bad bundle: %v\n%s", err, out)
	}
	if !equalMaps(e.counts(), c0) {
		t.Error("CLI uploaded or wrote despite the 422")
	}

	// Missing local files are reported before anything happens.
	os.Remove(filepath.Join(filepath.Dir(path), "IMG_4104.JPG"))
	out, err = e.ingestCLI(path)
	if err == nil || !strings.Contains(out, "IMG_4104.JPG not found") {
		t.Errorf("missing file: %v\n%s", err, out)
	}
}

func TestSupersedeAndCorrections(t *testing.T) {
	e := newEnv(t)
	mk := func(color, text, value string) string {
		return `{"items": [{"ext_key": "t/shirt", "kind": "clothing", "title": "Shirt",
		  "observations": [{"source": "care_label", "text": "` + text + `", "data": {"codes": ["RN 1"], "a": 1}}],
		  "identifiers": [{"scheme": "style", "value": "` + value + `", "observation": 0}],
		  "facts": [{"key": "color", "value": "` + color + `", "basis": "inferred", "confidence": 0.6, "sources": [{"note": "photo"}]},
		            {"key": "size", "value": "M", "basis": "observed", "sources": [{"observation": 0}]}]}]}`
	}
	res := e.apply(mk("navy", "SIZE M", "AB-1"), false)
	if res.Items[0].Action != "created" {
		t.Fatalf("%+v", res)
	}
	// Same content, different key order and whitespace: unchanged.
	same := `{"items": [{"title": "Shirt", "kind": "clothing", "ext_key": "t/shirt",
	  "observations": [{"data": {"a": 1, "codes": ["RN 1"]}, "text": "SIZE M", "source": "care_label"}],
	  "identifiers": [{"value": "AB-1", "scheme": "style", "observation": 0}],
	  "facts": [{"key": "color", "value": "navy", "basis": "inferred", "confidence": 0.6, "sources": [{"note": "photo"}]},
	            {"key": "size", "value": "M", "basis": "observed", "sources": [{"observation": 0}]}]}]}`
	if res := e.apply(same, false); res.Items[0].Action != "unchanged" {
		t.Fatalf("reordered bundle: %+v", res.Items[0])
	}

	res = e.apply(mk("black", "SIZE M\\nMADE IN PERU", "AB-2"), false)
	o := res.Items[0]
	joined := strings.Join(o.Changes, "; ")
	for _, want := range []string{"fact color superseded", "1 observation replaced", "identifier style AB-2 updated", "fact size superseded"} {
		if o.Action != "updated" || !strings.Contains(joined, want) {
			t.Errorf("changes %q lack %q", joined, want)
		}
	}
	d := e.item("t/shirt")
	if d.Attrs["color"] != "black" {
		t.Errorf("attrs: %v", d.Attrs)
	}
	var oldColor, newColor *struct {
		ID, Status string
		Sup, Ext   *string
	}
	for _, f := range d.Facts {
		if f.Key != "color" {
			continue
		}
		v := &struct {
			ID, Status string
			Sup, Ext   *string
		}{f.ID, f.Status, f.SupersedesID, f.ExtKey}
		if string(f.Value) == `"navy"` {
			oldColor = v
		} else {
			newColor = v
		}
	}
	if oldColor == nil || newColor == nil || oldColor.Status != "superseded" || oldColor.Ext != nil ||
		newColor.Status != "active" || newColor.Ext == nil || *newColor.Ext != "t/shirt#fact/0" ||
		newColor.Sup == nil || *newColor.Sup != oldColor.ID {
		t.Errorf("supersede chain: old %+v new %+v", oldColor, newColor)
	}
	if len(d.Observations) != 2 {
		t.Fatalf("observations: %+v", d.Observations)
	}
	keyed := 0
	for _, ob := range d.Observations {
		if ob.ExtKey != nil {
			keyed++
			if !strings.Contains(*ob.Text, "PERU") {
				t.Errorf("ext_key stayed on the old observation")
			}
		}
	}
	if keyed != 1 || len(d.Identifiers) != 1 || d.Identifiers[0].Value != "AB-2" {
		t.Errorf("observations keyed %d, identifiers %+v", keyed, d.Identifiers)
	}
	if res := e.apply(mk("black", "SIZE M\\nMADE IN PERU", "AB-2"), false); res.Items[0].Action != "unchanged" {
		t.Errorf("re-apply after corrections: %+v", res.Items[0])
	}

	// An owner rejection is never undone by re-applying.
	var fw map[string]any
	if code := e.req("POST", "/api/facts/"+newColor.ID+"/reject", nil, &fw); code != 200 {
		t.Fatalf("reject: %d", code)
	}
	if res := e.apply(mk("black", "SIZE M\\nMADE IN PERU", "AB-2"), false); res.Items[0].Action != "unchanged" {
		t.Errorf("re-apply after reject: %+v", res.Items[0])
	}
	if _, ok := e.item("t/shirt").Attrs["color"]; ok {
		t.Error("rejected fact came back")
	}
}

func TestCreateAndUpdateSemantics(t *testing.T) {
	e := newEnv(t)
	e.location("B-1", "Box 1", "container", "U2")
	// Location already inside the target: no staging, like POST /api/items.
	b1 := `{"items": [
	  {"ext_key": "s/boxed", "kind": "object", "title": "Lamp", "location": "code:B-1", "target_location": "code:U2", "tags": ["a"]},
	  {"ext_key": "s/child", "kind": "object", "title": "Shade", "parent": "s/boxed",
	   "observations": [{"text": "E27"}], "identifiers": [{"scheme": "upc", "value": "012345678905"}, {"scheme": "rn", "value": "999"}]}
	]}`
	res := e.apply(b1, false, "X-Actor", "agent:test")
	if res.Items[0].Action != "created" || res.Items[1].Action != "created" {
		t.Fatalf("%+v", res)
	}
	lamp := e.item("s/boxed")
	if lamp.TargetID != nil || lamp.Placement.Status != "placed" || len(lamp.Movements) != 1 || lamp.Movements[0].Kind != "move" ||
		lamp.Movements[0].Actor != "agent:test" || lamp.ReviewState != "needs_review" {
		t.Errorf("lamp: target %v placement %s movements %+v review %s", lamp.TargetID, lamp.Placement.Status, lamp.Movements, lamp.ReviewState)
	}
	shade := e.item("s/child")
	if shade.ParentID == nil || *shade.ParentID != lamp.ID || shade.Observations[0].RecordedBy != "agent:test" || shade.LocationID != nil {
		t.Errorf("shade: %+v", shade)
	}

	// Owner reviews and moves the lamp; a re-applied bundle (with a changed
	// location and new tag) never undoes that and only adds tags.
	if code := e.req("PATCH", "/api/items/"+lamp.ID, map[string]any{"review_state": "ok"}, nil); code != 200 {
		t.Fatal("patch")
	}
	if code := e.req("POST", "/api/moves", map[string]any{"item_ids": []string{lamp.ID}, "action": "move", "to": "code:BEDROOM"}, nil); code != 200 {
		t.Fatalf("move: %d", code)
	}
	locBefore := e.item("s/boxed").LocationID
	if locBefore == nil || *locBefore != e.loc["BEDROOM"] {
		t.Fatalf("move did not land: %v", locBefore)
	}
	b2 := strings.Replace(b1, `"tags": ["a"]`, `"tags": ["b"], "notes": "with shade"`, 1)
	b2 = strings.Replace(b2, `"location": "code:B-1"`, `"location": "code:U2"`, 1)
	res = e.apply(b2, false)
	if res.Items[0].Action != "updated" || strings.Join(res.Items[0].Changes, ",") != "notes,+tag b" || res.Items[1].Action != "unchanged" {
		t.Errorf("update: %+v", res.Items)
	}
	lamp = e.item("s/boxed")
	if lamp.ReviewState != "ok" || strings.Join(lamp.Tags, ",") != "a,b" || !eqPtr(lamp.LocationID, locBefore) {
		t.Errorf("after update: review %s tags %v loc %v (want %v)", lamp.ReviewState, lamp.Tags, lamp.LocationID, locBefore)
	}

	// Duplicate warning: the UPC is already on another item; RN numbers are
	// company ids and never warn.
	b3 := `{"items": [{"ext_key": "s/other", "kind": "object", "title": "Other shade",
	  "identifiers": [{"scheme": "upc", "value": "0123-4567-8905"}, {"scheme": "rn", "value": "999"}]}]}`
	res = e.apply(b3, true)
	if len(res.Items[0].Warnings) != 1 || !strings.Contains(res.Items[0].Warnings[0], "s/child") ||
		!strings.Contains(res.Items[0].Warnings[0], "possible duplicate") || res.Items[0].ID != nil {
		t.Errorf("warnings: %+v", res.Items[0])
	}

	// A parent cycle is a 422.
	cyc := `{"items": [{"ext_key": "s/boxed", "kind": "object", "title": "Lamp", "parent": "s/child"}]}`
	var er map[string]any
	if code := e.req("POST", "/api/ingest/bundle", json.RawMessage(cyc), &er); code != http.StatusUnprocessableEntity {
		t.Errorf("cycle: %d %v", code, er)
	}
}

func eqPtr(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// The worked example in docs/agents/INGEST_PLAYBOOK.md is the fixture
// bundle that TestFolderIngestTwice applies.
func TestPlaybookExampleIsTheFixture(t *testing.T) {
	doc, err := os.ReadFile(filepath.Join("..", "..", "docs", "agents", "INGEST_PLAYBOOK.md"))
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile("(?s)<!-- worked-example: internal/ingest/testdata/folder/bundle.json -->\\s*```json\\n(.*?)\\n```").FindSubmatch(doc)
	if m == nil {
		t.Fatal("worked example block not found in INGEST_PLAYBOOK.md")
	}
	fixture, err := os.ReadFile(filepath.Join("testdata", "folder", "bundle.json"))
	if err != nil {
		t.Fatal(err)
	}
	var a, b any
	if err := json.Unmarshal(m[1], &a); err != nil {
		t.Fatalf("playbook example is not valid JSON: %v", err)
	}
	json.Unmarshal(fixture, &b)
	ja, _ := json.Marshal(a)
	jb, _ := json.Marshal(b)
	if string(ja) != string(jb) {
		t.Error("the playbook's worked example differs from testdata/folder/bundle.json; keep them identical")
	}
}

// The enrich example in docs/agents/ENRICH_PLAYBOOK.md applies on top of
// the ingest example, and a positional ext_key collision is caught.
func TestEnrichPlaybookExample(t *testing.T) {
	e := newEnv(t)
	bundle := copyFixture(t)
	if out, err := e.ingestCLI(bundle); err != nil {
		t.Fatalf("ingest: %v\n%s", err, out)
	}
	doc, err := os.ReadFile(filepath.Join("..", "..", "docs", "agents", "ENRICH_PLAYBOOK.md"))
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile("(?s)<!-- enrich-example:[^>]*-->\\s*```json\\n(.*?)\\n```").FindSubmatch(doc)
	if m == nil {
		t.Fatal("enrich example block not found")
	}
	dir := filepath.Dir(bundle)
	enrich := filepath.Join(dir, "enrich.json")
	os.WriteFile(enrich, m[1], 0o644)
	os.WriteFile(filepath.Join(dir, "uniqlo-341-456789.html"), []byte("<!doctype html><html><title>BLOCKTECH Parka | UNIQLO US</title><p>341-456789</p></html>\n"), 0o644)
	stock, _ := os.ReadFile(filepath.Join(dir, "IMG_4104.JPG"))
	os.WriteFile(filepath.Join(dir, "stock-341-456789-1.jpg"), append(stock[:len(stock):len(stock)], 0), 0o644) // distinct bytes

	out, err := e.ingestCLI(enrich)
	if err != nil || !strings.Contains(out, "products: 0 created, 1 updated") || !strings.Contains(out, "2 uploaded") {
		t.Fatalf("enrich: %v\n%s", err, out)
	}
	jacket := e.item("batch-2026-11-01/navy-shell-jacket")
	if jacket.Attrs["style_name"] != "BLOCKTECH Parka" || jacket.Attrs["season"] != "2023 Spring/Summer" {
		t.Errorf("attrs after enrich: %v", jacket.Attrs)
	}
	if n := e.scalar(`SELECT count(*) FROM fact WHERE subject_type = 'product' AND status = 'active'`); n != "4" {
		t.Errorf("active product facts = %s, want 4 (season kept)", n)
	}
	if out, err := e.ingestCLI(enrich); err != nil || !strings.Contains(out, "products: 0 created, 0 updated, 1 unchanged") {
		t.Errorf("enrich re-apply: %v\n%s", err, out)
	}

	// Without explicit ext_keys the first fact would collide with the
	// ingest bundle's season fact: rejected, not silently superseded.
	bad := `{"products": [{"ext_key": "uniqlo/341-456789/23SS", "facts": [{"key": "style_name", "value": "X", "basis": "looked_up"}]}], "items": []}`
	var er struct {
		Error struct {
			Details []struct{ Path, Message string } `json:"details"`
		} `json:"error"`
	}
	if code := e.req("POST", "/api/ingest/bundle", json.RawMessage(bad), &er); code != http.StatusUnprocessableEntity ||
		len(er.Error.Details) != 1 || !strings.Contains(er.Error.Details[0].Message, "own ext_key") {
		t.Errorf("collision: %d %+v", code, er)
	}
}
