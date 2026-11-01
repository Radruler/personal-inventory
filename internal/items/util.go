package items

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
	"github.com/radruler/personal-inventory/internal/httpapi"
)

// Fact bases in projection precedence order.
const (
	BasisUser     = "user"
	BasisObserved = "observed"
	BasisLookedUp = "looked_up"
	BasisInferred = "inferred"
)

// FactBases lists the bases best first (user > observed > looked_up > inferred).
var FactBases = []string{BasisUser, BasisObserved, BasisLookedUp, BasisInferred}

// GoneReasons are the suggested values for item.gone_reason.
var GoneReasons = []string{"sold", "donated", "discarded", "lost", "other"}

// Opt is a PATCH field: Set reports presence, Null an explicit null.
type Opt[T any] struct {
	Set  bool
	Null bool
	V    T
}

// UnmarshalJSON implements json.Unmarshaler.
func (o *Opt[T]) UnmarshalJSON(b []byte) error {
	o.Set = true
	if string(bytes.TrimSpace(b)) == "null" {
		o.Null = true
		return nil
	}
	return json.Unmarshal(b, &o.V)
}

// ptr returns nil for a null Opt, else a pointer to its value.
func (o Opt[T]) ptr() *T {
	if o.Null {
		return nil
	}
	v := o.V
	return &v
}

func nullStr(n sql.NullString) *string {
	if !n.Valid {
		return nil
	}
	s := n.String
	return &s
}

// nullable converts a *string to a SQL argument (nil → NULL).
func nullable(s *string) any {
	if s == nil {
		return nil
	}
	return *s
}

// cleanOpt trims s; an empty result is nil.
func cleanOpt(s *string) *string {
	if s == nil {
		return nil
	}
	t := strings.TrimSpace(*s)
	if t == "" {
		return nil
	}
	return &t
}

func contains(list []string, v string) bool {
	for _, e := range list {
		if e == v {
			return true
		}
	}
	return false
}

// actorOr returns body if set, else the request actor.
func actorOr(body *string, fallback string) string {
	if body != nil && strings.TrimSpace(*body) != "" {
		return strings.TrimSpace(*body)
	}
	return fallback
}

// parseTime accepts RFC 3339 (with or without fractional seconds) and
// returns it in the canonical db.TimeFormat.
func parseTime(field, s string) (string, error) {
	t, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(s))
	if err != nil {
		return "", httpapi.Validation("%s: want an ISO-8601 timestamp like 2026-11-01T10:00:00.000Z", field)
	}
	return db.FormatTime(t), nil
}

var keyRE = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.\-]{0,99}$`)

// validKey reports whether k can be a fact key (also used in attr.<key>).
func validKey(k string) bool { return keyRE.MatchString(k) }

// exists reports whether a row with id exists in table (a trusted name).
func exists(ctx context.Context, q db.Querier, table, id string) (bool, error) {
	var one int
	err := q.QueryRowContext(ctx, `SELECT 1 FROM `+table+` WHERE id = ?`, id).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

// mustExist returns a 422 when the referenced row is missing.
func mustExist(ctx context.Context, q db.Querier, table, field, id string) error {
	ok, err := exists(ctx, q, table, id)
	if err != nil {
		return err
	}
	if !ok {
		return httpapi.Validation("%s: unknown %s %q", field, table, id)
	}
	return nil
}

// isJSONObject reports whether raw is a JSON object.
func isJSONObject(raw json.RawMessage) bool {
	t := bytes.TrimSpace(raw)
	return len(t) > 0 && t[0] == '{' && json.Valid(t)
}

// compactJSON returns raw compacted (raw must be valid JSON).
func compactJSON(raw json.RawMessage) string {
	var b bytes.Buffer
	if err := json.Compact(&b, raw); err != nil {
		return string(raw)
	}
	return b.String()
}

func decodeAttrs(s string) map[string]any {
	m := map[string]any{}
	_ = json.Unmarshal([]byte(s), &m)
	if m == nil {
		m = map[string]any{}
	}
	return m
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

// asValidation turns a 422 from a shared helper into a 400 (used for query
// parameters, where the contract only has 400).
func asBadRequest(err error) error {
	var e *httpapi.Error
	if errors.As(err, &e) && e.Code == httpapi.CodeValidation {
		return httpapi.BadRequest("%s", e.Message)
	}
	return err
}
