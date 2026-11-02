package ingest

import (
	"bytes"
	"encoding/json"
)

// Request shapes for POST /api/ingest/bundle (api/openapi.yaml Bundle*).
// Decoded with httpapi.DecodeJSON, so unknown fields are rejected.

// opt is a field whose presence matters: Set reports presence, Null an
// explicit null. Absent fields keep the stored value on update.
type opt[T any] struct {
	Set  bool
	Null bool
	V    T
}

func (o *opt[T]) UnmarshalJSON(b []byte) error {
	o.Set = true
	if string(bytes.TrimSpace(b)) == "null" {
		o.Null = true
		return nil
	}
	return json.Unmarshal(b, &o.V)
}

// MarshalJSON lets bundles round-trip (used by tests and the CLI).
func (o opt[T]) MarshalJSON() ([]byte, error) {
	if o.Null || !o.Set {
		return []byte("null"), nil
	}
	return json.Marshal(o.V)
}

// ptr returns nil when absent or null.
func (o opt[T]) ptr() *T {
	if !o.Set || o.Null {
		return nil
	}
	v := o.V
	return &v
}

type bundleMediaRef struct {
	MediaID *string     `json:"media_id,omitempty"`
	SHA256  *string     `json:"sha256,omitempty"`
	Path    *string     `json:"path,omitempty"`
	Role    *string     `json:"role,omitempty"`
	Sort    *int        `json:"sort,omitempty"`
	Caption opt[string] `json:"caption,omitzero"`
	Context *string     `json:"context,omitempty"`
}

type bundleObservation struct {
	ExtKey     *string         `json:"ext_key,omitempty"`
	Source     *string         `json:"source,omitempty"`
	MediaID    *string         `json:"media_id,omitempty"`
	Media      *string         `json:"media,omitempty"`
	Region     json.RawMessage `json:"region,omitempty"`
	Text       *string         `json:"text,omitempty"`
	Data       json.RawMessage `json:"data,omitempty"`
	RecordedBy *string         `json:"recorded_by,omitempty"`
}

type bundleIdentifier struct {
	ExtKey        *string `json:"ext_key,omitempty"`
	Scheme        string  `json:"scheme"`
	Value         string  `json:"value"`
	ObservationID *string `json:"observation_id,omitempty"`
	Observation   *int    `json:"observation,omitempty"`
	Note          *string `json:"note,omitempty"`
	RecordedBy    *string `json:"recorded_by,omitempty"`
}

type factSource struct {
	ObservationID *string `json:"observation_id,omitempty"`
	ReferenceID   *string `json:"reference_id,omitempty"`
	MediaID       *string `json:"media_id,omitempty"`
	URL           *string `json:"url,omitempty"`
	Note          *string `json:"note,omitempty"`
	Observation   *int    `json:"observation,omitempty"`
	Reference     *int    `json:"reference,omitempty"`
}

type bundleFact struct {
	ExtKey     *string         `json:"ext_key,omitempty"`
	Key        string          `json:"key"`
	Value      json.RawMessage `json:"value"`
	Basis      string          `json:"basis"`
	Confidence *float64        `json:"confidence,omitempty"`
	Sources    []factSource    `json:"sources,omitempty"`
	Note       *string         `json:"note,omitempty"`
	RecordedBy *string         `json:"recorded_by,omitempty"`
}

type bundleReference struct {
	ExtKey         *string         `json:"ext_key,omitempty"`
	URL            *string         `json:"url,omitempty"`
	Title          *string         `json:"title,omitempty"`
	Kind           *string         `json:"kind,omitempty"`
	ArchiveMediaID *string         `json:"archive_media_id,omitempty"`
	Archive        *bundleMediaRef `json:"archive,omitempty"`
	FetchedAt      *string         `json:"fetched_at,omitempty"`
	Notes          *string         `json:"notes,omitempty"`
	RecordedBy     *string         `json:"recorded_by,omitempty"`
}

type bundleProduct struct {
	ExtKey      string             `json:"ext_key"`
	Brand       opt[string]        `json:"brand,omitzero"`
	Name        *string            `json:"name,omitempty"`
	Notes       opt[string]        `json:"notes,omitzero"`
	Media       []bundleMediaRef   `json:"media,omitempty"`
	Identifiers []bundleIdentifier `json:"identifiers,omitempty"`
	Facts       []bundleFact       `json:"facts,omitempty"`
	References  []bundleReference  `json:"references,omitempty"`
}

// declares reports whether p carries content (not just an {ext_key} link).
func (p *bundleProduct) declares() bool {
	return p.Brand.Set || p.Name != nil || p.Notes.Set || len(p.Media) > 0 ||
		len(p.Identifiers) > 0 || len(p.Facts) > 0 || len(p.References) > 0
}

type bundleItem struct {
	ExtKey         string              `json:"ext_key"`
	Kind           *string             `json:"kind,omitempty"`
	Title          *string             `json:"title,omitempty"`
	Category       opt[string]         `json:"category,omitzero"`
	Quantity       *int                `json:"quantity,omitempty"`
	Notes          opt[string]         `json:"notes,omitzero"`
	Tags           []string            `json:"tags,omitempty"`
	Location       *string             `json:"location,omitempty"`
	TargetLocation *string             `json:"target_location,omitempty"`
	ReviewState    *string             `json:"review_state,omitempty"`
	Parent         *string             `json:"parent,omitempty"`
	Cover          *int                `json:"cover,omitempty"`
	Product        *bundleProduct      `json:"product,omitempty"`
	Media          []bundleMediaRef    `json:"media,omitempty"`
	Observations   []bundleObservation `json:"observations,omitempty"`
	Identifiers    []bundleIdentifier  `json:"identifiers,omitempty"`
	Facts          []bundleFact        `json:"facts,omitempty"`
	References     []bundleReference   `json:"references,omitempty"`
	RecordedBy     *string             `json:"recorded_by,omitempty"`
}

type bundleDefaults struct {
	Kind           *string `json:"kind,omitempty"`
	Location       *string `json:"location,omitempty"`
	TargetLocation *string `json:"target_location,omitempty"`
	ReviewState    *string `json:"review_state,omitempty"`
	RecordedBy     *string `json:"recorded_by,omitempty"`
}

// Bundle is the POST /api/ingest/bundle body.
type Bundle struct {
	Defaults *bundleDefaults `json:"defaults,omitempty"`
	Products []bundleProduct `json:"products,omitempty"`
	Items    []bundleItem    `json:"items"`
}

// Outcome is one BundleOutcome.
type Outcome struct {
	ExtKey   string   `json:"ext_key"`
	ID       *string  `json:"id"`
	Action   string   `json:"action"` // created | updated | unchanged
	Changes  []string `json:"changes"`
	Warnings []string `json:"warnings"`
}

// Result is the BundleResult response.
type Result struct {
	DryRun   bool      `json:"dry_run"`
	Items    []Outcome `json:"items"`
	Products []Outcome `json:"products"`
}

// Problem is one entry of a 422 response's details.
type Problem struct {
	Path    string `json:"path"`
	Message string `json:"message"`
}
