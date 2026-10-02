package quota

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// recordingDoer answers every usage request from a scripted reply and records
// which endpoint was asked. It replaces the network entirely, so no test ever
// contacts a provider or needs a credential.
type recordingDoer struct {
	mu       sync.Mutex
	calls    []string
	status   int
	body     string
	statuses []int
}

func (d *recordingDoer) Do(req *http.Request) (*http.Response, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.calls = append(d.calls, req.URL.String())
	status := d.status
	if len(d.statuses) > 0 {
		status = d.statuses[0]
		if len(d.statuses) > 1 {
			d.statuses = d.statuses[1:]
		}
	}
	if status == 0 {
		status = http.StatusOK
	}
	body := d.body
	if body == "" {
		body = "{}"
	}
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{},
		Body:       io.NopCloser(bytes.NewReader([]byte(body))),
	}, nil
}

func (d *recordingDoer) callCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.calls)
}

func (d *recordingDoer) endpointCount(substring string) int {
	d.mu.Lock()
	defer d.mu.Unlock()
	count := 0
	for _, call := range d.calls {
		if strings.Contains(call, substring) {
			count++
		}
	}
	return count
}

const claudeUsageBody = `{
  "limits": [
    {"kind": "session", "percent": 42, "resets_at": "2026-10-03T14:00:00Z"},
    {"kind": "usage", "group": "weekly", "percent": 61, "resets_at": "2026-10-08T09:00:00Z"}
  ]
}`

const codexUsageBody = `{
  "plan_type": "pro",
  "rate_limit": {
    "primary_window": {"used_percent": 12, "limit_window_seconds": 18000, "reset_at": "2026-10-03T13:00:00Z"},
    "secondary_window": {"used_percent": 34, "limit_window_seconds": 604800, "reset_at": "2026-10-09T09:00:00Z"}
  }
}`

func newTestPoller(t *testing.T, targets []Target, doer *recordingDoer) (*Poller, *Store, *fakeClock) {
	t.Helper()
	return newTestPollerWith(t, targets, doer, nil)
}

// newTestPollerWith lets a test override the spacing settings. The defaults put
// the provider-wide gap (10 min) above the first backoff (5 min), so a test that
// needs to observe the backoff on its own shortens the gap.
func newTestPollerWith(t *testing.T, targets []Target, doer *recordingDoer, tune func(*PollerConfig)) (*Poller, *Store, *fakeClock) {
	t.Helper()
	clock := &fakeClock{now: storeBase}
	store := NewStore(clock.Now)
	cfg := PollerConfig{
		Store:         store,
		Now:           clock.Now,
		Targets:       func() []Target { return targets },
		HTTPClientFor: func(Target) Doer { return doer },
	}
	if tune != nil {
		tune(&cfg)
	}
	poller := NewPoller(cfg)
	if poller == nil {
		t.Fatal("NewPoller() = nil")
	}
	return poller, store, clock
}

func TestPollerWritesReadingsForBothProviders(t *testing.T) {
	t.Parallel()

	claudeDoer := &recordingDoer{body: claudeUsageBody}
	poller, store, _ := newTestPoller(t, []Target{
		{AuthID: "claude-1", Provider: "claude", FileName: "claude-1.json", AccessToken: "test-token"},
	}, claudeDoer)
	poller.RunOnce(context.Background())

	if claudeDoer.endpointCount("api.anthropic.com") != 1 {
		t.Fatalf("claude endpoint calls = %d, want 1", claudeDoer.endpointCount("api.anthropic.com"))
	}
	reading, ok := store.Lookup("claude-1")
	if !ok {
		t.Fatal("claude reading missing")
	}
	if window, _ := reading.Window(WindowFiveHour); window.UsedPercent != 42 {
		t.Fatalf("five hour used percent = %.1f, want 42", window.UsedPercent)
	}
	if window, _ := reading.Window(WindowSevenDay); window.UsedPercent != 61 {
		t.Fatalf("seven day used percent = %.1f, want 61", window.UsedPercent)
	}

	codexDoer := &recordingDoer{body: codexUsageBody}
	codexPoller, codexStore, _ := newTestPoller(t, []Target{
		{AuthID: "codex-1", Provider: "codex", FileName: "codex-1.json", AccessToken: "test-token", AccountID: "acct-1"},
	}, codexDoer)
	codexPoller.RunOnce(context.Background())
	if codexDoer.endpointCount("chatgpt.com") != 1 {
		t.Fatalf("codex endpoint calls = %d, want 1", codexDoer.endpointCount("chatgpt.com"))
	}
	codexReading, ok := codexStore.Lookup("codex-1")
	if !ok {
		t.Fatal("codex reading missing")
	}
	if codexReading.Plan != "pro" {
		t.Fatalf("plan = %q, want pro", codexReading.Plan)
	}
	if window, _ := codexReading.Window(WindowWeekly); window.UsedPercent != 34 {
		t.Fatalf("weekly used percent = %.1f, want 34", window.UsedPercent)
	}
}

func TestPollerClaudeGapIsProviderWide(t *testing.T) {
	t.Parallel()

	doer := &recordingDoer{body: claudeUsageBody}
	targets := []Target{
		{AuthID: "claude-1", Provider: "claude", AccessToken: "test-token"},
		{AuthID: "claude-2", Provider: "claude", AccessToken: "test-token"},
		{AuthID: "claude-3", Provider: "claude", AccessToken: "test-token"},
	}
	poller, _, clock := newTestPoller(t, targets, doer)

	// Three idle Claude credentials, but only one call may go out: the gap is
	// shared across every credential because the throttle may be keyed by IP.
	poller.RunOnce(context.Background())
	if got := doer.callCount(); got != 1 {
		t.Fatalf("calls in the first pass = %d, want 1", got)
	}

	clock.Advance(DefaultClaudeMinGap - time.Second)
	poller.RunOnce(context.Background())
	if got := doer.callCount(); got != 1 {
		t.Fatalf("calls before the gap elapsed = %d, want 1", got)
	}

	clock.Advance(2 * time.Second)
	poller.RunOnce(context.Background())
	if got := doer.callCount(); got != 2 {
		t.Fatalf("calls after the gap elapsed = %d, want 2", got)
	}
}

func TestPollerCodexHasNoProviderWideGap(t *testing.T) {
	t.Parallel()

	doer := &recordingDoer{body: codexUsageBody}
	poller, _, _ := newTestPoller(t, []Target{
		{AuthID: "codex-1", Provider: "codex", AccessToken: "test-token"},
		{AuthID: "codex-2", Provider: "codex", AccessToken: "test-token"},
	}, doer)
	poller.RunOnce(context.Background())
	if got := doer.callCount(); got != 2 {
		t.Fatalf("codex calls = %d, want 2", got)
	}
}

func TestPollerSkipsFreshCredentials(t *testing.T) {
	t.Parallel()

	doer := &recordingDoer{body: codexUsageBody}
	poller, store, clock := newTestPoller(t, []Target{
		{AuthID: "codex-1", Provider: "codex", AccessToken: "test-token"},
	}, doer)

	// A reading from response headers is as good as a polled one, so a busy
	// credential is never polled.
	store.Observe(Identity{AuthID: "codex-1", Provider: "codex"},
		map[string]Window{WindowWeekly: {UsedPercent: 5, Known: true}}, clock.Now())
	poller.RunOnce(context.Background())
	if got := doer.callCount(); got != 0 {
		t.Fatalf("calls for a fresh credential = %d, want 0", got)
	}

	clock.Advance(DefaultCodexInterval + time.Second)
	poller.RunOnce(context.Background())
	if got := doer.callCount(); got != 1 {
		t.Fatalf("calls once the reading went stale = %d, want 1", got)
	}
}

func TestPollerSkipsDisabledAndTokenlessCredentials(t *testing.T) {
	t.Parallel()

	doer := &recordingDoer{body: codexUsageBody}
	poller, _, _ := newTestPoller(t, []Target{
		{AuthID: "disabled", Provider: "codex", AccessToken: "test-token", Disabled: true},
		{AuthID: "tokenless", Provider: "codex"},
		{AuthID: "unpolled", Provider: "gemini", AccessToken: "test-token"},
	}, doer)
	poller.RunOnce(context.Background())
	if got := doer.callCount(); got != 0 {
		t.Fatalf("calls = %d, want 0", got)
	}
}

func TestPollerClaude429BacksOffProviderWideAndDoubles(t *testing.T) {
	t.Parallel()

	doer := &recordingDoer{body: claudeUsageBody, statuses: []int{http.StatusTooManyRequests}}
	// The gap is shortened below the backoff so the backoff is what blocks the
	// second call, not the gap.
	poller, store, clock := newTestPollerWith(t, []Target{
		{AuthID: "claude-1", Provider: "claude", AccessToken: "test-token"},
		{AuthID: "claude-2", Provider: "claude", AccessToken: "test-token"},
	}, doer, func(cfg *PollerConfig) { cfg.ClaudeMinGap = time.Minute })

	poller.RunOnce(context.Background())
	if got := doer.callCount(); got != 1 {
		t.Fatalf("calls = %d, want 1", got)
	}
	backoff, until := poller.ClaudeBackoff()
	if backoff != DefaultClaudeBackoffStart {
		t.Fatalf("backoff = %s, want %s", backoff, DefaultClaudeBackoffStart)
	}
	if !until.Equal(storeBase.Add(DefaultClaudeBackoffStart)) {
		t.Fatalf("backoff until = %s, want %s", until, storeBase.Add(DefaultClaudeBackoffStart))
	}
	// A 429 leaves the previous reading alone; there just is not a new one.
	if _, ok := store.Lookup("claude-1"); ok {
		t.Fatal("a 429 must not write a reading")
	}

	// Inside the backoff nothing goes out, even once the minimum gap has passed.
	clock.Advance(2 * time.Minute)
	poller.RunOnce(context.Background())
	if got := doer.callCount(); got != 1 {
		t.Fatalf("calls during the backoff = %d, want 1", got)
	}

	// The next 429 doubles the pause.
	clock.Advance(DefaultClaudeBackoffStart)
	poller.RunOnce(context.Background())
	if got := doer.callCount(); got != 2 {
		t.Fatalf("calls after the backoff elapsed = %d, want 2", got)
	}
	backoff, _ = poller.ClaudeBackoff()
	if backoff != 2*DefaultClaudeBackoffStart {
		t.Fatalf("doubled backoff = %s, want %s", backoff, 2*DefaultClaudeBackoffStart)
	}
}

func TestPollerClaudeBackoffIsCappedAndClearedOnSuccess(t *testing.T) {
	t.Parallel()

	doer := &recordingDoer{body: claudeUsageBody, statuses: []int{http.StatusTooManyRequests}}
	poller, _, clock := newTestPollerWith(t, []Target{
		{AuthID: "claude-1", Provider: "claude", AccessToken: "test-token"},
	}, doer, func(cfg *PollerConfig) { cfg.ClaudeMinGap = time.Second })

	for i := 0; i < 8; i++ {
		poller.RunOnce(context.Background())
		_, until := poller.ClaudeBackoff()
		clock.now = until
		clock.Advance(time.Second)
	}
	backoff, _ := poller.ClaudeBackoff()
	if backoff != DefaultClaudeBackoffMax {
		t.Fatalf("capped backoff = %s, want %s", backoff, DefaultClaudeBackoffMax)
	}

	// A clean read clears the pause for every Claude credential.
	doer.statuses = []int{http.StatusOK}
	poller.RunOnce(context.Background())
	backoff, until := poller.ClaudeBackoff()
	if backoff != 0 || !until.IsZero() {
		t.Fatalf("backoff after success = %s until %s, want cleared", backoff, until)
	}
}

func TestPollerIgnoresUnparsableAndEmptyBodies(t *testing.T) {
	t.Parallel()

	for _, body := range []string{"not json", `{}`, `{"rate_limit":{}}`} {
		doer := &recordingDoer{body: body}
		poller, store, _ := newTestPoller(t, []Target{
			{AuthID: "codex-1", Provider: "codex", AccessToken: "test-token"},
		}, doer)
		poller.RunOnce(context.Background())
		if _, ok := store.Lookup("codex-1"); ok {
			t.Fatalf("body %q should not produce a reading", body)
		}
	}
}

func TestPollerStopsOnACancelledContext(t *testing.T) {
	t.Parallel()

	doer := &recordingDoer{body: codexUsageBody}
	poller, _, _ := newTestPoller(t, []Target{
		{AuthID: "codex-1", Provider: "codex", AccessToken: "test-token"},
	}, doer)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	poller.RunOnce(ctx)
	if got := doer.callCount(); got != 0 {
		t.Fatalf("calls = %d, want 0", got)
	}
}

func TestNewPollerRequiresAStoreAndTargets(t *testing.T) {
	t.Parallel()

	if NewPoller(PollerConfig{Targets: func() []Target { return nil }}) != nil {
		t.Fatal("a poller without a store should be nil")
	}
	if NewPoller(PollerConfig{Store: NewStore(nil)}) != nil {
		t.Fatal("a poller without targets should be nil")
	}
}
