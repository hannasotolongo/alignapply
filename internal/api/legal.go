package api

import (
	"bytes"
	"html"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"strings"
)

// Legal and contact pages are rendered on the server as complete HTML so
// they work without JavaScript. Payment processors and Google review these
// pages with crawlers that may not run scripts.
var legalPages = map[string]struct {
	file  string
	title string
}{
	"/terms":   {"legal/terms.html", "Terms of Service"},
	"/privacy": {"legal/privacy.html", "Privacy Policy"},
	"/refunds": {"legal/refunds.html", "Refund Policy"},
	"/contact": {"legal/contact.html", "Contact"},
}

var legalShell = template.Must(template.New("legal").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Title}} · CaseMade</title>
<link rel="icon" href="/favicon.svg" type="image/svg+xml">
<link rel="preconnect" href="https://fonts.googleapis.com">
<link rel="preconnect" href="https://fonts.gstatic.com" crossorigin>
<link rel="stylesheet" href="https://fonts.googleapis.com/css2?family=Inter:wght@400;500;600;700;800&display=swap">
<link rel="stylesheet" href="/styles.css">
</head>
<body>
<header class="site-nav"><div class="container">
  <a class="brand" href="/"><span class="brand-mark">C</span>CaseMade</a>
  <nav class="row">
    <a class="btn btn-ghost btn-sm" href="/pricing">Pricing</a>
    <a class="btn btn-primary btn-sm" href="/app/jobs">Open app</a>
  </nav>
</div></header>
<main class="legal container">{{.Body}}</main>
<footer class="site-footer"><div class="container row-between">
  <span>© CaseMade</span>
  <nav class="footer-links">
    <a href="/terms">Terms</a><a href="/privacy">Privacy</a><a href="/refunds">Refunds</a><a href="/contact">Contact</a>
  </nav>
</div></footer>
</body>
</html>`))

func (s *Server) handleLegalPage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		methodNotAllowed(w, "GET, HEAD")
		return
	}

	page, ok := legalPages[strings.TrimSuffix(r.URL.Path, "/")]
	if !ok || s.web.Static == nil {
		http.NotFound(w, r)
		return
	}

	content, err := fs.ReadFile(s.web.Static, page.file)
	if err != nil {
		log.Printf("read legal page %s: %v", page.file, err)
		http.NotFound(w, r)
		return
	}

	body := strings.ReplaceAll(string(content), "{{SUPPORT_EMAIL}}", supportEmailHTML(s.web.SupportEmail))

	var out bytes.Buffer
	if err := legalShell.Execute(&out, struct {
		Title string
		Body  template.HTML
	}{
		Title: page.title,
		// The page bodies are our own embedded files, not user input.
		Body: template.HTML(body),
	}); err != nil {
		log.Printf("render legal page %s: %v", page.file, err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	setSecurityHeaders(w)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=300")
	_, _ = w.Write(out.Bytes())
}

func supportEmailHTML(email string) string {
	email = strings.TrimSpace(email)
	if email == "" || !strings.Contains(email, "@") {
		return `<strong>our support team</strong> (contact email coming soon)`
	}
	escaped := html.EscapeString(email)
	return `<a href="mailto:` + escaped + `"><strong>` + escaped + `</strong></a>`
}
