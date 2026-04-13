package server

import (
	"fmt"
	"os"

	"tofi-core/internal/storage"

	"github.com/stripe/stripe-go/v82"
	"github.com/stripe/stripe-go/v82/customer"
)

// initStripe sets the Stripe API key from environment. Returns false if not configured.
func initStripe() bool {
	key := os.Getenv("STRIPE_SECRET_KEY")
	if key == "" {
		return false
	}
	stripe.Key = key
	return true
}

func stripeWebhookSecret() string {
	return os.Getenv("STRIPE_WEBHOOK_SECRET")
}

func stripePriceID() string {
	return os.Getenv("STRIPE_PRICE_ID")
}

// getOrCreateStripeCustomer finds or creates a Stripe customer for the given user.
func (s *Server) getOrCreateStripeCustomer(userID, email string) (string, error) {
	// Check if we already have a Stripe customer ID
	sub, err := s.db.GetSubscription(userID)
	if err != nil {
		return "", fmt.Errorf("get subscription: %w", err)
	}
	if sub != nil && sub.StripeCustomerID != "" {
		return sub.StripeCustomerID, nil
	}

	// Create a new Stripe customer
	params := &stripe.CustomerParams{
		Email: stripe.String(email),
	}
	params.AddMetadata("tofi_user_id", userID)
	c, err := customer.New(params)
	if err != nil {
		return "", fmt.Errorf("create stripe customer: %w", err)
	}

	// Save the customer ID
	record := &storage.SubscriptionRecord{
		UserID:           userID,
		Plan:             "free",
		Status:           "active",
		StripeCustomerID: c.ID,
	}
	if sub != nil {
		record.Plan = sub.Plan
		record.Status = sub.Status
		record.StripeSubscriptionID = sub.StripeSubscriptionID
		record.CurrentPeriodStart = sub.CurrentPeriodStart
		record.CurrentPeriodEnd = sub.CurrentPeriodEnd
	}
	if err := s.db.UpsertSubscription(record); err != nil {
		return "", fmt.Errorf("save stripe customer id: %w", err)
	}

	return c.ID, nil
}
