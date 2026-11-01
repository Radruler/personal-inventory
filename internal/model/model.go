// Package model holds the JSON types shared between domain packages. Each
// mirrors a schema in api/openapi.yaml with the same name; keep them in sync.
//
// Ownership: these shapes are part of the contract and change only through
// the coordinator. Domain-specific request/response types live in their own
// packages.
package model

import (
	"context"

	"github.com/radruler/personal-inventory/internal/db"
)

// Kinds, placement statuses and other closed enums.
const (
	KindClothing = "clothing"
	KindObject   = "object"
	KindUtility  = "utility"

	LifecycleOwned = "owned"
	LifecycleGone  = "gone"

	ReviewOK          = "ok"
	ReviewNeedsReview = "needs_review"

	PlacementGone        = "gone"
	PlacementStaged      = "staged"
	PlacementUnknown     = "unknown"
	PlacementApproximate = "approximate"
	PlacementPlaced      = "placed"

	LocSite      = "site"
	LocRoom      = "room"
	LocUnit      = "unit"
	LocShelf     = "shelf"
	LocContainer = "container"

	SubjectItem     = "item"
	SubjectProduct  = "product"
	SubjectLocation = "location"
)

// Kinds lists item kinds in display order.
var Kinds = []string{KindClothing, KindObject, KindUtility}

// LocationKinds lists location kinds from outermost to innermost.
var LocationKinds = []string{LocSite, LocRoom, LocUnit, LocShelf, LocContainer}

// MediaRef is the compact form of a media row used wherever a photo is shown.
// URLs are root-relative (/media/<sha256>/thumb).
type MediaRef struct {
	ID            string  `json:"id"`
	SHA256        string  `json:"sha256"`
	Mime          string  `json:"mime"`
	Width         *int    `json:"width"`
	Height        *int    `json:"height"`
	VariantStatus string  `json:"variant_status"`
	ThumbURL      *string `json:"thumb_url"`   // null until variants are ready (or for documents)
	DisplayURL    *string `json:"display_url"` // null until variants are ready (or for documents)
	OrigURL       string  `json:"orig_url"`
}

// LocationCrumb is one node on a location path.
type LocationCrumb struct {
	ID   string  `json:"id"`
	Code *string `json:"code"`
	Name string  `json:"name"`
	Kind string  `json:"kind"`
}

// LocationRef identifies a location together with its full path.
type LocationRef struct {
	LocationCrumb
	// Path runs from the root to this location, inclusive.
	Path []LocationCrumb `json:"path"`
	// Label is the display path, e.g. "Unit 2 › B-014" (site omitted when
	// the path is deeper than the site; codes preferred for containers).
	Label    string `json:"label"`
	Archived bool   `json:"archived"`
}

// Placement is the derived "where is it" status (PLAN.md §4.3).
type Placement struct {
	// Status: gone | staged | unknown | approximate | placed
	Status string `json:"status"`
	// Label is the human sentence: "Unit 2 › B-014", "Bedroom → going to Unit 2",
	// "Somewhere in Unit 2 (box unknown)", "Location unknown", "Sold · 2026-03".
	Label      string       `json:"label"`
	Location   *LocationRef `json:"location"`
	Target     *LocationRef `json:"target"`
	GoneReason *string      `json:"gone_reason"`
	GoneAt     *string      `json:"gone_at"`
}

// ItemSummary is the card shape used by lists, grids, box contents, staging
// and the inbox.
type ItemSummary struct {
	ID          string         `json:"id"`
	Kind        string         `json:"kind"`
	Title       string         `json:"title"`
	Category    *string        `json:"category"`
	Quantity    int            `json:"quantity"`
	Lifecycle   string         `json:"lifecycle"`
	ReviewState string         `json:"review_state"`
	Cover       *MediaRef      `json:"cover"`
	Placement   Placement      `json:"placement"`
	Attrs       map[string]any `json:"attrs"`
	Tags        []string       `json:"tags"`
	ProductID   *string        `json:"product_id"`
	ExtKey      *string        `json:"ext_key"`
	CreatedAt   string         `json:"created_at"`
	UpdatedAt   string         `json:"updated_at"`
}

// ItemSummaryLoader loads summaries for ids, preserving the order of ids and
// skipping ids that do not exist. Implemented by items.Summaries and injected
// through httpapi.Deps so locations and media need not import items.
type ItemSummaryLoader func(ctx context.Context, q db.Querier, ids []string) ([]ItemSummary, error)
