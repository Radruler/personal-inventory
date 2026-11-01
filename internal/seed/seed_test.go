package seed_test

import (
	"context"
	"os"
	"testing"

	"github.com/radruler/personal-inventory/internal/items"
	"github.com/radruler/personal-inventory/internal/media"
	"github.com/radruler/personal-inventory/internal/seed"
	"github.com/radruler/personal-inventory/internal/testutil"
)

func TestDemoSeed(t *testing.T) {
	if testing.Short() {
		t.Skip("generates ~100 images through vips")
	}
	d := testutil.NewDeps(t)
	ctx := context.Background()
	n, err := seed.Demo(ctx, d)
	if err != nil {
		t.Fatal(err)
	}
	if n < 55 {
		t.Fatalf("seeded %d items, want ~60", n)
	}
	if _, err := seed.Demo(ctx, d); err == nil {
		t.Fatalf("second seed should refuse a non-empty database")
	}

	var ids []string
	rows, err := d.DB.QueryContext(ctx, `SELECT id FROM item ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var id string
		rows.Scan(&id)
		ids = append(ids, id)
	}
	rows.Close()
	sums, err := items.Summaries(ctx, d.DB, ids)
	if err != nil || len(sums) != n {
		t.Fatalf("summaries: %d, %v", len(sums), err)
	}
	status := map[string]int{}
	covers := 0
	for _, s := range sums {
		status[s.Placement.Status]++
		if s.Cover != nil {
			covers++
			if s.Cover.ThumbURL == nil {
				t.Errorf("%s: cover without thumb", s.Title)
			} else if _, err := os.Stat(media.ThumbPath(d.Cfg, s.Cover.SHA256)); err != nil {
				t.Errorf("%s: thumb missing: %v", s.Title, err)
			}
		}
	}
	for _, st := range []string{"gone", "staged", "unknown", "approximate", "placed"} {
		if status[st] == 0 {
			t.Errorf("no demo item with placement %q (%v)", st, status)
		}
	}
	if covers == 0 || covers == n {
		t.Errorf("covers = %d of %d; want some items without photos", covers, n)
	}
	var hits int
	if err := d.DB.QueryRowContext(ctx, `SELECT count(*) FROM item_fts WHERE item_fts MATCH 'uniqlo'`).Scan(&hits); err != nil || hits == 0 {
		t.Errorf("fts 'uniqlo' hits = %d, %v", hits, err)
	}
}
