package locations

import (
	"encoding/json"

	"github.com/radruler/personal-inventory/internal/model"
)

// Location mirrors the openapi Location schema.
type Location struct {
	ID           string          `json:"id"`
	Code         *string         `json:"code"`
	Name         string          `json:"name"`
	Kind         string          `json:"kind"`
	ParentID     *string         `json:"parent_id"`
	CoverMediaID *string         `json:"cover_media_id"`
	Notes        *string         `json:"notes"`
	Layout       json.RawMessage `json:"layout"` // nil marshals as null
	ArchivedAt   *string         `json:"archived_at"`
	CreatedAt    string          `json:"created_at"`
	UpdatedAt    string          `json:"updated_at"`
}

// LocationNode mirrors the openapi LocationNode schema: a location with its
// label, cover, counts and (non-archived unless asked) children.
type LocationNode struct {
	Location
	Label       string          `json:"label"`
	Cover       *model.MediaRef `json:"cover"`
	ItemCount   int             `json:"item_count"`
	TotalCount  int             `json:"total_count"`
	StagedCount int             `json:"staged_count"`
	Children    []*LocationNode `json:"children"`

	path []model.LocationCrumb // root → this, inclusive
}

// LocationTree mirrors the openapi LocationTree schema.
type LocationTree struct {
	Roots        []*LocationNode `json:"roots"`
	UnknownCount int             `json:"unknown_count"`
}

// LocationDetail mirrors the openapi LocationDetail schema.
type LocationDetail struct {
	LocationNode
	Path        []model.LocationCrumb `json:"path"`
	Items       []model.ItemSummary   `json:"items"`
	StagedItems []model.ItemSummary   `json:"staged_items"`
	Movements   []Movement            `json:"movements"`
	Media       []MediaLinkView       `json:"media"`
}

// Movement mirrors the openapi Movement schema. It is exported so the items
// detail can render an item's history (see MovementsForItem).
type Movement struct {
	ID              string             `json:"id"`
	ItemID          *string            `json:"item_id"`
	LocationMovedID *string            `json:"location_moved_id"`
	Kind            string             `json:"kind"` // move | stage | unstage
	From            *model.LocationRef `json:"from"`
	To              *model.LocationRef `json:"to"`
	At              string             `json:"at"`
	Note            *string            `json:"note"`
	Actor           string             `json:"actor"`
}

// Movement kinds (movement.kind).
const (
	MoveKindMove    = "move"
	MoveKindStage   = "stage"
	MoveKindUnstage = "unstage"
)

// MoveResult mirrors the openapi MoveResult schema.
type MoveResult struct {
	Moved     int                 `json:"moved"`
	Movements []Movement          `json:"movements"`
	Items     []model.ItemSummary `json:"items"`
	Location  *LocationNode       `json:"location"`
}

// StagingGroup mirrors the openapi StagingGroup schema.
type StagingGroup struct {
	Target model.LocationRef   `json:"target"`
	Items  []model.ItemSummary `json:"items"`
}

// Staging mirrors the openapi Staging schema.
type Staging struct {
	Groups       []StagingGroup `json:"groups"`
	UnknownCount int            `json:"unknown_count"`
}

// MediaLinkView mirrors the openapi MediaLinkView schema.
type MediaLinkView struct {
	Media   mediaView `json:"media"`
	Role    string    `json:"role"`
	Sort    int       `json:"sort"`
	Caption *string   `json:"caption"`
}

// mediaView mirrors the openapi Media schema. Thread B owns media; this is a
// local read-only rendering until a shared loader exists (see report).
type mediaView struct {
	model.MediaRef
	Ext          string          `json:"ext"`
	Bytes        int64           `json:"bytes"`
	TakenAt      *string         `json:"taken_at"`
	OriginalName *string         `json:"original_name"`
	Exif         json.RawMessage `json:"exif"`
	Context      *string         `json:"context"`
	VariantError *string         `json:"variant_error"`
	CreatedAt    string          `json:"created_at"`
	Links        []mediaLink     `json:"links"`
}

type mediaLink struct {
	MediaID     string  `json:"media_id"`
	SubjectType string  `json:"subject_type"`
	SubjectID   string  `json:"subject_id"`
	Role        string  `json:"role"`
	Sort        int     `json:"sort"`
	Caption     *string `json:"caption"`
	CreatedAt   string  `json:"created_at"`
}

// optString distinguishes an absent JSON field (Set=false) from null
// (Set=true, Val=nil) and a value.
type optString struct {
	Set bool
	Val *string
}

func (o *optString) UnmarshalJSON(b []byte) error {
	o.Set = true
	if string(b) == "null" {
		o.Val = nil
		return nil
	}
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	o.Val = &s
	return nil
}

// optRaw is optString for an arbitrary JSON value.
type optRaw struct {
	Set bool
	Val json.RawMessage // nil = null
}

func (o *optRaw) UnmarshalJSON(b []byte) error {
	o.Set = true
	if string(b) == "null" {
		o.Val = nil
		return nil
	}
	o.Val = append(json.RawMessage(nil), b...)
	return nil
}
