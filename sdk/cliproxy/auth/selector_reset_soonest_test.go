package auth

import (
	"context"
	"net/http"
	"testing"
	"time"

	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/executor"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/quota"
)

var resetSoonestBase = time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)

// newResetSoonestFixture builds a selector bound to an isolated store and a
// fixed clock, so no test depends on the process-wide store or on wall time.
func newResetSoonestFixture(guard float64) (*ResetSoonestSelector, *quota.Store) {
	store := quota.NewStore(func() time.Time { return resetSoonestBase })
	selector := &ResetSoonestSelector{
		FiveHourGuardPercent: guard,
		Store:                store,
		Now:                  func() time.Time { return resetSoonestBase },
	}
	return selector, store
}

// seedLongWindow records a long-window reading for one credential.
func seedLongWindow(store *quota.Store, authID, provider, windowID string, usedPercent float64, resetIn time.Duration) {
	store.Observe(quota.Identity{AuthID: authID, Provider: provider, FileName: authID + ".json"},
		map[string]quota.Window{
			windowID: {UsedPercent: usedPercent, ResetAt: resetSoonestBase.Add(resetIn), Known: true, LearnedAt: resetSoonestBase},
		}, resetSoonestBase)
}

func TestResetSoonestSelectorPicksTheMostUrgentCredential(t *testing.T) {
	t.Parallel()

	selector, store := newResetSoonestFixture(0)
	auths := []*Auth{
		{ID: "plenty-left", Provider: "claude", FileName: "plenty-left.json"},
		{ID: "expiring", Provider: "claude", FileName: "expiring.json"},
	}
	// 90 percent left but four days away versus 40 percent left in three hours.
	seedLongWindow(store, "plenty-left", "claude", quota.WindowSevenDay, 10, 4*24*time.Hour)
	seedLongWindow(store, "expiring", "claude", quota.WindowSevenDay, 60, 3*time.Hour)

	auth, err := selector.Pick(context.Background(), "claude", "claude-opus-5", cliproxyexecutor.Options{}, auths)
	if err != nil {
		t.Fatalf("Pick() error = %v", err)
	}
	if auth.ID != "expiring" {
		t.Fatalf("picked %q, want expiring", auth.ID)
	}
	if selector.Picks() != 1 {
		t.Fatalf("Picks() = %d, want 1", selector.Picks())
	}
}

func TestResetSoonestSelectorRespectsThePriorityTier(t *testing.T) {
	t.Parallel()

	selector, store := newResetSoonestFixture(0)
	// The low-priority credential is far more urgent, but priority is the manual
	// override: only the highest available tier is offered to the strategy.
	auths := []*Auth{
		{ID: "high-tier", Provider: "claude", FileName: "high-tier.json", Attributes: map[string]string{"priority": "10"}},
		{ID: "low-tier-urgent", Provider: "claude", FileName: "low-tier-urgent.json", Attributes: map[string]string{"priority": "1"}},
	}
	seedLongWindow(store, "high-tier", "claude", quota.WindowSevenDay, 5, 6*24*time.Hour)
	seedLongWindow(store, "low-tier-urgent", "claude", quota.WindowSevenDay, 60, time.Hour)

	auth, err := selector.Pick(context.Background(), "claude", "claude-opus-5", cliproxyexecutor.Options{}, auths)
	if err != nil {
		t.Fatalf("Pick() error = %v", err)
	}
	if auth.ID != "high-tier" {
		t.Fatalf("picked %q, want high-tier: priority must outrank urgency", auth.ID)
	}

	// Once the high tier is blocked the low tier becomes reachable.
	auths[0].Disabled = true
	auth, err = selector.Pick(context.Background(), "claude", "claude-opus-5", cliproxyexecutor.Options{}, auths)
	if err != nil {
		t.Fatalf("Pick() after disabling the high tier error = %v", err)
	}
	if auth.ID != "low-tier-urgent" {
		t.Fatalf("picked %q, want low-tier-urgent", auth.ID)
	}
}

func TestResetSoonestSelectorAppliesTheFiveHourGuard(t *testing.T) {
	t.Parallel()

	selector, store := newResetSoonestFixture(90)
	auths := []*Auth{
		{ID: "hot", Provider: "claude", FileName: "hot.json"},
		{ID: "cool", Provider: "claude", FileName: "cool.json"},
	}
	store.Observe(quota.Identity{AuthID: "hot", Provider: "claude", FileName: "hot.json"}, map[string]quota.Window{
		quota.WindowFiveHour: {UsedPercent: 95, ResetAt: resetSoonestBase.Add(time.Hour), Known: true},
		quota.WindowSevenDay: {UsedPercent: 60, ResetAt: resetSoonestBase.Add(2 * time.Hour), Known: true},
	}, resetSoonestBase)
	store.Observe(quota.Identity{AuthID: "cool", Provider: "claude", FileName: "cool.json"}, map[string]quota.Window{
		quota.WindowFiveHour: {UsedPercent: 5, ResetAt: resetSoonestBase.Add(time.Hour), Known: true},
		quota.WindowSevenDay: {UsedPercent: 5, ResetAt: resetSoonestBase.Add(6 * 24 * time.Hour), Known: true},
	}, resetSoonestBase)

	auth, err := selector.Pick(context.Background(), "claude", "claude-opus-5", cliproxyexecutor.Options{}, auths)
	if err != nil {
		t.Fatalf("Pick() error = %v", err)
	}
	if auth.ID != "cool" {
		t.Fatalf("picked %q, want cool: the five hour guard demotes the hot credential", auth.ID)
	}
}

func TestResetSoonestSelectorRoundRobinsCredentialsWithNoReading(t *testing.T) {
	t.Parallel()

	selector, _ := newResetSoonestFixture(0)
	auths := []*Auth{
		{ID: "fresh-a", Provider: "gemini", FileName: "fresh-a.json"},
		{ID: "fresh-b", Provider: "gemini", FileName: "fresh-b.json"},
		{ID: "fresh-c", Provider: "gemini", FileName: "fresh-c.json"},
	}

	seen := make(map[string]int)
	for i := 0; i < 6; i++ {
		auth, err := selector.Pick(context.Background(), "gemini", "gemini-3-pro", cliproxyexecutor.Options{}, auths)
		if err != nil {
			t.Fatalf("Pick() error = %v", err)
		}
		seen[auth.ID]++
	}
	if len(seen) != 3 {
		t.Fatalf("picks hit %d credentials, want 3 (round-robin among unknowns): %v", len(seen), seen)
	}
	for id, count := range seen {
		if count != 2 {
			t.Fatalf("credential %q served %d of 6 picks, want 2", id, count)
		}
	}
}

func TestResetSoonestSelectorPrefersAKnownReadingOverAnUnknownOne(t *testing.T) {
	t.Parallel()

	selector, store := newResetSoonestFixture(0)
	auths := []*Auth{
		{ID: "unknown", Provider: "claude", FileName: "unknown.json"},
		{ID: "nearly-spent", Provider: "claude", FileName: "nearly-spent.json"},
	}
	// Even a credential with 1 percent left outranks one with no reading at all.
	seedLongWindow(store, "nearly-spent", "claude", quota.WindowSevenDay, 99, 7*24*time.Hour)

	auth, err := selector.Pick(context.Background(), "claude", "claude-opus-5", cliproxyexecutor.Options{}, auths)
	if err != nil {
		t.Fatalf("Pick() error = %v", err)
	}
	if auth.ID != "nearly-spent" {
		t.Fatalf("picked %q, want nearly-spent", auth.ID)
	}
}

func TestResetSoonestSelectorSkipsAHardLimitedCredential(t *testing.T) {
	t.Parallel()

	selector, store := newResetSoonestFixture(0)
	auths := []*Auth{
		{ID: "limited", Provider: "claude", FileName: "limited.json"},
		{ID: "healthy", Provider: "claude", FileName: "healthy.json"},
	}
	store.Observe(quota.Identity{AuthID: "limited", Provider: "claude", FileName: "limited.json"}, map[string]quota.Window{
		quota.WindowFiveHour: {UsedPercent: 100, HardLimited: true, ResetAt: resetSoonestBase.Add(time.Hour), Known: true},
		quota.WindowSevenDay: {UsedPercent: 80, ResetAt: resetSoonestBase.Add(time.Hour), Known: true},
	}, resetSoonestBase)
	seedLongWindow(store, "healthy", "claude", quota.WindowSevenDay, 10, 5*24*time.Hour)

	auth, err := selector.Pick(context.Background(), "claude", "claude-opus-5", cliproxyexecutor.Options{}, auths)
	if err != nil {
		t.Fatalf("Pick() error = %v", err)
	}
	if auth.ID != "healthy" {
		t.Fatalf("picked %q, want healthy", auth.ID)
	}
}

func TestResetSoonestSelectorFallsBackWhenEveryReadingLooksUnusable(t *testing.T) {
	t.Parallel()

	selector, store := newResetSoonestFixture(0)
	auths := []*Auth{{ID: "only", Provider: "claude", FileName: "only.json"}, {ID: "also", Provider: "claude", FileName: "also.json"}}
	for _, id := range []string{"only", "also"} {
		store.Observe(quota.Identity{AuthID: id, Provider: "claude", FileName: id + ".json"}, map[string]quota.Window{
			quota.WindowFiveHour: {UsedPercent: 100, ResetAt: resetSoonestBase.Add(time.Hour), Known: true},
		}, resetSoonestBase)
	}

	// Availability filtering already judged these credentials serviceable, so a
	// reading that says otherwise must not empty the pool and fail the request.
	auth, err := selector.Pick(context.Background(), "claude", "claude-opus-5", cliproxyexecutor.Options{}, auths)
	if err != nil {
		t.Fatalf("Pick() error = %v", err)
	}
	if auth == nil {
		t.Fatal("Pick() returned no auth; the strategy must not fail a serviceable request")
	}
}

func TestResetSoonestSelectorUsesTheFableWindowForFableRequests(t *testing.T) {
	t.Parallel()

	selector, store := newResetSoonestFixture(0)
	auths := []*Auth{
		{ID: "fable-spent", Provider: "claude", FileName: "fable-spent.json"},
		{ID: "fable-fresh", Provider: "claude", FileName: "fable-fresh.json"},
	}
	// Identical weekly windows; only the Fable window differs.
	store.Observe(quota.Identity{AuthID: "fable-spent", Provider: "claude", FileName: "fable-spent.json"}, map[string]quota.Window{
		quota.WindowSevenDay: {UsedPercent: 50, ResetAt: resetSoonestBase.Add(48 * time.Hour), Known: true},
		quota.WindowFable:    {UsedPercent: 95, ResetAt: resetSoonestBase.Add(48 * time.Hour), Known: true},
	}, resetSoonestBase)
	store.Observe(quota.Identity{AuthID: "fable-fresh", Provider: "claude", FileName: "fable-fresh.json"}, map[string]quota.Window{
		quota.WindowSevenDay: {UsedPercent: 50, ResetAt: resetSoonestBase.Add(48 * time.Hour), Known: true},
		quota.WindowFable:    {UsedPercent: 10, ResetAt: resetSoonestBase.Add(48 * time.Hour), Known: true},
	}, resetSoonestBase)

	// For a Fable request the nearly spent Fable window ranks lower, so the fresh
	// credential wins. On a non-Fable model the two tie and the ID breaks it.
	fableAuth, err := selector.Pick(context.Background(), "claude", "claude-fable-5-1", cliproxyexecutor.Options{}, auths)
	if err != nil {
		t.Fatalf("Pick() error = %v", err)
	}
	if fableAuth.ID != "fable-fresh" {
		t.Fatalf("fable pick = %q, want fable-fresh", fableAuth.ID)
	}
	opusAuth, err := selector.Pick(context.Background(), "claude", "claude-opus-5", cliproxyexecutor.Options{}, auths)
	if err != nil {
		t.Fatalf("Pick() error = %v", err)
	}
	if opusAuth.ID != "fable-fresh" && opusAuth.ID != "fable-spent" {
		t.Fatalf("opus pick = %q, want one of the two credentials", opusAuth.ID)
	}
}

func TestResetSoonestSelectorIngestsHeaderSignals(t *testing.T) {
	t.Parallel()

	selector, store := newResetSoonestFixture(0)
	// No seeded readings: the only source is the passive header snapshot upstream
	// already keeps on each credential.
	auths := []*Auth{
		{ID: "calm", Provider: "claude", FileName: "calm.json", Quota: QuotaState{
			ObservedAt: resetSoonestBase,
			Signals: map[string]string{
				"anthropic-ratelimit-unified-7d-utilization": "5",
				"anthropic-ratelimit-unified-7d-reset":       resetSoonestBase.Add(6 * 24 * time.Hour).Format(time.RFC3339),
			},
		}},
		{ID: "urgent", Provider: "claude", FileName: "urgent.json", Quota: QuotaState{
			ObservedAt: resetSoonestBase,
			Signals: map[string]string{
				"anthropic-ratelimit-unified-7d-utilization": "40",
				"anthropic-ratelimit-unified-7d-reset":       resetSoonestBase.Add(2 * time.Hour).Format(time.RFC3339),
			},
		}},
	}

	auth, err := selector.Pick(context.Background(), "claude", "claude-opus-5", cliproxyexecutor.Options{}, auths)
	if err != nil {
		t.Fatalf("Pick() error = %v", err)
	}
	if auth.ID != "urgent" {
		t.Fatalf("picked %q, want urgent", auth.ID)
	}
	if _, ok := store.Lookup("calm"); !ok {
		t.Fatal("the store should have learned the calm credential from its headers")
	}
}

func TestIngestAuthQuotaRecordsAndClearsAHardLimit(t *testing.T) {
	t.Parallel()

	store := quota.NewStore(func() time.Time { return resetSoonestBase })
	recoverAt := resetSoonestBase.Add(30 * time.Minute)
	auth := &Auth{ID: "a", Provider: "claude", FileName: "a.json", Quota: QuotaState{
		ObservedAt: resetSoonestBase,
		Signals: map[string]string{
			"anthropic-ratelimit-unified-5h-utilization": "100",
			"anthropic-ratelimit-unified-5h-reset":       recoverAt.Format(time.RFC3339),
		},
		Exceeded:      true,
		Reason:        "credential_quota",
		NextRecoverAt: recoverAt,
	}}

	IngestAuthQuota(store, []*Auth{auth}, resetSoonestBase)
	reading, ok := store.Lookup("a")
	if !ok {
		t.Fatal("reading missing")
	}
	window, _ := reading.Window(quota.WindowFiveHour)
	if !window.HardLimited {
		t.Fatalf("five hour = %+v, want hard limited", window)
	}

	// Leaving cooldown clears it.
	auth.Quota.Exceeded = false
	IngestAuthQuota(store, []*Auth{auth}, resetSoonestBase)
	reading, _ = store.Lookup("a")
	if window, _ = reading.Window(quota.WindowFiveHour); window.HardLimited {
		t.Fatal("leaving cooldown should clear the hard limit")
	}
}

func TestIngestAuthQuotaRegistersCredentialsWithNoSignals(t *testing.T) {
	t.Parallel()

	store := quota.NewStore(func() time.Time { return resetSoonestBase })
	IngestAuthQuota(store, []*Auth{{ID: "a", Provider: "codex", FileName: "codex-a.json"}}, resetSoonestBase)
	snapshot := store.Snapshot()
	entry, ok := snapshot["codex-a.json"]
	if !ok {
		t.Fatalf("snapshot = %v, want the credential listed even with no reading", snapshot)
	}
	if entry.Provider != "codex" {
		t.Fatalf("provider = %q, want codex", entry.Provider)
	}
	if len(entry.Windows) != 0 {
		t.Fatalf("windows = %+v, want none", entry.Windows)
	}
}

func TestSessionAffinityKeepsABoundSessionOnItsCredential(t *testing.T) {
	t.Parallel()

	resetSoonest, store := newResetSoonestFixture(0)
	selector := NewSessionAffinitySelectorWithConfig(SessionAffinityConfig{
		Fallback: resetSoonest,
		TTL:      time.Hour,
	})
	defer selector.Stop()

	auths := []*Auth{
		{ID: "first-choice", Provider: "claude", FileName: "first-choice.json"},
		{ID: "second-choice", Provider: "claude", FileName: "second-choice.json"},
	}
	seedLongWindow(store, "first-choice", "claude", quota.WindowSevenDay, 60, 2*time.Hour)
	seedLongWindow(store, "second-choice", "claude", quota.WindowSevenDay, 10, 6*24*time.Hour)

	opts := func() cliproxyexecutor.Options {
		return cliproxyexecutor.Options{
			Headers:         http.Header{"X-Claude-Code-Session-Id": []string{"session-reset-soonest"}},
			OriginalRequest: []byte(`{"messages":[{"role":"user","content":"hello"}]}`),
			Metadata:        map[string]any{},
		}
	}

	bound, err := selector.Pick(context.Background(), "claude", "claude-opus-5", opts(), auths)
	if err != nil {
		t.Fatalf("first Pick() error = %v", err)
	}
	if bound.ID != "first-choice" {
		t.Fatalf("first pick = %q, want first-choice", bound.ID)
	}

	// The other credential becomes far more urgent. An established binding
	// outranks the strategy, so the thread stays put for its prompt cache.
	seedLongWindowAt(store, "second-choice", quota.WindowSevenDay, 60, 30*time.Minute)
	again, err := selector.Pick(context.Background(), "claude", "claude-opus-5", opts(), auths)
	if err != nil {
		t.Fatalf("second Pick() error = %v", err)
	}
	if again.ID != "first-choice" {
		t.Fatalf("second pick = %q, want first-choice: affinity must hold the binding", again.ID)
	}

	// When the bound credential becomes unusable the wrapper re-picks through the
	// strategy, which now chooses the more urgent one.
	auths[0].Disabled = true
	rebound, err := selector.Pick(context.Background(), "claude", "claude-opus-5", opts(), auths)
	if err != nil {
		t.Fatalf("failover Pick() error = %v", err)
	}
	if rebound.ID != "second-choice" {
		t.Fatalf("failover pick = %q, want second-choice", rebound.ID)
	}
}

// seedLongWindowAt overwrites a long-window reading with a newer learned time so
// the store accepts it.
func seedLongWindowAt(store *quota.Store, authID, windowID string, usedPercent float64, resetIn time.Duration) {
	learnedAt := resetSoonestBase.Add(time.Second)
	store.Observe(quota.Identity{AuthID: authID, Provider: "claude", FileName: authID + ".json"},
		map[string]quota.Window{
			windowID: {UsedPercent: usedPercent, ResetAt: resetSoonestBase.Add(resetIn), Known: true, LearnedAt: learnedAt},
		}, learnedAt)
}
