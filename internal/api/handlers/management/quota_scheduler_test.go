package management

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
	coreauth "github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/auth"
	"github.com/router-for-me/CLIProxyAPI/v8/sdk/cliproxy/quota"
)

// useIsolatedQuotaStore points the status endpoint at a private store so the
// test does not depend on the process-wide one.
func useIsolatedQuotaStore(t *testing.T, now time.Time) *quota.Store {
	t.Helper()
	store := quota.NewStore(func() time.Time { return now })
	previous := quotaStoreForStatus
	quotaStoreForStatus = func() *quota.Store { return store }
	t.Cleanup(func() { quotaStoreForStatus = previous })
	return store
}

func requestQuotaSchedulerStatus(t *testing.T, h *Handler) map[string]any {
	t.Helper()
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v8/management/quota-scheduler/status", nil)
	h.GetQuotaSchedulerStatus(ctx)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var payload map[string]any
	if errDecode := json.Unmarshal(rec.Body.Bytes(), &payload); errDecode != nil {
		t.Fatalf("decode body %s: %v", rec.Body.String(), errDecode)
	}
	return payload
}

// TestQuotaSchedulerStatusMatchesTheContract pins the response to the shape the
// external Limits view decodes: an accounts map keyed by auth file name, each
// account carrying provider, plan, fetched_at and the canonical windows, each
// window carrying used_percent, reset_at, known and hard_limited.
func TestQuotaSchedulerStatusMatchesTheContract(t *testing.T) {
	t.Setenv("MANAGEMENT_PASSWORD", "")
	now := time.Now().UTC().Truncate(time.Second)
	store := useIsolatedQuotaStore(t, now)

	manager := coreauth.NewManager(nil, nil, nil)
	cfg := &config.Config{AuthDir: t.TempDir()}
	cfg.Routing.Strategy = "expiring-first"
	manager.SetConfig(cfg)

	registerAuthForLookupTest(t, manager, &coreauth.Auth{
		ID: "claude-a", Index: "index-claude-a", Provider: "claude",
		FileName:   "claude-julius@example.com.json",
		Attributes: map[string]string{"runtime_only": "true"},
	})
	registerAuthForLookupTest(t, manager, &coreauth.Auth{
		ID: "codex-a", Index: "index-codex-a", Provider: "codex",
		FileName:   "codex-abc123-pat@example.com-pro.json",
		Attributes: map[string]string{"runtime_only": "true"},
	})

	store.Observe(quota.Identity{
		AuthID: "claude-a", Provider: "claude", FileName: "claude-julius@example.com.json",
	}, map[string]quota.Window{
		quota.WindowFiveHour: {UsedPercent: 42, ResetAt: now.Add(3 * time.Hour), Known: true},
		quota.WindowSevenDay: {UsedPercent: 61, ResetAt: now.Add(72 * time.Hour), Known: true},
		quota.WindowFable:    {UsedPercent: 88, ResetAt: now.Add(72 * time.Hour), Known: true},
	}, now)
	store.Observe(quota.Identity{
		AuthID: "codex-a", Provider: "codex", FileName: "codex-abc123-pat@example.com-pro.json", Plan: "pro",
	}, map[string]quota.Window{
		quota.WindowFiveHour: {UsedPercent: 12, ResetAt: now.Add(2 * time.Hour), Known: true},
		quota.WindowWeekly:   {UsedPercent: 34, ResetAt: now.Add(120 * time.Hour), Known: true},
	}, now)

	h := NewHandlerWithoutConfigFilePath(cfg, manager)
	payload := requestQuotaSchedulerStatus(t, h)

	// Extra top-level keys the design adds on top of the contract.
	if payload["strategy"] != "reset-soonest" {
		t.Fatalf("strategy = %v, want reset-soonest", payload["strategy"])
	}
	generatedAt, _ := payload["generated_at"].(string)
	if _, errParse := time.Parse(time.RFC3339, generatedAt); errParse != nil {
		t.Fatalf("generated_at = %q, want RFC3339: %v", generatedAt, errParse)
	}

	accounts, ok := payload["accounts"].(map[string]any)
	if !ok {
		t.Fatalf("accounts = %v, want an object", payload["accounts"])
	}
	if len(accounts) != 2 {
		t.Fatalf("accounts = %v, want 2 entries keyed by auth file name", accounts)
	}

	claude, ok := accounts["claude-julius@example.com.json"].(map[string]any)
	if !ok {
		t.Fatalf("accounts keys = %v, want the Claude auth file name", accounts)
	}
	if claude["provider"] != "claude" {
		t.Fatalf("claude provider = %v, want claude", claude["provider"])
	}
	if fetchedAt, _ := claude["fetched_at"].(string); fetchedAt != now.Format(time.RFC3339) {
		t.Fatalf("claude fetched_at = %q, want %q", fetchedAt, now.Format(time.RFC3339))
	}
	for windowKey, wantPercent := range map[string]float64{"five_hour": 42, "seven_day": 61, "fable": 88} {
		window, okWindow := claude[windowKey].(map[string]any)
		if !okWindow {
			t.Fatalf("claude %s = %v, want an object", windowKey, claude[windowKey])
		}
		if percent, _ := window["used_percent"].(float64); percent != wantPercent {
			t.Fatalf("claude %s used_percent = %v, want %v", windowKey, window["used_percent"], wantPercent)
		}
		if known, _ := window["known"].(bool); !known {
			t.Fatalf("claude %s known = %v, want true", windowKey, window["known"])
		}
		if hardLimited, _ := window["hard_limited"].(bool); hardLimited {
			t.Fatalf("claude %s hard_limited = %v, want false", windowKey, window["hard_limited"])
		}
		resetAt, _ := window["reset_at"].(string)
		if _, errParse := time.Parse(time.RFC3339, resetAt); errParse != nil {
			t.Fatalf("claude %s reset_at = %q, want RFC3339: %v", windowKey, resetAt, errParse)
		}
	}
	// A Claude account has no weekly window; the key must be absent, not null,
	// so the contract's optional field decodes.
	if _, present := claude["weekly"]; present {
		t.Fatalf("claude account should not carry a weekly window: %v", claude)
	}

	codex, ok := accounts["codex-abc123-pat@example.com-pro.json"].(map[string]any)
	if !ok {
		t.Fatalf("accounts keys = %v, want the Codex auth file name", accounts)
	}
	if codex["provider"] != "codex" || codex["plan"] != "pro" {
		t.Fatalf("codex account = %v, want provider codex and plan pro", codex)
	}
	if _, present := codex["seven_day"]; present {
		t.Fatalf("codex account should not carry a seven_day window: %v", codex)
	}
	if _, present := codex["fable"]; present {
		t.Fatalf("codex account should not carry a fable window: %v", codex)
	}

	// The ranking view is grouped per provider family and ordered by pick order.
	ranking, ok := payload["ranking"].([]any)
	if !ok || len(ranking) != 2 {
		t.Fatalf("ranking = %v, want one entry per credential", payload["ranking"])
	}
	first, _ := ranking[0].(map[string]any)
	if first["auth"] != "claude-a" || first["provider"] != "claude" {
		t.Fatalf("first ranking entry = %v, want the claude credential", first)
	}
	if usable, _ := first["usable"].(bool); !usable {
		t.Fatalf("first ranking entry usable = %v, want true", first["usable"])
	}
	if first["ranking_window"] != "seven_day" {
		t.Fatalf("first ranking window = %v, want seven_day", first["ranking_window"])
	}
	if first["reason"] != quota.ReasonRanked {
		t.Fatalf("first ranking reason = %v, want %q", first["reason"], quota.ReasonRanked)
	}
	if urgency, _ := first["urgency"].(float64); urgency <= 0 {
		t.Fatalf("first ranking urgency = %v, want above zero", first["urgency"])
	}
}

func TestQuotaSchedulerStatusReportsAHardLimit(t *testing.T) {
	t.Setenv("MANAGEMENT_PASSWORD", "")
	now := time.Now().UTC().Truncate(time.Second)
	store := useIsolatedQuotaStore(t, now)
	recoverAt := now.Add(90 * time.Minute)

	manager := coreauth.NewManager(nil, nil, nil)
	cfg := &config.Config{AuthDir: t.TempDir()}
	manager.SetConfig(cfg)
	registerAuthForLookupTest(t, manager, &coreauth.Auth{
		ID: "claude-a", Index: "index-claude-a", Provider: "claude",
		FileName:   "claude-a.json",
		Attributes: map[string]string{"runtime_only": "true"},
		Quota: coreauth.QuotaState{
			Exceeded: true, Reason: "credential_quota", NextRecoverAt: recoverAt, ObservedAt: now,
		},
	})
	store.Observe(quota.Identity{AuthID: "claude-a", Provider: "claude", FileName: "claude-a.json"},
		map[string]quota.Window{
			quota.WindowFiveHour: {UsedPercent: 100, ResetAt: recoverAt, Known: true},
			quota.WindowSevenDay: {UsedPercent: 20, ResetAt: now.Add(96 * time.Hour), Known: true},
		}, now)

	h := NewHandlerWithoutConfigFilePath(cfg, manager)
	payload := requestQuotaSchedulerStatus(t, h)
	accounts, _ := payload["accounts"].(map[string]any)
	account, ok := accounts["claude-a.json"].(map[string]any)
	if !ok {
		t.Fatalf("accounts = %v, want claude-a.json", accounts)
	}
	fiveHour, _ := account["five_hour"].(map[string]any)
	if hardLimited, _ := fiveHour["hard_limited"].(bool); !hardLimited {
		t.Fatalf("five_hour = %v, want hard_limited true", fiveHour)
	}
	sevenDay, _ := account["seven_day"].(map[string]any)
	if hardLimited, _ := sevenDay["hard_limited"].(bool); hardLimited {
		t.Fatalf("seven_day = %v, want hard_limited false", sevenDay)
	}
	ranking, _ := payload["ranking"].([]any)
	entry, _ := ranking[0].(map[string]any)
	if usable, _ := entry["usable"].(bool); usable {
		t.Fatalf("ranking entry = %v, want unusable", entry)
	}
	if entry["reason"] != quota.ReasonShortWindowHardLimited {
		t.Fatalf("ranking reason = %v, want %q", entry["reason"], quota.ReasonShortWindowHardLimited)
	}
}

func TestQuotaSchedulerStatusWithNoCredentials(t *testing.T) {
	t.Setenv("MANAGEMENT_PASSWORD", "")
	now := time.Now().UTC().Truncate(time.Second)
	useIsolatedQuotaStore(t, now)

	manager := coreauth.NewManager(nil, nil, nil)
	cfg := &config.Config{AuthDir: t.TempDir()}
	manager.SetConfig(cfg)
	h := NewHandlerWithoutConfigFilePath(cfg, manager)

	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/v8/management/quota-scheduler/status", nil)
	h.GetQuotaSchedulerStatus(ctx)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	// An empty pool must still emit an accounts object and an empty ranking array,
	// not null, so the decoder on the other side succeeds.
	var raw struct {
		Accounts map[string]any `json:"accounts"`
		Ranking  []any          `json:"ranking"`
		Strategy string         `json:"strategy"`
	}
	if errDecode := json.Unmarshal(rec.Body.Bytes(), &raw); errDecode != nil {
		t.Fatal(errDecode)
	}
	if raw.Accounts == nil {
		t.Fatalf("accounts = nil, want an empty object: %s", rec.Body.String())
	}
	if len(raw.Accounts) != 0 {
		t.Fatalf("accounts = %v, want empty", raw.Accounts)
	}
	if raw.Ranking == nil {
		t.Fatalf("ranking = nil, want an empty array: %s", rec.Body.String())
	}
	if raw.Strategy != "round-robin" {
		t.Fatalf("strategy = %q, want round-robin", raw.Strategy)
	}
}

func TestQuotaSchedulerStatusSkipsDisabledCredentialsInTheRanking(t *testing.T) {
	t.Setenv("MANAGEMENT_PASSWORD", "")
	now := time.Now().UTC().Truncate(time.Second)
	useIsolatedQuotaStore(t, now)

	manager := coreauth.NewManager(nil, nil, nil)
	cfg := &config.Config{AuthDir: t.TempDir()}
	manager.SetConfig(cfg)
	registerAuthForLookupTest(t, manager, &coreauth.Auth{
		ID: "off", Index: "index-off", Provider: "claude", FileName: "off.json",
		Disabled: true, Status: coreauth.StatusDisabled,
		Attributes: map[string]string{"runtime_only": "true"},
	})
	h := NewHandlerWithoutConfigFilePath(cfg, manager)
	payload := requestQuotaSchedulerStatus(t, h)
	if ranking, _ := payload["ranking"].([]any); len(ranking) != 0 {
		t.Fatalf("ranking = %v, want no disabled credentials", ranking)
	}
}

func TestRoutingStrategyNameAndGuardDefaults(t *testing.T) {
	t.Parallel()

	var nilHandler *Handler
	if got := nilHandler.routingStrategyName(); got != "round-robin" {
		t.Fatalf("routingStrategyName() = %q, want round-robin", got)
	}
	if got := nilHandler.fiveHourGuardPercent(); got != quota.DefaultFiveHourGuardPercent {
		t.Fatalf("fiveHourGuardPercent() = %v, want %v", got, quota.DefaultFiveHourGuardPercent)
	}

	guard := 75.0
	cfg := &config.Config{}
	cfg.Routing.Strategy = "reset-soonest"
	cfg.Routing.ResetSoonest.FiveHourGuardPercent = &guard
	h := &Handler{cfg: cfg}
	if got := h.routingStrategyName(); got != "reset-soonest" {
		t.Fatalf("routingStrategyName() = %q, want reset-soonest", got)
	}
	if got := h.fiveHourGuardPercent(); got != 75 {
		t.Fatalf("fiveHourGuardPercent() = %v, want 75", got)
	}

	// An out-of-range guard is ignored rather than applied.
	outOfRange := 150.0
	cfg.Routing.ResetSoonest.FiveHourGuardPercent = &outOfRange
	if got := h.fiveHourGuardPercent(); got != quota.DefaultFiveHourGuardPercent {
		t.Fatalf("fiveHourGuardPercent() = %v, want the default", got)
	}

	// An unknown strategy is echoed back rather than silently renamed.
	cfg.Routing.Strategy = "mystery"
	if got := h.routingStrategyName(); got != "mystery" {
		t.Fatalf("routingStrategyName() = %q, want mystery", got)
	}
}
