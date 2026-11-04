package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/hannasotolongo/casemade-backend/internal/auth"
	"github.com/hannasotolongo/casemade-backend/internal/billing"
	"github.com/hannasotolongo/casemade-backend/internal/repository"
)

// ---------------------------------------------------------------------------
// Plans and quotas
// ---------------------------------------------------------------------------

// Plan is one purchasable Pro price.
type Plan struct {
	Key          string `json:"key"`
	Name         string `json:"name"`
	DisplayPrice string `json:"displayPrice"`
	Interval     string `json:"interval"`
	PriceID      string `json:"-"`
}

type BillingConfig struct {
	Plans         []Plan
	WebhookSecret string
}

const (
	featureJobMatch = "job_match"

	// Free accounts get a small rolling weekly allowance of job-match
	// searches. Each search fans out to job providers and LLM verification,
	// so this is the main cost to protect.
	FreeJobMatchesPerWeek = 3

	// Pro is "unlimited" for normal use; this hourly ceiling only stops
	// scripted abuse of the expensive matching pipeline.
	ProJobMatchesPerHour = 30
)

func (c BillingConfig) planByKey(key string) (Plan, bool) {
	for _, plan := range c.Plans {
		if plan.Key == key && plan.PriceID != "" {
			return plan, true
		}
	}
	return Plan{}, false
}

func (c BillingConfig) planByPriceID(priceID string) (Plan, bool) {
	for _, plan := range c.Plans {
		if plan.PriceID != "" && plan.PriceID == priceID {
			return plan, true
		}
	}
	return Plan{}, false
}

func (s *Server) billingEnabled() bool {
	return s.stripe != nil &&
		s.billingRepo != nil &&
		len(s.billingConfig.Plans) > 0
}

// ---------------------------------------------------------------------------
// Entitlements
// ---------------------------------------------------------------------------

type entitlement struct {
	Pro          bool
	Subscription repository.Subscription
	HasCustomer  bool
}

func (s *Server) entitlementFor(
	ctx context.Context,
	userID string,
) (entitlement, error) {
	if s.billingRepo == nil {
		return entitlement{}, nil
	}

	subscription, err := s.billingRepo.GetSubscription(ctx, userID)
	if errors.Is(err, repository.ErrSubscriptionNotFound) {
		return entitlement{}, nil
	}
	if err != nil {
		return entitlement{}, err
	}

	return entitlement{
		Pro:          subscription.IsPro(),
		Subscription: subscription,
		HasCustomer:  subscription.StripeCustomerID != "",
	}, nil
}

// requireSessionUser confirms the session belongs to an active user.
func (s *Server) requireSessionUser(
	w http.ResponseWriter,
	r *http.Request,
) (repository.User, bool) {
	if s.userCareerRepo == nil {
		writeError(w, http.StatusServiceUnavailable, "persistence unavailable")
		return repository.User{}, false
	}

	userID, err := auth.RequireUserID(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return repository.User{}, false
	}

	user, err := s.userCareerRepo.GetUserByID(r.Context(), userID)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return repository.User{}, false
	}

	return user, true
}

// requirePro wraps a session-authenticated handler so that only users with
// an active Pro subscription can reach it.
func (s *Server) requirePro(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, ok := s.requireSessionUser(w, r)
		if !ok {
			return
		}

		current, err := s.entitlementFor(r.Context(), user.ID)
		if err != nil {
			log.Printf("load entitlement for user %s: %v", user.ID, err)
			writeError(w, http.StatusInternalServerError, "failed to load subscription")
			return
		}

		if !current.Pro {
			writeJSON(w, http.StatusPaymentRequired, map[string]string{
				"error": "this feature requires AlignApply Pro",
				"code":  "pro_required",
			})
			return
		}

		next.ServeHTTP(w, r)
	})
}

// consumeJobMatchQuota enforces the free weekly allowance and the Pro hourly
// ceiling, then records the use. It writes the error response itself.
func (s *Server) consumeJobMatchQuota(
	w http.ResponseWriter,
	r *http.Request,
	userID string,
) bool {
	if s.billingRepo == nil {
		// Persistence is not configured (e.g. some tests); no metering.
		return true
	}

	current, err := s.entitlementFor(r.Context(), userID)
	if err != nil {
		log.Printf("load entitlement for user %s: %v", userID, err)
		writeError(w, http.StatusInternalServerError, "failed to load subscription")
		return false
	}

	limit, window, code, message :=
		FreeJobMatchesPerWeek,
		7*24*time.Hour,
		"free_limit_reached",
		"you've used this week's free job-match searches; upgrade to Pro for unlimited searches"

	if current.Pro {
		limit, window, code, message =
			ProJobMatchesPerHour,
			time.Hour,
			"rate_limited",
			"too many searches in the last hour; try again shortly"
	}

	used, err := s.billingRepo.CountUsageSince(
		r.Context(),
		userID,
		featureJobMatch,
		time.Now().Add(-window),
	)
	if err != nil {
		log.Printf("count usage for user %s: %v", userID, err)
		writeError(w, http.StatusInternalServerError, "failed to check usage")
		return false
	}

	if used >= limit {
		status := http.StatusPaymentRequired
		if current.Pro {
			status = http.StatusTooManyRequests
		}
		writeJSON(w, status, map[string]string{
			"error": message,
			"code":  code,
		})
		return false
	}

	if err := s.billingRepo.RecordUsage(
		r.Context(),
		userID,
		featureJobMatch,
	); err != nil {
		log.Printf("record usage for user %s: %v", userID, err)
		writeError(w, http.StatusInternalServerError, "failed to record usage")
		return false
	}

	return true
}

// ---------------------------------------------------------------------------
// Billing endpoints
// ---------------------------------------------------------------------------

type usageResponse struct {
	Used   int    `json:"used"`
	Limit  int    `json:"limit"`
	Period string `json:"period"`
}

type billingStatusResponse struct {
	Plan              string        `json:"plan"`
	PlanKey           string        `json:"planKey,omitempty"`
	Status            string        `json:"status"`
	CurrentPeriodEnd  *time.Time    `json:"currentPeriodEnd,omitempty"`
	CancelAtPeriodEnd bool          `json:"cancelAtPeriodEnd"`
	BillingEnabled    bool          `json:"billingEnabled"`
	CanManageBilling  bool          `json:"canManageBilling"`
	JobMatches        usageResponse `json:"jobMatches"`
}

func (s *Server) handleBillingPlans(
	w http.ResponseWriter,
	r *http.Request,
) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, "GET")
		return
	}

	plans := make([]Plan, 0, len(s.billingConfig.Plans))
	for _, plan := range s.billingConfig.Plans {
		if plan.PriceID != "" {
			plans = append(plans, plan)
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"billingEnabled":        s.billingEnabled(),
		"plans":                 plans,
		"freeJobMatchesPerWeek": FreeJobMatchesPerWeek,
	})
}

func (s *Server) handleBillingStatus(
	w http.ResponseWriter,
	r *http.Request,
) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, "GET")
		return
	}

	user, ok := s.requireSessionUser(w, r)
	if !ok {
		return
	}

	current, err := s.entitlementFor(r.Context(), user.ID)
	if err != nil {
		log.Printf("load entitlement for user %s: %v", user.ID, err)
		writeError(w, http.StatusInternalServerError, "failed to load subscription")
		return
	}

	response := billingStatusResponse{
		Plan:              "free",
		Status:            "none",
		BillingEnabled:    s.billingEnabled(),
		CanManageBilling:  s.billingEnabled() && current.HasCustomer,
		CancelAtPeriodEnd: current.Subscription.CancelAtPeriodEnd,
		CurrentPeriodEnd:  current.Subscription.CurrentPeriodEnd,
		JobMatches: usageResponse{
			Limit:  FreeJobMatchesPerWeek,
			Period: "week",
		},
	}

	if current.Subscription.Status != "" {
		response.Status = current.Subscription.Status
	}

	if plan, found := s.billingConfig.planByPriceID(
		current.Subscription.PriceID,
	); found {
		response.PlanKey = plan.Key
	}

	window := 7 * 24 * time.Hour
	if current.Pro {
		response.Plan = "pro"
		response.JobMatches = usageResponse{
			Limit:  ProJobMatchesPerHour,
			Period: "hour",
		}
		window = time.Hour
	}

	if s.billingRepo != nil {
		used, err := s.billingRepo.CountUsageSince(
			r.Context(),
			user.ID,
			featureJobMatch,
			time.Now().Add(-window),
		)
		if err != nil {
			log.Printf("count usage for user %s: %v", user.ID, err)
		}
		response.JobMatches.Used = used
	}

	writeJSON(w, http.StatusOK, response)
}

type checkoutRequest struct {
	Plan string `json:"plan"`
}

func (s *Server) handleBillingCheckout(
	w http.ResponseWriter,
	r *http.Request,
) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, "POST")
		return
	}

	if !s.billingEnabled() {
		writeError(w, http.StatusServiceUnavailable, "billing is not configured")
		return
	}

	user, ok := s.requireSessionUser(w, r)
	if !ok {
		return
	}

	var request checkoutRequest
	if err := decodeApplicationJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	plan, found := s.billingConfig.planByKey(strings.TrimSpace(request.Plan))
	if !found {
		writeError(w, http.StatusBadRequest, "unknown plan")
		return
	}

	current, err := s.entitlementFor(r.Context(), user.ID)
	if err != nil {
		log.Printf("load entitlement for user %s: %v", user.ID, err)
		writeError(w, http.StatusInternalServerError, "failed to start checkout")
		return
	}

	if current.Pro {
		writeJSON(w, http.StatusConflict, map[string]string{
			"error": "you already have Pro; manage your plan from the billing portal",
			"code":  "already_subscribed",
		})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()

	customerID := current.Subscription.StripeCustomerID
	if customerID == "" {
		customerID, err = s.stripe.CreateCustomer(ctx, user.Email, user.ID)
		if err != nil {
			log.Printf("create Stripe customer for user %s: %v", user.ID, err)
			writeError(w, http.StatusBadGateway, "failed to start checkout")
			return
		}

		attached, err := s.billingRepo.AttachStripeCustomer(ctx, user.ID, customerID)
		if err != nil {
			log.Printf("attach Stripe customer for user %s: %v", user.ID, err)
			writeError(w, http.StatusInternalServerError, "failed to start checkout")
			return
		}
		customerID = attached.StripeCustomerID
	}

	checkoutURL, err := s.stripe.CreateCheckoutSession(ctx, billing.CheckoutParams{
		CustomerID: customerID,
		PriceID:    plan.PriceID,
		UserID:     user.ID,
		SuccessURL: s.web.BaseURL + "/app/billing?checkout=success",
		CancelURL:  s.web.BaseURL + "/app/billing?checkout=canceled",
	})
	if err != nil {
		log.Printf("create checkout session for user %s: %v", user.ID, err)
		writeError(w, http.StatusBadGateway, "failed to start checkout")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"url": checkoutURL})
}

func (s *Server) handleBillingPortal(
	w http.ResponseWriter,
	r *http.Request,
) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, "POST")
		return
	}

	if !s.billingEnabled() {
		writeError(w, http.StatusServiceUnavailable, "billing is not configured")
		return
	}

	user, ok := s.requireSessionUser(w, r)
	if !ok {
		return
	}

	current, err := s.entitlementFor(r.Context(), user.ID)
	if err != nil {
		log.Printf("load entitlement for user %s: %v", user.ID, err)
		writeError(w, http.StatusInternalServerError, "failed to open billing portal")
		return
	}

	if !current.HasCustomer {
		writeError(w, http.StatusBadRequest, "no billing account yet; choose a plan first")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()

	portalURL, err := s.stripe.CreatePortalSession(
		ctx,
		current.Subscription.StripeCustomerID,
		s.web.BaseURL+"/app/billing",
	)
	if err != nil {
		log.Printf("create portal session for user %s: %v", user.ID, err)
		writeError(w, http.StatusBadGateway, "failed to open billing portal")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"url": portalURL})
}

const maxWebhookBodyBytes int64 = 1 << 20

func (s *Server) handleStripeWebhook(
	w http.ResponseWriter,
	r *http.Request,
) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, "POST")
		return
	}

	if !s.billingEnabled() || s.billingConfig.WebhookSecret == "" {
		writeError(w, http.StatusServiceUnavailable, "billing is not configured")
		return
	}

	payload, err := io.ReadAll(
		http.MaxBytesReader(w, r.Body, maxWebhookBodyBytes),
	)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid payload")
		return
	}

	event, err := billing.VerifyWebhook(
		payload,
		r.Header.Get("Stripe-Signature"),
		s.billingConfig.WebhookSecret,
		billing.DefaultWebhookTolerance,
		time.Now(),
	)
	if err != nil {
		log.Printf("reject Stripe webhook: %v", err)
		writeError(w, http.StatusBadRequest, "invalid signature")
		return
	}

	first, err := s.billingRepo.MarkWebhookEventProcessed(
		r.Context(),
		event.ID,
		event.Type,
	)
	if err != nil {
		log.Printf("record Stripe event %s: %v", event.ID, err)
		writeError(w, http.StatusInternalServerError, "failed to record event")
		return
	}

	if !first {
		w.WriteHeader(http.StatusOK)
		return
	}

	if err := s.processStripeEvent(r.Context(), event); err != nil {
		log.Printf("process Stripe event %s (%s): %v", event.ID, event.Type, err)

		// Let Stripe retry this event later.
		if forgetErr := s.billingRepo.ForgetWebhookEvent(
			context.Background(),
			event.ID,
		); forgetErr != nil {
			log.Printf("forget Stripe event %s: %v", event.ID, forgetErr)
		}

		writeError(w, http.StatusInternalServerError, "failed to process event")
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (s *Server) processStripeEvent(
	ctx context.Context,
	event billing.Event,
) error {
	var subscriptionID string

	switch event.Type {
	case "checkout.session.completed":
		var session struct {
			Mode         string `json:"mode"`
			Subscription string `json:"subscription"`
		}
		if err := json.Unmarshal(event.Data.Object, &session); err != nil {
			return err
		}
		if session.Mode != "subscription" || session.Subscription == "" {
			return nil
		}
		subscriptionID = session.Subscription

	case "customer.subscription.created",
		"customer.subscription.updated",
		"customer.subscription.deleted",
		"customer.subscription.paused",
		"customer.subscription.resumed":
		var object struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(event.Data.Object, &object); err != nil {
			return err
		}
		subscriptionID = object.ID

	default:
		return nil
	}

	// Events can arrive out of order, so always store the subscription's
	// current state from Stripe rather than the (possibly stale) payload.
	fetchCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	subscription, err := s.stripe.GetSubscription(fetchCtx, subscriptionID)
	if err != nil {
		return err
	}

	_, err = s.billingRepo.ApplyStripeSubscription(ctx, repository.StripeSubscriptionUpdate{
		StripeCustomerID:     subscription.Customer,
		StripeSubscriptionID: subscription.ID,
		Status:               subscription.Status,
		PriceID:              subscription.PriceID(),
		CurrentPeriodEnd:     subscription.PeriodEnd(),
		CancelAtPeriodEnd:    subscription.CancelAtPeriodEnd,
	})

	if errors.Is(err, repository.ErrSubscriptionNotFound) {
		// A customer that was not created by AlignApply checkout.
		log.Printf(
			"Stripe event %s: no AlignApply user for customer %s",
			event.ID,
			subscription.Customer,
		)
		return nil
	}

	return err
}

// cancelBillingForDeletedAccount stops billing before an account is removed.
func (s *Server) cancelBillingForDeletedAccount(
	ctx context.Context,
	userID string,
) error {
	if s.billingRepo == nil {
		return nil
	}

	subscription, err := s.billingRepo.GetSubscription(ctx, userID)
	if errors.Is(err, repository.ErrSubscriptionNotFound) {
		return nil
	}
	if err != nil {
		return err
	}

	if subscription.StripeSubscriptionID == "" ||
		subscription.Status == "canceled" ||
		subscription.Status == "incomplete_expired" {
		return nil
	}

	if s.stripe == nil {
		return errors.New("billing: Stripe client unavailable")
	}

	cancelCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	return s.stripe.CancelSubscription(cancelCtx, subscription.StripeSubscriptionID)
}
