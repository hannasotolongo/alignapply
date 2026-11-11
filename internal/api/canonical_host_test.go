package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWithCanonicalHost(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	h := WithCanonicalHost(ok)
	cases := []struct {
		method, host, path string
		code               int
		loc                string
	}{
		{"GET", "janecareers.com", "/pricing?x=1", 301, "https://www.janecareers.com/pricing?x=1"},
		{"GET", "www.janecareers.com", "/pricing", 200, ""},
		{"POST", "janecareers.com", "/api/v1/billing/webhook", 200, ""},
		{"GET", "janecareers.com", "/api/v1/billing/plans", 200, ""},
		{"GET", "janecareers.com", "/healthz", 200, ""},
		{"GET", "alignapply.onrender.com", "/", 200, ""},
	}
	for _, c := range cases {
		req := httptest.NewRequest(c.method, c.path, nil)
		req.Host = c.host
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != c.code || rec.Header().Get("Location") != c.loc {
			t.Errorf("%s %s%s: got %d %q", c.method, c.host, c.path, rec.Code, rec.Header().Get("Location"))
		}
	}
}
