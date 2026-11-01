package httpapi

import (
	"crypto/subtle"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
	"time"
)

// Auth requires "Authorization: Bearer <token>" on /api writes when token is
// non-empty. Reads stay open. Stateless: nothing is stored server-side.
func Auth(token string, next http.Handler) http.Handler {
	if token == "" {
		return next
	}
	want := []byte("Bearer " + token)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") && r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions {
			got := []byte(r.Header.Get("Authorization"))
			if subtle.ConstantTimeCompare(got, want) != 1 {
				WriteError(w, r, nil, Errf(http.StatusUnauthorized, CodeUnauthorized, "missing or wrong bearer token"))
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// Recover turns panics into 500 responses.
func Recover(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				if v == http.ErrAbortHandler {
					panic(v)
				}
				log.Error("panic", "path", r.URL.Path, "panic", v, "stack", string(debug.Stack()))
				WriteError(w, r, nil, Errf(http.StatusInternalServerError, CodeInternal, "internal error"))
			}
		}()
		next.ServeHTTP(w, r)
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
	bytes  int64
}

func (s *statusWriter) WriteHeader(code int) { s.status = code; s.ResponseWriter.WriteHeader(code) }
func (s *statusWriter) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	n, err := s.ResponseWriter.Write(b)
	s.bytes += int64(n)
	return n, err
}
func (s *statusWriter) Unwrap() http.ResponseWriter { return s.ResponseWriter }

// AccessLog logs one line per request at info level. Off by default
// (INV_ACCESS_LOG=1) because logs are disk writes on the NAS.
func AccessLog(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w}
		next.ServeHTTP(sw, r)
		log.Info("http", "method", r.Method, "path", r.URL.Path, "status", sw.status,
			"bytes", sw.bytes, "ms", time.Since(start).Milliseconds())
	})
}
