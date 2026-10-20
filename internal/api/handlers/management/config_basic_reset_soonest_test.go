package management

import "testing"

// TestNormalizeRoutingStrategyAcceptsResetSoonestAndItsAlias pins both spellings
// of the strategy. "expiring-first" is the name the replication spec uses; the
// canonical value stored in the config is "reset-soonest".
func TestNormalizeRoutingStrategyAcceptsResetSoonestAndItsAlias(t *testing.T) {
	t.Parallel()

	for _, input := range []string{
		"reset-soonest", "resetsoonest", "RESET-SOONEST", " reset-soonest ",
		"expiring-first", "expiringfirst", "Expiring-First",
	} {
		got, ok := normalizeRoutingStrategy(input)
		if !ok || got != "reset-soonest" {
			t.Fatalf("normalizeRoutingStrategy(%q) = %q, %v; want reset-soonest, true", input, got, ok)
		}
	}
}

// TestNormalizeRoutingStrategyKeepsTheExistingValues guards the upstream names
// against a regression from the new case.
func TestNormalizeRoutingStrategyKeepsTheExistingValues(t *testing.T) {
	t.Parallel()

	for input, want := range map[string]string{
		"":                     "round-robin",
		"round-robin":          "round-robin",
		"rr":                   "round-robin",
		"weighted-round-robin": "weighted-round-robin",
		"wrr":                  "weighted-round-robin",
		"fill-first":           "fill-first",
		"ff":                   "fill-first",
	} {
		got, ok := normalizeRoutingStrategy(input)
		if !ok || got != want {
			t.Fatalf("normalizeRoutingStrategy(%q) = %q, %v; want %q, true", input, got, ok, want)
		}
	}
	if _, ok := normalizeRoutingStrategy("soonest-reset"); ok {
		t.Fatal("an unknown strategy should be rejected")
	}
}
