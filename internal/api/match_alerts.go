package api

import (
	"context"
	"fmt"
	"html"
	"log"
	"net/http"
	"net/mail"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
	_ "time/tzdata" // the daily send window is in US Eastern time
	"unicode/utf8"

	"github.com/hannasotolongo/casemade-backend/internal/jobs"
	"github.com/hannasotolongo/casemade-backend/internal/mailer"
	"github.com/hannasotolongo/casemade-backend/internal/repository"
)

// Daily match emails. After the free trial a visitor can ask Jane to email
// new matches for the same job and place every morning:
//
//   - POST /api/v1/alerts saves the request and sends a confirmation email;
//     nothing else is sent until they click it (so nobody can sign someone
//     else up).
//   - /alerts/confirm turns the alert on and sends the first matches.
//   - Each morning (US Eastern) Jane scores her saved jobs against their
//     résumé and emails up to 5 matches they haven't been sent before. No
//     new matches, no email.
//   - Every email has a one-click unsubscribe, which also deletes the
//     résumé text kept for the alert.
//
// Daily emails go out only when COMPANY_POSTAL_ADDRESS is set (the law
// requires a mailing address in marketing email), and at most
// MATCH_ALERTS_DAILY_CAP a day (default 80, inside Resend's free 100/day).

const (
	alertMatchesPerEmail  = 5
	alertsPerEmailAddress = 3
	alertBatch            = 20
	alertTick             = 10 * time.Minute
	alertConfirmResendGap = 10 * time.Minute
	defaultAlertDailyCap  = 80
)

var alertZone = func() *time.Location {
	zone, err := time.LoadLocation("America/New_York")
	if err != nil {
		return time.UTC
	}
	return zone
}()

type alertRequest struct {
	Email      string `json:"email"`
	TargetRole string `json:"targetRole"`
	Location   string `json:"location"`
	ResumeText string `json:"resumeText"`
}

// alertDay counts daily emails sent today (Eastern), across all alerts.
var alertDay = struct {
	mu   sync.Mutex
	date string
	sent int
}{}

func alertDailyCap() int {
	if value, err := strconv.Atoi(strings.TrimSpace(os.Getenv("MATCH_ALERTS_DAILY_CAP"))); err == nil && value >= 0 {
		return value
	}
	return defaultAlertDailyCap
}

// takeAlertSlot reserves one of today's daily emails, or reports the cap is reached.
func takeAlertSlot() bool {
	today := time.Now().In(alertZone).Format("2006-01-02")
	alertDay.mu.Lock()
	defer alertDay.mu.Unlock()
	if alertDay.date != today {
		alertDay.date, alertDay.sent = today, 0
	}
	if alertDay.sent >= alertDailyCap() {
		return false
	}
	alertDay.sent++
	return true
}

func giveBackAlertSlot() {
	alertDay.mu.Lock()
	if alertDay.sent > 0 {
		alertDay.sent--
	}
	alertDay.mu.Unlock()
}

func postalAddress() string { return strings.TrimSpace(os.Getenv("COMPANY_POSTAL_ADDRESS")) }

func (s *Server) alertsReady() bool {
	return s.matchAlerts != nil && s.mailer != nil && s.web.BaseURL != ""
}

// POST /api/v1/alerts
func (s *Server) handleCreateAlert(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, "POST")
		return
	}
	if !s.alertsReady() {
		writeError(w, http.StatusServiceUnavailable, "Match emails aren't available yet.")
		return
	}
	var request alertRequest
	if err := decodeApplicationJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	email := strings.ToLower(strings.TrimSpace(request.Email))
	role := strings.TrimSpace(request.TargetRole)
	place := strings.TrimSpace(request.Location)
	resume := strings.TrimSpace(strings.ReplaceAll(strings.ToValidUTF8(request.ResumeText, ""), "\x00", ""))

	parsed, err := mail.ParseAddress(email)
	switch {
	case err != nil || parsed.Address != email || len(email) > 254 || !strings.Contains(email[strings.LastIndex(email, "@")+1:], "."):
		writeError(w, http.StatusBadRequest, "Enter a real email address.")
		return
	case role == "" || utf8.RuneCountInString(role) > 100 || place == "" || utf8.RuneCountInString(place) > 100:
		writeError(w, http.StatusBadRequest, "Tell Jane the job you want and your city.")
		return
	case utf8.RuneCountInString(resume) < minResumeTextChars || utf8.RuneCountInString(resume) > maxResumeTextChars:
		writeError(w, http.StatusBadRequest, "Jane needs your résumé to score new jobs.")
		return
	}
	if !s.alertLimiter.Allow("alert:" + clientIP(r)) {
		writeError(w, http.StatusTooManyRequests, "Too many sign-ups from here. Try again in an hour.")
		return
	}

	count, err := s.matchAlerts.CountActiveAlerts(r.Context(), email)
	if err != nil {
		log.Printf("[alerts] count for new alert: %v", err)
		writeError(w, http.StatusInternalServerError, "Something went wrong. Try again.")
		return
	}
	if count >= alertsPerEmailAddress {
		writeError(w, http.StatusConflict, "You already get 3 match emails. Unsubscribe from one to add another.")
		return
	}
	token, _, err := newAuthToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Something went wrong. Try again.")
		return
	}
	alert, err := s.matchAlerts.SaveAlert(r.Context(), email, role, place, resume, token)
	if err != nil {
		log.Printf("[alerts] save: %v", err)
		writeError(w, http.StatusInternalServerError, "Something went wrong. Try again.")
		return
	}
	if alert.Confirmed {
		writeJSON(w, http.StatusOK, map[string]string{"status": "active"})
		return
	}
	if alert.ConfirmSent == nil || time.Since(*alert.ConfirmSent) > alertConfirmResendGap {
		if err := s.matchAlerts.MarkConfirmSent(r.Context(), alert.ID); err != nil {
			log.Printf("[alerts] mark confirm sent: %v", err)
		}
		go s.sendAlertConfirmation(alert)
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "confirm"})
}

func (s *Server) alertLink(path, token string) string {
	// The site lives at www; the bare domain redirects, which would break
	// email apps' one-click unsubscribe (a POST can't follow a redirect).
	base := strings.Replace(s.web.BaseURL, "://janecareers.com", "://www.janecareers.com", 1)
	return base + path + "?token=" + token
}

func (s *Server) sendAlertConfirmation(alert repository.MatchAlert) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	link := s.alertLink("/alerts/confirm", alert.Token)
	what := fmt.Sprintf("%s jobs near %s", alert.TargetRole, alert.Location)
	err := s.mailer.Send(ctx, mailer.Message{
		To:      alert.Email,
		Subject: "Confirm your daily job matches from Jane",
		Text: "Tap the link to start getting new " + what + " every morning, scored against your résumé.\n\n" +
			"Confirm: " + link + "\n\nIf you didn't ask for this, ignore this email and nothing will be sent.\n\n— Jane Careers",
		HTML: emailShell(
			`<h1 style="margin:0 0 12px;font-size:24px">Confirm your daily matches</h1>`+
				`<p style="line-height:1.6;margin:0 0 22px">Jane will email you new <b>`+html.EscapeString(what)+`</b> every morning, scored against your résumé.</p>`+
				`<p style="margin:0 0 26px"><a href="`+html.EscapeString(link)+`" style="background:#111;color:#fff;padding:14px 22px;border-radius:12px;text-decoration:none;font-weight:700;display:inline-block">Yes, send me my matches</a></p>`+
				`<p style="color:#666;font-size:13px;line-height:1.5;margin:0">If you didn't ask for this, ignore this email and nothing will be sent.</p>`,
			""),
	})
	if err != nil {
		log.Printf("[alerts] confirmation email to alert %d: %v", alert.ID, err)
	}
}

// GET /alerts/confirm?token=
func (s *Server) handleConfirmAlert(w http.ResponseWriter, r *http.Request) {
	if !s.alertsReady() {
		alertPage(w, http.StatusServiceUnavailable, "Try again soon", "Match emails aren't available right now.")
		return
	}
	alert, ok, err := s.matchAlerts.ConfirmAlert(r.Context(), strings.TrimSpace(r.URL.Query().Get("token")))
	if err != nil {
		log.Printf("[alerts] confirm: %v", err)
		alertPage(w, http.StatusInternalServerError, "Something went wrong", "Try the link again in a minute.")
		return
	}
	if !ok {
		alertPage(w, http.StatusNotFound, "Link not found", "This link doesn't work anymore. Run a free search on Jane to sign up again.")
		return
	}
	if alert.EmailsSent == 0 && alert.LastSentAt == nil {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			s.sendAlertDigest(ctx, alert)
		}()
	}
	alertPage(w, http.StatusOK, "You're in.",
		fmt.Sprintf("Jane will email you new %s jobs near %s every morning. Your first matches are on the way.", alert.TargetRole, alert.Location))
}

// GET or POST /alerts/unsubscribe?token= (POST is the one-click unsubscribe
// email apps use).
func (s *Server) handleUnsubscribeAlert(w http.ResponseWriter, r *http.Request) {
	if s.matchAlerts == nil {
		alertPage(w, http.StatusServiceUnavailable, "Try again soon", "Unsubscribing isn't available right now.")
		return
	}
	_, err := s.matchAlerts.Unsubscribe(r.Context(), strings.TrimSpace(r.URL.Query().Get("token")))
	if err != nil {
		log.Printf("[alerts] unsubscribe: %v", err)
		alertPage(w, http.StatusInternalServerError, "Something went wrong", "Try the link again in a minute.")
		return
	}
	if r.Method == http.MethodPost {
		w.WriteHeader(http.StatusOK)
		return
	}
	alertPage(w, http.StatusOK, "You're unsubscribed.", "Jane won't email you matches anymore, and the résumé kept for them is deleted.")
}

// alertPage is the small page shown after a confirm or unsubscribe link.
func alertPage(w http.ResponseWriter, status int, title, message string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.WriteHeader(status)
	fmt.Fprintf(w, `<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>%s · Jane Careers</title><link rel="icon" href="/favicon.ico"></head>
<body style="margin:0;min-height:100vh;display:grid;place-items:center;background:#050506;color:#fff;font-family:-apple-system,BlinkMacSystemFont,Inter,Arial,sans-serif">
<main style="max-width:440px;padding:32px;text-align:center">
<img src="/jane-mini.png" alt="" width="110" height="112">
<h1 style="font-size:32px;margin:16px 0 10px;letter-spacing:-0.02em">%s</h1>
<p style="color:#b0b0b8;line-height:1.6;margin:0 0 26px">%s</p>
<a href="/" style="display:inline-block;background:#fff;color:#000;padding:13px 22px;border-radius:12px;font-weight:700;text-decoration:none">Go to Jane Careers</a>
</main></body></html>`, html.EscapeString(title), html.EscapeString(title), html.EscapeString(message))
}

// RunMatchAlerts sends the daily match emails until ctx ends.
func (s *Server) RunMatchAlerts(ctx context.Context) {
	if !s.alertsReady() {
		return
	}
	timer := time.NewTimer(2 * time.Minute)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		s.runMatchAlertsOnce(ctx)
		timer.Reset(alertTick)
	}
}

func (s *Server) runMatchAlertsOnce(ctx context.Context) {
	if postalAddress() == "" {
		return
	}
	// Mornings through evenings, Eastern: new matches arrive with coffee.
	if hour := time.Now().In(alertZone).Hour(); hour < 7 || hour >= 21 {
		return
	}
	due, err := s.matchAlerts.DueAlerts(ctx, time.Now().Add(-20*time.Hour), alertBatch)
	if err != nil {
		log.Printf("[alerts] due alerts: %v", err)
		return
	}
	for _, alert := range due {
		if ctx.Err() != nil {
			return
		}
		if !s.sendAlertDigest(ctx, alert) && dailyCapReached() {
			log.Printf("[alerts] daily cap of %d emails reached; the rest wait for tomorrow", alertDailyCap())
			return
		}
		time.Sleep(time.Second) // stay well under the email service's rate limit
	}
}

func dailyCapReached() bool {
	today := time.Now().In(alertZone).Format("2006-01-02")
	alertDay.mu.Lock()
	defer alertDay.mu.Unlock()
	return alertDay.date == today && alertDay.sent >= alertDailyCap()
}

// sendAlertDigest emails the alert's new matches, if there are any, and
// reports whether an email went out.
func (s *Server) sendAlertDigest(ctx context.Context, alert repository.MatchAlert) bool {
	if postalAddress() == "" {
		return false
	}
	matches := s.newAlertMatches(ctx, alert)
	if len(matches) == 0 {
		if err := s.matchAlerts.RecordAlertRun(ctx, alert.ID, nil, false); err != nil {
			log.Printf("[alerts] record empty run %d: %v", alert.ID, err)
		}
		return false
	}
	if !takeAlertSlot() {
		return false
	}
	ids := make([]string, 0, len(matches))
	for _, match := range matches {
		ids = append(ids, alertJobKey(match))
	}
	sendCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	if err := s.mailer.Send(sendCtx, s.alertDigestMessage(alert, matches)); err != nil {
		giveBackAlertSlot()
		log.Printf("[alerts] digest email for alert %d: %v", alert.ID, err)
		// Try again tomorrow rather than every few minutes.
		_ = s.matchAlerts.RecordAlertRun(ctx, alert.ID, nil, false)
		return false
	}
	if err := s.matchAlerts.RecordAlertRun(ctx, alert.ID, ids, true); err != nil {
		log.Printf("[alerts] record run %d: %v", alert.ID, err)
	}
	log.Printf("[alerts] sent %d matches for alert %d", len(matches), alert.ID)
	return true
}

func alertJobKey(match jobs.JobMatch) string {
	if match.ID != "" {
		return match.ID
	}
	return match.ApplyURL
}

// newAlertMatches scores Jane's saved jobs against the alert's résumé and
// returns the best ones near the place that haven't been emailed before.
func (s *Server) newAlertMatches(ctx context.Context, alert repository.MatchAlert) []jobs.JobMatch {
	if s.alertJobs == nil {
		return nil
	}
	search := jobs.SearchRequest{ResumeText: alert.ResumeText, TargetRole: alert.TargetRole, Location: alert.Location}
	normalizeSearchRequest(&search)
	searchCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	found, err := s.alertJobs.Search(searchCtx, search)
	if err != nil {
		log.Printf("[alerts] search for alert %d: %v", alert.ID, err)
		return nil
	}
	processed := s.pipeline.Process(search, found)

	sent := make(map[string]bool, len(alert.SentJobIDs))
	for _, id := range alert.SentJobIDs {
		sent[id] = true
	}
	scored := make([]jobs.JobMatch, 0, len(processed))
	for _, job := range processed {
		match := previewOrScore(search, job)
		match.DistanceMiles = job.DistanceMiles
		if match.MatchLevel == "Insufficient Evidence" || match.MatchLevel == previewMatchLevel || strings.TrimSpace(match.ApplyURL) == "" {
			continue
		}
		if sent[alertJobKey(match)] {
			continue
		}
		scored = append(scored, match)
	}
	sortJobMatches(scored)
	scored = nearbyTrialMatches(scored, jobs.StateOfPlace(alert.Location))
	if len(scored) > alertMatchesPerEmail {
		scored = scored[:alertMatchesPerEmail]
	}
	return scored
}

func (s *Server) alertDigestMessage(alert repository.MatchAlert, matches []jobs.JobMatch) mailer.Message {
	unsubscribe := s.alertLink("/alerts/unsubscribe", alert.Token)
	noun := "matches"
	if len(matches) == 1 {
		noun = "match"
	}
	subject := fmt.Sprintf("%d new %s %s near %s", len(matches), alert.TargetRole, noun, alert.Location)

	var text strings.Builder
	var rows strings.Builder
	fmt.Fprintf(&text, "Good morning! Jane found %d new %s jobs near %s that fit your résumé.\n\n", len(matches), alert.TargetRole, alert.Location)
	for _, match := range matches {
		meta := []string{}
		for _, part := range []string{match.Company, match.Location} {
			if strings.TrimSpace(part) != "" {
				meta = append(meta, strings.TrimSpace(part))
			}
		}
		if match.DistanceMiles != nil {
			meta = append(meta, fmt.Sprintf("%.0f miles away", *match.DistanceMiles))
		}
		fmt.Fprintf(&text, "%d%% match · %s\n%s\nApply on their site: %s\n\n", match.MatchPercentage, match.Title, strings.Join(meta, " · "), match.ApplyURL)
		fmt.Fprintf(&rows, `<tr><td style="padding:14px 0;border-bottom:1px solid #eee;vertical-align:top;width:64px">`+
			`<div style="width:52px;height:52px;border-radius:50%%;border:3px solid #111;text-align:center;line-height:52px;font-weight:800;font-size:15px">%d%%</div></td>`+
			`<td style="padding:14px 0 14px 12px;border-bottom:1px solid #eee;vertical-align:top">`+
			`<a href="%s" style="color:#111;font-weight:800;font-size:17px;text-decoration:none">%s</a>`+
			`<div style="color:#666;font-size:14px;margin-top:4px">%s</div>`+
			`<a href="%s" style="display:inline-block;margin-top:10px;color:#111;font-weight:700;font-size:14px">Apply on their site →</a></td></tr>`,
			match.MatchPercentage, html.EscapeString(match.ApplyURL), html.EscapeString(match.Title),
			html.EscapeString(strings.Join(meta, " · ")), html.EscapeString(match.ApplyURL))
	}
	signup := s.web.BaseURL + "/signup"
	fmt.Fprintf(&text, "Want a tailored résumé for each one and interview practice? %s\n\n", signup)
	footerText := fmt.Sprintf("You get this because you asked Jane for daily %s matches near %s. Unsubscribe: %s\nJane Careers · %s",
		alert.TargetRole, alert.Location, unsubscribe, postalAddress())
	text.WriteString(footerText)

	body := `<h1 style="margin:0 0 6px;font-size:24px">Good morning!</h1>` +
		`<p style="line-height:1.6;margin:0 0 8px">Jane found <b>` + strconv.Itoa(len(matches)) + ` new ` + html.EscapeString(alert.TargetRole) + ` ` + noun + `</b> near ` + html.EscapeString(alert.Location) + ` that fit your résumé.</p>` +
		`<table role="presentation" style="width:100%;border-collapse:collapse;margin:8px 0 22px">` + rows.String() + `</table>` +
		`<div style="background:#f4f4f5;border-radius:14px;padding:18px 20px;margin:0 0 8px">` +
		`<p style="margin:0 0 12px;line-height:1.5"><b>Get every match, a tailored résumé for each job, and interview practice.</b></p>` +
		`<a href="` + html.EscapeString(signup) + `" style="background:#111;color:#fff;padding:12px 20px;border-radius:10px;text-decoration:none;font-weight:700;display:inline-block">Try Jane Pro</a></div>`
	footer := `You get this because you asked Jane for daily ` + html.EscapeString(alert.TargetRole) + ` matches near ` + html.EscapeString(alert.Location) + `. ` +
		`<a href="` + html.EscapeString(unsubscribe) + `" style="color:#666">Unsubscribe</a><br>Jane Careers · ` + html.EscapeString(postalAddress())

	return mailer.Message{
		To:      alert.Email,
		Subject: subject,
		Text:    text.String(),
		HTML:    emailShell(body, footer),
		Headers: map[string]string{
			"List-Unsubscribe":      "<" + unsubscribe + ">",
			"List-Unsubscribe-Post": "List-Unsubscribe=One-Click",
		},
	}
}

// emailShell wraps an email body in Jane's simple light layout.
func emailShell(body, footer string) string {
	out := `<div style="background:#ffffff;padding:24px 12px"><div style="font-family:-apple-system,BlinkMacSystemFont,Inter,Arial,sans-serif;max-width:560px;margin:0 auto;color:#111">` +
		`<p style="margin:0 0 18px;font-weight:800;font-size:18px;letter-spacing:-0.01em">Jane <span style="color:#888;font-weight:600;font-size:12px;letter-spacing:0.3em">CAREERS</span></p>` +
		body
	if footer != "" {
		out += `<p style="color:#888;font-size:12px;line-height:1.6;margin:22px 0 0">` + footer + `</p>`
	}
	return out + `</div></div>`
}
