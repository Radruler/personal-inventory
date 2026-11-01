package media

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/radruler/personal-inventory/internal/db"
	"github.com/radruler/personal-inventory/internal/model"
	"github.com/radruler/personal-inventory/internal/testutil"
)

func TestGetBySha(t *testing.T) {
	_, h := newEnv(t)
	up := upload(t, h, 201, fileField("a.png", pngBytes(t, gradient(8, 8, 1))))
	rec := get(t, h, "HEAD", "/api/media/sha256/"+up.SHA256, nil)
	if rec.Code != 200 || rec.Header().Get("X-Media-Id") != up.ID || rec.Body.Len() != 0 {
		t.Errorf("HEAD known: %d %q %d bytes", rec.Code, rec.Header().Get("X-Media-Id"), rec.Body.Len())
	}
	if rec := get(t, h, "HEAD", "/api/media/sha256/"+strings.Repeat("a", 64), nil); rec.Code != 404 || rec.Body.Len() != 0 {
		t.Errorf("HEAD unknown: %d", rec.Code)
	}
	var m Media
	if rec := testutil.Do(t, h, "GET", "/api/media/sha256/"+up.SHA256, nil, &m); rec.Code != 200 || m.ID != up.ID || rec.Header().Get("X-Media-Id") != up.ID {
		t.Errorf("GET by sha: %d %+v", rec.Code, m)
	}
	if rec := testutil.Do(t, h, "GET", "/api/media/sha256/"+strings.Repeat("b", 64), nil, nil); rec.Code != 404 {
		t.Errorf("GET unknown sha: %d", rec.Code)
	}
	if rec := testutil.Do(t, h, "GET", "/api/media/sha256/xyz", nil, nil); rec.Code != 400 {
		t.Errorf("bad sha: %d", rec.Code)
	}
	if rec := testutil.Do(t, h, "GET", "/api/media/nope", nil, nil); rec.Code != 404 {
		t.Errorf("GET unknown id: %d", rec.Code)
	}
}

func TestPatchContext(t *testing.T) {
	_, h := newEnv(t)
	up := upload(t, h, 201, fileField("a.png", pngBytes(t, gradient(8, 8, 2))))
	var m Media
	testutil.Do(t, h, "PATCH", "/api/media/"+up.ID, map[string]any{"context": " bought in Tokyo 2019 "}, &m)
	if m.Context == nil || *m.Context != "bought in Tokyo 2019" {
		t.Errorf("set: %v", m.Context)
	}
	testutil.Do(t, h, "PATCH", "/api/media/"+up.ID, map[string]any{}, &m)
	if m.Context == nil {
		t.Error("absent field cleared context")
	}
	testutil.Do(t, h, "PATCH", "/api/media/"+up.ID, map[string]any{"context": nil}, &m)
	if m.Context != nil {
		t.Errorf("null should clear: %v", *m.Context)
	}
	if rec := testutil.Do(t, h, "PATCH", "/api/media/"+up.ID, map[string]any{"caption": "x"}, nil); rec.Code != 400 {
		t.Errorf("unknown field: %d", rec.Code)
	}
	if rec := testutil.Do(t, h, "PATCH", "/api/media/nope", map[string]any{"context": "x"}, nil); rec.Code != 404 {
		t.Errorf("unknown media: %d", rec.Code)
	}
}

func TestLinksAndDeleteRules(t *testing.T) {
	d, h := newEnv(t)
	exec1(t, d, `INSERT INTO item (id, kind, title) VALUES ('it1', 'clothing', 'Jacket'), ('it2', 'object', 'Lamp')`)
	exec1(t, d, `INSERT INTO product (id, name) VALUES ('pr1', 'AIRism Crew Neck T')`)
	exec1(t, d, `INSERT INTO location (id, code, name, kind) VALUES ('loc1', 'B-014', 'Box 14', 'container')`)
	up := upload(t, h, 201, fileField("a.png", pngBytes(t, gradient(32, 32, 3))))
	process(t, d)
	links := "/api/media/" + up.ID + "/links"

	var l Link
	if rec := testutil.Do(t, h, "POST", links, map[string]any{"subject_type": "item", "subject_id": "it1"}, &l); rec.Code != 200 {
		t.Fatalf("link: %d %s", rec.Code, rec.Body.String())
	}
	if l.Role != "other" || l.Sort != 0 || l.Caption != nil || l.MediaID != up.ID || l.CreatedAt == "" {
		t.Errorf("new link %+v", l)
	}
	// Upsert: same subject updates role/caption, keeps sort.
	testutil.Do(t, h, "POST", links, map[string]any{"subject_type": "item", "subject_id": "it1", "role": "flat_lay", "caption": "front"}, &l)
	if l.Role != "flat_lay" || l.Sort != 0 || l.Caption == nil || *l.Caption != "front" {
		t.Errorf("upserted link %+v", l)
	}
	testutil.Do(t, h, "POST", links, map[string]any{"subject_type": "item", "subject_id": "it1", "caption": nil}, &l)
	if l.Role != "flat_lay" || l.Caption != nil {
		t.Errorf("caption null should clear, role kept: %+v", l)
	}
	// Location by code, product by id.
	testutil.Do(t, h, "POST", links, map[string]any{"subject_type": "location", "subject_id": "code:b-014", "role": "box_contents"}, &l)
	if l.SubjectID != "loc1" {
		t.Errorf("location code not resolved: %+v", l)
	}
	testutil.Do(t, h, "POST", links, map[string]any{"subject_type": "product", "subject_id": "pr1", "role": "stock", "sort": 4}, &l)
	if l.Sort != 4 {
		t.Errorf("explicit sort: %+v", l)
	}
	for _, c := range []struct {
		body map[string]any
		code int
	}{
		{map[string]any{"subject_type": "item", "subject_id": "missing"}, 422},
		{map[string]any{"subject_type": "location", "subject_id": "code:NOPE"}, 422},
		{map[string]any{"subject_type": "box", "subject_id": "it1"}, 422},
		{map[string]any{"subject_type": "item"}, 422},
		{map[string]any{"subject_type": "item", "subject_id": "it1", "colour": "red"}, 400},
	} {
		if rec := testutil.Do(t, h, "POST", links, c.body, nil); rec.Code != c.code {
			t.Errorf("%v: %d %s, want %d", c.body, rec.Code, rec.Body.String(), c.code)
		}
	}
	if rec := testutil.Do(t, h, "POST", "/api/media/nope/links", map[string]any{"subject_type": "item", "subject_id": "it1"}, nil); rec.Code != 404 {
		t.Errorf("link unknown media: %d", rec.Code)
	}
	m := getMedia(t, h, up.ID)
	if len(m.Links) != 3 {
		t.Errorf("links %+v", m.Links)
	}

	del := "/api/media/" + up.ID
	expect409 := func(why string) {
		t.Helper()
		if rec := testutil.Do(t, h, "DELETE", del, nil, nil); rec.Code != 409 {
			t.Errorf("delete while %s: %d", why, rec.Code)
		}
	}
	expect409("linked")
	for _, q := range []string{"?subject_type=item&subject_id=it1", "?subject_type=location&subject_id=code:B-014", "?subject_type=product&subject_id=pr1"} {
		if rec := testutil.Do(t, h, "DELETE", links+q, nil, nil); rec.Code != 204 {
			t.Errorf("unlink %s: %d %s", q, rec.Code, rec.Body.String())
		}
	}
	if rec := testutil.Do(t, h, "DELETE", links+"?subject_type=item&subject_id=it1", nil, nil); rec.Code != 404 {
		t.Errorf("unlink twice: %d", rec.Code)
	}
	if rec := testutil.Do(t, h, "DELETE", links+"?subject_type=item", nil, nil); rec.Code != 400 {
		t.Errorf("unlink without subject_id: %d", rec.Code)
	}

	// Other uses block deletion too.
	for _, use := range []struct{ why, set, unset string }{
		{"observation source", `INSERT INTO observation (id, item_id, media_id) VALUES ('ob1', 'it1', ?)`, `DELETE FROM observation`},
		{"reference archive", `INSERT INTO reference (id, subject_type, subject_id, archive_media_id) VALUES ('rf1', 'item', 'it1', ?)`, `DELETE FROM reference`},
		{"item cover", `UPDATE item SET cover_media_id = ? WHERE id = 'it2'`, `UPDATE item SET cover_media_id = NULL`},
		{"location cover", `UPDATE location SET cover_media_id = ? WHERE id = 'loc1'`, `UPDATE location SET cover_media_id = NULL`},
	} {
		exec1(t, d, use.set, up.ID)
		expect409(use.why)
		exec1(t, d, use.unset)
	}

	if rec := testutil.Do(t, h, "DELETE", del, nil, nil); rec.Code != 204 {
		t.Fatalf("delete unused: %d %s", rec.Code, rec.Body.String())
	}
	for _, p := range []string{OrigPath(d.Cfg, up.SHA256, "png"), ThumbPath(d.Cfg, up.SHA256), DisplayPath(d.Cfg, up.SHA256)} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("%s still exists", p)
		}
	}
	if rec := testutil.Do(t, h, "GET", del, nil, nil); rec.Code != 404 {
		t.Errorf("GET deleted: %d", rec.Code)
	}
	if rec := testutil.Do(t, h, "DELETE", del, nil, nil); rec.Code != 404 {
		t.Errorf("delete twice: %d", rec.Code)
	}
}

func TestInboxOrderingAndPaging(t *testing.T) {
	d, h := newEnv(t)
	exec1(t, d, `INSERT INTO item (id, kind, title, review_state, created_at) VALUES
		('old', 'clothing', 'Old', 'needs_review', '2026-10-01T00:00:00.000Z'),
		('new', 'clothing', 'New', 'needs_review', '2026-10-02T00:00:00.000Z'),
		('ok',  'clothing', 'Fine', 'ok', '2026-10-03T00:00:00.000Z')`)
	insert := func(id, taken, created string) {
		var tk any
		if taken != "" {
			tk = taken
		}
		exec1(t, d, `INSERT INTO media (id, sha256, mime, ext, bytes, taken_at, variant_status, created_at)
			VALUES (?, ?, 'image/jpeg', 'jpg', 1, ?, 'ready', ?)`, id, shaHex([]byte(id)), tk, created)
	}
	insert("m-late", "2026-10-30T12:00:02.000Z", "2026-11-01T00:00:01.000Z")
	insert("m-early", "2026-10-30T12:00:00.000Z", "2026-11-01T00:00:03.000Z")
	insert("m-tie-b", "2026-10-30T12:00:01.000Z", "2026-11-01T00:00:05.000Z")
	insert("m-tie-a", "2026-10-30T12:00:01.000Z", "2026-11-01T00:00:04.000Z")
	insert("m-none1", "", "2026-11-01T00:00:00.000Z")
	insert("m-none2", "", "2026-11-01T00:00:09.000Z")
	// Used media is not in the Inbox.
	insert("u-link", "2026-01-01T00:00:00.000Z", "2026-11-01T00:00:00.000Z")
	insert("u-obs", "2026-01-01T00:00:00.000Z", "2026-11-01T00:00:00.000Z")
	insert("u-ref", "2026-01-01T00:00:00.000Z", "2026-11-01T00:00:00.000Z")
	insert("u-cover", "2026-01-01T00:00:00.000Z", "2026-11-01T00:00:00.000Z")
	exec1(t, d, `INSERT INTO media_link (media_id, subject_type, subject_id) VALUES ('u-link', 'item', 'old')`)
	exec1(t, d, `INSERT INTO observation (id, item_id, media_id) VALUES ('o1', 'old', 'u-obs')`)
	exec1(t, d, `INSERT INTO reference (id, subject_type, subject_id, archive_media_id) VALUES ('r1', 'item', 'old', 'u-ref')`)
	exec1(t, d, `UPDATE item SET cover_media_id = 'u-cover' WHERE id = 'ok'`)

	var gotIDs []string
	d.ItemSummaries = func(ctx context.Context, q db.Querier, ids []string) ([]model.ItemSummary, error) {
		gotIDs = ids
		out := []model.ItemSummary{}
		for _, id := range ids {
			out = append(out, model.ItemSummary{ID: id, Tags: []string{}, Attrs: map[string]any{}})
		}
		return out, nil
	}

	want := []string{"m-early", "m-tie-a", "m-tie-b", "m-late", "m-none1", "m-none2"}
	var seen []string
	cursor := ""
	pages := 0
	for {
		path := "/api/inbox?limit=4"
		if cursor != "" {
			path += "&cursor=" + cursor
		}
		var in Inbox
		var raw map[string]json.RawMessage
		rec := testutil.Do(t, h, "GET", path, nil, &raw)
		if rec.Code != 200 {
			t.Fatalf("%s: %d %s", path, rec.Code, rec.Body.String())
		}
		json.Unmarshal(rec.Body.Bytes(), &in)
		for _, k := range []string{"media", "next_cursor", "media_count", "review_items", "review_count"} {
			if _, ok := raw[k]; !ok {
				t.Errorf("inbox lacks %s", k)
			}
		}
		if in.MediaCount != 6 || in.ReviewCount != 2 {
			t.Errorf("counts %d/%d, want 6/2", in.MediaCount, in.ReviewCount)
		}
		if len(in.ReviewItems) != 2 || in.ReviewItems[0].ID != "new" || in.ReviewItems[1].ID != "old" {
			t.Errorf("review items %v (loader got %v)", in.ReviewItems, gotIDs)
		}
		for _, m := range in.Media {
			seen = append(seen, m.ID)
			if m.Links == nil {
				t.Error("links must be []")
			}
		}
		pages++
		if in.NextCursor == nil {
			break
		}
		cursor = *in.NextCursor
		if pages > 5 {
			t.Fatal("paging does not end")
		}
	}
	if strings.Join(seen, ",") != strings.Join(want, ",") || pages != 2 {
		t.Errorf("order %v in %d pages, want %v in 2", seen, pages, want)
	}
	if rec := testutil.Do(t, h, "GET", "/api/inbox?cursor=not-a-cursor!", nil, nil); rec.Code != 400 {
		t.Errorf("bad cursor: %d", rec.Code)
	}

	// Without an item loader review_items is still [].
	d.ItemSummaries = nil
	var raw map[string]json.RawMessage
	testutil.Do(t, h, "GET", "/api/inbox", nil, &raw)
	if string(raw["review_items"]) != "[]" {
		t.Errorf("review_items %s", raw["review_items"])
	}
}
