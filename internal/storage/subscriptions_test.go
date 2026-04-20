package storage

import (
	"testing"
)

// Cohort is the founding-rate lock mechanism — once stamped, upsert must
// never overwrite it even when the caller passes an empty cohort in the
// record. These tests codify that invariant.
func TestUpsertSubscription_PreservesCohort(t *testing.T) {
	db, err := InitDB(t.TempDir())
	if err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	defer db.Close()

	// First upsert stamps the cohort at checkout.
	s1 := &SubscriptionRecord{
		UserID:           "u1",
		Plan:             "developer",
		Status:           "active",
		PricingCohort:    "founding_developer",
		StripeCustomerID: "cus_1",
	}
	if err := db.UpsertSubscription(s1); err != nil {
		t.Fatalf("first upsert: %v", err)
	}

	// Webhook re-entry with no cohort (e.g. invoice.paid carries no metadata).
	// Cohort must survive.
	s2 := &SubscriptionRecord{
		UserID:           "u1",
		Plan:             "developer",
		Status:           "active",
		PricingCohort:    "", // caller didn't know the cohort
		StripeCustomerID: "cus_1",
	}
	if err := db.UpsertSubscription(s2); err != nil {
		t.Fatalf("second upsert: %v", err)
	}

	got, err := db.GetSubscription("u1")
	if err != nil || got == nil {
		t.Fatalf("get subscription: %v (nil=%v)", err, got == nil)
	}
	if got.PricingCohort != "founding_developer" {
		t.Errorf("cohort wiped by upsert: got %q, want founding_developer", got.PricingCohort)
	}
}

func TestStampPricingCohort_OnlyFillsEmpty(t *testing.T) {
	db, err := InitDB(t.TempDir())
	if err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	defer db.Close()

	// Seed an empty-cohort row (existing user pre-migration).
	if err := db.UpsertSubscription(&SubscriptionRecord{
		UserID: "u1", Plan: "free", Status: "active",
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// First stamp writes.
	if err := db.StampPricingCohort("u1", "founding_developer"); err != nil {
		t.Fatalf("stamp: %v", err)
	}
	got, _ := db.GetSubscription("u1")
	if got.PricingCohort != "founding_developer" {
		t.Errorf("first stamp: got %q, want founding_developer", got.PricingCohort)
	}

	// Second stamp must NOT overwrite (founding rate is forever).
	if err := db.StampPricingCohort("u1", "launch_developer"); err != nil {
		t.Fatalf("stamp 2: %v", err)
	}
	got, _ = db.GetSubscription("u1")
	if got.PricingCohort != "founding_developer" {
		t.Errorf("second stamp overwrote: got %q, want founding_developer", got.PricingCohort)
	}
}

func TestCountActivePricingCohort(t *testing.T) {
	db, err := InitDB(t.TempDir())
	if err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	defer db.Close()

	seed := []struct {
		id     string
		plan   string
		status string
		cohort string
	}{
		{"u1", "developer", "active", "founding_developer"},
		{"u2", "developer", "active", "founding_developer"},
		{"u3", "developer", "past_due", "founding_developer"}, // still counts
		{"u4", "developer", "cancelled", "founding_developer"}, // does NOT count
		{"u5", "developer", "active", "launch_developer"},
		{"u6", "pro", "active", "founding_pro"},
	}
	for _, s := range seed {
		if err := db.UpsertSubscription(&SubscriptionRecord{
			UserID: s.id, Plan: s.plan, Status: s.status, PricingCohort: s.cohort,
		}); err != nil {
			t.Fatalf("seed %s: %v", s.id, err)
		}
	}

	got, err := db.CountActivePricingCohort("founding_developer")
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if got != 3 {
		t.Errorf("founding_developer: got %d, want 3 (2 active + 1 past_due)", got)
	}

	got, _ = db.CountActivePricingCohort("launch_developer")
	if got != 1 {
		t.Errorf("launch_developer: got %d, want 1", got)
	}

	got, _ = db.CountActivePricingCohort("founding_pro")
	if got != 1 {
		t.Errorf("founding_pro: got %d, want 1", got)
	}

	got, _ = db.CountActivePricingCohort("nonexistent")
	if got != 0 {
		t.Errorf("nonexistent cohort: got %d, want 0", got)
	}
}
