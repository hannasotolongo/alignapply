package api

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"sync"
)

// staticETags caches a content hash per embedded file. Embedded files have
// no modification time, so without an ETag browsers re-download app.js and
// styles.css on every visit; with one they get a tiny 304 instead.
var staticETags sync.Map

func staticETag(static fs.FS, name string) string {
	if v, ok := staticETags.Load(name); ok {
		return v.(string)
	}
	data, err := fs.ReadFile(static, name)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	tag := `"` + hex.EncodeToString(sum[:8]) + `"`
	staticETags.Store(name, tag)
	return tag
}

// versionedIndex is index.html with each asset link tagged by the asset's
// content hash (/app.js?v=1a2b…). A new deploy changes the links, so phones
// and browsers load the new files at once instead of a saved old copy.
var versionedIndex struct {
	once sync.Once
	data []byte
	etag string
}

func indexWithVersions(static fs.FS) ([]byte, string) {
	versionedIndex.once.Do(func() {
		data, err := fs.ReadFile(static, "index.html")
		if err != nil {
			return
		}
		page := string(data)
		for _, asset := range []string{"styles.css", "app.js"} {
			tag := strings.Trim(staticETag(static, asset), `"`)
			if tag == "" {
				continue
			}
			page = strings.ReplaceAll(page, `"/`+asset+`"`, `"/`+asset+`?v=`+tag+`"`)
		}
		sum := sha256.Sum256([]byte(page))
		versionedIndex.data = []byte(page)
		versionedIndex.etag = `"` + hex.EncodeToString(sum[:8]) + `"`
	})
	return versionedIndex.data, versionedIndex.etag
}

func serveIndex(w http.ResponseWriter, r *http.Request, static fs.FS) bool {
	data, etag := indexWithVersions(static)
	if data == nil {
		return false
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("ETag", etag)
	if match := r.Header.Get("If-None-Match"); match != "" && match == etag {
		w.WriteHeader(http.StatusNotModified)
		return true
	}
	if r.Method == http.MethodGet {
		_, _ = w.Write(data)
	}
	return true
}

// newStaticHandler serves the embedded web app. Paths that are not real
// files (e.g. /app/jobs) fall back to index.html so client-side routing works.
func newStaticHandler(static fs.FS) http.Handler {
	files := http.FileServer(http.FS(static))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			methodNotAllowed(w, "GET, HEAD")
			return
		}

		setSecurityHeaders(w)

		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if name == "" {
			name = "index.html"
		}

		if strings.HasPrefix(name, "legal/") {
			http.NotFound(w, r)
			return
		}

		if name == "index.html" && serveIndex(w, r, static) {
			return
		}

		if info, err := fs.Stat(static, name); err == nil && !info.IsDir() {
			if tag := staticETag(static, name); tag != "" {
				w.Header().Set("ETag", tag)
			}
			if name == "index.html" {
				w.Header().Set("Cache-Control", "no-cache")
			} else if r.URL.Query().Get("v") != "" {
				// Versioned links change with every deploy, so the file at
				// this exact address never changes.
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			} else {
				// Short cache: new deploys show up within minutes, and the
				// ETag makes the re-check after that almost free.
				w.Header().Set("Cache-Control", "public, max-age=300")
			}
			files.ServeHTTP(w, r)
			return
		}

		// Guide pages live in folders as index.html, so search engines get
		// real HTML at clean URLs like /interview-questions/office-manager.
		if path.Ext(name) == "" {
			page := path.Join(name, "index.html")
			if data, err := fs.ReadFile(static, page); err == nil {
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				w.Header().Set("Cache-Control", "public, max-age=3600")
				if r.Method == http.MethodGet {
					_, _ = w.Write(data)
				}
				return
			}
		}

		// Missing asset files should 404, not return the HTML shell.
		if path.Ext(name) != "" {
			http.NotFound(w, r)
			return
		}

		if serveIndex(w, r, static) {
			return
		}
		w.Header().Set("Cache-Control", "no-cache")
		if tag := staticETag(static, "index.html"); tag != "" {
			w.Header().Set("ETag", tag)
		}
		r2 := r.Clone(r.Context())
		r2.URL.Path = "/"
		files.ServeHTTP(w, r2)
	})
}

func setSecurityHeaders(w http.ResponseWriter) {
	h := w.Header()
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("X-Frame-Options", "DENY")
	h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
	// The microphone is used only for spoken interview practice on our own
	// pages; camera and location are never used.
	h.Set("Permissions-Policy", "microphone=(self), camera=(), geolocation=()")
	h.Set(
		"Content-Security-Policy",
		"default-src 'self'; "+
			// Cloudflare Web Analytics: the beacon script and where it reports visits.
			"script-src 'self' https://static.cloudflareinsights.com; "+
			"style-src 'self' https://fonts.googleapis.com; "+
			"font-src https://fonts.gstatic.com; "+
			"img-src 'self' data:; "+
			"connect-src 'self' https://cloudflareinsights.com; "+
			"frame-ancestors 'none'; "+
			"base-uri 'self'; "+
			"form-action 'self'",
	)
}
