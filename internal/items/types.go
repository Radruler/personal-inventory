package items

import (
	"encoding/json"

	"github.com/radruler/personal-inventory/internal/locations"
	"github.com/radruler/personal-inventory/internal/model"
)

// Response shapes owned by this package. Each mirrors the api/openapi.yaml
// schema of the same name; required nullable fields are always emitted.

// Observation mirrors the Observation schema (write responses omit media).
type Observation struct {
	ID         string          `json:"id"`
	ItemID     string          `json:"item_id"`
	Source     string          `json:"source"`
	MediaID    *string         `json:"media_id"`
	Region     json.RawMessage `json:"region"` // nil marshals as null
	Text       *string         `json:"text"`
	Data       json.RawMessage `json:"data"`
	RecordedBy string          `json:"recorded_by"`
	RecordedAt string          `json:"recorded_at"`
	ExtKey     *string         `json:"ext_key"`
}

// ObservationView is an Observation inside ItemDetail, with its photo.
type ObservationView struct {
	Observation
	Media *model.MediaRef `json:"media"`
}

// Identifier mirrors the Identifier schema.
type Identifier struct {
	ID            string  `json:"id"`
	SubjectType   string  `json:"subject_type"`
	SubjectID     string  `json:"subject_id"`
	Scheme        string  `json:"scheme"`
	Value         string  `json:"value"`
	ValueNorm     string  `json:"value_norm"`
	ObservationID *string `json:"observation_id"`
	Note          *string `json:"note"`
	RecordedBy    string  `json:"recorded_by"`
	RecordedAt    string  `json:"recorded_at"`
	ExtKey        *string `json:"ext_key"`
}

// Fact mirrors the Fact schema.
type Fact struct {
	ID           string          `json:"id"`
	SubjectType  string          `json:"subject_type"`
	SubjectID    string          `json:"subject_id"`
	Key          string          `json:"key"`
	Value        json.RawMessage `json:"value"`
	Basis        string          `json:"basis"`
	Confidence   float64         `json:"confidence"`
	Sources      json.RawMessage `json:"sources"`
	Status       string          `json:"status"`
	SupersedesID *string         `json:"supersedes_id"`
	Note         *string         `json:"note"`
	RecordedBy   string          `json:"recorded_by"`
	RecordedAt   string          `json:"recorded_at"`
	ExtKey       *string         `json:"ext_key"`
}

// FactWrite is returned by every fact write: the fact and the subject's
// recomputed attrs.
type FactWrite struct {
	Fact  Fact           `json:"fact"`
	Attrs map[string]any `json:"attrs"`
}

// Reference mirrors the Reference schema (write responses omit archive).
type Reference struct {
	ID             string  `json:"id"`
	SubjectType    string  `json:"subject_type"`
	SubjectID      string  `json:"subject_id"`
	URL            *string `json:"url"`
	Title          *string `json:"title"`
	Kind           string  `json:"kind"`
	ArchiveMediaID *string `json:"archive_media_id"`
	FetchedAt      *string `json:"fetched_at"`
	Notes          *string `json:"notes"`
	RecordedBy     string  `json:"recorded_by"`
	RecordedAt     string  `json:"recorded_at"`
	ExtKey         *string `json:"ext_key"`
}

// ReferenceView is a Reference inside a detail view, with its archive.
type ReferenceView struct {
	Reference
	Archive *model.MediaRef `json:"archive"`
}

// MediaLink mirrors the MediaLink schema.
type MediaLink struct {
	MediaID     string  `json:"media_id"`
	SubjectType string  `json:"subject_type"`
	SubjectID   string  `json:"subject_id"`
	Role        string  `json:"role"`
	Sort        int     `json:"sort"`
	Caption     *string `json:"caption"`
	CreatedAt   string  `json:"created_at"`
}

// Media mirrors the Media schema (a media row with all of its links).
type Media struct {
	model.MediaRef
	Ext          string          `json:"ext"`
	Bytes        int64           `json:"bytes"`
	TakenAt      *string         `json:"taken_at"`
	OriginalName *string         `json:"original_name"`
	Exif         json.RawMessage `json:"exif"`
	Context      *string         `json:"context"`
	VariantError *string         `json:"variant_error"`
	CreatedAt    string          `json:"created_at"`
	Links        []MediaLink     `json:"links"`
}

// MediaLinkView mirrors MediaLinkView: one photo linked to the subject.
type MediaLinkView struct {
	Media   Media   `json:"media"`
	Role    string  `json:"role"`
	Sort    int     `json:"sort"`
	Caption *string `json:"caption"`
}

// AttrSource says which fact won a projected attrs key.
type AttrSource struct {
	FactID      string `json:"fact_id"`
	SubjectType string `json:"subject_type"`
}

// ItemDetail mirrors the ItemDetail schema.
type ItemDetail struct {
	model.ItemSummary
	Notes            *string               `json:"notes"`
	ParentItemID     *string               `json:"parent_item_id"`
	LocationID       *string               `json:"location_id"`
	TargetLocationID *string               `json:"target_location_id"`
	CoverMediaID     *string               `json:"cover_media_id"`
	GoneReason       *string               `json:"gone_reason"`
	GoneAt           *string               `json:"gone_at"`
	AttrSources      map[string]AttrSource `json:"attr_sources"`
	Media            []MediaLinkView       `json:"media"`
	Observations     []ObservationView     `json:"observations"`
	Identifiers      []Identifier          `json:"identifiers"`
	Facts            []Fact                `json:"facts"`
	References       []ReferenceView       `json:"references"`
	Movements        []locations.Movement  `json:"movements"`
	Product          *ProductDetail        `json:"product"`
	Parent           *model.ItemSummary    `json:"parent"`
	Children         []model.ItemSummary   `json:"children"`
}

// Product mirrors the Product schema.
type Product struct {
	ID        string          `json:"id"`
	Brand     *string         `json:"brand"`
	Name      string          `json:"name"`
	Attrs     map[string]any  `json:"attrs"`
	Notes     *string         `json:"notes"`
	ExtKey    *string         `json:"ext_key"`
	CreatedAt string          `json:"created_at"`
	UpdatedAt string          `json:"updated_at"`
	Cover     *model.MediaRef `json:"cover"`
	ItemCount int             `json:"item_count"`
}

// ProductDetail mirrors the ProductDetail schema.
type ProductDetail struct {
	Product
	Media       []MediaLinkView     `json:"media"`
	Identifiers []Identifier        `json:"identifiers"`
	Facts       []Fact              `json:"facts"`
	References  []ReferenceView     `json:"references"`
	Items       []model.ItemSummary `json:"items"`
}

// ProductList mirrors the ProductList schema.
type ProductList struct {
	Products   []Product `json:"products"`
	NextCursor *string   `json:"next_cursor"`
}

// ItemList mirrors the ItemList schema.
type ItemList struct {
	Items      []model.ItemSummary `json:"items"`
	NextCursor *string             `json:"next_cursor"`
	Total      int                 `json:"total"`
}

// TagSet mirrors the TagSet schema.
type TagSet struct {
	Tags []string `json:"tags"`
}

// FacetValue, FacetGroup and Facets mirror the facet schemas.
type FacetValue struct {
	Value string `json:"value"`
	Label string `json:"label"`
	Count int    `json:"count"`
}

type FacetGroup struct {
	Field  string       `json:"field"`
	Label  string       `json:"label"`
	Values []FacetValue `json:"values"`
}

type Facets struct {
	Total  int          `json:"total"`
	Groups []FacetGroup `json:"groups"`
}

// IdentifierMatch and IdentifierLookup mirror the lookup schemas.
type IdentifierMatch struct {
	Identifier Identifier         `json:"identifier"`
	Item       *model.ItemSummary `json:"item"`
	Product    *Product           `json:"product"`
}

type IdentifierLookup struct {
	ValueNorm string            `json:"value_norm"`
	Matches   []IdentifierMatch `json:"matches"`
}

// Registry shapes for GET /api/schema.

type RegistryEntry struct {
	Value       string  `json:"value"`
	Label       string  `json:"label"`
	Sort        int     `json:"sort"`
	Description *string `json:"description"`
}

type MediaRoleEntry struct {
	RegistryEntry
	CoverRank *int `json:"cover_rank"`
}

type FactKey struct {
	Key         string   `json:"key"`
	Label       string   `json:"label"`
	ValueType   string   `json:"value_type"`
	Kinds       []string `json:"kinds"`
	Filterable  bool     `json:"filterable"`
	Sort        int      `json:"sort"`
	Description *string  `json:"description"`
}

type IdentifierScheme struct {
	Scheme      string  `json:"scheme"`
	Label       string  `json:"label"`
	Brand       *string `json:"brand"`
	Description *string `json:"description"`
	Pattern     *string `json:"pattern"`
	Example     *string `json:"example"`
	Notes       *string `json:"notes"`
}

type SchemaInfo struct {
	Kinds              []RegistryEntry    `json:"kinds"`
	MediaRoles         []MediaRoleEntry   `json:"media_roles"`
	ObservationSources []RegistryEntry    `json:"observation_sources"`
	LocationKinds      []string           `json:"location_kinds"`
	FactBases          []string           `json:"fact_bases"`
	GoneReasons        []string           `json:"gone_reasons"`
	FactKeys           []FactKey          `json:"fact_keys"`
	IdentifierSchemes  []IdentifierScheme `json:"identifier_schemes"`
}
