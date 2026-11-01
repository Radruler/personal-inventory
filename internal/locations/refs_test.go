package locations_test

import (
	"context"
	"testing"

	"github.com/radruler/personal-inventory/internal/locations"
	"github.com/radruler/personal-inventory/internal/model"
	"github.com/radruler/personal-inventory/internal/testutil"
)

func ptr(s string) *string { return &s }

func TestDerivePlacement(t *testing.T) {
	code := ptr("B-014")
	unit := model.LocationRef{LocationCrumb: model.LocationCrumb{ID: "u", Name: "Unit 2", Kind: "unit"}, Label: "Unit 2"}
	box := model.LocationRef{LocationCrumb: model.LocationCrumb{ID: "b", Code: code, Name: "Coats", Kind: "container"}, Label: "Unit 2 › B-014"}
	room := model.LocationRef{LocationCrumb: model.LocationCrumb{ID: "r", Name: "Bedroom", Kind: "room"}, Label: "Bedroom"}
	cases := []struct {
		name          string
		lifecycle     string
		reason, at    *string
		loc, target   *model.LocationRef
		status, label string
	}{
		{"gone", "gone", ptr("sold"), ptr("2026-03-04T00:00:00.000Z"), &box, nil, "gone", "Sold · 2026-03"},
		{"staged", "owned", nil, nil, &room, &unit, "staged", "Bedroom → going to Unit 2"},
		{"staged from unknown", "owned", nil, nil, nil, &unit, "staged", "Location unknown → going to Unit 2"},
		{"unknown", "owned", nil, nil, nil, nil, "unknown", "Location unknown"},
		{"approximate", "owned", nil, nil, &unit, nil, "approximate", "Somewhere in Unit 2 (box unknown)"},
		{"placed box", "owned", nil, nil, &box, nil, "placed", "Unit 2 › B-014"},
		{"placed room", "owned", nil, nil, &room, nil, "placed", "Bedroom"},
	}
	for _, c := range cases {
		p := locations.DerivePlacement(c.lifecycle, c.reason, c.at, c.loc, c.target)
		if p.Status != c.status || p.Label != c.label {
			t.Errorf("%s: got %s %q, want %s %q", c.name, p.Status, p.Label, c.status, c.label)
		}
	}
}

func TestRefsAndResolve(t *testing.T) {
	d := testutil.NewDeps(t)
	ctx := context.Background()
	for _, q := range []string{
		`INSERT INTO location (id, code, name, kind) VALUES ('s', 'SS', 'StoreSafe', 'site')`,
		`INSERT INTO location (id, code, name, kind, parent_id) VALUES ('u', 'U2', 'Unit 2', 'unit', 's')`,
		`INSERT INTO location (id, code, name, kind, parent_id) VALUES ('b', 'B-014', 'Coats', 'container', 'u')`,
	} {
		if _, err := d.DB.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	refs, err := locations.Refs(ctx, d.DB, []string{"b", "s"})
	if err != nil {
		t.Fatal(err)
	}
	if got := refs["b"].Label; got != "Unit 2 › B-014" {
		t.Errorf("box label = %q", got)
	}
	if len(refs["b"].Path) != 3 || refs["b"].Path[0].ID != "s" {
		t.Errorf("box path = %+v", refs["b"].Path)
	}
	if got := refs["s"].Label; got != "StoreSafe" {
		t.Errorf("site label = %q", got)
	}
	id, err := locations.ResolveRef(ctx, d.DB, "code:b-014")
	if err != nil || id != "b" {
		t.Errorf("resolve code = %q, %v", id, err)
	}
	if _, err := locations.ResolveRef(ctx, d.DB, "code:NOPE"); err == nil {
		t.Errorf("resolve unknown code: want error")
	}
}
