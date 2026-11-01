// Package seed fills an empty database with demo content (`inv seed --demo`)
// so the frontend has realistic data: ~60 items across kinds, locations and
// placement states, with generated placeholder photos run through vips.
package seed

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/radruler/personal-inventory/internal/db"
	"github.com/radruler/personal-inventory/internal/httpapi"
	"github.com/radruler/personal-inventory/internal/items"
	"github.com/radruler/personal-inventory/internal/media"
)

// Run implements `inv seed`.
func Run(ctx context.Context, d *httpapi.Deps, args []string) error {
	fs := flag.NewFlagSet("seed", flag.ContinueOnError)
	demo := fs.Bool("demo", false, "insert demo content")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if !*demo {
		return errors.New("usage: inv seed --demo")
	}
	n, err := Demo(ctx, d)
	if err != nil {
		return err
	}
	d.Log.Info("seeded demo data", "items", n)
	return nil
}

// Demo inserts the demo dataset. It refuses to run on a database that
// already has items or locations. It returns the number of items created.
func Demo(ctx context.Context, d *httpapi.Deps) (int, error) {
	var count int
	if err := d.DB.QueryRowContext(ctx, `SELECT (SELECT count(*) FROM item) + (SELECT count(*) FROM location)`).Scan(&count); err != nil {
		return 0, err
	}
	if count > 0 {
		return 0, errors.New("seed --demo needs an empty database")
	}
	s := &seeder{d: d, rng: rand.New(rand.NewPCG(42, 2026)), base: time.Date(2026, 9, 1, 9, 0, 0, 0, time.UTC)}
	err := d.DB.Tx(ctx, func(tx *sql.Tx) error {
		s.tx = tx
		return s.run(ctx)
	})
	return s.items, err
}

type seeder struct {
	d     *httpapi.Deps
	tx    *sql.Tx
	rng   *rand.Rand
	base  time.Time
	tick  int
	items int
	loc   map[string]string // code -> id
}

func (s *seeder) now() string {
	s.tick++
	return db.FormatTime(s.base.Add(time.Duration(s.tick) * 17 * time.Minute))
}

func (s *seeder) exec(ctx context.Context, q string, args ...any) error {
	_, err := s.tx.ExecContext(ctx, q, args...)
	return err
}

type locSpec struct{ code, name, kind, parent string }

var demoLocations = []locSpec{
	{"HOME", "Home", "site", ""},
	{"BEDROOM", "Bedroom", "room", "HOME"},
	{"CLOSET", "Hall closet", "room", "HOME"},
	{"OFFICE", "Office", "room", "HOME"},
	{"SH-1", "Office shelf", "shelf", "OFFICE"},
	{"GARAGE", "Garage", "room", "HOME"},
	{"B-020", "Packing box (garage)", "container", "GARAGE"},
	{"SS", "StoreSafe Eastside", "site", ""},
	{"U1", "Unit 1", "unit", "SS"},
	{"U2", "Unit 2", "unit", "SS"},
	{"B-001", "Winter coats", "container", "U2"},
	{"B-002", "Sweaters", "container", "U2"},
	{"B-003", "Summer shirts", "container", "U2"},
	{"B-004", "Shoes", "container", "U2"},
	{"B-005", "Formal", "container", "U2"},
	{"B-006", "Cables & chargers", "container", "U1"},
	{"B-007", "Kitchen extras", "container", "U1"},
}

type itemSpec struct {
	kind, title, category string
	brand, color, size    string
	loc, target           string // location codes; "" = unknown / not staged
	photos                int
	review                bool
	gone                  string // gone reason
	qty                   int
	label                 string // care-label text → observation (+ identifiers)
	tags                  []string
}

var palette = map[string]color.RGBA{
	"navy": {0x1f, 0x2a, 0x44, 255}, "black": {0x22, 0x22, 0x24, 255}, "grey": {0x8a, 0x8d, 0x91, 255},
	"olive": {0x6b, 0x6f, 0x3a, 255}, "white": {0xee, 0xec, 0xe6, 255}, "cream": {0xe8, 0xdc, 0xc2, 255},
	"red": {0xa8, 0x32, 0x32, 255}, "blue": {0x3d, 0x6f, 0xb4, 255}, "brown": {0x6e, 0x4b, 0x32, 255},
	"green": {0x3f, 0x7a, 0x4f, 255}, "tan": {0xc2, 0xa0, 0x74, 255}, "pink": {0xd9, 0x8f, 0xa0, 255},
	"silver": {0xb8, 0xbc, 0xc2, 255}, "yellow": {0xd8, 0xb4, 0x3a, 255},
}

var demoItems = []itemSpec{
	{kind: "clothing", title: "Navy shell jacket", category: "outerwear/shell", brand: "Uniqlo", color: "navy", size: "M", loc: "B-001", photos: 2,
		label: "SHELL: 100% NYLON\nLINING: 100% POLYESTER\nRN 12345\nMADE IN VIETNAM\n341-456789(23SS)", tags: []string{"rain"}},
	{kind: "clothing", title: "Black wool overcoat", category: "outerwear/coat", brand: "COS", color: "black", size: "48", loc: "B-001", photos: 2},
	{kind: "clothing", title: "Olive down parka", category: "outerwear/parka", brand: "Patagonia", color: "olive", size: "L", loc: "B-001", photos: 1, tags: []string{"winter"}},
	{kind: "clothing", title: "Grey fleece pullover", category: "tops/fleece", brand: "Patagonia", color: "grey", size: "M", loc: "B-001", photos: 1},
	{kind: "clothing", title: "Cream cable-knit sweater", category: "tops/sweater", brand: "J.Crew", color: "cream", size: "M", loc: "B-002", photos: 2, tags: []string{"wool"}},
	{kind: "clothing", title: "Navy merino crewneck", category: "tops/sweater", brand: "Uniqlo", color: "navy", size: "M", loc: "B-002", photos: 1,
		label: "100% WOOL (EXTRA FINE MERINO)\nRN 12345\nMADE IN CHINA\n450-512345(22FW)"},
	{kind: "clothing", title: "Grey lambswool cardigan", category: "tops/cardigan", brand: "Muji", color: "grey", size: "L", loc: "B-002", photos: 1},
	{kind: "clothing", title: "Red Fair Isle sweater", category: "tops/sweater", color: "red", size: "M", loc: "B-002", photos: 1, tags: []string{"holiday"}},
	{kind: "clothing", title: "Brown corduroy overshirt", category: "tops/overshirt", brand: "Gap", color: "brown", size: "M", loc: "B-002", photos: 1},
	{kind: "clothing", title: "White linen shirt", category: "tops/shirt", brand: "Uniqlo", color: "white", size: "M", loc: "B-003", photos: 2,
		label: "100% LINEN\nRN 12345\nMADE IN CHINA\n455-334412(24SS)"},
	{kind: "clothing", title: "Blue chambray shirt", category: "tops/shirt", brand: "Levi's", color: "blue", size: "M", loc: "B-003", photos: 1},
	{kind: "clothing", title: "Green Hawaiian shirt", category: "tops/shirt", color: "green", size: "L", loc: "B-003", photos: 1, tags: []string{"vacation"}},
	{kind: "clothing", title: "Tan chino shorts", category: "bottoms/shorts", brand: "J.Crew", color: "tan", size: "32", loc: "B-003", photos: 1},
	{kind: "clothing", title: "Pink oxford shirt", category: "tops/shirt", brand: "Brooks Brothers", color: "pink", size: "15.5", loc: "B-003", photos: 1},
	{kind: "clothing", title: "Striped breton tee", category: "tops/tee", brand: "Saint James", color: "navy", size: "M", loc: "B-003", photos: 2},
	{kind: "clothing", title: "Brown leather boots", category: "shoes/boots", brand: "Red Wing", color: "brown", size: "US 10", loc: "B-004", photos: 2},
	{kind: "clothing", title: "White canvas sneakers", category: "shoes/sneakers", brand: "Converse", color: "white", size: "US 10", loc: "B-004", photos: 1},
	{kind: "clothing", title: "Black dress shoes", category: "shoes/dress", brand: "Allen Edmonds", color: "black", size: "US 10", loc: "B-004", photos: 1},
	{kind: "clothing", title: "Navy suit jacket", category: "suits/jacket", brand: "Suitsupply", color: "navy", size: "40R", loc: "B-005", photos: 2},
	{kind: "clothing", title: "Navy suit trousers", category: "suits/trousers", brand: "Suitsupply", color: "navy", size: "32", loc: "B-005", photos: 1},
	{kind: "clothing", title: "Silk tie, burgundy", category: "accessories/tie", color: "red", loc: "B-005", photos: 1},
	{kind: "clothing", title: "Grey wool trousers", category: "bottoms/trousers", brand: "COS", color: "grey", size: "32", loc: "U2", photos: 1},
	{kind: "clothing", title: "Black puffer vest", category: "outerwear/vest", brand: "Uniqlo", color: "black", size: "M", loc: "U2", photos: 1},
	{kind: "clothing", title: "Olive field jacket", category: "outerwear/jacket", brand: "Alpha Industries", color: "olive", size: "M", loc: "U2", photos: 2},
	{kind: "clothing", title: "Blue raw denim jeans", category: "bottoms/jeans", brand: "Levi's", color: "blue", size: "32x32", loc: "BEDROOM", target: "U2", photos: 2},
	{kind: "clothing", title: "Grey hoodie", category: "tops/hoodie", brand: "Champion", color: "grey", size: "L", loc: "BEDROOM", target: "U2", photos: 1},
	{kind: "clothing", title: "Black turtleneck", category: "tops/sweater", brand: "Uniqlo", color: "black", size: "M", loc: "BEDROOM", target: "U2", photos: 1},
	{kind: "clothing", title: "Cream wool scarf", category: "accessories/scarf", color: "cream", loc: "CLOSET", target: "U2", photos: 1},
	{kind: "clothing", title: "Navy beanie", category: "accessories/hat", color: "navy", loc: "CLOSET", target: "U2", photos: 0},
	{kind: "clothing", title: "Yellow rain jacket", category: "outerwear/shell", brand: "Stutterheim", color: "yellow", size: "M", loc: "CLOSET", target: "U2", photos: 1},
	{kind: "clothing", title: "Black running shorts", category: "bottoms/shorts", brand: "Nike", color: "black", size: "M", loc: "BEDROOM", photos: 1},
	{kind: "clothing", title: "White tees", category: "tops/tee", brand: "Uniqlo", color: "white", size: "M", loc: "BEDROOM", photos: 1, qty: 4},
	{kind: "clothing", title: "Grey sweatpants", category: "bottoms/sweatpants", color: "grey", size: "L", loc: "BEDROOM", photos: 0},
	{kind: "clothing", title: "Brown leather belt", category: "accessories/belt", color: "brown", loc: "CLOSET", photos: 1},
	{kind: "clothing", title: "Wool socks", category: "accessories/socks", brand: "Darn Tough", color: "grey", loc: "BEDROOM", photos: 0, qty: 6},
	{kind: "clothing", title: "Green flannel shirt", category: "tops/shirt", color: "green", size: "M", photos: 1, review: true},
	{kind: "clothing", title: "Blue denim jacket", category: "outerwear/jacket", color: "blue", size: "M", photos: 2, review: true},
	{kind: "clothing", title: "Black jacket (unsure)", category: "outerwear", color: "black", photos: 1, review: true},
	{kind: "clothing", title: "Red windbreaker", category: "outerwear/shell", brand: "The North Face", color: "red", size: "M", loc: "B-020", photos: 1},
	{kind: "clothing", title: "Tan trench coat", category: "outerwear/coat", brand: "Burberry", color: "tan", size: "50", gone: "sold", photos: 2},
	{kind: "clothing", title: "Pink sweatshirt", category: "tops/sweatshirt", color: "pink", size: "M", gone: "donated", photos: 1},
	{kind: "object", title: "Polaroid SX-70 camera", category: "electronics/camera", brand: "Polaroid", color: "silver", loc: "SH-1", photos: 2, tags: []string{"vintage"}},
	{kind: "object", title: "Stand mixer", category: "kitchen/appliance", brand: "KitchenAid", color: "red", loc: "B-007", photos: 1},
	{kind: "object", title: "Cast iron skillet", category: "kitchen/cookware", brand: "Lodge", color: "black", loc: "B-007", photos: 1},
	{kind: "object", title: "Ceramic vase", category: "decor/vase", color: "cream", loc: "OFFICE", photos: 1},
	{kind: "object", title: "Framed print, mountains", category: "decor/art", color: "blue", loc: "U1", photos: 1},
	{kind: "object", title: "Paperback box: sci-fi", category: "books", loc: "U1", photos: 1, qty: 1},
	{kind: "object", title: "Desk lamp", category: "decor/lighting", brand: "IKEA", color: "white", loc: "OFFICE", photos: 1},
	{kind: "object", title: "Camping tent, 2-person", category: "outdoor/tent", brand: "REI", color: "green", loc: "GARAGE", target: "U1", photos: 1},
	{kind: "object", title: "Record player", category: "electronics/audio", brand: "Audio-Technica", color: "black", loc: "OFFICE", photos: 2},
	{kind: "object", title: "Old iPhone", category: "electronics/phone", brand: "Apple", color: "silver", photos: 1},
	{kind: "object", title: "Bike helmet", category: "outdoor/bike", brand: "Giro", color: "black", loc: "GARAGE", photos: 0},
	{kind: "object", title: "Tool box", category: "tools", brand: "Stanley", color: "yellow", loc: "GARAGE", photos: 1, review: true},
	{kind: "utility", title: "USB-C cable 1m", category: "cable/usb-c", brand: "Anker", color: "black", loc: "B-006", photos: 1, qty: 4},
	{kind: "utility", title: "HDMI cable 2m", category: "cable/hdmi", color: "black", loc: "B-006", photos: 0, qty: 2},
	{kind: "utility", title: "USB wall chargers", category: "power/charger", brand: "Apple", color: "white", loc: "B-006", photos: 1, qty: 3},
	{kind: "utility", title: "AA batteries", category: "power/battery", brand: "Eneloop", color: "white", loc: "SH-1", photos: 1, qty: 16},
	{kind: "utility", title: "Extension cord", category: "power/extension", color: "white", loc: "GARAGE", photos: 0},
	{kind: "utility", title: "Bed sheets, queen", category: "linens/sheets", color: "white", loc: "CLOSET", photos: 1, qty: 2},
	{kind: "utility", title: "Bath towels", category: "linens/towels", color: "grey", loc: "CLOSET", target: "U1", photos: 1, qty: 4},
	{kind: "utility", title: "Packing tape", category: "supplies/tape", loc: "B-020", photos: 0, qty: 3},
}

// inboxContexts are unlinked photos waiting for an agent.
var inboxContexts = []struct{ color, context string }{
	{"olive", "the olive one, bought in Tokyo 2019"},
	{"navy", ""},
	{"brown", "grandpa's, keep forever"},
	{"blue", "goes with the denim jacket"},
}

func (s *seeder) run(ctx context.Context) error {
	s.loc = map[string]string{}
	for _, l := range demoLocations {
		id := db.NewID()
		var parent any
		if l.parent != "" {
			parent = s.loc[l.parent]
		}
		ts := s.now()
		if err := s.exec(ctx, `INSERT INTO location (id, code, name, kind, parent_id, created_at, updated_at) VALUES (?,?,?,?,?,?,?)`,
			id, l.code, l.name, l.kind, parent, ts, ts); err != nil {
			return fmt.Errorf("location %s: %w", l.code, err)
		}
		s.loc[l.code] = id
	}

	// One product shared by the Uniqlo shell jacket.
	productID := db.NewID()
	ts := s.now()
	if err := s.exec(ctx, `INSERT INTO product (id, brand, name, attrs, ext_key, created_at, updated_at) VALUES (?,?,?,?,?,?,?)`,
		productID, "Uniqlo", "Blocktech Parka (2023)", `{"retail_price":{"amount":79.9,"currency":"USD"},"season":"2023 Spring/Summer"}`,
		"uniqlo/341-456789/23SS", ts, ts); err != nil {
		return err
	}
	for _, f := range []struct {
		key   string
		value any
		basis string
		conf  float64
	}{
		{"retail_price", map[string]any{"amount": 79.9, "currency": "USD"}, "looked_up", 0.7},
		{"season", "2023 Spring/Summer", "inferred", 0.8},
	} {
		if err := s.fact(ctx, "product", productID, f.key, f.value, f.basis, f.conf, `[{"note":"demo seed"}]`); err != nil {
			return err
		}
	}

	for i, it := range demoItems {
		if err := s.item(ctx, i, it, productID); err != nil {
			return fmt.Errorf("item %q: %w", it.title, err)
		}
	}

	for i, ib := range inboxContexts {
		mid, err := s.photo(ctx, ib.color, 1000+i, "inbox")
		if err != nil {
			return err
		}
		if ib.context != "" {
			if err := s.exec(ctx, `UPDATE media SET context = ? WHERE id = ?`, ib.context, mid); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *seeder) item(ctx context.Context, i int, it itemSpec, productID string) error {
	id := db.NewID()
	ts := s.now()
	qty := it.qty
	if qty == 0 {
		qty = 1
	}
	review := "ok"
	if it.review {
		review = "needs_review"
	}
	recordedBy := "user"
	if it.review {
		recordedBy = "agent:demo-seed"
	}
	var loc, target, lifecycle, goneReason, goneAt, product any = nil, nil, "owned", nil, nil, nil
	if it.loc != "" {
		loc = s.loc[it.loc]
	}
	if it.target != "" {
		target = s.loc[it.target]
	}
	if it.gone != "" {
		lifecycle, goneReason, goneAt = "gone", it.gone, db.FormatTime(s.base.AddDate(0, -2, i))
	}
	if strings.Contains(it.label, "341-456789") {
		product = productID
	}

	attrs := map[string]any{}
	if it.brand != "" {
		attrs["brand"] = it.brand
	}
	if it.color != "" {
		attrs["color"] = it.color
	}
	if it.size != "" {
		attrs["size"] = it.size
	}
	if product != nil {
		attrs["retail_price"] = map[string]any{"amount": 79.9, "currency": "USD"}
		attrs["season"] = "2023 Spring/Summer"
	}
	ab, _ := json.Marshal(attrs)

	var category any
	if it.category != "" {
		category = it.category
	}
	if err := s.exec(ctx, `INSERT INTO item (id, kind, title, category, product_id, quantity, location_id, target_location_id,
	      lifecycle, gone_reason, gone_at, review_state, attrs, ext_key, created_at, updated_at)
	      VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		id, it.kind, it.title, category, product, qty, loc, target, lifecycle, goneReason, goneAt, review, string(ab),
		fmt.Sprintf("demo/%02d", i), ts, ts); err != nil {
		return err
	}
	s.items++

	for _, t := range it.tags {
		if err := s.exec(ctx, `INSERT INTO tag (item_id, tag) VALUES (?, ?)`, id, t); err != nil {
			return err
		}
	}

	// Photos: flat lay first, then a label/detail shot.
	var labelMedia string
	for p := 0; p < it.photos; p++ {
		mid, err := s.photo(ctx, it.color, i*10+p, it.category)
		if err != nil {
			return err
		}
		role := "flat_lay"
		if p == 1 {
			role = "detail"
			if it.label != "" {
				role = "care_label"
				labelMedia = mid
			}
		}
		if err := s.exec(ctx, `INSERT INTO media_link (media_id, subject_type, subject_id, role, sort, created_at) VALUES (?, 'item', ?, ?, ?, ?)`,
			mid, id, role, p, s.now()); err != nil {
			return err
		}
	}

	// Raw layer: a care label transcription plus extracted identifiers.
	var obsID string
	if it.label != "" {
		obsID = db.NewID()
		var mediaArg any
		if labelMedia != "" {
			mediaArg = labelMedia
		}
		data := map[string]any{"codes": codesFrom(it.label)}
		db2, _ := json.Marshal(data)
		if err := s.exec(ctx, `INSERT INTO observation (id, item_id, source, media_id, text, data, recorded_by, recorded_at) VALUES (?,?,?,?,?,?,?,?)`,
			obsID, id, "care_label", mediaArg, it.label, string(db2), recordedBy, s.now()); err != nil {
			return err
		}
		for _, line := range strings.Split(it.label, "\n") {
			switch {
			case strings.HasPrefix(line, "RN "):
				if err := s.ident(ctx, id, "rn", strings.TrimPrefix(line, "RN "), obsID); err != nil {
					return err
				}
			case strings.Contains(line, "(") && strings.Contains(line, "-"):
				code, season, _ := strings.Cut(strings.TrimSuffix(line, ")"), "(")
				if err := s.ident(ctx, id, "uniqlo.product_code", code, obsID); err != nil {
					return err
				}
				if err := s.ident(ctx, id, "uniqlo.season_code", season, obsID); err != nil {
					return err
				}
			}
		}
	}

	// Derived layer: brand/color/size facts with provenance.
	basis, conf, src := "user", 1.0, `[{"note":"demo seed"}]`
	if it.review {
		basis, conf = "inferred", 0.6
	}
	for _, kv := range [][2]string{{"brand", it.brand}, {"color", it.color}, {"size", it.size}} {
		if kv[1] == "" {
			continue
		}
		b, c, sr := basis, conf, src
		if obsID != "" && kv[0] == "brand" {
			b, c, sr = "observed", 1.0, fmt.Sprintf(`[{"observation_id":%q}]`, obsID)
		}
		if err := s.fact(ctx, "item", id, kv[0], kv[1], b, c, sr); err != nil {
			return err
		}
	}

	// Movement history for anything with a location.
	if loc != nil {
		if err := s.exec(ctx, `INSERT INTO movement (id, item_id, kind, from_location_id, to_location_id, at, actor) VALUES (?,?,?,?,?,?,?)`,
			db.NewID(), id, "move", nil, loc, ts, recordedBy); err != nil {
			return err
		}
	}
	if target != nil {
		if err := s.exec(ctx, `INSERT INTO movement (id, item_id, kind, from_location_id, to_location_id, at, actor) VALUES (?,?,?,?,?,?,?)`,
			db.NewID(), id, "stage", loc, target, s.now(), "user"); err != nil {
			return err
		}
	}
	return items.Reindex(ctx, s.tx, id)
}

func (s *seeder) fact(ctx context.Context, subjectType, subjectID, key string, value any, basis string, conf float64, sources string) error {
	vb, _ := json.Marshal(value)
	return s.exec(ctx, `INSERT INTO fact (id, subject_type, subject_id, key, value, basis, confidence, sources, recorded_by, recorded_at)
	      VALUES (?,?,?,?,?,?,?,?,?,?)`, db.NewID(), subjectType, subjectID, key, string(vb), basis, conf, sources, "user", s.now())
}

func (s *seeder) ident(ctx context.Context, itemID, scheme, value, obsID string) error {
	return s.exec(ctx, `INSERT INTO identifier (id, subject_type, subject_id, scheme, value, value_norm, observation_id, recorded_at)
	      VALUES (?, 'item', ?, ?, ?, ?, ?, ?)`, db.NewID(), itemID, scheme, value, Norm(value), obsID, s.now())
}

// Norm is the identifier normalization used by the seed; items owns the
// canonical version (uppercase, strip spaces, dashes, dots and slashes).
func Norm(v string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case ' ', '-', '.', '/', '_', '\t':
			return -1
		}
		if r >= 'a' && r <= 'z' {
			return r - 32
		}
		return r
	}, v)
}

func codesFrom(label string) []string {
	var out []string
	for _, line := range strings.Split(label, "\n") {
		if strings.HasPrefix(line, "RN ") || strings.Contains(line, "(") {
			out = append(out, line)
		}
	}
	return out
}

// photo generates a placeholder PNG, stores it as an original and builds its
// variants with vips. It returns the media id.
func (s *seeder) photo(ctx context.Context, colorName string, n int, category string) (string, error) {
	img := placeholder(colorName, n, category)
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return "", err
	}
	b := buf.Bytes()
	sum := sha256.Sum256(b)
	sha := hex.EncodeToString(sum[:])
	cfg := s.d.Cfg
	orig := media.OrigPath(cfg, sha, "png")
	if err := os.MkdirAll(filepath.Dir(orig), 0o755); err != nil {
		return "", err
	}
	tmp := orig + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, orig); err != nil {
		return "", err
	}
	if err := media.GenerateVariants(ctx, cfg, orig, sha); err != nil {
		return "", err
	}
	id := db.NewID()
	bounds := img.Bounds()
	taken := db.FormatTime(s.base.Add(time.Duration(n) * time.Minute))
	err := s.exec(ctx, `INSERT INTO media (id, sha256, mime, ext, bytes, width, height, taken_at, original_name, variant_status, created_at)
	      VALUES (?,?,?,?,?,?,?,?,?,?,?)`, id, sha, "image/png", "png", len(b), bounds.Dx(), bounds.Dy(), taken,
		fmt.Sprintf("DEMO_%04d.png", n), "ready", s.now())
	return id, err
}

// placeholder draws a simple, distinct image: a tinted background and a
// garment-ish or box-ish silhouette in the item's color.
func placeholder(colorName string, n int, category string) image.Image {
	const w, h = 900, 1200
	c, ok := palette[colorName]
	if !ok {
		c = color.RGBA{0x9a, 0x8f, 0x80, 255}
	}
	bg := color.RGBA{0xf3, 0xf1, 0xed, 255}
	if n%3 == 1 {
		bg = color.RGBA{0xe6, 0xe9, 0xec, 255}
	}
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	shirt := strings.HasPrefix(category, "tops") || strings.HasPrefix(category, "outerwear") || strings.HasPrefix(category, "suits/jacket")
	trousers := strings.HasPrefix(category, "bottoms") || strings.HasPrefix(category, "suits/trousers")
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			in := false
			fx, fy := float64(x)/w, float64(y)/h
			switch {
			case shirt:
				body := fx > 0.28 && fx < 0.72 && fy > 0.22 && fy < 0.85
				sleeves := fy > 0.22 && fy < 0.5 && ((fx > 0.12 && fx <= 0.28 && fy-0.22 < (0.28-fx)*1.6+0.12) || (fx >= 0.72 && fx < 0.88 && fy-0.22 < (fx-0.72)*1.6+0.12))
				neck := (fx-0.5)*(fx-0.5)+(fy-0.22)*(fy-0.22)*0.5 < 0.004
				in = (body || sleeves) && !neck
			case trousers:
				waist := fx > 0.3 && fx < 0.7 && fy > 0.15 && fy < 0.3
				legL := fx > 0.3 && fx < 0.49 && fy >= 0.3 && fy < 0.88
				legR := fx > 0.51 && fx < 0.7 && fy >= 0.3 && fy < 0.88
				in = waist || legL || legR
			default:
				dx, dy := fx-0.5, fy-0.5
				in = (dx*dx+dy*dy*0.56 < 0.06) || (fx > 0.25 && fx < 0.75 && fy > 0.62 && fy < 0.78)
			}
			if in {
				shade := uint8((x*7 + y*3 + n*13) % 18)
				img.SetRGBA(x, y, color.RGBA{sub(c.R, shade), sub(c.G, shade), sub(c.B, shade), 255})
			} else {
				img.SetRGBA(x, y, bg)
			}
		}
	}
	// Corner pixel encodes n so every placeholder has a distinct sha256.
	img.SetRGBA(0, 0, color.RGBA{uint8(n), uint8(n >> 8), 0xa5, 255})
	return img
}

func sub(a, b uint8) uint8 {
	if a < b {
		return 0
	}
	return a - b
}
