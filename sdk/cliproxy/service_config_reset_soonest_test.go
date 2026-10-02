package cliproxy

import (
	"testing"
	"time"

	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/quota"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/config"
)

func TestNormalizedRoutingRuntimeStateAcceptsResetSoonestAndItsAlias(t *testing.T) {
	t.Parallel()

	for _, raw := range []string{"reset-soonest", "resetsoonest", "expiring-first", "expiringfirst", "RESET-SOONEST"} {
		cfg := &config.Config{}
		cfg.Routing.Strategy = raw
		state := normalizedRoutingRuntimeState(cfg)
		if state.strategy != "reset-soonest" {
			t.Fatalf("strategy for %q = %q, want reset-soonest", raw, state.strategy)
		}
		// The poller exists to feed this strategy, so it defaults on with it.
		if !state.quotaPollerEnabled {
			t.Fatalf("quota poller for %q should default on", raw)
		}
	}
}

func TestNormalizedRoutingRuntimeStateDefaults(t *testing.T) {
	t.Parallel()

	state := normalizedRoutingRuntimeState(nil)
	if state.strategy != "round-robin" {
		t.Fatalf("strategy = %q, want round-robin", state.strategy)
	}
	if state.fiveHourGuardPercent != quota.DefaultFiveHourGuardPercent {
		t.Fatalf("guard = %v, want %v", state.fiveHourGuardPercent, quota.DefaultFiveHourGuardPercent)
	}
	if state.quotaPollerEnabled {
		t.Fatal("the poller should default off outside reset-soonest")
	}
	if state.quotaPollerClaudeEvery != quota.DefaultClaudeInterval ||
		state.quotaPollerClaudeGap != quota.DefaultClaudeMinGap ||
		state.quotaPollerCodexEvery != quota.DefaultCodexInterval {
		t.Fatalf("poller intervals = %+v, want the package defaults", state)
	}
}

func TestNormalizedRoutingRuntimeStateReadsResetSoonestSettings(t *testing.T) {
	t.Parallel()

	guard := 75.0
	disabled := false
	cfg := &config.Config{}
	cfg.Routing.Strategy = "reset-soonest"
	cfg.Routing.ResetSoonest.FiveHourGuardPercent = &guard
	cfg.Routing.QuotaPoller.Enabled = &disabled
	cfg.Routing.QuotaPoller.ClaudeInterval = "45m"
	cfg.Routing.QuotaPoller.ClaudeMinGap = "15m"
	cfg.Routing.QuotaPoller.CodexInterval = "2m"

	state := normalizedRoutingRuntimeState(cfg)
	if state.fiveHourGuardPercent != 75 {
		t.Fatalf("guard = %v, want 75", state.fiveHourGuardPercent)
	}
	// An explicit setting always wins over the strategy default.
	if state.quotaPollerEnabled {
		t.Fatal("an explicit enabled:false should disable the poller")
	}
	if state.quotaPollerClaudeEvery != 45*time.Minute {
		t.Fatalf("claude interval = %s, want 45m", state.quotaPollerClaudeEvery)
	}
	if state.quotaPollerClaudeGap != 15*time.Minute {
		t.Fatalf("claude gap = %s, want 15m", state.quotaPollerClaudeGap)
	}
	if state.quotaPollerCodexEvery != 2*time.Minute {
		t.Fatalf("codex interval = %s, want 2m", state.quotaPollerCodexEvery)
	}
}

func TestNormalizedRoutingRuntimeStateIgnoresBadValues(t *testing.T) {
	t.Parallel()

	guard := 150.0
	cfg := &config.Config{}
	cfg.Routing.Strategy = "reset-soonest"
	cfg.Routing.ResetSoonest.FiveHourGuardPercent = &guard
	cfg.Routing.QuotaPoller.ClaudeInterval = "not a duration"
	cfg.Routing.QuotaPoller.CodexInterval = "-5m"

	state := normalizedRoutingRuntimeState(cfg)
	if state.fiveHourGuardPercent != quota.DefaultFiveHourGuardPercent {
		t.Fatalf("guard = %v, want the default", state.fiveHourGuardPercent)
	}
	if state.quotaPollerClaudeEvery != quota.DefaultClaudeInterval {
		t.Fatalf("claude interval = %s, want the default", state.quotaPollerClaudeEvery)
	}
	if state.quotaPollerCodexEvery != quota.DefaultCodexInterval {
		t.Fatalf("codex interval = %s, want the default", state.quotaPollerCodexEvery)
	}
}

func TestNewRoutingSelectorBuildsResetSoonest(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{}
	cfg.Routing.Strategy = "expiring-first"
	selector := newRoutingSelector(normalizedRoutingRuntimeState(cfg))
	resetSoonest, ok := selector.(*coreauth.ResetSoonestSelector)
	if !ok {
		t.Fatalf("selector = %T, want *coreauth.ResetSoonestSelector", selector)
	}
	if resetSoonest.FiveHourGuardPercent != quota.DefaultFiveHourGuardPercent {
		t.Fatalf("guard = %v, want %v", resetSoonest.FiveHourGuardPercent, quota.DefaultFiveHourGuardPercent)
	}

	// With session affinity on, reset-soonest becomes the wrapper's fallback so
	// an established binding keeps its credential.
	cfg.Routing.SessionAffinity = true
	wrapped := newRoutingSelector(normalizedRoutingRuntimeState(cfg))
	affinity, ok := wrapped.(*coreauth.SessionAffinitySelector)
	if !ok {
		t.Fatalf("selector = %T, want *coreauth.SessionAffinitySelector", wrapped)
	}
	defer affinity.Stop()
}

// TestRoutingRuntimeStateStaysComparable guards the != comparison in
// applyManagerConfig: an uncomparable field there would panic at runtime.
func TestRoutingRuntimeStateStaysComparable(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{}
	cfg.Routing.Strategy = "reset-soonest"
	left := normalizedRoutingRuntimeState(cfg)
	right := normalizedRoutingRuntimeState(cfg)
	if left != right {
		t.Fatal("two states from the same config should compare equal")
	}
	cfg.Routing.QuotaPoller.CodexInterval = "9m"
	if left == normalizedRoutingRuntimeState(cfg) {
		t.Fatal("a changed poller interval should produce a different state")
	}
}
