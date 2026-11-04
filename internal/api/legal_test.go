package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hannasotolongo/casemade-backend/internal/web"
)

func TestLegalPagesRenderWithoutJavaScript(t *testing.T) {
	server := NewServer(Dependencies{
		Web: WebConfig{Static: web.Static(), SupportEmail: "help@casemade.test"},
	})

	for path, want := range map[string]string{
		"/terms":   "Terms of Service",
		"/privacy": "Privacy Policy",
		"/refunds": "Refund &amp; Cancellation Policy",
		"/contact": "Contact us",
	} {
		recorder := httptest.NewRecorder()
		server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		body := recorder.Body.String()

		if recorder.Code != http.StatusOK || !strings.Contains(body, want) {
			t.Fatalf("%s: status %d, missing %q", path, recorder.Code, want)
		}
		if strings.Contains(body, "{{SUPPORT_EMAIL}}") {
			t.Fatalf("%s: support email placeholder not filled", path)
		}
		if path != "/terms" && !strings.Contains(body, "mailto:help@casemade.test") {
			t.Fatalf("%s: support email link missing", path)
		}
	}

	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/legal/terms.html", nil))
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("raw legal fragment status = %d, want 404", recorder.Code)
	}
}
