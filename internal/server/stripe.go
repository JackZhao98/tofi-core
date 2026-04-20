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

// Founding-rate slot caps. The first N subscribers in each paid plan
// lock in the founding price; the N+1'th pays the launch price. Kept as
// package-level constants so the frontend slot counter matches the
// backend availability check exactly.
const (
	FoundingSlotsDeveloper = 100
	FoundingSlotsPro       = 100
)

// cohortPriceEnvKey maps a pricing cohort to the Stripe price ID env var
// we should read for checkout. Unknown cohorts fall through to the plan's
// generic price so existing (pre-cohort) installs still work.
//
//	founding_developer → STRIPE_PRICE_FOUNDING_DEVELOPER (fallback: STRIPE_PRICE_DEVELOPER)
//	launch_developer   → STRIPE_PRICE_LAUNCH_DEVELOPER   (fallback: STRIPE_PRICE_DEVELOPER)
//	founding_pro       → STRIPE_PRICE_FOUNDING_PRO       (fallback: STRIPE_PRICE_PRO)
//	launch_pro         → STRIPE_PRICE_LAUNCH_PRO         (fallback: STRIPE_PRICE_PRO)
//	legacy             → STRIPE_PRICE_DEVELOPER          (grandfathered)
func stripePriceForCohort(cohort string) string {
	switch cohort {
	case "founding_developer":
		if v := os.Getenv("STRIPE_PRICE_FOUNDING_DEVELOPER" + stripeEnvSuffix()); v != "" {
			return v
		}
		return stripePriceID("developer")
	case "launch_developer":
		if v := os.Getenv("STRIPE_PRICE_LAUNCH_DEVELOPER" + stripeEnvSuffix()); v != "" {
			return v
		}
		return stripePriceID("developer")
	case "founding_pro":
		if v := os.Getenv("STRIPE_PRICE_FOUNDING_PRO" + stripeEnvSuffix()); v != "" {
			return v
		}
		return stripePriceID("pro")
	case "launch_pro":
		if v := os.Getenv("STRIPE_PRICE_LAUNCH_PRO" + stripeEnvSuffix()); v != "" {
			return v
		}
		return stripePriceID("pro")
	case "legacy":
		return stripePriceID("developer")
	}
	return ""
}

// resolveCheckoutCohort decides which pricing cohort a user should get at
// the moment they start a Stripe checkout. Once set in a subscription it
// never changes; this function is only consulted for brand-new paid subs
// or resubscribes after a cancellation. Returns the cohort name and the
// resolved Stripe price ID.
func (s *Server) resolveCheckoutCohort(userID, plan string) (cohort, priceID string) {
	// Existing cohort wins — grandfathered forever.
	if sub, _ := s.db.GetSubscription(userID); sub != nil && sub.PricingCohort != "" {
		return sub.PricingCohort, stripePriceForCohort(sub.PricingCohort)
	}

	switch plan {
	case "developer":
		used, _ := s.db.CountActivePricingCohort("founding_developer")
		if used < FoundingSlotsDeveloper {
			return "founding_developer", stripePriceForCohort("founding_developer")
		}
		return "launch_developer", stripePriceForCohort("launch_developer")
	case "pro":
		used, _ := s.db.CountActivePricingCohort("founding_pro")
		if used < FoundingSlotsPro {
			return "founding_pro", stripePriceForCohort("founding_pro")
		}
		return "launch_pro", stripePriceForCohort("launch_pro")
	}
	return "", ""
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
