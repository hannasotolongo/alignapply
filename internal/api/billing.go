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
	"unicode/utf8"

	"github.com/hannasotolongo/casemade-backend/internal/auth"
	"github.com/hannasotolongo/casemade-backend/internal/billing"
	emailservice "github.com/hannasotolongo/casemade-backend/internal/email"
	"github.com/hannasotolongo/casemade-backend/internal/repository"
)

// ---------------------------------------------------------------------------
// Plans
// ---------------------------------------------------------------------------

const (
	PlanKindSubscription = "subscription" // recurring, may include a trial
	PlanKindPass         = "pass"         // one-time payment for N days of Pro
	PlanKindOrganization = "organization" // school license for an email domain
)

// Plan is one purchasable price.
type Plan struct {
	Key          string `json:"key"`
	Name         string `json:"name"`
	Kind         string `json:"kind"`
	DisplayPrice string `json:"displayPrice"`
	Interval     string `json:"interval"`
	TrialDays    int    `json:"trialDays,omitempty"`
	PassDays     int    `json:"passDays,omitempty"`
	PriceID      string `json:"-"`
}

type BillingConfig struct {
	Plans         []Plan
	WebhookSecret string
}

const (
	featureJobMatch = "job_match"

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

// Public email providers can never be licensed as a school domain;
// otherwise one purchase could unlock Pro for every Gmail user.
var publicEmailDomains = map[string]struct{}{
	"gmail.com": {}, "googlemail.com": {}, "yahoo.com": {}, "ymail.com": {},
	"outlook.com": {}, "hotmail.com": {}, "live.com": {}, "msn.com": {},
	"icloud.com": {}, "me.com": {}, "mac.com": {}, "aol.com": {},
	"proton.me": {}, "protonmail.com": {}, "pm.me": {}, "gmx.com": {},
	"gmx.net": {}, "mail.com": {}, "yandex.com": {}, "zoho.com": {},
	"hey.com": {}, "fastmail.com": {}, "tutanota.com": {}, "qq.com": {},
	"163.com": {}, "comcast.net": {}, "att.net": {}, "verizon.net": {},
}

func emailDomain(email string) string {
	at := strings.LastIndex(email, "@")
	if at < 0 {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(email[at+1:]))
}

// ---------------------------------------------------------------------------
// Entitlements
// ---------------------------------------------------------------------------

const (
	sourceSubscription = "subscription"
	sourceTrial        = "trial"
	sourcePass         = "pass"
	sourceOrganization = "organization"
	sourceOpen         = "open" // billing not configured yet: everyone has access
)

type entitlement struct {
	Pro           bool
	Source        string
	Subscription  repository.Subscription
	HasCustomer   bool
	TrialEligible bool
	EmailVerified bool
	Organization  *repository.Organization // license covering this user
	OwnedLicense  *repository.Organization // license this user bought
}

func (s *Server) entitlementFor(ctx context.Context, user repository.User) (entitlement, error) {
	current := entitlement{TrialEligible: true}

	if s.accountsRepo != nil {
		verified, err := s.accountsRepo.EmailVerified(ctx, user.ID)
		if err != nil {
			return entitlement{}, err
		}
		current.EmailVerified = verified

		if owned, err := s.accountsRepo.OrganizationByOwner(ctx, user.ID); err == nil {
			current.OwnedLicense = &owned
		}
	}

	if s.billingRepo != nil {
		subscription, err := s.billingRepo.GetSubscription(ctx, user.ID)
		switch {
		case err == nil:
			current.Subscription = subscription
			current.HasCustomer = subscription.StripeCustomerID != ""
			current.TrialEligible = !subscription.TrialUsed && !subscription.SubscriptionActive()
			if subscription.SubscriptionActive() {
				current.Pro = true
				current.Source = sourceSubscription
				if subscription.Status == "trialing" {
					current.Source = sourceTrial
				}
			} else if subscription.PassActive(time.Now()) {
				current.Pro = true
				current.Source = sourcePass
			}
		case !errors.Is(err, repository.ErrSubscriptionNotFound):
			return entitlement{}, err
		}
	}

	if !current.Pro && current.EmailVerified && s.accountsRepo != nil {
		if org, err := s.accountsRepo.ActiveOrganizationForDomain(ctx, emailDomain(user.Email)); err == nil {
			current.Pro = true
			current.Source = sourceOrganization
			current.Organization = &org
		}
	}

	if !current.Pro && !s.billingEnabled() {
		current.Pro = true
		current.Source = sourceOpen
	}

	return current, nil
}

// requireSessionUser confirms the session belongs to an active user.
func (s *Server) requireSessionUser(w http.ResponseWriter, r *http.Request) (repository.User, bool) {
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

func writeSubscriptionRequired(w http.ResponseWriter) {
	writeJSON(w, http.StatusPaymentRequired, map[string]string{
		"error": "start your free trial to use this feature",
		"code":  "pro_required",
	})
}

// requirePro wraps a session-authenticated handler so that only users with
// Pro access (trial, subscription, pass, or school license) can reach it.
func (s *Server) requirePro(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, ok := s.requireSessionUser(w, r)
		if !ok {
			return
		}

		current, err := s.entitlementFor(r.Context(), user)
		if err != nil {
			log.Printf("load entitlement for user %s: %v", user.ID, err)
			writeError(w, http.StatusInternalServerError, "failed to load subscription")
			return
		}

		if !current.Pro {
			writeSubscriptionRequired(w)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// consumeJobMatchQuota requires Pro access, enforces the hourly fair-use
// ceiling, and records the search. It writes the error response itself.
func (s *Server) consumeJobMatchQuota(w http.ResponseWriter, r *http.Request, user repository.User) bool {
	current, err := s.entitlementFor(r.Context(), user)
	if err != nil {
		log.Printf("load entitlement for user %s: %v", user.ID, err)
		writeError(w, http.StatusInternalServerError, "failed to load subscription")
		return false
	}

	if !current.Pro {
		writeSubscriptionRequired(w)
		return false
	}

	if s.billingRepo == nil {
		return true
	}

	used, err := s.billingRepo.CountUsageSince(r.Context(), user.ID, featureJobMatch, time.Now().Add(-time.Hour))
	if err != nil {
		log.Printf("count usage for user %s: %v", user.ID, err)
		writeError(w, http.StatusInternalServerError, "failed to check usage")
		return false
	}

	if used >= ProJobMatchesPerHour {
		writeJSON(w, http.StatusTooManyRequests, map[string]string{
			"error": "too many searches in the last hour; try again shortly",
			"code":  "rate_limited",
		})
		return false
	}

	if err := s.billingRepo.RecordUsage(r.Context(), user.ID, featureJobMatch); err != nil {
		log.Printf("record usage for user %s: %v", user.ID, err)
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

type licenseResponse struct {
	Name              string     `json:"name"`
	Domain            string     `json:"domain"`
	Status            string     `json:"status"`
	Active            bool       `json:"active"`
	CurrentPeriodEnd  *time.Time `json:"currentPeriodEnd,omitempty"`
	CancelAtPeriodEnd bool       `json:"cancelAtPeriodEnd"`
	CanManage         bool       `json:"canManage"`
}

type billingStatusResponse struct {
	Plan              string           `json:"plan"`
	Source            string           `json:"source,omitempty"`
	PlanKey           string           `json:"planKey,omitempty"`
	Status            string           `json:"status"`
	CurrentPeriodEnd  *time.Time       `json:"currentPeriodEnd,omitempty"`
	CancelAtPeriodEnd bool             `json:"cancelAtPeriodEnd"`
	PassExpiresAt     *time.Time       `json:"passExpiresAt,omitempty"`
	TrialEligible     bool             `json:"trialEligible"`
	OrganizationName  string           `json:"organizationName,omitempty"`
	OwnedLicense      *licenseResponse `json:"ownedLicense,omitempty"`
	BillingEnabled    bool             `json:"billingEnabled"`
	CanManageBilling  bool             `json:"canManageBilling"`
	Email             string           `json:"email"`
	EmailVerified     bool             `json:"emailVerified"`
	EmailEnabled      bool             `json:"emailEnabled"`
	JobMatches        usageResponse    `json:"jobMatches"`
}

func (s *Server) handleBillingPlans(w http.ResponseWriter, r *http.Request) {
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
		"billingEnabled": s.billingEnabled(),
		"plans":          plans,
		"gmailEnabled":   gmailConfigured(),
		"emailEnabled":   s.emailEnabled(),
		"supportEmail":   strings.TrimSpace(s.web.SupportEmail),
	})
}

func (s *Server) handleBillingStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, "GET")
		return
	}

	user, ok := s.requireSessionUser(w, r)
	if !ok {
		return
	}

	current, err := s.entitlementFor(r.Context(), user)
	if err != nil {
		log.Printf("load entitlement for user %s: %v", user.ID, err)
		writeError(w, http.StatusInternalServerError, "failed to load subscription")
		return
	}

	response := billingStatusResponse{
		Plan:              "none",
		Source:            current.Source,
		Status:            "none",
		BillingEnabled:    s.billingEnabled(),
		CanManageBilling:  s.billingEnabled() && current.HasCustomer,
		CancelAtPeriodEnd: current.Subscription.CancelAtPeriodEnd,
		CurrentPeriodEnd:  current.Subscription.CurrentPeriodEnd,
		PassExpiresAt:     current.Subscription.PassExpiresAt,
		TrialEligible:     current.TrialEligible,
		Email:             user.Email,
		EmailVerified:     current.EmailVerified,
		EmailEnabled:      s.emailEnabled(),
		JobMatches:        usageResponse{Limit: ProJobMatchesPerHour, Period: "hour"},
	}

	if current.Subscription.Status != "" {
		response.Status = current.Subscription.Status
	}
	if current.Pro {
		response.Plan = "pro"
	}
	if current.Organization != nil {
		response.OrganizationName = current.Organization.Name
	}
	if plan, found := s.billingConfig.planByPriceID(current.Subscription.PriceID); found {
		response.PlanKey = plan.Key
	}
	if owned := current.OwnedLicense; owned != nil {
		response.OwnedLicense = &licenseResponse{
			Name:              owned.Name,
			Domain:            owned.EmailDomain,
			Status:            owned.Status,
			Active:            owned.Active(),
			CurrentPeriodEnd:  owned.CurrentPeriodEnd,
			CancelAtPeriodEnd: owned.CancelAtPeriodEnd,
			CanManage:         s.billingEnabled() && owned.StripeCustomerID != "",
		}
	}

	if s.billingRepo != nil {
		used, err := s.billingRepo.CountUsageSince(r.Context(), user.ID, featureJobMatch, time.Now().Add(-time.Hour))
		if err != nil {
			log.Printf("count usage for user %s: %v", user.ID, err)
		}
		response.JobMatches.Used = used
	}

	writeJSON(w, http.StatusOK, response)
}

type checkoutRequest struct {
	Plan             string `json:"plan"`
	OrganizationName string `json:"organizationName,omitempty"`
}

func (s *Server) handleBillingCheckout(w http.ResponseWriter, r *http.Request) {
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

	current, err := s.entitlementFor(r.Context(), user)
	if err != nil {
		log.Printf("load entitlement for user %s: %v", user.ID, err)
		writeError(w, http.StatusInternalServerError, "failed to start checkout")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()

	if plan.Kind == PlanKindOrganization {
		s.startOrganizationCheckout(ctx, w, user, plan, current, request.OrganizationName)
		return
	}

	switch {
	case current.Source == sourceOrganization:
		writeJSON(w, http.StatusConflict, map[string]string{
			"error": "you already have Pro through your school's license",
			"code":  "already_subscribed",
		})
		return
	case current.Subscription.SubscriptionActive():
		writeJSON(w, http.StatusConflict, map[string]string{
			"error": "you already have an active plan; manage it from the billing portal",
			"code":  "already_subscribed",
		})
		return
	case plan.Kind == PlanKindSubscription && current.Source == sourcePass:
		writeJSON(w, http.StatusConflict, map[string]string{
			"error": "your Sprint pass is still active; you can subscribe after it ends",
			"code":  "already_subscribed",
		})
		return
	}

	customerID, err := s.ensureUserCustomer(ctx, user, current)
	if err != nil {
		log.Printf("ensure Stripe customer for user %s: %v", user.ID, err)
		writeError(w, http.StatusBadGateway, "failed to start checkout")
		return
	}

	params := billing.CheckoutParams{
		CustomerID: customerID,
		PriceID:    plan.PriceID,
		UserID:     user.ID,
		SuccessURL: s.web.BaseURL + "/app/billing?checkout=success",
		CancelURL:  s.web.BaseURL + "/app/billing?checkout=canceled",
		Metadata:   map[string]string{"plan_key": plan.Key},
	}

	switch plan.Kind {
	case PlanKindPass:
		params.Mode = "payment"
	default:
		params.Mode = "subscription"
		if current.TrialEligible && plan.TrialDays > 0 {
			params.TrialDays = plan.TrialDays
		}
	}

	checkoutURL, err := s.stripe.CreateCheckoutSession(ctx, params)
	if err != nil {
		log.Printf("create checkout session for user %s: %v", user.ID, err)
		writeError(w, http.StatusBadGateway, "failed to start checkout")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"url": checkoutURL})
}

func (s *Server) ensureUserCustomer(ctx context.Context, user repository.User, current entitlement) (string, error) {
	if current.Subscription.StripeCustomerID != "" {
		return current.Subscription.StripeCustomerID, nil
	}

	customerID, err := s.stripe.CreateCustomer(ctx, user.Email, user.ID)
	if err != nil {
		return "", err
	}

	attached, err := s.billingRepo.AttachStripeCustomer(ctx, user.ID, customerID)
	if err != nil {
		return "", err
	}
	return attached.StripeCustomerID, nil
}

// startOrganizationCheckout sells an annual school license. The licensed
// domain is the buyer's own verified email domain, never a typed value, so
// nobody can license a domain they don't belong to.
func (s *Server) startOrganizationCheckout(
	ctx context.Context,
	w http.ResponseWriter,
	user repository.User,
	plan Plan,
	current entitlement,
	organizationName string,
) {
	if s.accountsRepo == nil {
		writeError(w, http.StatusServiceUnavailable, "school licenses are not available")
		return
	}
	if !current.EmailVerified {
		writeJSON(w, http.StatusForbidden, map[string]string{
			"error": "confirm your school email address before buying a school license",
			"code":  "email_unverified",
		})
		return
	}

	domain := emailDomain(user.Email)
	if _, public := publicEmailDomains[domain]; public || domain == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error": "sign in with your school or organization email to buy a license; personal email providers can't be licensed",
			"code":  "public_email_domain",
		})
		return
	}

	organizationName = strings.TrimSpace(organizationName)
	if n := utf8.RuneCountInString(organizationName); n < 2 || n > 120 {
		writeError(w, http.StatusBadRequest, "enter your school or program name")
		return
	}

	org, err := s.accountsRepo.UpsertOrganizationForOwner(ctx, organizationName, domain, user.ID)
	if errors.Is(err, repository.ErrDomainAlreadyLicensed) {
		writeJSON(w, http.StatusConflict, map[string]string{
			"error": "someone at " + domain + " already set up a CaseMade license; contact support to be added",
			"code":  "domain_taken",
		})
		return
	}
	if err != nil {
		log.Printf("create organization for user %s: %v", user.ID, err)
		writeError(w, http.StatusInternalServerError, "failed to start checkout")
		return
	}
	if org.Active() {
		writeJSON(w, http.StatusConflict, map[string]string{
			"error": "your school license is already active; manage it from Plan & billing",
			"code":  "already_subscribed",
		})
		return
	}

	customerID := org.StripeCustomerID
	if customerID == "" {
		customerID, err = s.stripe.CreateCustomer(ctx, user.Email, "org-"+org.ID)
		if err != nil {
			log.Printf("create Stripe customer for organization %s: %v", org.ID, err)
			writeError(w, http.StatusBadGateway, "failed to start checkout")
			return
		}
		if err := s.accountsRepo.AttachOrganizationCustomer(ctx, org.ID, customerID); err != nil {
			log.Printf("attach Stripe customer for organization %s: %v", org.ID, err)
			writeError(w, http.StatusInternalServerError, "failed to start checkout")
			return
		}
	}

	checkoutURL, err := s.stripe.CreateCheckoutSession(ctx, billing.CheckoutParams{
		CustomerID: customerID,
		PriceID:    plan.PriceID,
		UserID:     user.ID,
		Mode:       "subscription",
		SuccessURL: s.web.BaseURL + "/app/billing?checkout=school",
		CancelURL:  s.web.BaseURL + "/app/billing?checkout=canceled",
		Metadata:   map[string]string{"plan_key": plan.Key, "organization_id": org.ID},
	})
	if err != nil {
		log.Printf("create school checkout for organization %s: %v", org.ID, err)
		writeError(w, http.StatusBadGateway, "failed to start checkout")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"url": checkoutURL})
}

type portalRequest struct {
	Target string `json:"target,omitempty"` // "" (personal) or "organization"
}

func (s *Server) handleBillingPortal(w http.ResponseWriter, r *http.Request) {
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

	var request portalRequest
	if r.ContentLength > 0 {
		if err := decodeApplicationJSON(r, &request); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
	}

	current, err := s.entitlementFor(r.Context(), user)
	if err != nil {
		log.Printf("load entitlement for user %s: %v", user.ID, err)
		writeError(w, http.StatusInternalServerError, "failed to open billing portal")
		return
	}

	customerID := current.Subscription.StripeCustomerID
	if request.Target == "organization" {
		customerID = ""
		if current.OwnedLicense != nil {
			customerID = current.OwnedLicense.StripeCustomerID
		}
	}

	if customerID == "" {
		writeError(w, http.StatusBadRequest, "no billing account yet; choose a plan first")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()

	portalURL, err := s.stripe.CreatePortalSession(ctx, customerID, s.web.BaseURL+"/app/billing")
	if err != nil {
		log.Printf("create portal session for user %s: %v", user.ID, err)
		writeError(w, http.StatusBadGateway, "failed to open billing portal")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"url": portalURL})
}

// ---------------------------------------------------------------------------
// Stripe webhooks
// ---------------------------------------------------------------------------

const maxWebhookBodyBytes int64 = 1 << 20

func (s *Server) handleStripeWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, "POST")
		return
	}

	if !s.billingEnabled() || s.billingConfig.WebhookSecret == "" {
		writeError(w, http.StatusServiceUnavailable, "billing is not configured")
		return
	}

	payload, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxWebhookBodyBytes))
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

	first, err := s.billingRepo.MarkWebhookEventProcessed(r.Context(), event.ID, event.Type)
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
		if forgetErr := s.billingRepo.ForgetWebhookEvent(context.Background(), event.ID); forgetErr != nil {
			log.Printf("forget Stripe event %s: %v", event.ID, forgetErr)
		}

		writeError(w, http.StatusInternalServerError, "failed to process event")
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (s *Server) processStripeEvent(ctx context.Context, event billing.Event) error {
	var subscriptionID string

	switch event.Type {
	case "checkout.session.completed", "checkout.session.async_payment_succeeded":
		var session struct {
			Mode              string            `json:"mode"`
			Subscription      string            `json:"subscription"`
			Customer          string            `json:"customer"`
			PaymentStatus     string            `json:"payment_status"`
			ClientReferenceID string            `json:"client_reference_id"`
			Metadata          map[string]string `json:"metadata"`
		}
		if err := json.Unmarshal(event.Data.Object, &session); err != nil {
			return err
		}

		if session.Mode == "payment" {
			return s.grantPassFromCheckout(ctx, event.ID, session.Customer, session.ClientReferenceID, session.PaymentStatus, session.Metadata)
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

	update := repository.StripeSubscriptionUpdate{
		StripeCustomerID:     subscription.Customer,
		StripeSubscriptionID: subscription.ID,
		Status:               subscription.Status,
		PriceID:              subscription.PriceID(),
		CurrentPeriodEnd:     subscription.PeriodEnd(),
		CancelAtPeriodEnd:    subscription.CancelAtPeriodEnd,
	}

	_, err = s.billingRepo.ApplyStripeSubscription(ctx, update)
	if !errors.Is(err, repository.ErrSubscriptionNotFound) {
		return err
	}

	// Not a personal subscription; it may be a school license.
	if s.accountsRepo != nil {
		err = s.accountsRepo.ApplyOrganizationSubscription(ctx, update)
		if !errors.Is(err, repository.ErrOrganizationNotFound) {
			return err
		}
	}

	log.Printf("Stripe event %s: no CaseMade user or school for customer %s", event.ID, subscription.Customer)
	return nil
}

func (s *Server) grantPassFromCheckout(
	ctx context.Context,
	eventID, customerID, userID, paymentStatus string,
	metadata map[string]string,
) error {
	if paymentStatus != "paid" {
		// Delayed payment methods report success in a later event.
		return nil
	}

	plan, found := s.billingConfig.planByKey(metadata["plan_key"])
	if !found || plan.Kind != PlanKindPass || plan.PassDays <= 0 {
		log.Printf("Stripe event %s: payment for unknown pass plan %q", eventID, metadata["plan_key"])
		return nil
	}

	_, err := s.billingRepo.GrantPass(ctx, customerID, userID, plan.PassDays)
	if errors.Is(err, repository.ErrSubscriptionNotFound) {
		log.Printf("Stripe event %s: no CaseMade user %s for customer %s", eventID, userID, customerID)
		return nil
	}
	return err
}

// cancelBillingForDeletedAccount stops a user's personal subscription before
// their account is removed. School licenses are left running because they
// belong to the school, not to the person who bought them.
func (s *Server) cancelBillingForDeletedAccount(ctx context.Context, userID string) error {
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

// gmailConfigured reports whether Google OAuth is set up on this server, so
// the web app can hide Gmail sync instead of sending users to an error.
func gmailConfigured() bool {
	_, err := emailservice.GmailOAuthConfig()
	return err == nil
}
