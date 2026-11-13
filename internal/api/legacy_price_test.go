package api

import "testing"

func TestPlanByPriceIDKeepsLegacyPrices(t *testing.T) {
	config := BillingConfig{Plans: []Plan{
		{Key: "monthly", PriceID: "price_new_pro", LegacyPriceIDs: []string{"price_old_pro"}},
		{Key: "interview", PriceID: "price_new_prep", LegacyPriceIDs: []string{"price_old_prep"}, Access: AccessInterview},
	}}
	for priceID, want := range map[string]string{
		"price_new_pro": "monthly", "price_old_pro": "monthly",
		"price_new_prep": "interview", "price_old_prep": "interview",
	} {
		plan, ok := config.planByPriceID(priceID)
		if !ok || plan.Key != want {
			t.Errorf("planByPriceID(%q) = %q, %v; want %q", priceID, plan.Key, ok, want)
		}
	}
	if _, ok := config.planByPriceID(""); ok {
		t.Error("an empty price ID should not match a plan")
	}
	if _, ok := config.planByPriceID("price_unknown"); ok {
		t.Error("an unknown price ID should not match a plan")
	}
}
