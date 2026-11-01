// Package webui serves the embedded Svelte SPA (built into ./dist by
// `make web`). Unknown paths fall back to index.html for client routing.
package webui

import (
	"bytes"
	"embed"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"
	"time"
)

//go:embed all:dist
var dist embed.FS

// Handler serves the SPA. Hashed assets under /_app/immutable/ are cached
// forever; everything else (index.html) is revalidated.
func Handler() http.Handler {
	root, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if name == "" {
			name = "index.html"
		}
		if !serveFile(w, r, root, name) {
			serveFile(w, r, root, "index.html")
		}
	})
}

func serveFile(w http.ResponseWriter, r *http.Request, root fs.FS, name string) bool {
	st, err := fs.Stat(root, name)
	if err != nil || st.IsDir() {
		return false
	}
	ctype := mime.TypeByExtension(path.Ext(name))
	if ctype == "" {
		ctype = "application/octet-stream"
	}
	h := w.Header()
	h.Set("Content-Type", ctype)
	h.Add("Vary", "Accept-Encoding")
	if strings.HasPrefix(name, "_app/immutable/") {
		h.Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		h.Set("Cache-Control", "no-cache")
	}
	// Prefer precompressed siblings written by the build (adapter-static precompress).
	ae := r.Header.Get("Accept-Encoding")
	for _, enc := range []struct{ token, ext string }{{"br", ".br"}, {"gzip", ".gz"}} {
		if strings.Contains(ae, enc.token) {
			if b, err := fs.ReadFile(root, name+enc.ext); err == nil {
				h.Set("Content-Encoding", enc.token)
				http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(b))
				return true
			}
		}
	}
	b, err := fs.ReadFile(root, name)
	if err != nil {
		return false
	}
	http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(b))
	return true
}
