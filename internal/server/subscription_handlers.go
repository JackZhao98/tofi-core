package server

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"tofi-core/internal/storage"

	"github.com/stripe/stripe-go/v82"
	checkoutsession "github.com/stripe/stripe-go/v82/checkout/session"
	portalsession "github.com/stripe/stripe-go/v82/billingportal/session"
	"github.com/stripe/stripe-go/v82/webhook"
)

// handleGetSubscription GET /api/v1/user/subscription
func (s *Server) handleGetSubscription(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value(UserContextKey).(string)

	plan, _ := s.db.GetUserPlan(userID)
	sub, _ := s.db.GetSubscription(userID)

	limits := PlanDefs["free"]
	if l, ok := PlanDefs[plan]; ok {
		limits = l
	}

	resp := map[string]any{
		"plan":   plan,
		"status": "active",
		"limits": limits,
	}
	if sub != nil {
		resp["status"] = sub.Status
		resp["current_period_end"] = sub.CurrentPeriodEnd
		resp["stripe_customer_id"] = sub.StripeCustomerID
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

// handleGetUsage GET /api/v1/user/subscription/usage
func (s *Server) handleGetUsage(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value(UserContextKey).(string)
	limits := s.getUserPlanLimits(userID)

	appsUsed, _ := s.db.CountUserApps(userID)
	dailyUsed, _ := s.db.CountDailyRuns(userID)
	concurrentUsed, _ := s.db.CountRunningRuns(userID)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"apps":            map[string]int{"used": appsUsed, "limit": limits.MaxApps},
		"daily_runs":      map[string]int{"used": dailyUsed, "limit": limits.DailyRuns},
		"concurrent_runs": map[string]int{"used": concurrentUsed, "limit": limits.ConcurrentRuns},
	})
}

// handleCreateCheckout POST /api/v1/user/subscription/checkout
func (s *Server) handleCreateCheckout(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value(UserContextKey).(string)

	priceID := stripePriceID()
	if priceID == "" {
		writeJSONError(w, http.StatusServiceUnavailable, ErrInternal, "Stripe not configured", "")
		return
	}

	// Get user email for Stripe customer
	u, err := s.db.GetUser(userID)
	if err != nil {
		writeJSONError(w, http.StatusNotFound, ErrNotFound, "User not found", "")
		return
	}

	customerID, err := s.getOrCreateStripeCustomer(userID, u.Email)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, ErrInternal, fmt.Sprintf("Failed to create Stripe customer: %v", err), "")
		return
	}

	// Parse optional success/cancel URLs from body
	var body struct {
		SuccessURL string `json:"success_url"`
		CancelURL  string `json:"cancel_url"`
	}
	json.NewDecoder(r.Body).Decode(&body)

	if body.SuccessURL == "" {
		body.SuccessURL = "https://tofi.sentiosurge.com/plan/success?session_id={CHECKOUT_SESSION_ID}"
	}
	if body.CancelURL == "" {
		body.CancelURL = "https://tofi.sentiosurge.com/plan/cancel"
	}

	params := &stripe.CheckoutSessionParams{
		Customer: stripe.String(customerID),
		Mode:     stripe.String(string(stripe.CheckoutSessionModeSubscription)),
		LineItems: []*stripe.CheckoutSessionLineItemParams{
			{
				Price:    stripe.String(priceID),
				Quantity: stripe.Int64(1),
			},
		},
		SuccessURL:        stripe.String(body.SuccessURL),
		CancelURL:         stripe.String(body.CancelURL),
		ClientReferenceID: stripe.String(userID),
	}

	session, err := checkoutsession.New(params)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, ErrInternal, fmt.Sprintf("Failed to create checkout: %v", err), "")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"checkout_url": session.URL,
	})
}

// handleCreatePortal POST /api/v1/user/subscription/portal
func (s *Server) handleCreatePortal(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value(UserContextKey).(string)

	sub, err := s.db.GetSubscription(userID)
	if err != nil || sub == nil || sub.StripeCustomerID == "" {
		writeJSONError(w, http.StatusBadRequest, ErrBadRequest, "No billing account found", "Upgrade to Developer first")
		return
	}

	var body struct {
		ReturnURL string `json:"return_url"`
	}
	json.NewDecoder(r.Body).Decode(&body)
	if body.ReturnURL == "" {
		body.ReturnURL = "https://tofi.sentiosurge.com/settings"
	}

	params := &stripe.BillingPortalSessionParams{
		Customer:  stripe.String(sub.StripeCustomerID),
		ReturnURL: stripe.String(body.ReturnURL),
	}
	session, err := portalsession.New(params)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, ErrInternal, fmt.Sprintf("Failed to create portal: %v", err), "")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"portal_url": session.URL,
	})
}

// handleListSubEvents GET /api/v1/user/subscription/events
func (s *Server) handleListSubEvents(w http.ResponseWriter, r *http.Request) {
	userID := r.Context().Value(UserContextKey).(string)

	events, err := s.db.ListSubscriptionEvents(userID, 20)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, ErrInternal, "Failed to fetch events", "")
		return
	}
	if events == nil {
		events = []*storage.SubscriptionEvent{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"events": events})
}

// handleStripeWebhook POST /api/v1/webhooks/stripe
// Public endpoint — no auth middleware. Verified by Stripe signature.
func (s *Server) handleStripeWebhook(w http.ResponseWriter, r *http.Request) {
	secret := stripeWebhookSecret()
	if secret == "" {
		http.Error(w, "Webhook not configured", http.StatusServiceUnavailable)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 65536))
	if err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	event, err := webhook.ConstructEvent(body, r.Header.Get("Stripe-Signature"), secret)
	if err != nil {
		log.Printf("[stripe] webhook signature verification failed: %v", err)
		http.Error(w, "Invalid signature", http.StatusBadRequest)
		return
	}

	switch event.Type {
	case "checkout.session.completed":
		s.handleCheckoutCompleted(event)
	case "invoice.paid":
		s.handleInvoicePaid(event)
	case "invoice.payment_failed":
		s.handleInvoicePaymentFailed(event)
	case "customer.subscription.deleted":
		s.handleSubscriptionDeleted(event)
	default:
		log.Printf("[stripe] unhandled event type: %s", event.Type)
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"received": true}`))
}

func (s *Server) handleCheckoutCompleted(event stripe.Event) {
	var session stripe.CheckoutSession
	if err := json.Unmarshal(event.Data.Raw, &session); err != nil {
		log.Printf("[stripe] failed to parse checkout session: %v", err)
		return
	}

	userID := session.ClientReferenceID
	if userID == "" {
		log.Printf("[stripe] checkout completed but no client_reference_id")
		return
	}

	sub := &storage.SubscriptionRecord{
		UserID:               userID,
		Plan:                 "developer",
		Status:               "active",
		StripeCustomerID:     session.Customer.ID,
		StripeSubscriptionID: session.Subscription.ID,
	}
	if err := s.db.UpsertSubscription(sub); err != nil {
		log.Printf("[stripe] failed to upsert subscription for %s: %v", userID, err)
		return
	}

	s.db.LogSubscriptionEvent(userID, "upgraded", "free", "developer", event.ID, "{}")
	log.Printf("[stripe] user %s upgraded to developer", userID)
}

func (s *Server) handleInvoicePaid(event stripe.Event) {
	var invoice struct {
		Customer     string `json:"customer"`
		Subscription string `json:"subscription"`
		PeriodStart  int64  `json:"period_start"`
		PeriodEnd    int64  `json:"period_end"`
	}
	if err := json.Unmarshal(event.Data.Raw, &invoice); err != nil {
		log.Printf("[stripe] failed to parse invoice: %v", err)
		return
	}

	// Find user by Stripe customer ID
	sub := s.findSubByCustomerID(invoice.Customer)
	if sub == nil {
		log.Printf("[stripe] invoice.paid but no user found for customer %s", invoice.Customer)
		return
	}

	sub.CurrentPeriodStart = time.Unix(invoice.PeriodStart, 0).UTC().Format(time.RFC3339)
	sub.CurrentPeriodEnd = time.Unix(invoice.PeriodEnd, 0).UTC().Format(time.RFC3339)
	sub.Status = "active"

	if err := s.db.UpsertSubscription(sub); err != nil {
		log.Printf("[stripe] failed to update period for %s: %v", sub.UserID, err)
		return
	}

	s.db.LogSubscriptionEvent(sub.UserID, "renewed", "developer", "developer", event.ID, "{}")
	log.Printf("[stripe] subscription renewed for user %s", sub.UserID)
}

func (s *Server) handleInvoicePaymentFailed(event stripe.Event) {
	var invoice struct {
		Customer string `json:"customer"`
	}
	if err := json.Unmarshal(event.Data.Raw, &invoice); err != nil {
		return
	}

	sub := s.findSubByCustomerID(invoice.Customer)
	if sub == nil {
		return
	}

	sub.Status = "past_due"
	s.db.UpsertSubscription(sub)
	s.db.LogSubscriptionEvent(sub.UserID, "payment_failed", sub.Plan, sub.Plan, event.ID, "{}")
	log.Printf("[stripe] payment failed for user %s", sub.UserID)
}

func (s *Server) handleSubscriptionDeleted(event stripe.Event) {
	var stripeSub struct {
		Customer string `json:"customer"`
	}
	if err := json.Unmarshal(event.Data.Raw, &stripeSub); err != nil {
		return
	}

	sub := s.findSubByCustomerID(stripeSub.Customer)
	if sub == nil {
		return
	}

	oldPlan := sub.Plan
	sub.Plan = "free"
	sub.Status = "active"
	sub.StripeSubscriptionID = ""
	sub.CurrentPeriodStart = ""
	sub.CurrentPeriodEnd = ""

	s.db.UpsertSubscription(sub)
	s.db.LogSubscriptionEvent(sub.UserID, "downgraded", oldPlan, "free", event.ID, "{}")
	log.Printf("[stripe] user %s downgraded to free (subscription deleted)", sub.UserID)
}

// findSubByCustomerID looks up a subscription by Stripe customer ID.
func (s *Server) findSubByCustomerID(customerID string) *storage.SubscriptionRecord {
	sub, _ := s.db.GetSubscriptionByStripeCustomer(customerID)
	return sub
}

// startSubscriptionExpiryChecker runs a goroutine that checks for expired subscriptions every hour.
func (s *Server) startSubscriptionExpiryChecker() {
	go func() {
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			expired, err := s.db.GetExpiredSubscriptions()
			if err != nil {
				log.Printf("[plan] failed to check expired subscriptions: %v", err)
				continue
			}
			for _, sub := range expired {
				oldPlan := sub.Plan
				sub.Plan = "free"
				sub.Status = "active"
				sub.StripeSubscriptionID = ""
				sub.CurrentPeriodStart = ""
				sub.CurrentPeriodEnd = ""
				s.db.UpsertSubscription(sub)
				s.db.LogSubscriptionEvent(sub.UserID, "expired", oldPlan, "free", "", "{}")
				log.Printf("[plan] auto-downgraded user %s from %s to free (expired)", sub.UserID, oldPlan)
			}
		}
	}()
}
