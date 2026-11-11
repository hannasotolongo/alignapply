package api

import (
	"net/http"
	"strings"
)

// canonicalHost is the one address search engines should index.
const canonicalHost = "www.janecareers.com"

// WithCanonicalHost sends page visits on the bare domain (janecareers.com)
// to www.janecareers.com with a permanent redirect, so Google shows and
// ranks a single address. API calls, webhooks and non-GET requests are left
// alone so payments and sign-in keep working on either host.
func WithCanonicalHost(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := strings.ToLower(r.Host)
		if i := strings.IndexByte(host, ':'); i >= 0 {
			host = host[:i]
		}
		if host == "janecareers.com" &&
			(r.Method == http.MethodGet || r.Method == http.MethodHead) &&
			!strings.HasPrefix(r.URL.Path, "/api/") &&
			r.URL.Path != "/healthz" {
			http.Redirect(w, r, "https://"+canonicalHost+r.URL.RequestURI(), http.StatusMovedPermanently)
			return
		}
		next.ServeHTTP(w, r)
	})
}
