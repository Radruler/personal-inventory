// Package httpapi holds what every domain package needs to serve the API:
// shared dependencies, the JSON error shape, request decoding, response
// helpers, cursors and middleware. It imports no domain package; the route
// table is assembled in internal/server.
package httpapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/radruler/personal-inventory/internal/config"
	"github.com/radruler/personal-inventory/internal/db"
	"github.com/radruler/personal-inventory/internal/model"
)

// Deps is passed to every package's Register.
type Deps struct {
	DB  *db.DB
	Cfg config.Config
	Log *slog.Logger

	// ItemSummaries is items.Summaries, injected so packages that list items
	// (locations, media inbox) do not import internal/items.
	ItemSummaries model.ItemSummaryLoader

	// Version is the build version reported by /api/health.
	Version string
}

// Error is an API error. It is rendered as {"error": {...}}.
type Error struct {
	Status  int    `json:"-"`
	Code    string `json:"code"`
	Message string `json:"message"`
	Details any    `json:"details,omitempty"`
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

// Error codes (api/openapi.yaml ErrorCode).
const (
	CodeBadRequest     = "bad_request"
	CodeValidation     = "validation"
	CodeUnauthorized   = "unauthorized"
	CodeNotFound       = "not_found"
	CodeConflict       = "conflict"
	CodeTooLarge       = "payload_too_large"
	CodeUnsupported    = "unsupported_media_type"
	CodeNotImplemented = "not_implemented"
	CodeInternal       = "internal"
)

// Errf builds an *Error.
func Errf(status int, code, format string, args ...any) *Error {
	return &Error{Status: status, Code: code, Message: fmt.Sprintf(format, args...)}
}

// BadRequest, Validation, NotFound and Conflict are shorthands for Errf.
func BadRequest(format string, args ...any) *Error {
	return Errf(http.StatusBadRequest, CodeBadRequest, format, args...)
}

func Validation(format string, args ...any) *Error {
	return Errf(http.StatusUnprocessableEntity, CodeValidation, format, args...)
}

func NotFound(format string, args ...any) *Error {
	return Errf(http.StatusNotFound, CodeNotFound, format, args...)
}

func Conflict(format string, args ...any) *Error {
	return Errf(http.StatusConflict, CodeConflict, format, args...)
}

// WithDetails returns e with Details set.
func (e *Error) WithDetails(d any) *Error { e.Details = d; return e }

// WriteJSON writes v as JSON with the given status.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

// WriteError renders err. Non-*Error values become 500 internal and are logged.
func WriteError(w http.ResponseWriter, r *http.Request, log *slog.Logger, err error) {
	var e *Error
	if !errors.As(err, &e) {
		if errors.Is(err, context.Canceled) {
			return
		}
		if log != nil {
			log.Error("request failed", "method", r.Method, "path", r.URL.Path, "err", err)
		}
		e = &Error{Status: http.StatusInternalServerError, Code: CodeInternal, Message: "internal error"}
	}
	WriteJSON(w, e.Status, map[string]any{"error": e})
}

// Handler adapts a handler that returns an error.
func (d *Deps) Handler(fn func(w http.ResponseWriter, r *http.Request) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := fn(w, r); err != nil {
			WriteError(w, r, d.Log, err)
		}
	}
}

// NotImplemented is the placeholder handler for routes a thread has not
// built yet.
func NotImplemented(w http.ResponseWriter, r *http.Request) {
	WriteError(w, r, nil, Errf(http.StatusNotImplemented, CodeNotImplemented,
		"%s %s is not implemented yet", r.Method, r.URL.Path))
}

// MaxJSONBody caps JSON request bodies (bundles can be large).
const MaxJSONBody = 32 << 20

// DecodeJSON decodes the request body into v. Unknown fields are rejected so
// agents notice typos.
func DecodeJSON(r *http.Request, v any) error {
	dec := json.NewDecoder(io.LimitReader(r.Body, MaxJSONBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		if errors.Is(err, io.EOF) {
			return BadRequest("request body is empty")
		}
		return BadRequest("invalid JSON body: %v", err)
	}
	if dec.More() {
		return BadRequest("invalid JSON body: trailing data")
	}
	return nil
}

// Cursor is an opaque pagination cursor: base64url of JSON.
type Cursor map[string]any

// EncodeCursor returns the opaque form of c.
func EncodeCursor(c Cursor) string {
	b, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(b)
}

// DecodeCursor parses an opaque cursor. Empty input yields a nil Cursor.
func DecodeCursor(s string) (Cursor, error) {
	if s == "" {
		return nil, nil
	}
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return nil, BadRequest("invalid cursor")
	}
	var c Cursor
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, BadRequest("invalid cursor")
	}
	return c, nil
}

// Limit parses ?limit= with a default and maximum.
func Limit(r *http.Request, def, max int) (int, error) {
	s := r.URL.Query().Get("limit")
	if s == "" {
		return def, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 {
		return 0, BadRequest("limit must be a positive integer")
	}
	if n > max {
		n = max
	}
	return n, nil
}

// Actor returns who is making the request: the X-Actor header if present
// (agents send e.g. "agent:claude-opus-5-5"), else "user". Body fields such as
// recorded_by take precedence where the contract has them.
func Actor(r *http.Request) string {
	if a := strings.TrimSpace(r.Header.Get("X-Actor")); a != "" {
		return a
	}
	return "user"
}
