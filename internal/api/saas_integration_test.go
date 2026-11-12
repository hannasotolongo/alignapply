package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/hannasotolongo/casemade-backend/internal/auth"
	"github.com/hannasotolongo/casemade-backend/internal/billing"
	"github.com/hannasotolongo/casemade-backend/internal/jobs"
	"github.com/hannasotolongo/casemade-backend/internal/mailer"
	"github.com/hannasotolongo/casemade-backend/internal/repository"
	"github.com/jackc/pgx/v5/pgxpool"
)

// fakeStripe implements the handful of Stripe endpoints the backend calls.
type fakeStripe struct {
	mu            sync.Mutex
	customers     int
	subscriptions map[string]map[string]any
	canceled      []string
	lastCheckout  url.Values
}

func (f *fakeStripe) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	_ = r.ParseForm()

	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/v1/customers":
		f.customers++
		writeJSON(w, http.StatusOK, map[string]any{"id": fmt.Sprintf("cus_test_%d_%d", time.Now().UnixNano(), f.customers)})

	case r.Method == http.MethodPost && r.URL.Path == "/v1/checkout/sessions":
		f.lastCheckout = r.PostForm
		writeJSON(w, http.StatusOK, map[string]any{"url": "https://checkout.stripe.test/session"})

	case r.Method == http.MethodPost && r.URL.Path == "/v1/billing_portal/sessions":
		writeJSON(w, http.StatusOK, map[string]any{"url": "https://billing.stripe.test/portal"})

	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v1/subscriptions/"):
		id := strings.TrimPrefix(r.URL.Path, "/v1/subscriptions/")
		subscription, ok := f.subscriptions[id]
		if !ok {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": map[string]any{"type": "invalid_request_error", "message": "no such subscription"}})
			return
		}
		writeJSON(w, http.StatusOK, subscription)

	case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/v1/subscriptions/"):
		id := strings.TrimPrefix(r.URL.Path, "/v1/subscriptions/")
		f.canceled = append(f.canceled, id)
		writeJSON(w, http.StatusOK, map[string]any{"id": id, "status": "canceled"})

	default:
		writeJSON(w, http.StatusNotFound, map[string]any{"error": map[string]any{"message": "unhandled " + r.Method + " " + r.URL.Path}})
	}
}

func TestWebSaaSIntegration(t *testing.T) {
	databaseURL := os.Getenv("ALIGNAPPLY_TEST_DATABASE_URL")
	if databaseURL == "" {
		databaseURL = defaultAPITestDatabaseURL
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Skipf("PostgreSQL unavailable: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := pool.Ping(ctx); err != nil {
		t.Skipf("PostgreSQL unavailable: %v", err)
	}

	var hasBilling bool
	if err := pool.QueryRow(ctx, `SELECT to_regclass('public.subscriptions') IS NOT NULL`).Scan(&hasBilling); err != nil || !hasBilling {
		t.Skip("migration 006_web_saas.sql not applied")
	}

	userCareerRepo, _ := repository.NewUserCareerRepository(pool)
	jobRepo, _ := repository.NewJobRepository(pool)
	userJobsRepo, _ := repository.NewUserJobsRepository(pool)
	appleAuthRepo, _ := repository.NewAppleAuthRepository(pool)
	billingRepo, _ := repository.NewBillingRepository(pool)
	accountsRepo := repository.NewAccountsRepository(userCareerRepo)

	// Fake Resend: captures every email the server sends.
	var mailMu sync.Mutex
	var sentMail []map[string]any
	mailServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var message map[string]any
		_ = json.NewDecoder(r.Body).Decode(&message)
		mailMu.Lock()
		sentMail = append(sentMail, message)
		mailMu.Unlock()
		writeJSON(w, http.StatusOK, map[string]string{"id": "email_test"})
	}))
	t.Cleanup(mailServer.Close)
	mailClient, _ := mailer.NewResend("re_test_fake", "Jane Careers <hello@casemade.test>")
	mailClient.WithAPIBase(mailServer.URL)

	tokenRE := regexp.MustCompile(`token=([A-Za-z0-9_-]+)`)
	waitForToken := func(t *testing.T, to, subject string, after int) (string, int) {
		t.Helper()
		for i := 0; i < 100; i++ {
			mailMu.Lock()
			for index := after; index < len(sentMail); index++ {
				message := sentMail[index]
				recipients, _ := message["to"].([]any)
				text, _ := message["text"].(string)
				if len(recipients) == 1 && recipients[0] == to && strings.Contains(message["subject"].(string), subject) {
					mailMu.Unlock()
					if match := tokenRE.FindStringSubmatch(text); match != nil {
						return match[1], index + 1
					}
					t.Fatalf("email without token: %q", text)
				}
			}
			mailMu.Unlock()
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatalf("no %q email sent to %s", subject, to)
		return "", 0
	}

	sessionManager, err := auth.NewSessionManager("alignapply-saas-integration-test-secret-123456789")
	if err != nil {
		t.Fatal(err)
	}

	stripeFake := &fakeStripe{subscriptions: map[string]map[string]any{}}
	stripeServer := httptest.NewServer(stripeFake)
	t.Cleanup(stripeServer.Close)

	stripeClient, _ := billing.NewClient("sk_test_fake")
	stripeClient.WithAPIBase(stripeServer.URL)

	const webhookSecret = "whsec_integration"

	server := NewServer(Dependencies{
		Repositories: Repositories{
			UserCareer: userCareerRepo,
			Jobs:       jobRepo,
			UserJobs:   userJobsRepo,
			AppleAuth:  appleAuthRepo,
			Billing:    billingRepo,
			Accounts:   accountsRepo,
		},
		SessionManager: sessionManager,
		Stripe:         stripeClient,
		Mailer:         mailClient,
		BillingConfig: BillingConfig{
			WebhookSecret: webhookSecret,
			Plans: []Plan{
				{Key: "monthly", Name: "Pro Monthly", Kind: PlanKindSubscription, Interval: "month", TrialDays: 3, DisplayPrice: "$14.99", PriceID: "price_monthly"},
				{Key: "sprint", Name: "Job Search Sprint", Kind: PlanKindPass, Interval: "90 days", PassDays: 90, DisplayPrice: "$39", PriceID: "price_sprint"},
				{Key: "school", Name: "School License", Kind: PlanKindOrganization, Interval: "year", DisplayPrice: "$1,000", PriceID: "price_school"},
				{Key: "interview", Name: "Interview Prep", Kind: PlanKindSubscription, Interval: "month", Access: AccessInterview, DisplayPrice: "$8.99", PriceID: "price_interview"},
			},
		},
		Web: WebConfig{
			BaseURL: "https://casemade.test",
			Static:  fstest.MapFS{"index.html": {Data: []byte("<!doctype html><title>AlignApply</title>")}},
		},
	})
	handler := server.Handler()

	schoolDomain := fmt.Sprintf("school%d.test", time.Now().UnixNano())
	email := "owner@" + schoolDomain
	var userID string

	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM users WHERE LOWER(email) LIKE '%' || $1`, schoolDomain)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM organizations WHERE email_domain = $1`, schoolDomain)
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM stripe_webhook_events WHERE event_id LIKE 'evt_saas_%'`)
	})

	do := func(method, path string, body any, cookie *http.Cookie, headers map[string]string) *httptest.ResponseRecorder {
		var reader *bytes.Reader
		switch value := body.(type) {
		case nil:
			reader = bytes.NewReader(nil)
		case []byte:
			reader = bytes.NewReader(value)
		default:
			encoded, _ := json.Marshal(value)
			reader = bytes.NewReader(encoded)
		}
		request := httptest.NewRequest(method, path, reader)
		request.Header.Set("Content-Type", "application/json")
		if cookie != nil {
			request.AddCookie(cookie)
		}
		for key, value := range headers {
			request.Header.Set(key, value)
		}
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		return recorder
	}

	var session *http.Cookie

	t.Run("signup sets HttpOnly session cookie", func(t *testing.T) {
		response := do(http.MethodPost, "/api/v1/auth/signup", map[string]string{
			"email": email, "password": "a-strong-password", "firstName": "SaaS", "lastName": "User", "phone": "(646) 555-0100",
		}, nil, nil)
		if response.Code != http.StatusOK {
			t.Fatalf("status = %d; body = %s", response.Code, response.Body)
		}

		for _, cookie := range response.Result().Cookies() {
			if cookie.Name == auth.SessionCookieName {
				session = cookie
			}
		}
		if session == nil || !session.HttpOnly || session.SameSite != http.SameSiteLaxMode {
			t.Fatalf("session cookie missing or insecure: %+v", session)
		}

		var body webSessionResponse
		_ = json.NewDecoder(response.Body).Decode(&body)
		userID = body.User.ID
		if userID == "" {
			t.Fatal("signup returned no user ID")
		}
	})

	t.Run("duplicate signup is rejected case-insensitively", func(t *testing.T) {
		response := do(http.MethodPost, "/api/v1/auth/signup", map[string]string{
			"email": strings.ToUpper(email), "password": "a-strong-password", "firstName": "SaaS", "lastName": "User", "phone": "6465550100",
		}, nil, nil)
		if response.Code != http.StatusConflict {
			t.Fatalf("status = %d, want 409; body = %s", response.Code, response.Body)
		}
	})

	t.Run("login rejects wrong password and accepts right one", func(t *testing.T) {
		if response := do(http.MethodPost, "/api/v1/auth/login", map[string]string{"email": email, "password": "not-the-password"}, nil, nil); response.Code != http.StatusUnauthorized {
			t.Fatalf("wrong password status = %d", response.Code)
		}
		if response := do(http.MethodPost, "/api/v1/auth/login", map[string]string{"email": email, "password": "a-strong-password"}, nil, nil); response.Code != http.StatusOK {
			t.Fatalf("login status = %d; body = %s", response.Code, response.Body)
		}
	})

	t.Run("cookie authenticates /me", func(t *testing.T) {
		response := do(http.MethodGet, "/api/v1/me", nil, session, nil)
		if response.Code != http.StatusOK {
			t.Fatalf("status = %d; body = %s", response.Code, response.Body)
		}
	})

	t.Run("job matches require authentication", func(t *testing.T) {
		response := do(http.MethodPost, "/api/v1/job-matches", map[string]any{}, nil, nil)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", response.Code)
		}
	})

	t.Run("job matches run on the Free plan, trimmed", func(t *testing.T) {
		response := do(http.MethodPost, "/api/v1/job-matches", map[string]any{
			"careerProfile": map[string]any{"skills": []string{"Go"}},
			"targetRole":    "Software Engineer",
			"location":      "Miami, FL",
		}, session, nil)

		if response.Code != http.StatusOK || response.Header().Get("X-Jane-Locked") == "" {
			t.Fatalf("status = %d; locked header = %q; body = %s", response.Code, response.Header().Get("X-Jane-Locked"), response.Body)
		}
		var matches []jobs.JobMatch
		_ = json.NewDecoder(response.Body).Decode(&matches)
		if len(matches) > FreeMatchesShown {
			t.Fatalf("free search returned %d matches, want at most %d", len(matches), FreeMatchesShown)
		}
	})

	t.Run("resume recommendation requires Pro", func(t *testing.T) {
		response := do(http.MethodPost, "/api/v1/resume-recommendation", map[string]any{}, session, nil)
		if response.Code != http.StatusPaymentRequired {
			t.Fatalf("status = %d, want 402; body = %s", response.Code, response.Body)
		}
	})

	t.Run("billing status reports no plan and a trial available", func(t *testing.T) {
		response := do(http.MethodGet, "/api/v1/billing/status", nil, session, nil)
		var status billingStatusResponse
		_ = json.NewDecoder(response.Body).Decode(&status)
		if response.Code != http.StatusOK || status.Plan != "none" || !status.TrialEligible || status.EmailVerified || !status.EmailEnabled {
			t.Fatalf("status = %d; body = %+v", response.Code, status)
		}
	})

	var customerID string

	t.Run("checkout creates customer and session", func(t *testing.T) {
		if response := do(http.MethodPost, "/api/v1/billing/checkout", map[string]string{"plan": "bogus"}, session, nil); response.Code != http.StatusBadRequest {
			t.Fatalf("unknown plan status = %d", response.Code)
		}

		response := do(http.MethodPost, "/api/v1/billing/checkout", map[string]string{"plan": "monthly"}, session, nil)
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "checkout.stripe.test") {
			t.Fatalf("status = %d; body = %s", response.Code, response.Body)
		}

		if got := stripeFake.lastCheckout.Get("line_items[0][price]"); got != "price_monthly" {
			t.Fatalf("checkout price = %q", got)
		}
		if got := stripeFake.lastCheckout.Get("success_url"); got != "https://casemade.test/app/activate?checkout=success" {
			t.Fatalf("success_url = %q", got)
		}
		if got := stripeFake.lastCheckout.Get("subscription_data[trial_period_days]"); got != "3" {
			t.Fatalf("first checkout trial days = %q, want 3", got)
		}

		subscription, err := billingRepo.GetSubscription(ctx, userID)
		if err != nil || subscription.StripeCustomerID == "" {
			t.Fatalf("customer not stored: %+v, %v", subscription, err)
		}
		customerID = subscription.StripeCustomerID

		// A second checkout reuses the same customer.
		do(http.MethodPost, "/api/v1/billing/checkout", map[string]string{"plan": "monthly"}, session, nil)
		if stripeFake.customers != 1 {
			t.Fatalf("customers created = %d, want 1", stripeFake.customers)
		}
	})

	sendWebhook := func(eventID, eventType string, object map[string]any, signWith string) *httptest.ResponseRecorder {
		payload, _ := json.Marshal(map[string]any{"id": eventID, "type": eventType, "data": map[string]any{"object": object}})
		return do(http.MethodPost, "/api/v1/billing/webhook", payload, nil, map[string]string{
			"Stripe-Signature": billing.SignWebhookPayload(payload, signWith, time.Now()),
		})
	}

	t.Run("webhook with bad signature is rejected", func(t *testing.T) {
		response := sendWebhook("evt_saas_bad", "checkout.session.completed", map[string]any{}, "whsec_wrong")
		if response.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", response.Code)
		}
	})

	t.Run("checkout webhook activates Pro", func(t *testing.T) {
		periodEnd := time.Now().Add(30 * 24 * time.Hour).Unix()
		stripeFake.mu.Lock()
		stripeFake.subscriptions["sub_saas"] = map[string]any{
			"id": "sub_saas", "customer": customerID, "status": "active", "cancel_at_period_end": false,
			"items": map[string]any{"data": []any{map[string]any{"current_period_end": periodEnd, "price": map[string]any{"id": "price_monthly"}}}},
		}
		stripeFake.mu.Unlock()

		response := sendWebhook("evt_saas_checkout", "checkout.session.completed", map[string]any{
			"mode": "subscription", "subscription": "sub_saas", "customer": customerID,
		}, webhookSecret)
		if response.Code != http.StatusOK {
			t.Fatalf("status = %d; body = %s", response.Code, response.Body)
		}

		// Replays are acknowledged without reprocessing.
		if replay := sendWebhook("evt_saas_checkout", "checkout.session.completed", map[string]any{}, webhookSecret); replay.Code != http.StatusOK {
			t.Fatalf("replay status = %d", replay.Code)
		}

		status := do(http.MethodGet, "/api/v1/billing/status", nil, session, nil)
		var body billingStatusResponse
		_ = json.NewDecoder(status.Body).Decode(&body)
		if body.Plan != "pro" || body.PlanKey != "monthly" || body.CurrentPeriodEnd == nil || !body.CanManageBilling {
			t.Fatalf("billing status after webhook = %+v", body)
		}
	})

	t.Run("Pro passes the gate and checkout is blocked", func(t *testing.T) {
		// Empty body: passes the Pro gate and fails validation instead.
		response := do(http.MethodPost, "/api/v1/resume-recommendation", map[string]any{}, session, nil)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400 (past gate); body = %s", response.Code, response.Body)
		}

		if checkout := do(http.MethodPost, "/api/v1/billing/checkout", map[string]string{"plan": "monthly"}, session, nil); checkout.Code != http.StatusConflict {
			t.Fatalf("checkout while Pro status = %d, want 409", checkout.Code)
		}

		if portal := do(http.MethodPost, "/api/v1/billing/portal", nil, session, nil); portal.Code != http.StatusOK {
			t.Fatalf("portal status = %d; body = %s", portal.Code, portal.Body)
		}
	})

	t.Run("cancellation webhook removes Pro", func(t *testing.T) {
		stripeFake.mu.Lock()
		stripeFake.subscriptions["sub_saas"]["status"] = "canceled"
		stripeFake.mu.Unlock()

		response := sendWebhook("evt_saas_deleted", "customer.subscription.deleted", map[string]any{"id": "sub_saas"}, webhookSecret)
		if response.Code != http.StatusOK {
			t.Fatalf("status = %d", response.Code)
		}

		if gated := do(http.MethodPost, "/api/v1/resume-recommendation", map[string]any{}, session, nil); gated.Code != http.StatusPaymentRequired {
			t.Fatalf("status after cancel = %d, want 402", gated.Code)
		}
	})

	t.Run("Interview Prep unlocks only interview practice", func(t *testing.T) {
		if fit := do(http.MethodPost, "/api/v1/fit-check", map[string]any{}, nil, nil); fit.Code != http.StatusUnauthorized || !strings.Contains(fit.Body.String(), "sign_up_required") {
			t.Fatalf("anonymous fit check status = %d; body = %s", fit.Code, fit.Body)
		}
		// The Free plan gets one practice set; past the gate there is no AI in tests (503).
		if free := do(http.MethodPost, "/api/v1/interview-questions", map[string]any{}, session, nil); free.Code != http.StatusServiceUnavailable {
			t.Fatalf("interview practice on the Free plan = %d; body = %s", free.Code, free.Body)
		}

		stripeFake.mu.Lock()
		stripeFake.subscriptions["sub_interview"] = map[string]any{
			"id": "sub_interview", "customer": customerID, "status": "active", "cancel_at_period_end": false,
			"items": map[string]any{"data": []any{map[string]any{"current_period_end": time.Now().Add(30 * 24 * time.Hour).Unix(), "price": map[string]any{"id": "price_interview"}}}},
		}
		stripeFake.mu.Unlock()
		if response := sendWebhook("evt_saas_interview", "customer.subscription.created", map[string]any{"id": "sub_interview"}, webhookSecret); response.Code != http.StatusOK {
			t.Fatalf("webhook status = %d; body = %s", response.Code, response.Body)
		}

		var status billingStatusResponse
		_ = json.NewDecoder(do(http.MethodGet, "/api/v1/billing/status", nil, session, nil).Body).Decode(&status)
		if status.Plan != "interview" || status.PlanKey != "interview" {
			t.Fatalf("billing status = %+v", status)
		}

		// Past the plan gate (no AI configured in tests, so 503).
		if practice := do(http.MethodPost, "/api/v1/interview-questions", map[string]any{}, session, nil); practice.Code != http.StatusServiceUnavailable {
			t.Fatalf("interview practice on Interview Prep = %d; body = %s", practice.Code, practice.Body)
		}
		for _, path := range []string{"/api/v1/resume-recommendation", "/api/v1/fit-check", "/api/v1/application-kit"} {
			if gated := do(http.MethodPost, path, map[string]any{}, session, nil); gated.Code != http.StatusPaymentRequired {
				t.Fatalf("%s on Interview Prep = %d, want 402", path, gated.Code)
			}
		}
		checkout := do(http.MethodPost, "/api/v1/billing/checkout", map[string]string{"plan": "monthly"}, session, nil)
		if checkout.Code != http.StatusConflict || !strings.Contains(checkout.Body.String(), "switch_in_portal") {
			t.Fatalf("upgrade checkout = %d; body = %s", checkout.Code, checkout.Body)
		}

		stripeFake.mu.Lock()
		stripeFake.subscriptions["sub_interview"]["status"] = "canceled"
		stripeFake.mu.Unlock()
		sendWebhook("evt_saas_interview_end", "customer.subscription.deleted", map[string]any{"id": "sub_interview"}, webhookSecret)
	})

	t.Run("a second subscription gets no new trial", func(t *testing.T) {
		response := do(http.MethodPost, "/api/v1/billing/checkout", map[string]string{"plan": "monthly"}, session, nil)
		if response.Code != http.StatusOK {
			t.Fatalf("status = %d; body = %s", response.Code, response.Body)
		}
		if got := stripeFake.lastCheckout.Get("subscription_data[trial_period_days]"); got != "" {
			t.Fatalf("repeat checkout trial days = %q, want none", got)
		}
	})

	t.Run("Sprint pass checkout and payment grant 90 days of Pro", func(t *testing.T) {
		response := do(http.MethodPost, "/api/v1/billing/checkout", map[string]string{"plan": "sprint"}, session, nil)
		if response.Code != http.StatusOK || stripeFake.lastCheckout.Get("mode") != "payment" ||
			stripeFake.lastCheckout.Get("metadata[plan_key]") != "sprint" {
			t.Fatalf("sprint checkout = %d %s; form = %v", response.Code, response.Body, stripeFake.lastCheckout)
		}

		// Unpaid sessions grant nothing.
		sendWebhook("evt_saas_pass_unpaid", "checkout.session.completed", map[string]any{
			"mode": "payment", "customer": customerID, "client_reference_id": userID,
			"payment_status": "unpaid", "metadata": map[string]string{"plan_key": "sprint"},
		}, webhookSecret)
		if gated := do(http.MethodPost, "/api/v1/resume-recommendation", map[string]any{}, session, nil); gated.Code != http.StatusPaymentRequired {
			t.Fatalf("unpaid pass granted access: %d", gated.Code)
		}

		paid := sendWebhook("evt_saas_pass_paid", "checkout.session.completed", map[string]any{
			"mode": "payment", "customer": customerID, "client_reference_id": userID,
			"payment_status": "paid", "metadata": map[string]string{"plan_key": "sprint"},
		}, webhookSecret)
		if paid.Code != http.StatusOK {
			t.Fatalf("pass webhook = %d", paid.Code)
		}

		status := do(http.MethodGet, "/api/v1/billing/status", nil, session, nil)
		var body billingStatusResponse
		_ = json.NewDecoder(status.Body).Decode(&body)
		if body.Plan != "pro" || body.Source != sourcePass || body.PassExpiresAt == nil ||
			body.PassExpiresAt.Before(time.Now().Add(89*24*time.Hour)) {
			t.Fatalf("status after pass = %+v", body)
		}

		if subscribe := do(http.MethodPost, "/api/v1/billing/checkout", map[string]string{"plan": "monthly"}, session, nil); subscribe.Code != http.StatusConflict {
			t.Fatalf("subscribing during pass = %d, want 409", subscribe.Code)
		}
	})

	signUp := func(t *testing.T, address string) *http.Cookie {
		t.Helper()
		response := do(http.MethodPost, "/api/v1/auth/signup", map[string]string{"email": address, "password": "a-strong-password", "firstName": "Test", "lastName": "User", "phone": "6465550100"}, nil, nil)
		if response.Code != http.StatusOK {
			t.Fatalf("sign up %s = %d %s", address, response.Code, response.Body)
		}
		for _, cookie := range response.Result().Cookies() {
			if cookie.Name == auth.SessionCookieName {
				return cookie
			}
		}
		t.Fatal("no session cookie")
		return nil
	}

	statusFor := func(t *testing.T, cookie *http.Cookie) billingStatusResponse {
		t.Helper()
		var body billingStatusResponse
		_ = json.NewDecoder(do(http.MethodGet, "/api/v1/billing/status", nil, cookie, nil).Body).Decode(&body)
		return body
	}

	verify := func(t *testing.T, address string, after int) int {
		t.Helper()
		token, next := waitForToken(t, address, "Confirm your Jane Careers email", after)
		response := do(http.MethodGet, "/verify-email?token="+token, nil, nil, nil)
		if response.Code != http.StatusFound || response.Header().Get("Location") != "/app/billing?verified=1" {
			t.Fatalf("verify link = %d %s", response.Code, response.Header().Get("Location"))
		}
		if reuse := do(http.MethodGet, "/verify-email?token="+token, nil, nil, nil); reuse.Header().Get("Location") != "/app/billing?verified=invalid" {
			t.Fatalf("reused verify link = %s, want invalid", reuse.Header().Get("Location"))
		}
		return next
	}

	t.Run("school license covers verified users at exactly that domain", func(t *testing.T) {
		unverified := do(http.MethodPost, "/api/v1/billing/checkout", map[string]string{"plan": "school", "organizationName": "Test School of Nursing"}, session, nil)
		if unverified.Code != http.StatusForbidden || !strings.Contains(unverified.Body.String(), "email_unverified") {
			t.Fatalf("unverified school checkout = %d %s", unverified.Code, unverified.Body)
		}

		// Sign-up already sent a verification email to the owner.
		mailCursor := verify(t, email, 0)
		if !statusFor(t, session).EmailVerified {
			t.Fatal("owner email not verified")
		}

		checkout := do(http.MethodPost, "/api/v1/billing/checkout", map[string]string{"plan": "school", "organizationName": "Test School of Nursing"}, session, nil)
		if checkout.Code != http.StatusOK || stripeFake.lastCheckout.Get("line_items[0][price]") != "price_school" ||
			stripeFake.lastCheckout.Get("metadata[organization_id]") == "" {
			t.Fatalf("school checkout = %d %s; form = %v", checkout.Code, checkout.Body, stripeFake.lastCheckout)
		}

		var orgCustomer string
		if err := pool.QueryRow(ctx, `SELECT stripe_customer_id FROM organizations WHERE email_domain = $1`, schoolDomain).Scan(&orgCustomer); err != nil || orgCustomer == "" || orgCustomer == customerID {
			t.Fatalf("organization customer = %q (personal %q), err %v", orgCustomer, customerID, err)
		}

		stripeFake.mu.Lock()
		stripeFake.subscriptions["sub_school"] = map[string]any{
			"id": "sub_school", "customer": orgCustomer, "status": "active",
			"items": map[string]any{"data": []any{map[string]any{"current_period_end": time.Now().Add(365 * 24 * time.Hour).Unix(), "price": map[string]any{"id": "price_school"}}}},
		}
		stripeFake.mu.Unlock()
		if response := sendWebhook("evt_saas_school", "customer.subscription.created", map[string]any{"id": "sub_school"}, webhookSecret); response.Code != http.StatusOK {
			t.Fatalf("school webhook = %d", response.Code)
		}

		owner := statusFor(t, session)
		if owner.OwnedLicense == nil || !owner.OwnedLicense.Active || owner.OwnedLicense.Domain != schoolDomain {
			t.Fatalf("owner license = %+v", owner.OwnedLicense)
		}

		student := "student@" + schoolDomain
		studentSession := signUp(t, student)
		if before := statusFor(t, studentSession); before.Plan != "none" {
			t.Fatalf("unverified student plan = %q, want none", before.Plan)
		}
		mailCursor = verify(t, student, mailCursor)
		after := statusFor(t, studentSession)
		if after.Plan != "pro" || after.Source != sourceOrganization || after.OrganizationName != "Test School of Nursing" {
			t.Fatalf("verified student status = %+v", after)
		}
		if response := do(http.MethodPost, "/api/v1/resume-recommendation", map[string]any{}, studentSession, nil); response.Code != http.StatusBadRequest {
			t.Fatalf("student past Pro gate = %d, want 400", response.Code)
		}

		sub := "alum@dept." + schoolDomain
		subSession := signUp(t, sub)
		verify(t, sub, mailCursor)
		if status := statusFor(t, subSession); status.Plan != "none" {
			t.Fatalf("subdomain user plan = %q, want none (no subdomains)", status.Plan)
		}
	})

	t.Run("password reset by email", func(t *testing.T) {
		student := "student@" + schoolDomain

		mailMu.Lock()
		cursor := len(sentMail)
		mailMu.Unlock()

		if response := do(http.MethodPost, "/api/v1/auth/password-reset/request", map[string]string{"email": "nobody@" + schoolDomain}, nil, nil); response.Code != http.StatusOK {
			t.Fatalf("reset for unknown email = %d, want 200 (no account enumeration)", response.Code)
		}
		if response := do(http.MethodPost, "/api/v1/auth/password-reset/request", map[string]string{"email": student}, nil, nil); response.Code != http.StatusOK {
			t.Fatalf("reset request = %d", response.Code)
		}
		token, _ := waitForToken(t, student, "Reset your Jane Careers password", cursor)

		if weak := do(http.MethodPost, "/api/v1/auth/password-reset/confirm", map[string]string{"token": token, "password": "short"}, nil, nil); weak.Code != http.StatusBadRequest {
			t.Fatalf("weak password = %d", weak.Code)
		}
		if ok := do(http.MethodPost, "/api/v1/auth/password-reset/confirm", map[string]string{"token": token, "password": "brand-new-password"}, nil, nil); ok.Code != http.StatusOK {
			t.Fatalf("confirm = %d %s", ok.Code, ok.Body)
		}
		if reuse := do(http.MethodPost, "/api/v1/auth/password-reset/confirm", map[string]string{"token": token, "password": "another-new-password"}, nil, nil); reuse.Code != http.StatusBadRequest {
			t.Fatalf("reused reset token = %d, want 400", reuse.Code)
		}
		if old := do(http.MethodPost, "/api/v1/auth/login", map[string]string{"email": student, "password": "a-strong-password"}, nil, nil); old.Code != http.StatusUnauthorized {
			t.Fatalf("old password still works: %d", old.Code)
		}
		if fresh := do(http.MethodPost, "/api/v1/auth/login", map[string]string{"email": student, "password": "brand-new-password"}, nil, nil); fresh.Code != http.StatusOK {
			t.Fatalf("new password login = %d", fresh.Code)
		}
	})

	t.Run("track job adds application with job summary", func(t *testing.T) {
		response := do(http.MethodPost, "/api/v1/applications/track", map[string]any{
			"job": map[string]any{"id": fmt.Sprintf("job-%d", time.Now().UnixNano()), "source": "lever", "title": "Backend Engineer", "company": "Acme", "applyURL": "https://jobs.example.com/1"},
		}, session, nil)
		if response.Code != http.StatusCreated {
			t.Fatalf("status = %d; body = %s", response.Code, response.Body)
		}

		if bad := do(http.MethodPost, "/api/v1/applications/track", map[string]any{
			"job": map[string]any{"title": "X", "company": "Y", "applyURL": "javascript:alert(1)"},
		}, session, nil); bad.Code != http.StatusBadRequest {
			t.Fatalf("javascript: URL status = %d, want 400", bad.Code)
		}

		list := do(http.MethodGet, "/api/v1/applications", nil, session, nil)
		var applications []applicationResponse
		_ = json.NewDecoder(list.Body).Decode(&applications)
		if len(applications) != 1 || applications[0].Job == nil || applications[0].Job.Company != "Acme" {
			t.Fatalf("applications = %+v", applications)
		}
	})

	t.Run("résumé library, profile draft, and preferences", func(t *testing.T) {
		resumeText := "Jane Doe\nSUMMARY\nBackend engineer with 5 years of experience building Go services.\n" +
			"EXPERIENCE\nSoftware Engineer, Acme Corp 2020-2025\nBuilt REST APIs in Go and PostgreSQL.\n" +
			"EDUCATION\nB.S. Computer Science, University of Florida\nSKILLS\nGo, PostgreSQL, Docker"

		if short := do(http.MethodPost, "/api/v1/resumes", map[string]any{"filename": "x.pdf", "resumeText": "too short"}, session, nil); short.Code != http.StatusBadRequest {
			t.Fatalf("short résumé status = %d, want 400", short.Code)
		}

		created := do(http.MethodPost, "/api/v1/resumes", map[string]any{"filename": "jane.pdf", "resumeText": resumeText}, session, nil)
		if created.Code != http.StatusCreated {
			t.Fatalf("create résumé status = %d; body = %s", created.Code, created.Body)
		}
		var resume resumeResponse
		_ = json.NewDecoder(created.Body).Decode(&resume)
		if resume.ID == "" || !resume.IsPrimary || resume.ResumeText != "" {
			t.Fatalf("created résumé = %+v (first résumé must be primary; text must not be echoed)", resume)
		}

		list := do(http.MethodGet, "/api/v1/resumes", nil, session, nil)
		var resumes []resumeResponse
		_ = json.NewDecoder(list.Body).Decode(&resumes)
		if len(resumes) != 1 || resumes[0].ResumeText != "" {
			t.Fatalf("list = %+v", resumes)
		}

		full := do(http.MethodGet, "/api/v1/resumes/"+resume.ID, nil, session, nil)
		var fullResume resumeResponse
		_ = json.NewDecoder(full.Body).Decode(&fullResume)
		if !strings.Contains(fullResume.ResumeText, "PostgreSQL") {
			t.Fatalf("full résumé text missing: %+v", fullResume)
		}

		draft := do(http.MethodGet, "/api/v1/resumes/"+resume.ID+"/profile-draft", nil, session, nil)
		var draftBody profileDraftResponse
		_ = json.NewDecoder(draft.Body).Decode(&draftBody)
		if draft.Code != http.StatusOK || len(draftBody.Evidence) == 0 {
			t.Fatalf("profile draft = %d %+v", draft.Code, draftBody)
		}

		// Another user's ID cannot read this résumé.
		if missing := do(http.MethodGet, "/api/v1/resumes/00000000-0000-0000-0000-000000000000", nil, session, nil); missing.Code != http.StatusNotFound {
			t.Fatalf("unknown résumé status = %d, want 404", missing.Code)
		}

		saved := do(http.MethodPut, "/api/v1/preferences", map[string]any{"minimumSalary": "90000", "remote": true, "onboardingComplete": true}, session, nil)
		if saved.Code != http.StatusOK {
			t.Fatalf("save preferences = %d %s", saved.Code, saved.Body)
		}
		got := do(http.MethodGet, "/api/v1/preferences", nil, session, nil)
		var preferences preferencesPayload
		_ = json.NewDecoder(got.Body).Decode(&preferences)
		if !preferences.Remote || preferences.MinimumSalary != "90000" || !preferences.OnboardingComplete {
			t.Fatalf("preferences = %+v", preferences)
		}

		if deleted := do(http.MethodDelete, "/api/v1/resumes/"+resume.ID, nil, session, nil); deleted.Code != http.StatusNoContent {
			t.Fatalf("delete résumé = %d", deleted.Code)
		}
	})

	t.Run("web app routes fall back to index and API 404s stay JSON", func(t *testing.T) {
		page := do(http.MethodGet, "/app/jobs", nil, nil, nil)
		if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), "AlignApply") {
			t.Fatalf("SPA fallback status = %d", page.Code)
		}
		if page.Header().Get("Content-Security-Policy") == "" {
			t.Fatal("missing Content-Security-Policy header")
		}

		if missing := do(http.MethodGet, "/api/v1/nope", nil, nil, nil); missing.Code != http.StatusNotFound || !strings.Contains(missing.Body.String(), "not found") {
			t.Fatalf("API 404 = %d %s", missing.Code, missing.Body)
		}
	})

	t.Run("account deletion cancels an active subscription", func(t *testing.T) {
		stripeFake.mu.Lock()
		stripeFake.subscriptions["sub_saas"]["status"] = "active"
		stripeFake.mu.Unlock()
		sendWebhook("evt_saas_reactivated", "customer.subscription.updated", map[string]any{"id": "sub_saas"}, webhookSecret)

		response := do(http.MethodDelete, "/api/v1/account", nil, session, nil)
		if response.Code != http.StatusNoContent {
			t.Fatalf("status = %d; body = %s", response.Code, response.Body)
		}

		if len(stripeFake.canceled) != 1 || stripeFake.canceled[0] != "sub_saas" {
			t.Fatalf("canceled = %v", stripeFake.canceled)
		}

		if me := do(http.MethodGet, "/api/v1/me", nil, session, nil); me.Code != http.StatusUnauthorized {
			t.Fatalf("/me after delete = %d", me.Code)
		}
	})
}
