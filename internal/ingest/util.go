package ingest

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/radruler/personal-inventory/internal/db"
)

// Local equivalents of small unexported helpers in internal/items (this
// package may not edit items; see the thread G report for export proposals).

var (
	keyRE = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.\-]{0,99}$`)
	shaRE = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

var factBases = []string{"user", "observed", "looked_up", "inferred"}

// companySchemes identify a company, not a product, so sharing one is not a
// sign of a duplicate (no duplicate warnings for them).
var companySchemes = map[string]bool{"rn": true, "ca": true, "wpl": true}

func contains(list []string, v string) bool {
	for _, e := range list {
		if e == v {
			return true
		}
	}
	return false
}

// clean trims s; an empty result is nil.
func clean(s *string) *string {
	if s == nil {
		return nil
	}
	t := strings.TrimSpace(*s)
	if t == "" {
		return nil
	}
	return &t
}

func nullable(s *string) any {
	if s == nil {
		return nil
	}
	return *s
}

func nullStr(n sql.NullString) *string {
	if !n.Valid {
		return nil
	}
	s := n.String
	return &s
}

func eqStr(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// cleanTags trims, drops empties and de-duplicates case-insensitively.
func cleanTags(in []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, t := range in {
		t = strings.TrimSpace(t)
		k := strings.ToLower(t)
		if t == "" || seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, t)
	}
	return out
}

func isNull(raw json.RawMessage) bool {
	t := bytes.TrimSpace(raw)
	return len(t) == 0 || string(t) == "null"
}

func isJSONObject(raw json.RawMessage) bool {
	t := bytes.TrimSpace(raw)
	return len(t) > 0 && t[0] == '{' && json.Valid(t)
}

func compactJSON(raw []byte) string {
	var b bytes.Buffer
	if err := json.Compact(&b, raw); err != nil {
		return string(raw)
	}
	return b.String()
}

// canonicalJSON re-encodes JSON with sorted object keys and numbers kept as
// written, so key order and whitespace never count as a change.
func canonicalJSON(raw []byte) string {
	if len(bytes.TrimSpace(raw)) == 0 {
		return ""
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return string(raw)
	}
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
	return strings.TrimSuffix(b.String(), "\n")
}

// parseTime accepts RFC 3339 and returns db.TimeFormat.
func parseTime(s string) (string, bool) {
	t, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(s))
	if err != nil {
		return "", false
	}
	return db.FormatTime(t), true
}

// cleanRegion validates {x,y,w,h} (0..1) and returns its stored form.
func cleanRegion(raw json.RawMessage) (*string, string) {
	if isNull(raw) {
		return nil, ""
	}
	var r struct {
		X, Y, W, H *float64
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&r); err != nil || r.X == nil || r.Y == nil || r.W == nil || r.H == nil {
		return nil, "want {x, y, w, h} as 0..1 fractions"
	}
	for _, v := range []float64{*r.X, *r.Y, *r.W, *r.H} {
		if v < 0 || v > 1 {
			return nil, "values must be between 0 and 1"
		}
	}
	b, _ := json.Marshal(map[string]float64{"x": *r.X, "y": *r.Y, "w": *r.W, "h": *r.H})
	s := string(b)
	return &s, ""
}

// rowOwner returns the owner of the row with ext_key in table:
// ("item", item_id) for observations, (subject_type, subject_id) otherwise.
func rowOwner(ctx context.Context, q db.Querier, table, ext string) (id, subjectType, subjectID string, found bool, err error) {
	query := `SELECT id, subject_type, subject_id FROM ` + table + ` WHERE ext_key = ?`
	if table == "observation" {
		query = `SELECT id, 'item', item_id FROM observation WHERE ext_key = ?`
	}
	err = q.QueryRowContext(ctx, query, ext).Scan(&id, &subjectType, &subjectID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", "", false, nil
	}
	return id, subjectType, subjectID, err == nil, err
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
