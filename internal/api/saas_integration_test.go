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
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/hannasotolongo/casemade-backend/internal/auth"
	"github.com/hannasotolongo/casemade-backend/internal/billing"
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
		},
		SessionManager: sessionManager,
		Stripe:         stripeClient,
		BillingConfig: BillingConfig{
			WebhookSecret: webhookSecret,
			Plans: []Plan{
				{Key: "monthly", Name: "Pro Monthly", Interval: "month", DisplayPrice: "$2.99", PriceID: "price_monthly"},
			},
		},
		Web: WebConfig{
			BaseURL: "https://alignapply.test",
			Static:  fstest.MapFS{"index.html": {Data: []byte("<!doctype html><title>AlignApply</title>")}},
		},
	})
	handler := server.Handler()

	email := fmt.Sprintf("saas-%d@example.com", time.Now().UnixNano())
	var userID string

	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM users WHERE LOWER(email) = LOWER($1)`, email)
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
			"email": email, "password": "a-strong-password", "displayName": "SaaS User",
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
			"email": strings.ToUpper(email), "password": "a-strong-password",
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

	t.Run("free tier quota blocks job matches once used", func(t *testing.T) {
		for i := 0; i < FreeJobMatchesPerWeek; i++ {
			if err := billingRepo.RecordUsage(ctx, userID, featureJobMatch); err != nil {
				t.Fatal(err)
			}
		}

		response := do(http.MethodPost, "/api/v1/job-matches", map[string]any{
			"careerProfile": map[string]any{"skills": []string{"Go"}},
			"targetRole":    "Software Engineer",
			"location":      "Miami, FL",
		}, session, nil)

		if response.Code != http.StatusPaymentRequired || !strings.Contains(response.Body.String(), "free_limit_reached") {
			t.Fatalf("status = %d; body = %s", response.Code, response.Body)
		}
	})

	t.Run("resume recommendation requires Pro", func(t *testing.T) {
		response := do(http.MethodPost, "/api/v1/resume-recommendation", map[string]any{}, session, nil)
		if response.Code != http.StatusPaymentRequired {
			t.Fatalf("status = %d, want 402; body = %s", response.Code, response.Body)
		}
	})

	t.Run("billing status reports free plan", func(t *testing.T) {
		response := do(http.MethodGet, "/api/v1/billing/status", nil, session, nil)
		var status billingStatusResponse
		_ = json.NewDecoder(response.Body).Decode(&status)
		if response.Code != http.StatusOK || status.Plan != "free" || status.JobMatches.Used != FreeJobMatchesPerWeek {
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
		if got := stripeFake.lastCheckout.Get("success_url"); got != "https://alignapply.test/app/billing?checkout=success" {
			t.Fatalf("success_url = %q", got)
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
