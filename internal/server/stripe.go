package server

import (
	"fmt"
	"log"
	"os"
	"strings"

	"tofi-core/internal/storage"

	"github.com/stripe/stripe-go/v82"
	"github.com/stripe/stripe-go/v82/customer"
)

// stripeEnvSuffix returns "_SANDBOX" when STRIPE_MODE=sandbox, "" otherwise.
func stripeEnvSuffix() string {
	if strings.EqualFold(os.Getenv("STRIPE_MODE"), "sandbox") {
		return "_SANDBOX"
	}
	return ""
}

// stripeEnv reads a Stripe env var, appending the sandbox suffix when active.
func stripeEnv(key string) string {
	return os.Getenv(key + stripeEnvSuffix())
}

// initStripe sets the Stripe API key from environment. Returns false if not configured.
func initStripe() bool {
	key := stripeEnv("STRIPE_SECRET_KEY")
	if key == "" {
		return false
	}
	stripe.Key = key
	mode := "live"
	if stripeEnvSuffix() == "_SANDBOX" {
		mode = "sandbox"
	}
	log.Printf("[stripe] initialized (%s mode)", mode)
	return true
}

func stripeWebhookSecret() string {
	return stripeEnv("STRIPE_WEBHOOK_SECRET")
}

func stripePriceID(plan string) string {
	key := "STRIPE_PRICE_" + strings.ToUpper(plan)
	return os.Getenv(key + stripeEnvSuffix())
}

// getOrCreateStripeCustomer finds or creates a Stripe customer for the given user.
func (s *Server) getOrCreateStripeCustomer(userID, email string) (string, error) {
	sub, err := s.db.GetSubscription(userID)
	if err != nil {
		return "", fmt.Errorf("get subscription: %w", err)
	}
	if sub != nil && sub.StripeCustomerID != "" {
		return sub.StripeCustomerID, nil
	}

	params := &stripe.CustomerParams{
		Email: stripe.String(email),
	}
	params.AddMetadata("tofi_user_id", userID)
	c, err := customer.New(params)
	if err != nil {
		return "", fmt.Errorf("create stripe customer: %w", err)
	}

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
