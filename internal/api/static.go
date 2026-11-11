package api

import (
	"io/fs"
	"net/http"
	"path"
	"strings"
)

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

		if info, err := fs.Stat(static, name); err == nil && !info.IsDir() {
			if name == "index.html" {
				w.Header().Set("Cache-Control", "no-cache")
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

		w.Header().Set("Cache-Control", "no-cache")
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
